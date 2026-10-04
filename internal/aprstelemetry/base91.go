// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package aprstelemetry

import (
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// The 91 digits of base 91, as used by the compressed telemetry format.
const (
	base91Min = '!'
	base91Max = '{'
)

// base91Digit is the value of one base 91 digit, or Nothing if c is not one.
func base91Digit(c byte) maybe.Maybe[int] {
	if c < base91Min || c > base91Max {
		logrus.WithField("character", string(c)).Debug("Not a valid character for base 91 telemetry data")

		return maybe.Nothing[int]()
	}

	return maybe.Just(int(c - base91Min))
}

// base91Pair is the value of two base 91 digits, most significant first, or
// Nothing if either is not a base 91 digit.
func base91Pair(first, second byte) maybe.Maybe[int] {
	return maybe.Bind(base91Digit(first), func(hi int) maybe.Maybe[int] {
		return maybe.Fmap(func(lo int) int { return hi*91 + lo }, base91Digit(second))
	})
}
