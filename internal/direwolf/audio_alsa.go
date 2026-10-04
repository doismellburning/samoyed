// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"os"
	"strings"

	"github.com/gordonklaus/portaudio"
)

// parseALSADeviceName parses ALSA device names like "hw:Card,Dev,Sub" or
// "plughw:Card,Dev,Sub" and returns the card name and device number.
// Returns ("", -1) if the name doesn't match the ALSA pattern.
func parseALSADeviceName(name string) (cardName string, devNum int) {
	// Strip prefix: hw:, plughw:, etc.
	var lower = strings.ToLower(name)

	var idx = strings.Index(lower, "hw:")
	if idx < 0 {
		return "", -1
	}
	var rest = name[idx+3:] // after "hw:"

	// Split by comma: Card,Dev[,Sub]
	var parts = strings.SplitN(rest, ",", 3)
	if len(parts) < 2 {
		return parts[0], -1
	}

	devNum = -1
	fmt.Sscanf(parts[1], "%d", &devNum)

	return parts[0], devNum
}

// parseALSACardsProc parses the content of /proc/asound/cards and returns a
// map from ALSA card ID (e.g. "FTDX10") to card number.
func parseALSACardsProc(content string) map[string]int {
	var result = make(map[string]int)
	for line := range strings.SplitSeq(content, "\n") {
		var trimmed = strings.TrimSpace(line)
		var bracketStart = strings.Index(trimmed, "[")
		if bracketStart < 0 {
			continue
		}
		var bracketEnd = strings.Index(trimmed, "]")
		if bracketEnd <= bracketStart {
			continue
		}
		var numStr = strings.TrimSpace(trimmed[:bracketStart])
		var cardID = strings.TrimSpace(trimmed[bracketStart+1 : bracketEnd])
		var num int
		var _, sscanfErr = fmt.Sscanf(numStr, "%d", &num)
		if sscanfErr != nil {
			continue
		}
		result[cardID] = num
	}

	return result
}

// alsaCardsPath is where Linux lists the ALSA sound cards.
const alsaCardsPath = "/proc/asound/cards"

// resolveALSACardNumber resolves an ALSA card ID (e.g. "FTDX10", as used in
// plughw:FTDX10,0) to its numeric card index by reading cardsPath, normally
// alsaCardsPath.  Returns (0, false) if the card ID cannot be resolved.
func resolveALSACardNumber(cardID string, cardsPath string) (int, bool) {
	var content, err = os.ReadFile(cardsPath) //nolint:gosec // alsaCardsPath, or a test's own file.
	if err != nil {
		return 0, false
	}
	var cards = parseALSACardsProc(string(content))
	var num, ok = cards[cardID]

	return num, ok
}

// matchPortAudioDeviceByName searches devices for one matching name using
// several strategies: exact match, substring match, and ALSA-style name
// matching (including resolution of udev-assigned card IDs via
// cardsPath, normally alsaCardsPath). Returns nil if no match is found.
func matchPortAudioDeviceByName(name string, forInput bool, devices []*portaudio.DeviceInfo, cardsPath string) *portaudio.DeviceInfo {
	var devMatchesDirection = func(dev *portaudio.DeviceInfo) bool {
		if forInput {
			return dev.MaxInputChannels > 0
		}

		return dev.MaxOutputChannels > 0
	}

	// Try exact match first.
	for _, dev := range devices {
		if dev.Name == name && devMatchesDirection(dev) {
			return dev
		}
	}

	// Try substring match (check both directions).
	for _, dev := range devices {
		if devMatchesDirection(dev) {
			var nameLower = strings.ToLower(name)
			var devLower = strings.ToLower(dev.Name)
			if strings.Contains(devLower, nameLower) || strings.Contains(nameLower, devLower) {
				return dev
			}
		}
	}

	// Try ALSA-style name matching.
	// Config names like "plughw:Loopback,1,1" need to match PortAudio names
	// like "Loopback: PCM (hw:0,1)".
	// Extract card name and device number from the config, then match against
	// the card name prefix and (hw:X,Dev) pattern in PortAudio device names.
	var cardName, devNum = parseALSADeviceName(name)
	if cardName != "" {
		// First try matching by card name substring in PortAudio device names.
		// This covers the common case where the ALSA card name matches part of
		// the PortAudio description (e.g. "Loopback" appearing in "Loopback: PCM (hw:0,1)").
		for _, dev := range devices {
			if !devMatchesDirection(dev) {
				continue
			}
			var devLower = strings.ToLower(dev.Name)
			if !strings.Contains(devLower, strings.ToLower(cardName)) {
				continue
			}
			if devNum >= 0 {
				var target = fmt.Sprintf(",%d)", devNum)
				if strings.Contains(dev.Name, target) {
					return dev
				}
			} else {
				return dev
			}
		}

		// The card name may be a udev-assigned ALSA card ID that differs from
		// the hardware description PortAudio uses. Resolve it to a numeric card
		// index via /proc/asound/cards (Linux) and match by (hw:N,M).
		if cardNum, ok := resolveALSACardNumber(cardName, cardsPath); ok {
			for _, dev := range devices {
				if !devMatchesDirection(dev) {
					continue
				}
				if devNum >= 0 {
					var target = fmt.Sprintf("(hw:%d,%d)", cardNum, devNum)
					if strings.Contains(dev.Name, target) {
						return dev
					}
				} else {
					var target = fmt.Sprintf("(hw:%d,", cardNum)
					if strings.Contains(dev.Name, target) {
						return dev
					}
				}
			}
		}
	}

	return nil
}
