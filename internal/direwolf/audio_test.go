// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UDP audio output ---

// setupAdev0 returns audio devices with a fresh adev_s, and nothing open, at
// index 0.
func setupAdev0() (*AudioDevices, *adev_s) {
	var d = new(AudioDevices)
	d.dev[0] = new(adev_s)

	return d, d.dev[0]
}

// openAudio opens the audio devices pa describes, which must work, and closes
// them again when the test is done.
func openAudio(t *testing.T, pa *RadioConfig) *AudioDevices {
	t.Helper()

	var d, err = AudioOpen(t.Context(), pa)
	require.NoError(t, err)

	t.Cleanup(d.Close)

	return d
}

func Test_Flush_UDP_sendsBytes(t *testing.T) {
	// Start a UDP listener to receive the audio output.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	// Dial from the "transmitter" side.
	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var d, dev = setupAdev0()
	dev.udp_out_sock = conn
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)

	var testData = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	copy(dev.outbuf, testData)
	dev.outbufLen = len(testData)

	var result = d.Flush(0)
	assert.Equal(t, 0, result)
	assert.Equal(t, 0, dev.outbufLen, "output buffer should be cleared after flush")

	// Receive and verify the packet contents.
	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

// --- anyDeviceRequiresPortAudio ---

func makeRadioConfig(inName, outName string) *RadioConfig {
	var pa = new(RadioConfig)
	pa.adev[0].defined = 1
	pa.adev[0].adevice_in = inName
	pa.adev[0].adevice_out = outName

	return pa
}

func Test_anyDeviceRequiresPortAudio(t *testing.T) {
	tests := []struct {
		name string
		pa   *RadioConfig
		want bool
	}{
		{
			name: "no devices defined",
			pa:   new(RadioConfig),
			want: false,
		},
		{
			name: "stdin in, udp out",
			pa:   makeRadioConfig("stdin", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "dash in, udp out",
			pa:   makeRadioConfig("-", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "udp in (uppercase), udp out",
			pa:   makeRadioConfig("UDP:7355", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "stdin in, soundcard out",
			pa:   makeRadioConfig("stdin", "default"),
			want: true,
		},
		{
			name: "soundcard in, udp out",
			pa:   makeRadioConfig("default", "udp:127.0.0.1:1234"),
			want: true,
		},
		{
			name: "soundcard in, soundcard out",
			pa:   makeRadioConfig("default", "default"),
			want: true,
		},
		{
			name: "stdin in and out, as a single-name ADEVICE gives",
			pa:   makeRadioConfig("stdin", "stdin"),
			want: false,
		},
		{
			name: "dash in and out",
			pa:   makeRadioConfig("-", "-"),
			want: false,
		},
		{
			name: "udp in and out, as a single-name ADEVICE gives",
			pa:   makeRadioConfig("udp:7355", "udp:7355"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, anyDeviceRequiresPortAudio(tt.pa))
		})
	}
}

func Test_Flush_UDP_emptyBuffer_isNoop(t *testing.T) {
	var d, dev = setupAdev0()
	dev.udp_out_sock = &net.UDPConn{} // non-nil socket; must not be written to
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	dev.outbufLen = 0

	// Should return 0 without attempting a write.
	assert.Equal(t, 0, d.Flush(0))
}

func Test_udpSilenceKeepalive_chunkSizeAndCleanShutdown(t *testing.T) {
	// Start a UDP listener to receive the keepalive silence.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var d, dev = setupAdev0()
	dev.udp_out_sock = conn
	dev.bitsPerSample = 16
	dev.bytesPerFrame = 2  // mono, 16-bit
	dev.sampleRate = 44100 // 20ms of samples at this rate exceeds UDP_AUDIO_OUT_BUF_MAXLEN, so the chunk must be capped

	var stop = make(chan struct{})
	var done = make(chan struct{})

	go func() {
		defer close(done)
		d.udpSilenceKeepalive(t.Context(), 0, stop)
	}()

	// Receive a chunk and verify it's frame-aligned and capped. The buffer
	// is deliberately larger than UDP_AUDIO_OUT_BUF_MAXLEN so an oversize
	// datagram would show up as a too-large read rather than being silently
	// truncated by ReadFrom.
	var buf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN*2)
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))

	var n int
	n, _, err = listener.ReadFrom(buf)
	require.NoError(t, err)
	assert.NotZero(t, n)
	assert.LessOrEqual(t, n, UDP_AUDIO_OUT_BUF_MAXLEN, "chunk length must not exceed UDP_AUDIO_OUT_BUF_MAXLEN")
	assert.Zero(t, n%dev.bytesPerFrame, "chunk length must be a multiple of bytesPerFrame")

	// Closing stop should make the goroutine exit promptly.
	close(stop)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("udpSilenceKeepalive did not stop after stop was closed")
	}
}

