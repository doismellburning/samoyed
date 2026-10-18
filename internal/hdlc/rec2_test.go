// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hdlc

import (
	"bytes"
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
)

// The APRS sanity check looks for a control byte and PID after the
// addresses.  A frame that ends after the control byte used to have the PID
// read from past its end, which the fuzzer found by way of a bit-fix retry.
func TestSanityCheckAPRSFrameEndsAfterControl(t *testing.T) {
	t.Parallel()

	var frame = bytes.Repeat([]byte{'A' << 1}, 21) // Three addresses...
	frame[20] |= 1                                 // ...the last marked as such,
	frame = append(frame, 0x03)                    // then a UI control byte and nothing more.

	assert.NotPanics(t, func() {
		assert.False(t, sanity_check(frame, phy.BitFixSingle, phy.SanityAPRS))
	})

	// All addresses, with no control byte either.
	assert.NotPanics(t, func() {
		assert.False(t, sanity_check(frame[:21], phy.BitFixSingle, phy.SanityAPRS))
	})
}

// AX.25 addresses hold upper case letters and digits, padded with spaces.
func TestSanityCheckAX25AddressCharacters(t *testing.T) {
	t.Parallel()

	var frame = func(dest string) []byte {
		var f []byte
		for _, addr := range []string{dest, "Q2TEST"} {
			for k := range 6 {
				f = append(f, addr[k]<<1)
			}

			f = append(f, 0x60)
		}

		f[13] |= 1 // The last address.

		return append(f, 0x03, 0xf0)
	}

	assert.True(t, sanity_check(frame("Q1TEST"), phy.BitFixSingle, phy.SanityAX25))
	assert.True(t, sanity_check(frame("Q1    "), phy.BitFixSingle, phy.SanityAX25))
	assert.False(t, sanity_check(frame("q1TEST"), phy.BitFixSingle, phy.SanityAX25), "lower case")
	assert.False(t, sanity_check(frame("Q1-EST"), phy.BitFixSingle, phy.SanityAX25), "punctuation")
	assert.False(t, sanity_check(frame(" Q1TES"), phy.BitFixSingle, phy.SanityAX25), "leading space")
}
