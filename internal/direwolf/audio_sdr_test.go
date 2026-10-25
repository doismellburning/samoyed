// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/sdr"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/doismellburning/samoyed/internal/wav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sdrTestAudioRate = 48000

// sdrTestPackets makes audio of packets, at sdrTestAudioRate, from -1 to 1.
func sdrTestPackets(t *testing.T, packets ...string) []float64 {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "packets.wav")

	var opts = newGenPacketsOptions()
	opts.SampleRate = sdrTestAudioRate

	var g, err = NewGenPackets(opts, name)
	require.NoError(t, err)

	for _, p := range packets {
		require.NoError(t, g.SendPacket(p))
	}

	require.NoError(t, g.Close())

	var f, openErr = os.Open(name) //nolint:gosec // G304: the test's own file
	require.NoError(t, openErr)

	defer f.Close()

	var _, size, headerErr = wav.ReadHeader(f)
	require.NoError(t, headerErr)

	var samples = make([]int16, size/2)
	require.NoError(t, binary.Read(f, binary.LittleEndian, samples))

	var audio = make([]float64, len(samples))
	for i, s := range samples {
		audio[i] = float64(s) / 32768
	}

	return audio
}

// sdrTestConfig is a configuration for one audio device receiving
// frequencies from the rtl_tcp server at address.
func sdrTestConfig(address string, frequencies ...float64) *RadioConfig {
	var pa = makeRadioConfig(rtlTCPPrefix+address, rtlTCPPrefix+address)
	pa.adev[0].num_channels = len(frequencies)
	pa.adev[0].samples_per_sec = sdrTestAudioRate
	pa.adev[0].bits_per_sample = 16
	pa.adev[0].sdr = maybe.Just(sdrParams{frequencies: frequencies, gain: maybe.Just(30.0), ppm: 2})

	return pa
}

// readFrames reads n frames of audio from device 0, which has channels
// channels, returning each channel's samples as a mono .WAV file.
func readFrames(t *testing.T, d *AudioDevices, channels int, n int) []string {
	t.Helper()

	var files = make([]*bytes.Buffer, channels)
	for c := range files {
		files[c] = new(bytes.Buffer)
	}

	for range n {
		for c := range channels {
			var lo = d.GetByte(0)
			var hi = d.GetByte(0)
			require.GreaterOrEqual(t, lo, 0)
			require.GreaterOrEqual(t, hi, 0)

			files[c].WriteByte(byte(lo & 0xff))
			files[c].WriteByte(byte(hi & 0xff))
		}
	}

	var names = make([]string, channels)

	for c, pcm := range files {
		names[c] = filepath.Join(t.TempDir(), "channel.wav")

		var w, err = wav.Create(names[c], wav.Format{NumChannels: 1, SamplesPerSec: sdrTestAudioRate, BitsPerSample: 16})
		require.NoError(t, err)

		_, err = io.Copy(w, pcm)
		require.NoError(t, err)
		require.NoError(t, w.Close())
	}

	return names
}

