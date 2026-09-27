// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIL2PTXFEC(t *testing.T) {
	var testData = []struct {
		version     Version
		max_fec     int
		fec_level   int
		use_max_fec int
	}{
		// v0.4 says what it is doing in the header bit.
		{Version0_4, 0, 0, 0},
		{Version0_4, 1, 1, 1},
		// v0.6 always uses 16 parity symbols and reserves the bit.
		{Version0_6, 0, 0, 1},
		{Version0_6, 1, 0, 1},
		// Compatibility transmits v0.4.
		{VersionCompat, 0, 0, 0},
		{VersionCompat, 1, 1, 1},
	}

	for _, testDatum := range testData {
		var fec_level, use_max_fec = il2p_tx_fec(testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.fec_level, fec_level, "FEC level for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
		assert.Equal(t, testDatum.use_max_fec, use_max_fec, "max FEC for v%s, max_fec %d", testDatum.version, testDatum.max_fec)
	}
}

func TestIL2PRXMaxFEC(t *testing.T) {
	// Only v0.4 reads the bit; the others know it is reserved.
	assert.Equal(t, 0, RxMaxFEC(Version0_4, 0))
	assert.Equal(t, 1, RxMaxFEC(Version0_4, 1))
	assert.Equal(t, 1, RxMaxFEC(Version0_6, 0))
	assert.Equal(t, 1, RxMaxFEC(Version0_6, 1))
	assert.Equal(t, 1, RxMaxFEC(VersionCompat, 0))
	assert.Equal(t, 1, RxMaxFEC(VersionCompat, 1))
}
