// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"strconv"
	"strings"

	"github.com/gordonklaus/portaudio"
	"github.com/sirupsen/logrus"
)

// alsaHardwareMarker appears in the PortAudio name of an ALSA device that is a
// real sound card - "USB Audio CODEC: USB Audio (hw:1,0)" - and not in the
// names of the ALSA plugins that PortAudio enumerates alongside them, nor in
// any CoreAudio or WASAPI device name.
const alsaHardwareMarker = "(hw:"

// isSoundServerDeviceName reports whether name, lower-cased, is the ALSA plugin
// that is a sound server's own device.
func isSoundServerDeviceName(name string) bool {
	switch name {
	case "jack", "pipewire", "pulse":
		return true
	default:
		return false
	}
}

// audioSoundServerPresent reports whether a sound server is running.
//
// PortAudio drops a plugin it cannot open while building its device list, so
// one of these being enumerated at all means the server answered.
func audioSoundServerPresent(devices []*portaudio.DeviceInfo) bool {
	for _, dev := range devices {
		var lower = strings.ToLower(strings.TrimSpace(dev.Name))

		if isSoundServerDeviceName(lower) {
			return true
		}

		// A plugin can be qualified by what it wraps: "pulse:SERVER=...".
		if colon := strings.IndexByte(lower, ':'); colon > 0 && isSoundServerDeviceName(lower[:colon]) {
			return true
		}
	}

	return false
}

// alsaCardDevices returns the ALSA sound cards among devices that can capture
// (forInput) or play (!forInput), leaving out the plugins enumerated alongside
// them.
func alsaCardDevices(devices []*portaudio.DeviceInfo, forInput bool) []*portaudio.DeviceInfo {
	var cards []*portaudio.DeviceInfo

	for _, dev := range devices {
		if forInput && dev.MaxInputChannels <= 0 {
			continue
		}

		if !forInput && dev.MaxOutputChannels <= 0 {
			continue
		}

		if !strings.Contains(dev.Name, alsaHardwareMarker) {
			continue
		}

		cards = append(cards, dev)
	}

	return cards
}

// alsaDefaultCardWarning describes, for one direction, why ALSA's default
// device may not be the one wanted.
type alsaDefaultCardWarning struct {
	direction string   // "receive" or "transmit"
	cards     []string // The cards that could serve instead, as ADEVICE names
}

// alsaDefaultCardWarnings looks for audio devices left at the default on a
// machine where that default is nobody's choice.
//
// With a sound server running, or on macOS and Windows, the default device is
// the one chosen in the sound settings, and is left alone.  Bare ALSA has no
// such choice: alsa.conf's "defaults.pcm.card 0" makes the default card 0,
// whatever card 0 turns out to be - on a headless machine with a USB radio
// interface in it, usually the built-in audio.  Where some other card could
// serve that direction, say so, but use the default as ever: picking a card
// for the operator is a guess, and one they can make better with a line of
// configuration.
func alsaDefaultCardWarnings(pa *RadioConfig, devices []*portaudio.DeviceInfo) []alsaDefaultCardWarning {
	if audioSoundServerPresent(devices) {
		return nil
	}

	var receiveDefault, transmitDefault = false, false

	for a := range MAX_ADEVS {
		if pa.adev[a].defined == 0 {
			continue
		}

		if audioNameIsDefault(pa.adev[a].adevice_in) {
			receiveDefault = true
		}

		if audioOutType(&pa.adev[a]) == AUDIO_OUT_TYPE_SOUNDCARD && audioNameIsDefault(pa.adev[a].adevice_out) {
			transmitDefault = true
		}
	}

	var warnings []alsaDefaultCardWarning

	if receiveDefault {
		if others := alsaCardsOtherThanZero(devices, true); len(others) > 0 {
			warnings = append(warnings, alsaDefaultCardWarning{direction: "receive", cards: others})
		}
	}

	if transmitDefault {
		if others := alsaCardsOtherThanZero(devices, false); len(others) > 0 {
			warnings = append(warnings, alsaDefaultCardWarning{direction: "transmit", cards: others})
		}
	}

	return warnings
}

// alsaCardsOtherThanZero returns, as ADEVICE names ("plughw:1,0"), the cards
// other than card 0 that can serve the given direction - but only where card 0
// can serve it too.  If card 0 cannot, ALSA's default cannot open that way,
// and PortAudio falls back to the first card that can, which is not card 0.
func alsaCardsOtherThanZero(devices []*portaudio.DeviceInfo, forInput bool) []string {
	var names []string

	var cardZero = false

	for _, dev := range alsaCardDevices(devices, forInput) {
		var card, devNum = parseALSADeviceName(dev.Name)
		if card == "" || devNum < 0 {
			continue
		}

		if card == "0" {
			cardZero = true

			continue
		}

		names = append(names, "plughw:"+card+","+strconv.Itoa(devNum))
	}

	if !cardZero {
		return nil
	}

	return names
}

// audioNameIsDefault reports whether an audio device name asks for the
// system's default device rather than naming one.
func audioNameIsDefault(name string) bool {
	return name == "" || strings.EqualFold(name, DEFAULT_ADEVICE)
}

// warnAboutALSADefaultCard logs alsaDefaultCardWarnings.
func warnAboutALSADefaultCard(pa *RadioConfig, devices []*portaudio.DeviceInfo) {
	for _, w := range alsaDefaultCardWarnings(pa, devices) {
		logrus.WithFields(logrus.Fields{
			"direction": w.direction,
			"others":    strings.Join(w.cards, " "),
		}).Warn("No sound server, so the default audio device is ALSA card 0; set ADEVICE if your radio is on another card")
	}
}
