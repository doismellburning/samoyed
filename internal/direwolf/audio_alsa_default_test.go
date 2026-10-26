// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/gordonklaus/portaudio"
	"github.com/stretchr/testify/assert"
)

// piWithUSBInterface is what PortAudio enumerates on a Raspberry Pi with a USB
// sound card in it and no sound server running.
func piWithUSBInterface() []*portaudio.DeviceInfo {
	return []*portaudio.DeviceInfo{
		makeDevice("bcm2835 Headphones: - (hw:0,0)", 0, 8),
		makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 2),
		makeDevice("sysdefault", 0, 8),
		makeDevice("default", 32, 32),
	}
}

func Test_alsaDefaultCardWarnings(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name    string
		inName  string
		outName string
		devices []*portaudio.DeviceInfo
		want    []alsaDefaultCardWarning
	}{
		{
			name:    "both sides at the default, radio on card 1",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: piWithUSBInterface(),
			want: []alsaDefaultCardWarning{
				{direction: "transmit", cards: []string{"plughw:1,0"}},
			},
		},
		{
			name:    "card 0 captures too, so receive is at risk as well",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: []*portaudio.DeviceInfo{
				makeDevice("HDA Intel PCH: ALC257 Analog (hw:0,0)", 2, 2),
				makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 2),
				makeDevice("default", 32, 32),
			},
			want: []alsaDefaultCardWarning{
				{direction: "receive", cards: []string{"plughw:1,0"}},
				{direction: "transmit", cards: []string{"plughw:1,0"}},
			},
		},
		{
			name:    "card 0 cannot play, so PortAudio falls back for transmit",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: []*portaudio.DeviceInfo{
				makeDevice("Capture Only: - (hw:0,0)", 2, 0),
				makeDevice("USB Audio CODEC: USB Audio (hw:1,0)", 2, 2),
				makeDevice("default", 32, 32),
			},
			want: []alsaDefaultCardWarning{
				{direction: "receive", cards: []string{"plughw:1,0"}},
			},
		},
		{
			name:    "a sound server's default is a choice",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: append(piWithUSBInterface(), makeDevice("pipewire", 64, 64)),
			want:    nil,
		},
		{
			name:    "the only card is card 0",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: []*portaudio.DeviceInfo{
				makeDevice("USB Audio CODEC: USB Audio (hw:0,0)", 2, 2),
				makeDevice("default", 32, 32),
			},
			want: nil,
		},
		{
			name:    "named devices are left alone",
			inName:  "plughw:1,0",
			outName: "plughw:1,0",
			devices: piWithUSBInterface(),
			want:    nil,
		},
		{
			name:    "only the side left at the default",
			inName:  "udp:7355",
			outName: DEFAULT_ADEVICE,
			devices: piWithUSBInterface(),
			want: []alsaDefaultCardWarning{
				{direction: "transmit", cards: []string{"plughw:1,0"}},
			},
		},
		{
			name:    "macOS names carry no ALSA card",
			inName:  DEFAULT_ADEVICE,
			outName: DEFAULT_ADEVICE,
			devices: []*portaudio.DeviceInfo{
				makeDevice("MacBook Pro Microphone", 1, 0),
				makeDevice("USB Audio CODEC", 2, 2),
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var pa = makeRadioConfig(tt.inName, tt.outName)

			assert.Equal(t, tt.want, alsaDefaultCardWarnings(pa, tt.devices))
		})
	}
}