// noSuchAudioDevice is a device name no soundcard will match, for testing what
// happens when an output device turns out not to be there.
const noSuchAudioDevice = "Q1TEST no such audio device"

// nullSoundcard is the name nullSoundcards gives the device it declares when
// a test needs only the one.
const nullSoundcard = "samoyed_null"

// nullSoundcards gives PortAudio soundcards to open where there is no sound
// hardware - a container, a CI runner - by declaring ALSA "null" devices,
// which swallow whatever is played and record silence, in an .asoundrc that
// HOME points at for the rest of the test.  ALSA is Linux only, so elsewhere
// the test is skipped.
func nullSoundcards(t *testing.T, names ...string) {
	t.Helper()

	if runtime.GOOS != "linux" {
		t.Skip("ALSA null devices are Linux only")
	}

	// github.com/gordonklaus/portaudio hands its stream id to C as
	// unsafe.Pointer(id), which checkptr - on under the race detector - can
	// take for a bad pointer and abort the whole test binary over.
	if raceEnabled {
		t.Skip("the PortAudio binding trips checkptr under the race detector")
	}

	var asoundrc strings.Builder

	for _, name := range names {
		asoundrc.WriteString("pcm." + name + " {\n\ttype null\n\thint { show on description \"Samoyed test\" }\n}\n")
	}

	var home = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".asoundrc"), []byte(asoundrc.String()), 0o600))

	// alsa-lib reads HOME through the C environment, which Setenv reaches
	// under cgo.
	t.Setenv("HOME", home)
}

// --- audioOutType ---

func Test_audioOutType(t *testing.T) {
	tests := []struct {
		name      string
		inName    string
		ouName    string
		specified bool
		want      audio_out_type_e
	}{
		{"soundcard", "plughw:1,0", "plughw:1,0", false, AUDIO_OUT_TYPE_SOUNDCARD},
		{"separate soundcards", "plughw:1,0", "plughw:2,0", true, AUDIO_OUT_TYPE_SOUNDCARD},
		{"udp destination", "plughw:1,0", "udp:127.0.0.1:7355", true, AUDIO_OUT_TYPE_UDP},
		{"udp in, udp destination", "udp:7355", "udp:127.0.0.1:7356", true, AUDIO_OUT_TYPE_UDP},
		{"stdin both ways", "stdin", "stdin", false, AUDIO_OUT_TYPE_NONE},
		{"dash both ways", "-", "-", false, AUDIO_OUT_TYPE_NONE},
		{"stdin out only", "plughw:1,0", "stdin", true, AUDIO_OUT_TYPE_NONE},
		{"udp listen port copied to the output side", "udp:7355", "udp:7355", false, AUDIO_OUT_TYPE_NONE},
		{"udp listen port, mixed case", "UDP:7355", "udp:7355", false, AUDIO_OUT_TYPE_NONE},
		{"same udp name on both sides, but named for transmit", "udp:7355", "udp:7355", true, AUDIO_OUT_TYPE_UDP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pa = makeRadioConfig(tt.inName, tt.ouName)
			pa.adev[0].adevice_out_specified = tt.specified

			assert.Equal(t, tt.want, audioOutType(&pa.adev[0]))
		})
	}
}

// --- AudioOpen with no output device ---

