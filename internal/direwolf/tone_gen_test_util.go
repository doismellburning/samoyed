// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

//nolint:gochecknoglobals
package direwolf

import "github.com/sirupsen/logrus"

// toneGenCapture, when set, is given the bits a frame serializes to instead of
// the tone generator.  It is how a test sees what would have gone out over the
// air without an audio device to send it to.
var toneGenCapture func(channel int, data int)

// putBit hands one bit to whatever is standing in for the modulator: the
// capture if a test has set one, the sender's tone generator otherwise.
func (s *Layer2Sender) putBit(data int) {
	if toneGenCapture != nil {
		toneGenCapture(s.channel, data)

		return
	}

	if s.toneGenerator == nil {
		logrus.WithField("channel", s.channel).Error("Invalid channel for tone generation")

		return
	}

	s.toneGenerator.PutBit(data)
}
