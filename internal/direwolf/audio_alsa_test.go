// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gordonklaus/portaudio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parseALSADeviceName ---

func Test_parseALSADeviceName(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var card, dev = parseALSADeviceName(tt.input)
			assert.Equal(t, tt.wantCard, card)
			assert.Equal(t, tt.wantDevNum, dev)
		})
	}
}

// --- parseALSACardsProc ---

func Test_parseALSACardsProc(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// fakeALSACards writes a fake /proc/asound/cards to a temp file and returns
// its path.
func fakeALSACards(t *testing.T, content string) string {
	t.Helper()
	var tmp = filepath.Join(t.TempDir(), "cards")
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))

	return tmp
}

// Test the udev card ID scenario from doismellburning/samoyed#468:
// The user configures "plughw:FTDX10,0" but PortAudio enumerates the device
// as "USB AUDIO  CODEC: USB Audio (hw:2,0)" because it uses the hardware
// description, not the udev-assigned ALSA card ID.
func Test_matchPortAudioDeviceByName_udevCardID(t *testing.T) {
	t.Parallel()

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
		t.Parallel()

		var dev = matchPortAudioDeviceByName("plughw:FTDX10,0", true, devices, fakeALSACards(t, cardsContent))
		assert.NotNil(t, dev)
		assert.Equal(t, "USB AUDIO  CODEC: USB Audio (hw:2,0)", dev.Name)
	})

	t.Run("FT991A resolves via card ID", func(t *testing.T) {
		t.Parallel()

		var dev = matchPortAudioDeviceByName("plughw:FT991A,0", true, devices, fakeALSACards(t, cardsContent))
		assert.NotNil(t, dev)
		assert.Equal(t, "USB Audio CODEC: USB Audio (hw:3,0)", dev.Name)
	})
}

func Test_matchPortAudioDeviceByName_exactMatch(t *testing.T) {
	t.Parallel()

	var devices = []*portaudio.DeviceInfo{
		makeDevice("HDA Intel PCH: ALC3234 Analog (hw:0,0)", 2, 2),
		makeDevice("USB AUDIO  CODEC: USB Audio (hw:2,0)", 2, 2),
	}

	var dev = matchPortAudioDeviceByName("USB AUDIO  CODEC: USB Audio (hw:2,0)", true, devices, alsaCardsPath)
	assert.NotNil(t, dev)
	assert.Equal(t, "USB AUDIO  CODEC: USB Audio (hw:2,0)", dev.Name)
}

func Test_matchPortAudioDeviceByName_substrMatch(t *testing.T) {
	t.Parallel()

	var devices = []*portaudio.DeviceInfo{
		makeDevice("Loopback: PCM (hw:0,0)", 2, 2),
		makeDevice("Loopback: PCM (hw:0,1)", 2, 2),
	}

	// "Loopback" substring should match the first device that contains it.
	var dev = matchPortAudioDeviceByName("Loopback", true, devices, alsaCardsPath)
	assert.NotNil(t, dev)
}

func Test_matchPortAudioDeviceByName_alsaStyleLoopback(t *testing.T) {
	t.Parallel()

	var devices = []*portaudio.DeviceInfo{
		makeDevice("Loopback: PCM (hw:0,0)", 2, 2),
		makeDevice("Loopback: PCM (hw:0,1)", 2, 2),
	}

	// plughw:Loopback,1 should match the device with (hw:0,1).
	var dev = matchPortAudioDeviceByName("plughw:Loopback,1", true, devices, alsaCardsPath)
	assert.NotNil(t, dev)
	assert.Equal(t, "Loopback: PCM (hw:0,1)", dev.Name)
}

func Test_matchPortAudioDeviceByName_noMatch(t *testing.T) {
	t.Parallel()

	var cards = fakeALSACards(t, "")
	var devices = []*portaudio.DeviceInfo{
		makeDevice("HDA Intel PCH: ALC3234 Analog (hw:0,0)", 2, 2),
	}

	var dev = matchPortAudioDeviceByName("plughw:NonExistent,0", true, devices, cards)
	assert.Nil(t, dev)
}

