// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseALSADeviceName ---

func Test_parseALSADeviceName(t *testing.T) {
	tests := []struct {
		input      string
		wantCard   string
		wantDevNum int
	}{
		{"plughw:FTDX10,0", "FTDX10", 0},
		{"plughw:FT991A,0", "FT991A", 0},
		{"hw:1,0", "1", 0},
		{"plughw:Loopback,1,1", "Loopback", 1},
		{"hw:PCH,0", "PCH", 0},
		{"default", "", -1},
		{"", "", -1},
		{"SomeDevice", "", -1},
		{"plughw:Card", "Card", -1}, // no device number
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var card, dev = parseALSADeviceName(tt.input)
			assert.Equal(t, tt.wantCard, card)
			assert.Equal(t, tt.wantDevNum, dev)
		})
	}
}

// --- parseALSACardsProc ---

func Test_parseALSACardsProc(t *testing.T) {
	// Typical /proc/asound/cards content with udev-assigned card IDs.
	var content = ` 0 [PCH            ]: HDA-Intel - HDA Intel PCH
                      HDA Intel PCH at 0xb1240000 irq 142
 2 [FTDX10         ]: USB-Audio - USB AUDIO  CODEC
                      USB AUDIO  CODEC at usb-0000:00:14.0-5.2, full speed
 3 [FT991A         ]: USB Audio - USB Audio CODEC
                      USB Audio CODEC at usb-0000:00:14.0-6.2, full speed`

	var got = parseALSACardsProc(content)

	assert.Equal(t, 0, got["PCH"])
	assert.Equal(t, 2, got["FTDX10"])
	assert.Equal(t, 3, got["FT991A"])
}

func Test_parseALSACardsProc_empty(t *testing.T) {
	assert.Empty(t, parseALSACardsProc(""))
}

// --- matchPortAudioDeviceByName ---

// makeDevice constructs a portaudio.DeviceInfo for use in tests.
func makeDevice(name string, maxIn, maxOut int) *portaudio.DeviceInfo {
	return &portaudio.DeviceInfo{
		Index:                    0,
		Name:                     name,
		MaxInputChannels:         maxIn,
		MaxOutputChannels:        maxOut,
		DefaultLowInputLatency:   0,
		DefaultLowOutputLatency:  0,
		DefaultHighInputLatency:  0,
		DefaultHighOutputLatency: 0,
		DefaultSampleRate:        0,
		HostApi:                  nil,
	}
}

// setFakeALSACards writes a fake /proc/asound/cards to a temp file, points
// alsaCardsPath at it for the duration of the test, and restores the original
// path via t.Cleanup.
func setFakeALSACards(t *testing.T, content string) {
	t.Helper()
	var tmp = filepath.Join(t.TempDir(), "cards")
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))
	var orig = alsaCardsPath
	alsaCardsPath = tmp
	t.Cleanup(func() { alsaCardsPath = orig })
}

// Test the udev card ID scenario from doismellburning/samoyed#468:
// The user configures "plughw:FTDX10,0" but PortAudio enumerates the device
// as "USB AUDIO  CODEC: USB Audio (hw:2,0)" because it uses the hardware
// description, not the udev-assigned ALSA card ID.
func Test_matchPortAudioDeviceByName_udevCardID(t *testing.T) {
	// Simulate the PortAudio device list on the user's machine.
	var devices = []*portaudio.DeviceInfo{
		makeDevice("HDA Intel PCH: ALC3234 Analog (hw:0,0)", 2, 2),
		makeDevice("HDA Intel PCH: HDMI 0 (hw:0,3)", 0, 2),
		makeDevice("USB AUDIO  CODEC: USB Audio (hw:2,0)", 2, 2), // FTDX10
		makeDevice("USB Audio CODEC: USB Audio (hw:3,0)", 2, 2),  // FT991A
	}

	var cardsContent = ` 0 [PCH            ]: HDA-Intel - HDA Intel PCH
 2 [FTDX10         ]: USB-Audio - USB AUDIO  CODEC
 3 [FT991A         ]: USB Audio - USB Audio CODEC`

	t.Run("FTDX10 resolves via card ID", func(t *testing.T) {
		setFakeALSACards(t, cardsContent)
		var dev = matchPortAudioDeviceByName("plughw:FTDX10,0", true, devices)
		assert.NotNil(t, dev)
		assert.Equal(t, "USB AUDIO  CODEC: USB Audio (hw:2,0)", dev.Name)
	})

	t.Run("FT991A resolves via card ID", func(t *testing.T) {
		setFakeALSACards(t, cardsContent)
		var dev = matchPortAudioDeviceByName("plughw:FT991A,0", true, devices)
		assert.NotNil(t, dev)
		assert.Equal(t, "USB Audio CODEC: USB Audio (hw:3,0)", dev.Name)
	})
}

