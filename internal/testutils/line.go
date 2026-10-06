// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/require"
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

// NRZIDecode recovers the data bits from line levels sent NRZI: a one leaves
// the line as it was, a zero inverts it.  The line starts low, as the one
// LineLevels records does.
func NRZIDecode(levels []int) []bool {
	var data = make([]bool, 0, len(levels))
	var previous = 0

	for _, level := range levels {
		data = append(data, level == previous)
		previous = level
	}

	return data
}

// LevelBits takes line levels as the bits they are, for what goes out without
// NRZI, such as EAS SAME.
func LevelBits(levels []int) []bool {
	var bits = make([]bool, 0, len(levels))

	for _, level := range levels {
		bits = append(bits, level != 0)
	}

	return bits
}

// PackLSBFirst reassembles bytes from bits sent least significant bit first,
// as HDLC, FX.25 and EAS SAME send them.  There must be a whole number of
// bytes.
func PackLSBFirst(tb testing.TB, bits []bool) []byte {
	tb.Helper()

	require.Zero(tb, len(bits)%8, "a whole number of bytes should have been sent")

	var out = make([]byte, 0, len(bits)/8)

	for i := 0; i < len(bits); i += 8 {
		var b byte

		for j := range 8 {
			if bits[i+j] {
				b |= 1 << j
			}
		}

		out = append(out, b)
	}

	return out
}
