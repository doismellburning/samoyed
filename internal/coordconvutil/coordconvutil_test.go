// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package coordconvutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tzneal/coordconv"
)

func TestHemisphereFromRune(t *testing.T) {
	assert.Equal(t, coordconv.HemisphereNorth, HemisphereFromRune('N'))
	assert.Equal(t, coordconv.HemisphereSouth, HemisphereFromRune('S'))
	assert.Equal(t, coordconv.HemisphereInvalid, HemisphereFromRune('n'))
	assert.Equal(t, coordconv.HemisphereInvalid, HemisphereFromRune('X'))
}

func TestHemisphereToRune(t *testing.T) {
	assert.Equal(t, 'N', HemisphereToRune(coordconv.HemisphereNorth))
	assert.Equal(t, 'S', HemisphereToRune(coordconv.HemisphereSouth))
	assert.Equal(t, '!', HemisphereToRune(coordconv.HemisphereInvalid))
}

func TestHemisphereRoundTrip(t *testing.T) {
	for _, r := range []rune{'N', 'S'} {
		assert.Equal(t, r, HemisphereToRune(HemisphereFromRune(r)))
	}
}