// A packet sent on each of two frequencies comes out on the channel for that
// frequency, and only there.
func Test_AudioOpen_rtlTCP_receivesEachFrequency(t *testing.T) {
	t.Parallel()

	var frequencies = []float64{144_800_000, 145_000_000}

	var tuning, err = sdr.NewTuning(frequencies, sdrTestAudioRate)
	require.NoError(t, err)

	var first = sdrTestPackets(t, "Q1TEST>APDW17:>First", "Q1TEST>APDW17:>Again")
	var second = sdrTestPackets(t, "Q2TEST>APDW17:>Second")

	// The packets go at half of full scale, so this is 3 kHz deviation.
	var iq = testutils.FMModulate(tuning.SampleRate,
		testutils.FMSignal{Offset: tuning.Offsets[0], Deviation: 6000, Audio: first, AudioRate: sdrTestAudioRate},
		testutils.FMSignal{Offset: tuning.Offsets[1], Deviation: 6000, Audio: second, AudioRate: sdrTestAudioRate},
	)

	var server = testutils.NewRTLTCPServer(t, iq)

	var d, openErr = AudioOpen(t.Context(), sdrTestConfig(server.Address, frequencies...))
	require.NoError(t, openErr)

	defer d.Close()

	assert.False(t, d.transmitAvailable(0), "an SDR is receive only")

	assert.Equal(t, []testutils.RTLTCPCommand{
		{Command: 0x02, Param: safecast.RequireConvert[int32](t, tuning.SampleRate)},
		{Command: 0x05, Param: 2},
		{Command: 0x01, Param: safecast.RequireConvert[int32](t, tuning.Centre)},
		{Command: 0x03, Param: 1},
		{Command: 0x04, Param: 300},
	}, server.RequireCommands(t, 5))

	var frames = len(iq) / 2 / (tuning.SampleRate / sdrTestAudioRate)
	var names = readFrames(t, d, 2, frames)

	var opts = new(AtestOptions)
	opts.IL2PVersion = "0.6"

	var atest, atestErr = NewAtest(opts)
	require.NoError(t, atestErr)

	for c, want := range []int{2, 1} {
		var result, decodeErr = atest.DecodeFile(names[c])
		require.NoError(t, decodeErr)
		assert.Equal(t, want, result.PacketsDecoded, "channel %d", c)
	}
}

// When rtl_tcp goes away, the device reports that it has nothing more.
func Test_AudioOpen_rtlTCP_failsWhenServerGoes(t *testing.T) {
	t.Parallel()

	var ln, port = testutils.Listen(t)

	go func() {
		var conn, err = ln.Accept()
		if err != nil {
			return
		}

		_, _ = conn.Write(append([]byte("RTL0"), make([]byte, 8)...))

		// Hear the client out - five commands - before sending a little
		// and going.
		_, _ = io.ReadFull(conn, make([]byte, 5*5))
		_, _ = conn.Write(make([]byte, 4096))
		_ = conn.Close()
	}()

	var d, err = AudioOpen(t.Context(), sdrTestConfig("127.0.0.1:"+port, 144_800_000))
	require.NoError(t, err)

	defer d.Close()

	for d.GetByte(0) >= 0 {
	}
}

func Test_AudioOpen_rtlTCP_refuses(t *testing.T) {
	t.Parallel()

	var testCases = map[string]func(pa *RadioConfig){
		"no SDR settings":   func(pa *RadioConfig) { pa.adev[0].sdr = maybe.Nothing[sdrParams]() },
		"channels disagree": func(pa *RadioConfig) { pa.adev[0].num_channels = 2 },
		"8 bit audio":       func(pa *RadioConfig) { pa.adev[0].bits_per_sample = 8 },
		"too far apart": func(pa *RadioConfig) {
			pa.adev[0].num_channels = 2
			pa.adev[0].sdr = maybe.Just(sdrParams{frequencies: []float64{144e6, 146e6}, gain: maybe.Nothing[float64](), ppm: 0})
		},
		"nothing listening": func(pa *RadioConfig) {
			pa.adev[0].adevice_in = rtlTCPPrefix + "127.0.0.1:" + testutils.UnusedPort(t)
		},
		"named for transmit": func(pa *RadioConfig) { pa.adev[0].adevice_out_specified = true },
	}

	for name, change := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var server = testutils.NewRTLTCPServer(t, nil)

			var pa = sdrTestConfig(server.Address, 144_800_000)
			change(pa)

			var _, err = AudioOpen(t.Context(), pa)
			require.Error(t, err)
		})
	}
}

func Test_audioNameIsRTLTCP(t *testing.T) {
	t.Parallel()

	assert.True(t, audioNameIsRTLTCP("rtl_tcp:localhost:1234"))
	assert.True(t, audioNameIsRTLTCP("RTL_TCP:localhost:1234"))
	assert.False(t, audioNameIsRTLTCP("udp:7355"))
	assert.False(t, anyDeviceRequiresPortAudio(makeRadioConfig("rtl_tcp:localhost:1234", "rtl_tcp:localhost:1234")))
}
