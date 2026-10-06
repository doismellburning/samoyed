// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hdlcFlag is the 01111110 that HDLC sends before and after each frame.
const hdlcFlag byte = 0x7e

// Destuff drops the zero an HDLC sender inserts after five consecutive ones,
// so that a frame never holds a flag's six.
func Destuff(bits []bool) []bool {
	var data = make([]bool, 0, len(bits))
	var ones = 0

	for _, bit := range bits {
		if ones == 5 {
			ones = 0

			continue // The stuffed zero, which was never data.
		}

		data = append(data, bit)

		if bit {
			ones++
		} else {
			ones = 0
		}
	}

	return data
}

// HDLCFrameFromLevels takes the frame out of the line levels an HDLC sender
// sent for it, NRZI from a low start: a flag at each end, and between them the
// frame, its FCS included, with the stuffing the sender added taken out.
func HDLCFrameFromLevels(tb testing.TB, levels []int) []byte {
	tb.Helper()

	var data = NRZIDecode(levels)

	require.Greater(tb, len(data), 16, "there should be a frame between the flags")

	// Flags are sent without stuffing, so they are whole bytes at each end.
	assert.Equal(tb, []byte{hdlcFlag}, PackLSBFirst(tb, data[:8]), "missing start flag")
	assert.Equal(tb, []byte{hdlcFlag}, PackLSBFirst(tb, data[len(data)-8:]), "missing end flag")

	return PackLSBFirst(tb, Destuff(data[8:len(data)-8]))
}
