// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package bitstuff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzUnstuff covers taking the HDLC frame back out of an FX.25 codeblock,
// whose contents anyone transmitting on the channel controls.  Whatever
// Unstuff accepts, stuffing it again and unstuffing that gives it back.
func FuzzUnstuff(f *testing.F) {
	var frame = []byte("Q1TEST>APDW17:hello")

	var stuffed, _ = Stuff(frame, 0)
	f.Add(stuffed)

	var padded, _ = Stuff(frame, 64)
	f.Add(padded)

	f.Add([]byte{})
	f.Add([]byte{0x00, flag})
	f.Add([]byte{flag, 0xff})
	f.Add([]byte{flag, 0xf0, 0x03})
	f.Add([]byte{flag, 0x00})

	f.Fuzz(func(t *testing.T, in []byte) {
		var out, err = Unstuff(in)
		if err != nil || len(out) == 0 {
			return
		}

		var again, _ = Stuff(out, 0)

		var back, backErr = Unstuff(again)
		require.NoError(t, backErr)
		assert.Equal(t, out, back)
	})
}
