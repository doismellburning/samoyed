// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

// Package coordconvutil holds utilities for working with
// https://github.com/tzneal/coordconv
package coordconvutil

import (
	"github.com/tzneal/coordconv"
)

// HemisphereFromRune turns Dire Wolf's N or S into a coordconv.Hemisphere, and
// anything else into coordconv.HemisphereInvalid.
func HemisphereFromRune(_hemi rune) coordconv.Hemisphere {
	switch _hemi {
	case 'N':
		return coordconv.HemisphereNorth
	case 'S':
		return coordconv.HemisphereSouth
	default:
		return coordconv.HemisphereInvalid
	}
}

// HemisphereToRune turns a coordconv.Hemisphere into N or S, or ! for
// coordconv.HemisphereInvalid and ? for anything else.
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
