// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
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
	var listener, err = net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	// Dial from the "transmitter" side.
	var conn net.Conn
	conn, err = net.Dial("udp", listener.LocalAddr().String())
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

func Test_audioFlushReal_UDP_emptyBuffer_isNoop(t *testing.T) {
	var dev = setupAdev0(t)
	dev.udp_out_sock = &net.UDPConn{} // non-nil socket; must not be written to
	dev.outbufSizeInBytes = UDP_AUDIO_OUT_BUF_MAXLEN
	dev.outbuf = make([]byte, UDP_AUDIO_OUT_BUF_MAXLEN)
	dev.outbufLen = 0

	// Should return 0 without attempting a write.
	assert.Equal(t, 0, audio_flush_real(0))
}