// A receive-only configuration must open on a machine with no audio output
// device at all, rather than refusing to start.  "ADEVICE stdin" - and the
// "-" command line argument - is exactly that: it leaves "stdin" as the
// output device name too, which is nothing we can transmit through.
func Test_audioOpen_stdinOnly_hasNoOutputDevice(t *testing.T) {
	var pa = makeRadioConfig("stdin", "stdin")
	var d = openAudio(t, pa)

	assert.Nil(t, d.dev[0].outputStream)
	assert.Nil(t, d.dev[0].udp_out_sock)

	// The point of issue #501: nothing here needs a soundcard, so PortAudio
	// is never initialized.
	assert.False(t, d.portaudioHeld)

	// Whatever the transmit path produces on the open device is discarded,
	// not written anywhere, and does not upset the buffer bookkeeping.
	require.Equal(t, 0, d.Put(0, 42))
	assert.Equal(t, -1, d.Flush(0))
	assert.Equal(t, 0, d.dev[0].outbufLen)

	// Closing must not terminate a PortAudio this open never initialized.
	d.Close()
	assert.False(t, d.portaudioHeld)
}

// An output device we only defaulted to, and which turns out not to exist,
// leaves the station receive-only rather than stopping it.
func Test_audioOpen_defaultedOutputDeviceMissing_isNotFatal(t *testing.T) {
	var pa = makeRadioConfig("stdin", noSuchAudioDevice)

	var d = openAudio(t, pa)

	assert.Nil(t, d.dev[0].outputStream)
}

// An output device named for transmit in the configuration is asked for
// specifically, so not finding it is a configuration error, not a reason to
// quietly transmit nothing.
func Test_audioOpen_namedOutputDeviceMissing_isFatal(t *testing.T) {
	var pa = makeRadioConfig("stdin", noSuchAudioDevice)
	pa.adev[0].adevice_out_specified = true

	var _, openErr = AudioOpen(t.Context(), pa)
	assert.Error(t, openErr)
}

// Naming standard input, or a UDP port to listen on, as the transmit device
// is a configuration error too - neither can transmit.
func Test_audioOpen_namedOutputDeviceCannotTransmit_isFatal(t *testing.T) {
	var pa = makeRadioConfig("stdin", "stdin")
	pa.adev[0].adevice_out_specified = true

	var _, openErr = AudioOpen(t.Context(), pa)
	assert.Error(t, openErr)
}

// Close must not return while the UDP silence keepalive AudioOpen started is
// still running: that goroutine reads the device without a lock, so anything
// that touches it afterwards would race with it.
func Test_audioClose_waitsForUDPSilenceKeepalive(t *testing.T) {
	var listener, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var pa = makeRadioConfig("stdin", "udp:"+listener.LocalAddr().String())
	pa.adev[0].adevice_out_specified = true

	var d = openAudio(t, pa)
	require.NotNil(t, d.dev[0].udp_out_sock)

	// Let the keepalive tick a few times.
	time.Sleep(5 * silenceKeepaliveInterval)

	d.Close()

	// What a caller is entitled to do once Close has returned; the race
	// detector reports this write if the keepalive can still be reading.
	d.dev[0] = nil
}

// --- GetByte ---

// What GetByte reads is counted in samples by the device's own format, and
// reported at the interval its configuration asked for - AudioOpen hands the
// device both, and there is nothing else to ask.
func Test_GetByte_recordsStatisticsFromTheDevicesOwnSettings(t *testing.T) {
	var pa = makeRadioConfig("udp:0", "stdin")
	pa.adev[0].num_channels = 2
	pa.adev[0].bits_per_sample = 16
	pa.statistics_interval = 100

	var d = openAudio(t, pa)

	var conn, err = new(net.Dialer).DialContext(t.Context(), "udp", d.dev[0].udp_sock.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	// 2 channels of 16 bits is 4 bytes a sample.
	var datagram = make([]byte, 40)

	// The first read only starts the statistics off, and the second is
	// counted.  The first report is due 3 seconds after the first read, and
	// resets the count, so both datagrams go out before anything reads them:
	// the two reads then follow one another with nothing in between.
	for range 2 {
		_, err = conn.Write(datagram)
		require.NoError(t, err)
	}

	for range datagram {
		require.Equal(t, 0, d.GetByte(0))
	}

	var samples, _ = d.dev[0].stats.counts()
	assert.Equal(t, 0, samples)

	require.Equal(t, 0, d.GetByte(0))

	assert.Equal(t, 100, d.dev[0].statisticsInterval)

	var errors int

	samples, errors = d.dev[0].stats.counts()
	assert.Equal(t, 10, samples)
	assert.Equal(t, 0, errors)
}

// --- applyCommandLineAudioSource ---

func Test_applyCommandLineAudioSource(t *testing.T) {
	tests := []struct {
		name      string
		ouName    string
		specified bool
		wantOut   string
	}{
		{"default output follows the source", DEFAULT_ADEVICE, false, "stdin"},
		{"default output in any case follows the source", "DEFAULT", false, "stdin"},
		{"a named transmit device is left alone", "plughw:2,0", true, "plughw:2,0"},
		{"a single-name ADEVICE keeps its soundcard for transmit", "plughw:1,0", false, "plughw:1,0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pa = makeRadioConfig("plughw:1,0", tt.ouName)
			pa.adev[0].adevice_out_specified = tt.specified

			applyCommandLineAudioSource(&pa.adev[0], "stdin")

			assert.Equal(t, "stdin", pa.adev[0].adevice_in)
			assert.Equal(t, tt.wantOut, pa.adev[0].adevice_out)
		})
	}
}

