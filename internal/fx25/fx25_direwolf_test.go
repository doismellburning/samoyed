// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fx25

import (
	"math/bits"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Dire Wolf's fx25_init checked its tables, and the popcount they rely on,
// each time it ran.  The tables are constant, so checking them once here is
// enough.

func TestFX25TablesAreConsistent(t *testing.T) {
	for j := range 16 {
		for k := range 16 {
			var distance = bits.OnesCount64(tags[j].value ^ tags[k].value)
			if j == k {
				assert.Equal(t, 0, distance, "tag %d against itself", j)
			} else {
				assert.Equal(t, 32, distance, "tag %d against tag %d", j, k)
			}
		}
	}

	for j := CTagMin; j <= CTagMax; j++ {
		assert.Equal(t, int(fx25Tab[tags[j].itab].nroots), tags[j].n_block_radio-tags[j].k_data_radio, "tag %d", j)
		assert.Equal(t, int(fx25Tab[tags[j].itab].nroots), tags[j].n_block_rs-tags[j].k_data_rs, "tag %d", j)
		assert.Equal(t, BlockSize, tags[j].n_block_rs, "tag %d", j)
	}

	for i := range nTab {
		assert.NotNil(t, fx25Tab[i].rs, "codec %d", i)
	}
}

func TestFX25PickMode(t *testing.T) {
	var cases = []struct {
		fxMode, dlen, want int
	}{
		{100 + 1, 239, 1},
		{100 + 1, 240, -1},

		{100 + 5, 223, 5},
		{100 + 5, 224, -1},

		{100 + 9, 191, 9},
		{100 + 9, 192, -1},

		{16, 32, 4},
		{16, 64, 3},
		{16, 128, 2},
		{16, 239, 1},
		{16, 240, -1},

		{32, 32, 8},
		{32, 64, 7},
		{32, 128, 6},
		{32, 223, 5},
		{32, 234, -1},

		{64, 64, 11},
		{64, 128, 10},
		{64, 191, 9},
		{64, 192, -1},

		{1, 32, 4},
		{1, 33, 3},
		{1, 64, 3},
		{1, 65, 6},
		{1, 128, 6},
		{1, 191, 9},
		{1, 223, 5},
		{1, 239, 1},
		{1, 240, -1},
	}

	for _, c := range cases {
		assert.Equal(t, c.want, pickMode(c.fxMode, c.dlen), "fx_mode %d, %d data bytes", c.fxMode, c.dlen)
	}
}
