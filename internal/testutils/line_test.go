// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
)

// LineLevels records each level as it is written, NRZI or not, starting from
// a low line.
func TestLineLevelsRecordsWhatIsWritten(t *testing.T) {
	var levels = LineLevels(func(line *linecode.Encoder) {
		line.WriteNRZI(true)  // No change: stays low.
		line.WriteNRZI(false) // Inverts: high.
		line.Write(false, false)
		line.Write(true, false)
	})

	assert.Equal(t, []int{0, 1, 0, 1}, levels)
}

// PackLevels puts the first level in the least significant bit, and pads a
// final partial byte with zeros.
func TestPackLevelsIsLeastSignificantBitFirst(t *testing.T) {
	assert.Equal(t, []byte{0xAB, 0x01}, PackLevels([]int{1, 1, 0, 1, 0, 1, 0, 1, 1}))
	assert.Empty(t, PackLevels(nil))
}
