// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package touchtone

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Text is converted one ASCII letter or digit at a time.  Ranging over a
// string yields runes, which the unicode package classifies by Unicode's
// rules: '٣' (Arabic-Indic three) is a digit there, and turning it into a
// button indexed the translation table out of range, while 'À' is an upper
// case letter with no button to find (issue #1019).
func TestTextNonASCII(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"٣", "À", "\xc0"} {
		var _, errors = TextToMultipress(text, true)
		assert.Equal(t, 1, errors, "TextToMultipress(%q)", text)

		var buttons string
		buttons, errors = TextToTwoKey(text, true)
		assert.Equal(t, 1, errors, "TextToTwoKey(%q)", text)
		assert.Equal(t, "0A", buttons, "TextToTwoKey(%q) treats it as a space", text)

		buttons, errors = TextToCall10("Q1"+text, true)
		assert.Equal(t, 1, errors, "TextToCall10(%q)", "Q1"+text)
		assert.Empty(t, buttons, "TextToCall10(%q)", "Q1"+text)
	}
}

// Each byte, not each rune, becomes two digits, with anything unprintable
// shown as '?'.
func TestTextToASCII2DNonASCII(t *testing.T) {
	t.Parallel()

	var buttons, errors = TextToASCII2D("AÀ", true)
	assert.Equal(t, 0, errors)
	assert.Equal(t, "333131", buttons)
}