func Test_matchPortAudioDeviceByName_exactMatch(t *testing.T) {
	var devices = []*portaudio.DeviceInfo{
		makeDevice("HDA Intel PCH: ALC3234 Analog (hw:0,0)", 2, 2),
		makeDevice("USB AUDIO  CODEC: USB Audio (hw:2,0)", 2, 2),
	}

	var dev = matchPortAudioDeviceByName("USB AUDIO  CODEC: USB Audio (hw:2,0)", true, devices)
	assert.NotNil(t, dev)
	assert.Equal(t, "USB AUDIO  CODEC: USB Audio (hw:2,0)", dev.Name)
}

func Test_matchPortAudioDeviceByName_substrMatch(t *testing.T) {
	var devices = []*portaudio.DeviceInfo{
		makeDevice("Loopback: PCM (hw:0,0)", 2, 2),
		makeDevice("Loopback: PCM (hw:0,1)", 2, 2),
	}

	// "Loopback" substring should match the first device that contains it.
	var dev = matchPortAudioDeviceByName("Loopback", true, devices)
	assert.NotNil(t, dev)
}

func Test_matchPortAudioDeviceByName_alsaStyleLoopback(t *testing.T) {
	var devices = []*portaudio.DeviceInfo{
		makeDevice("Loopback: PCM (hw:0,0)", 2, 2),
		makeDevice("Loopback: PCM (hw:0,1)", 2, 2),
	}

	// plughw:Loopback,1 should match the device with (hw:0,1).
	var dev = matchPortAudioDeviceByName("plughw:Loopback,1", true, devices)
	assert.NotNil(t, dev)
	assert.Equal(t, "Loopback: PCM (hw:0,1)", dev.Name)
}

func Test_matchPortAudioDeviceByName_noMatch(t *testing.T) {
	setFakeALSACards(t, "")
	var devices = []*portaudio.DeviceInfo{
		makeDevice("HDA Intel PCH: ALC3234 Analog (hw:0,0)", 2, 2),
	}

	var dev = matchPortAudioDeviceByName("plughw:NonExistent,0", true, devices)
	assert.Nil(t, dev)
}

// --- automatic device detection ---

// alsaDeviceList is what PortAudio enumerates on a Linux machine with a USB
// sound card in it and no sound server running: the cards, and the ALSA
// plugins that could be opened without one.
func alsaDeviceList() []*portaudio.DeviceInfo {
	return []*portaudio.DeviceInfo{
		makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
		makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 2),
		makeDevice("sysdefault", 0, 8),
		makeDevice("hdmi", 0, 8),
		makeDevice("default", 0, 8),
	}
}

// allFormatsSupported stands in for audioDeviceSupportsFormat where the test
// is not about what the card can do.
func allFormatsSupported(_ *portaudio.DeviceInfo, _ *adev_param_s, _ bool) bool {
	return true
}

