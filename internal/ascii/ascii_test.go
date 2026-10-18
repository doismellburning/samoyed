// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ascii

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lower = "abcdefghijklmnopqrstuvwxyz"
	digit = "0123456789"
	punct = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
)

// in reports whether c is in set, which holds only ASCII.
func in(set string, c rune) bool {
	return c >= 0 && c < 0x80 && strings.ContainsRune(set, c)
}

// TestBytesMatchCLocale checks every byte against the character sets the C
// standard gives for the "C" locale.
func TestBytesMatchCLocale(t *testing.T) {
	t.Parallel()

	for i := range 256 {
		var b = byte(i)
		var c = rune(i)

		assert.Equal(t, in(digit, c), IsDigit(b), "IsDigit(%#02x)", i)
		assert.Equal(t, in(upper, c), IsUpper(b), "IsUpper(%#02x)", i)
		assert.Equal(t, in(lower, c), IsLower(b), "IsLower(%#02x)", i)
		assert.Equal(t, in(upper+lower, c), IsAlpha(b), "IsAlpha(%#02x)", i)
		assert.Equal(t, in(upper+lower+digit, c), IsAlnum(b), "IsAlnum(%#02x)", i)
		assert.Equal(t, in(punct, c), IsPunct(b), "IsPunct(%#02x)", i)
		assert.Equal(t, in(upper+lower+digit+punct+" ", c), IsPrint(b), "IsPrint(%#02x)", i)

		if in(lower, c) {
			assert.Equal(t, b-'a'+'A', ToUpper(b), "ToUpper(%#02x)", i)
		} else {
			assert.Equal(t, b, ToUpper(b), "ToUpper(%#02x)", i)
		}
	}
}

// TestRunesOutsideASCII checks runes that the unicode package would put in a
// class.
func TestRunesOutsideASCII(t *testing.T) {
	t.Parallel()

	for _, c := range []rune{'À', 'à', '٣', '¹', '¡', 'ı', 'ſ', '\u212a', -1, 0x10ffff} {
		assert.False(t, IsDigit(c), "IsDigit(%U)", c)
		assert.False(t, IsUpper(c), "IsUpper(%U)", c)
		assert.False(t, IsLower(c), "IsLower(%U)", c)
		assert.False(t, IsAlpha(c), "IsAlpha(%U)", c)
		assert.False(t, IsAlnum(c), "IsAlnum(%U)", c)
		assert.False(t, IsPunct(c), "IsPunct(%U)", c)
		assert.False(t, IsPrint(c), "IsPrint(%U)", c)
		assert.Equal(t, c, ToUpper(c), "ToUpper(%U)", c)
	}

	assert.True(t, IsUpper('Q'))
	assert.Equal(t, 'Q', ToUpper('q'))
}
