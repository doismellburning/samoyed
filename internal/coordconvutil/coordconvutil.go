// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package coordconvutil holds utilities for working with
// https://github.com/tzneal/coordconv
package coordconvutil

import (
	"github.com/tzneal/coordconv"
)

func HemisphereRuneToCoordconvHemisphere(_hemi rune) coordconv.Hemisphere {
	switch _hemi {
	case 'N':
		return coordconv.HemisphereNorth
	case 'S':
		return coordconv.HemisphereSouth
	default:
		return coordconv.HemisphereInvalid
	}
}

func HemisphereToRune(h coordconv.Hemisphere) rune {
	switch h {
	case coordconv.HemisphereNorth:
		return 'N'
	case coordconv.HemisphereSouth:
		return 'S'
	case coordconv.HemisphereInvalid:
		return '!'
	default:
		return '?'
	}
}