func Test_audioSoundServerPresent(t *testing.T) {
	t.Run("bare ALSA", func(t *testing.T) {
		assert.False(t, audioSoundServerPresent(alsaDeviceList()))
	})

	t.Run("PulseAudio or PipeWire answering", func(t *testing.T) {
		var devices = append(alsaDeviceList(), makeDevice("pulse", 32, 32))
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("JACK", func(t *testing.T) {
		var devices = append(alsaDeviceList(), makeDevice("jack", 64, 64))
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("a qualified plugin name", func(t *testing.T) {
		var devices = []*portaudio.DeviceInfo{makeDevice("pulse:SERVER=/run/user/1000/pulse/native", 32, 32)}
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("a card named after one", func(t *testing.T) {
		var devices = []*portaudio.DeviceInfo{makeDevice("PulseTest: USB Audio (hw:1,0)", 2, 2)}
		assert.False(t, audioSoundServerPresent(devices))
	})
}

// The ALSA plugins alongside the cards say nothing about what is attached to
// the machine, so only the cards are candidates.
func Test_alsaCardDevices(t *testing.T) {
	var in = alsaCardDevices(alsaDeviceList(), true)
	require.Len(t, in, 1)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", in[0].Name)

	var out = alsaCardDevices(alsaDeviceList(), false)
	require.Len(t, out, 2)
	assert.Equal(t, "bcm2835 Headphones: - (hw:0,0)", out[0].Name)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", out[1].Name)
}

// No CoreAudio or WASAPI device name carries the ALSA hardware marker, so
// nothing on those platforms is a candidate and the system default stands.
func Test_alsaCardDevices_otherHostAPIs(t *testing.T) {
	var devices = []*portaudio.DeviceInfo{
		makeDevice("Built-in Microphone", 2, 0),
		makeDevice("Built-in Output", 0, 2),
		makeDevice("USB Audio CODEC", 2, 2),
	}

	assert.Empty(t, alsaCardDevices(devices, true))
	assert.Empty(t, alsaCardDevices(devices, false))
}

func Test_autoDetectAudioDevice(t *testing.T) {
	t.Run("the only card", func(t *testing.T) {
		var dev = autoDetectAudioDevice(alsaDeviceList(), true)
		require.NotNil(t, dev)
		assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", dev.Name)
	})

	t.Run("a choice to make is not our choice", func(t *testing.T) {
		assert.Nil(t, autoDetectAudioDevice(alsaDeviceList(), false))
	})

	t.Run("no card at all", func(t *testing.T) {
		var devices = []*portaudio.DeviceInfo{makeDevice("default", 32, 32)}
		assert.Nil(t, autoDetectAudioDevice(devices, true))
	})
}

// A sound server's default is the operator's own choice of device, so it is
// followed rather than second-guessed.
func Test_resolveDefaultAudioDevices_soundServerRunning(t *testing.T) {
	var devices = append(alsaDeviceList(), makeDevice("pulse", 32, 32))

	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, devices, allFormatsSupported)

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// With no sound server to have chosen anything, a machine with one sound card
// needs no ADEVICE: both directions land on the card, including the transmit
// side, where the other playback device is the machine's own speakers rather
// than the other half of the radio.
func Test_resolveDefaultAudioDevices_theOnlyCard(t *testing.T) {
	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_out)
}

// "ADEVICE auto" asks for the same thing out loud.
func Test_resolveDefaultAudioDevices_autoKeyword(t *testing.T) {
	var pa = makeAudioConfig(AUTO_ADEVICE, AUTO_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_out)
}

// A card that can only capture leaves the transmit side to be detected on its
// own, and a machine with more than one playback device keeps the default.
func Test_resolveDefaultAudioDevices_captureOnlyCard(t *testing.T) {
	var devices = []*portaudio.DeviceInfo{
		makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
		makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 0),
		makeDevice("default", 32, 32),
	}

	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, devices, allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "bcm2835 Headphones: - (hw:0,0)", pa.adev[0].adevice_out)
}

// A device named for transmit is a choice of its own, so an explicit default
// there is detected on its own terms rather than following the input side.
func Test_resolveDefaultAudioDevices_outputSpecified(t *testing.T) {
	var pa = makeAudioConfig(DEFAULT_ADEVICE, AUTO_ADEVICE)
	pa.adev[0].adevice_out_specified = true

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, AUTO_ADEVICE, pa.adev[0].adevice_out)
}

// Anything the configuration names is left alone, as is anything that is not a
// sound card.
func Test_resolveDefaultAudioDevices_leavesOthersAlone(t *testing.T) {
	tests := []struct {
		name    string
		inName  string
		outName string
		wantIn  string
		wantOut string
	}{
		{"named devices", "plughw:1,0", "plughw:2,0", "plughw:1,0", "plughw:2,0"},
		{"standard input", "stdin", "stdin", "stdin", "stdin"},
		{"UDP both ways", "udp:7355", "udp:localhost:7356", "udp:7355", "udp:localhost:7356"},
		{
			"UDP in, card out",
			"udp:7355", DEFAULT_ADEVICE,
			"udp:7355", "bcm2835 Headphones: - (hw:0,0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var devices = []*portaudio.DeviceInfo{
				makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
				makeDevice("default", 32, 32),
			}

			var pa = makeAudioConfig(tt.inName, tt.outName)

			resolveDefaultAudioDevices(pa, devices, allFormatsSupported)

			assert.Equal(t, tt.wantIn, pa.adev[0].adevice_in)
			assert.Equal(t, tt.wantOut, pa.adev[0].adevice_out)
		})
	}
}

