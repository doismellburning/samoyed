// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
)

// A zero after five ones is a stuffed one, whether the next bit is a one or
// a zero, and the count starts again after it.
func TestDestuffDropsTheZeroAfterFiveOnes(t *testing.T) {
	var stuffed = []bool{true, true, true, true, true, false, true, true, true, true, true, false, false}

	assert.Equal(t, []bool{true, true, true, true, true, true, true, true, true, true, false}, Destuff(stuffed))
}

// A frame comes out from between its flags with the stuffing taken out.
func TestHDLCFrameFromLevelsTakesTheFrameFromBetweenTheFlags(t *testing.T) {
	// 0xff needs a stuffed zero after its fifth one.
	var levels = LineLevels(func(line *linecode.Encoder) {
		// The run of ones carries on from one byte to the next.
		var ones = 0

		var write = func(b byte, stuff bool) {
			for range 8 {
				var bit = b&1 != 0
				line.WriteNRZI(bit)

				if bit {
					ones++
				} else {
					ones = 0
				}

				if stuff && ones == 5 {
					line.WriteNRZI(false)

					ones = 0
				}

				b >>= 1
			}
		}

		write(hdlcFlag, false)
		write(0xff, true)
		write(0x01, true)
		write(hdlcFlag, false)
	})

	assert.Equal(t, []byte{0xff, 0x01}, HDLCFrameFromLevels(t, levels))
}