func Test_matchPortAudioDeviceByName_directionFilter(t *testing.T) {
	t.Parallel()

	// Two devices for the same ALSA card ID: one input-only, one output-only.
	// This can happen with some USB audio interfaces.
	var cards = fakeALSACards(t, " 2 [MYCARD         ]: USB-Audio - My Audio Device")
	var devices = []*portaudio.DeviceInfo{
		makeDevice("My Audio Device: USB Audio (hw:2,0)", 0, 2), // output only
		makeDevice("My Audio Device: USB Audio (hw:2,1)", 2, 0), // input only
	}

	// Input search should not return an output-only device.
	var dev = matchPortAudioDeviceByName("plughw:MYCARD,0", true, devices, cards)
	assert.Nil(t, dev)

	// Output search should not return an input-only device.
	dev = matchPortAudioDeviceByName("plughw:MYCARD,1", false, devices, cards)
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

// allFormatsSupported stands in for AudioDevices.audioDeviceSupportsFormat where the test
// is not about what the card can do.
func allFormatsSupported(_ *portaudio.DeviceInfo, _ *adev_param_s, _ bool) bool {
	return true
}

func Test_audioSoundServerPresent(t *testing.T) {
	t.Parallel()

	t.Run("bare ALSA", func(t *testing.T) {
		t.Parallel()

		assert.False(t, audioSoundServerPresent(alsaDeviceList()))
	})

	t.Run("PulseAudio or PipeWire answering", func(t *testing.T) {
		t.Parallel()

		var devices = append(alsaDeviceList(), makeDevice("pulse", 32, 32))
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("JACK", func(t *testing.T) {
		t.Parallel()

		var devices = append(alsaDeviceList(), makeDevice("jack", 64, 64))
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("a qualified plugin name", func(t *testing.T) {
		t.Parallel()

		var devices = []*portaudio.DeviceInfo{makeDevice("pulse:SERVER=/run/user/1000/pulse/native", 32, 32)}
		assert.True(t, audioSoundServerPresent(devices))
	})

	t.Run("a card named after one", func(t *testing.T) {
		t.Parallel()

		var devices = []*portaudio.DeviceInfo{makeDevice("PulseTest: USB Audio (hw:1,0)", 2, 2)}
		assert.False(t, audioSoundServerPresent(devices))
	})
}

// The ALSA plugins alongside the cards say nothing about what is attached to
// the machine, so only the cards are candidates.
func Test_alsaCardDevices(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	var devices = []*portaudio.DeviceInfo{
		makeDevice("Built-in Microphone", 2, 0),
		makeDevice("Built-in Output", 0, 2),
		makeDevice("USB Audio CODEC", 2, 2),
	}

	assert.Empty(t, alsaCardDevices(devices, true))
	assert.Empty(t, alsaCardDevices(devices, false))
}

func Test_autoDetectAudioDevice(t *testing.T) {
	t.Parallel()

	t.Run("the only card", func(t *testing.T) {
		t.Parallel()

		var dev = autoDetectAudioDevice(alsaDeviceList(), true)
		require.NotNil(t, dev)
		assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", dev.Name)
	})

	t.Run("a choice to make is not our choice", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, autoDetectAudioDevice(alsaDeviceList(), false))
	})

	t.Run("no card at all", func(t *testing.T) {
		t.Parallel()

		var devices = []*portaudio.DeviceInfo{makeDevice("default", 32, 32)}
		assert.Nil(t, autoDetectAudioDevice(devices, true))
	})
}

// A sound server's default is the operator's own choice of device, so it is
// followed rather than second-guessed.
func Test_resolveDefaultAudioDevices_soundServerRunning(t *testing.T) {
	t.Parallel()

	var devices = append(alsaDeviceList(), makeDevice("pulse", 32, 32))

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, devices, allFormatsSupported)

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// With no sound server to have chosen anything, a machine with one sound card
// needs no ADEVICE: both directions land on the card, including the transmit
// side, where the other playback device is the machine's own speakers rather
// than the other half of the radio.
func Test_resolveDefaultAudioDevices_theOnlyCard(t *testing.T) {
	t.Parallel()

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_out)
}

// "ADEVICE auto" asks for the same thing out loud.
func Test_resolveDefaultAudioDevices_autoKeyword(t *testing.T) {
	t.Parallel()

	var pa = makeRadioConfig(AUTO_ADEVICE, AUTO_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_out)
}

// A card that can only capture leaves the transmit side to be detected on its
// own, and a machine with more than one playback device keeps the default.
func Test_resolveDefaultAudioDevices_captureOnlyCard(t *testing.T) {
	t.Parallel()

	var devices = []*portaudio.DeviceInfo{
		makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
		makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 0),
		makeDevice("default", 32, 32),
	}

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, devices, allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, "bcm2835 Headphones: - (hw:0,0)", pa.adev[0].adevice_out)
}

// A device named for transmit is a choice of its own, so an explicit default
// there is detected on its own terms rather than following the input side.
func Test_resolveDefaultAudioDevices_outputSpecified(t *testing.T) {
	t.Parallel()

	var pa = makeRadioConfig(DEFAULT_ADEVICE, AUTO_ADEVICE)
	pa.adev[0].adevice_out_specified = true

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, "USB Audio CODEC: USB Audio (hw:1,0)", pa.adev[0].adevice_in)
	assert.Equal(t, AUTO_ADEVICE, pa.adev[0].adevice_out)
}

// Anything the configuration names is left alone, as is anything that is not a
// sound card.
func Test_resolveDefaultAudioDevices_leavesOthersAlone(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var devices = []*portaudio.DeviceInfo{
				makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
				makeDevice("default", 32, 32),
			}

			var pa = makeRadioConfig(tt.inName, tt.outName)

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
	t.Parallel()

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), func(_ *portaudio.DeviceInfo, _ *adev_param_s, _ bool) bool {
		return false
	})

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// A configuration that defines more than one audio device is choosing devices
// by hand; the card detection would find is probably the one already named.
func Test_resolveDefaultAudioDevices_severalDevicesDefined(t *testing.T) {
	t.Parallel()

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)
	pa.adev[1].defined = 1
	pa.adev[1].adevice_in = "USB Audio CODEC: USB Audio (hw:1,0)"
	pa.adev[1].adevice_out = "USB Audio CODEC: USB Audio (hw:1,0)"

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_in)
	assert.Equal(t, DEFAULT_ADEVICE, pa.adev[0].adevice_out)
}

// An audio device that was never defined has nothing to resolve.
func Test_resolveDefaultAudioDevices_undefinedDevice(t *testing.T) {
	t.Parallel()

	var pa = makeRadioConfig(DEFAULT_ADEVICE, DEFAULT_ADEVICE)

	resolveDefaultAudioDevices(pa, alsaDeviceList(), allFormatsSupported)

	assert.Empty(t, pa.adev[1].adevice_in)
	assert.Empty(t, pa.adev[1].adevice_out)
}