// --- transmitAvailable ---

func Test_transmitAvailable(t *testing.T) {
	t.Run("no audio devices", func(t *testing.T) {
		var d *AudioDevices

		assert.False(t, d.transmitAvailable(0))
	})

	t.Run("device that was never opened", func(t *testing.T) {
		var d = new(AudioDevices)

		assert.False(t, d.transmitAvailable(0))
	})

	t.Run("device open with no output", func(t *testing.T) {
		var d, _ = setupAdev0()
		assert.False(t, d.transmitAvailable(0))
	})

	t.Run("device with UDP output", func(t *testing.T) {
		var d, dev = setupAdev0()
		dev.udp_out_sock = &net.UDPConn{}

		assert.True(t, d.transmitAvailable(0))
	})

	t.Run("device number out of range", func(t *testing.T) {
		var d, _ = setupAdev0()

		assert.False(t, d.transmitAvailable(-1))
		assert.False(t, d.transmitAvailable(MAX_ADEVS))
	})
}

// Regression test for #671: an unusable sound device setting used to be an
// assert whose message was the C boolean expression that failed, so a bad
// ACHANNELS or ARATE gave a stack trace rather than a sentence naming the
// directive.
func Test_adev_param_validate(t *testing.T) {
	var good = new(adev_param_s)
	good.bits_per_sample = DEFAULT_BITS_PER_SAMPLE
	good.num_channels = DEFAULT_NUM_CHANNELS
	good.samples_per_sec = DEFAULT_SAMPLES_PER_SEC

	require.NoError(t, good.validate())

	for _, bits := range []int{0, 1, 12, 24, 32} {
		var adev = *good
		adev.bits_per_sample = bits
		require.ErrorContains(t, adev.validate(), "bits per audio sample")
	}

	for _, channels := range []int{-1, 0, 3, MAX_ADEVS + 1} {
		var adev = *good
		adev.num_channels = channels
		require.ErrorContains(t, adev.validate(), "ACHANNELS")
	}

	for _, rate := range []int{0, MIN_SAMPLES_PER_SEC - 1, MAX_SAMPLES_PER_SEC + 1} {
		var adev = *good
		adev.samples_per_sec = rate
		require.ErrorContains(t, adev.validate(), "ARATE")
	}

	// The message says what it will take, not which expression failed.
	var adev = *good
	adev.num_channels = 3
	require.EqualError(t, adev.validate(), "number of audio channels (ACHANNELS) must be 1 or 2, not 3")
}

// Standard input running out is the end of the run rather than a failure,
// but it is for DirewolfMain to end it, through the teardown: the device says
// it has no more to give, and remembers that it ran out rather than failed.
func TestStdinEndOfFileEndsInput(t *testing.T) {
	var _, w = setStdin(t)

	var d = openAudio(t, makeRadioConfig("-", "-"))

	var _, err = w.Write([]byte{0x11})
	require.NoError(t, err)
	require.NoError(t, w.Close())

	assert.Equal(t, 0x11, d.GetByte(0))
	assert.False(t, d.inputEnded(0), "there was a byte still to read")

	var output = testutils.CaptureOutput(t, func() {
		assert.Equal(t, -1, d.GetByte(0))
		assert.True(t, d.inputEnded(0), "standard input ran out")

		// The other channel of a stereo device asks too.
		assert.Equal(t, -1, d.GetByte(0))
	})

	assert.Equal(t, 1, strings.Count(output, "End of file on stdin"), "said once: %s", output)
}