// A card that cannot be opened the way the configuration asks - busy, or no
// mono, or not 44100 - is not an improvement on the default device, which is
// typically a plugin or sound server that converts whatever it is given.
func Test_resolveDefaultAudioDevices_unsupportedFormat(t *testing.T) {
	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), func(_ *portaudio.DeviceInfo, _ *adev_param_s, _ bool) bool {
		return false
	})

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// A configuration that defines more than one audio device is choosing devices
// by hand; the card detection would find is probably the one already named.
func Test_resolveDefaultAudioDevices_severalDevicesDefined(t *testing.T) {
	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)
	pa.adev[1].defined = 1
	pa.adev[1].adevice_in = "USB Audio CODEC: USB Audio (hw:1,0)"
	pa.adev[1].adevice_out = "USB Audio CODEC: USB Audio (hw:1,0)"

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// An audio device that was never defined has nothing to resolve.
func Test_resolveDefaultAudioDevices_undefinedDevice(t *testing.T) {
	var pa = makeAudioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Empty(t, pa.adev[1].adevice_in)
	assert.Empty(t, pa.adev[1].adevice_out)
}

func Test_matchPortAudioDeviceByName_directionFilter(t *testing.T) {
	// Two devices for the same ALSA card ID: one input-only, one output-only.
	// This can happen with some USB audio interfaces.
	setFakeALSACards(t, " 2 [MYCARD         ]: USB-Audio - My Audio Device")
	var devices = []*portaudio.DeviceInfo{
		makeDevice("My Audio Device: USB Audio (hw:2,0)", 0, 2), // output only
		makeDevice("My Audio Device: USB Audio (hw:2,1)", 2, 0), // input only
	}

	// Input search should not return an output-only device.
	var dev = matchPortAudioDeviceByName("plughw:MYCARD,0", true, devices)
	assert.Nil(t, dev)

	// Output search should not return an input-only device.
	dev = matchPortAudioDeviceByName("plughw:MYCARD,1", false, devices)
	assert.Nil(t, dev)
}

// --- UDP audio output ---

// setupAdev0 installs a fresh adev_s at index 0 and restores the original on
// test cleanup.  Using index 0 is safe because audio tests are sequential.
func setupAdev0(t *testing.T) *adev_s {
	t.Helper()

	var prev = adev[0]
	t.Cleanup(func() { adev[0] = prev })

	adev[0] = new(adev_s)

	return adev[0]
}

