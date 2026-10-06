// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"github.com/doismellburning/samoyed/internal/linecode"
)

// LineLevels runs fn with a new linecode.Encoder, which starts low, and
// returns every level written to it, one per bit, in order.  It is what a
// sender put on the line, for a test to check.
func LineLevels(fn func(line *linecode.Encoder)) []int {
	var levels []int

	fn(linecode.NewEncoder(func(level int) {
		levels = append(levels, level)
	}))

	return levels
}

// PackLevels packs line levels eight to a byte, least significant bit first,
// the order the receivers' fuzz targets unpack their input in, so that what a
// sender puts on the line can seed them.  A final partial byte is padded with
// zeros.
func PackLevels(levels []int) []byte {
	var out = make([]byte, (len(levels)+7)/8)

	for i, level := range levels {
		if level != 0 {
			out[i/8] |= 1 << (i % 8)
		}
	}

	return out
}
