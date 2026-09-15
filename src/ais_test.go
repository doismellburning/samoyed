// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Latitude and longitude are two's complement, so the southern and western
// hemispheres depend on the field being sign extended from its own width.
func Test_get_field_signed(t *testing.T) {
	var testCases = []struct {
		length   uint
		raw      int
		expected int
	}{
		{6, 0b011111, 31},
		{6, 0b100000, -32},
		{6, 0b111111, -1},
		{27, 91 * 600000, 91 * 600000},
		{27, (1 << 27) - 1, -1},
		{28, int(-71.06*600000) & ((1 << 28) - 1), int(-71.06 * 600000)},
		{31, (1 << 30) - 1, (1 << 30) - 1},
		{31, 1 << 30, -(1 << 30)},
	}

	for _, tc := range testCases {
		var base = make([]byte, 8)
		set_field(base, 1, tc.length, tc.raw) // Offset 1 to catch byte-aligned assumptions.
		assert.Equal(t, tc.expected, get_field_signed(base, 1, tc.length), "%d bits of %b", tc.length, tc.raw)
	}
}