func Test_audioFlushReal_UDP_sendsBytes(t *testing.T) {
	// Start a UDP listener to receive the audio output.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	// Dial from the "transmitter" side.
	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var dev = setupAdev0(t)
	dev.udp_out_sock = conn
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)

	var testData = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	copy(dev.outbuf, testData)
	dev.outbufLen = len(testData)

	var result = audio_flush_real(0)
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

func makeAudioConfig(inName, outName string) *audio_s {
	var pa = new(audio_s)
	pa.adev[0].defined = 1
	pa.adev[0].adevice_in = inName
	pa.adev[0].adevice_out = outName

	return pa
}

func Test_anyDeviceRequiresPortAudio(t *testing.T) {
	tests := []struct {
		name string
		pa   *audio_s
		want bool
	}{
		{
			name: "no devices defined",
			pa:   new(audio_s),
			want: false,
		},
		{
			name: "stdin in, udp out",
			pa:   makeAudioConfig("stdin", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "dash in, udp out",
			pa:   makeAudioConfig("-", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "udp in (uppercase), udp out",
			pa:   makeAudioConfig("UDP:7355", "udp:127.0.0.1:1234"),
			want: false,
		},
		{
			name: "stdin in, soundcard out",
			pa:   makeAudioConfig("stdin", "default"),
			want: true,
		},
		{
			name: "soundcard in, udp out",
			pa:   makeAudioConfig("default", "udp:127.0.0.1:1234"),
			want: true,
		},
		{
			name: "soundcard in, soundcard out",
			pa:   makeAudioConfig("default", "default"),
			want: true,
		},
		{
			name: "stdin in and out, as a single-name ADEVICE gives",
			pa:   makeAudioConfig("stdin", "stdin"),
			want: false,
		},
		{
			name: "dash in and out",
			pa:   makeAudioConfig("-", "-"),
			want: false,
		},
		{
			name: "udp in and out, as a single-name ADEVICE gives",
			pa:   makeAudioConfig("udp:7355", "udp:7355"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, anyDeviceRequiresPortAudio(tt.pa))
		})
	}
}

func Test_audioFlushReal_UDP_emptyBuffer_isNoop(t *testing.T) {
	var dev = setupAdev0(t)
	dev.udp_out_sock = &net.UDPConn{} // non-nil socket; must not be written to
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	dev.outbufLen = 0

	// Should return 0 without attempting a write.
	assert.Equal(t, 0, audio_flush_real(0))
}

func Test_audioUDPSilenceKeepalive_chunkSizeAndCleanShutdown(t *testing.T) {
	// Start a UDP listener to receive the keepalive silence.
	var listener, err = new(net.ListenConfig).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	var conn net.Conn
	conn, err = new(net.Dialer).DialContext(context.Background(), "udp", listener.LocalAddr().String())
	require.NoError(t, err)

	defer conn.Close()

	var dev = setupAdev0(t)
	dev.udp_out_sock = conn
	dev.bitsPerSample = 16
	dev.bytesPerFrame = 2  // mono, 16-bit
	dev.sampleRate = 44100 // 20ms of samples at this rate exceeds UDP_AUDIO_OUT_BUF_MAXLEN, so the chunk must be capped

	var prevXmitSvc = xmitSvc
	t.Cleanup(func() { xmitSvc = prevXmitSvc })
	xmitSvc = &XmitService{} //nolint:exhaustruct_v5

	var stop = make(chan struct{})
	var done = make(chan struct{})

	go func() {
		defer close(done)
		audioUDPSilenceKeepalive(0, stop)
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
		t.Fatal("audioUDPSilenceKeepalive did not stop after stop was closed")
	}
}

// noSuchAudioDevice is a device name no soundcard will match, for testing what
// happens when an output device turns out not to be there.
const noSuchAudioDevice = "Q1TEST no such audio device"

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
			var pa = makeAudioConfig(tt.inName, tt.ouName)
			pa.adev[0].adevice_out_specified = tt.specified

			assert.Equal(t, tt.want, audioOutType(&pa.adev[0]))
		})
	}
}

// --- audio_open with no output device ---

// A receive-only configuration must open on a machine with no audio output
// device at all, rather than refusing to start.  "ADEVICE stdin" - and the
// "-" command line argument - is exactly that: it leaves "stdin" as the
// output device name too, which is nothing we can transmit through.
func Test_audioOpen_stdinOnly_hasNoOutputDevice(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		audio_close()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var pa = makeAudioConfig("stdin", "stdin")
	var refsBefore = portaudioRefCount

	require.Equal(t, 0, audio_open(pa))

	assert.Nil(t, adev[0].outputStream)
	assert.Nil(t, adev[0].udp_out_sock)

	// The point of issue #501: nothing here needs a soundcard, so PortAudio
	// is never initialized.
	assert.Equal(t, refsBefore, portaudioRefCount)

	// Whatever the transmit path produces on the open device is discarded,
	// not written anywhere, and does not upset the buffer bookkeeping.
	require.Equal(t, 0, audio_put_real(0, 42))
	assert.Equal(t, -1, audio_flush_real(0))
	assert.Equal(t, 0, adev[0].outbufLen)

	// Closing must not release a PortAudio reference this open never took.
	audio_close()
	assert.Equal(t, refsBefore, portaudioRefCount)
}

// An output device we only defaulted to, and which turns out not to exist,
// leaves the station receive-only rather than stopping it.
func Test_audioOpen_defaultedOutputDeviceMissing_isNotFatal(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		audio_close()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var pa = makeAudioConfig("stdin", noSuchAudioDevice)

	require.Equal(t, 0, audio_open(pa))

	assert.Nil(t, adev[0].outputStream)
}

// An output device named for transmit in the configuration is asked for
// specifically, so not finding it is a configuration error, not a reason to
// quietly transmit nothing.
func Test_audioOpen_namedOutputDeviceMissing_isFatal(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		audio_close()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var pa = makeAudioConfig("stdin", noSuchAudioDevice)
	pa.adev[0].adevice_out_specified = true

	assert.Equal(t, -1, audio_open(pa))
}

// Naming standard input, or a UDP port to listen on, as the transmit device
// is a configuration error too - neither can transmit.
func Test_audioOpen_namedOutputDeviceCannotTransmit_isFatal(t *testing.T) {
	var prevAdev = adev
	var prevConfig = save_audio_config_p

	t.Cleanup(func() {
		audio_close()

		adev = prevAdev
		save_audio_config_p = prevConfig
	})

	var pa = makeAudioConfig("stdin", "stdin")
	pa.adev[0].adevice_out_specified = true

	assert.Equal(t, -1, audio_open(pa))
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
			var pa = makeAudioConfig("plughw:1,0", tt.ouName)
			pa.adev[0].adevice_out_specified = tt.specified

			applyCommandLineAudioSource(&pa.adev[0], "stdin")

			assert.Equal(t, "stdin", pa.adev[0].adevice_in)
			assert.Equal(t, tt.wantOut, pa.adev[0].adevice_out)
		})
	}
}

// --- audio_transmit_available ---

func Test_audio_transmit_available(t *testing.T) {
	t.Run("device that was never opened", func(t *testing.T) {
		var prev = adev[0]

		t.Cleanup(func() { adev[0] = prev })

		adev[0] = nil

		assert.False(t, audio_transmit_available(0))
	})

	t.Run("device open with no output", func(t *testing.T) {
		setupAdev0(t)
		assert.False(t, audio_transmit_available(0))
	})

	t.Run("device with UDP output", func(t *testing.T) {
		var dev = setupAdev0(t)
		dev.udp_out_sock = &net.UDPConn{}

		assert.True(t, audio_transmit_available(0))
	})

	t.Run("device number out of range", func(t *testing.T) {
		assert.False(t, audio_transmit_available(-1))
		assert.False(t, audio_transmit_available(MAX_ADEVS))
	})
}
