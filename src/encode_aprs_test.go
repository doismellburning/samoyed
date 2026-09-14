// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The callers of phg_data_extension and compressed_position only check that at
// least one of power, height and gain was specified, so the others arrive as
// G_UNKNOWN.  That used to reach Sqrt/Log2, and the resulting NaN converted to
// a NUL byte in the middle of the transmitted packet.

func Test_phg_data_extension_partially_specified(t *testing.T) {
	assert.Equal(t, "PHG7368", phg_data_extension(50, 100, 6, "N"), "all specified")

	assert.Equal(t, "PHG7000", phg_data_extension(50, G_UNKNOWN, G_UNKNOWN, ""), "power only")
	assert.Equal(t, "PHG0100", phg_data_extension(G_UNKNOWN, 20, G_UNKNOWN, ""), "height only")
	assert.Equal(t, "PHG0060", phg_data_extension(G_UNKNOWN, G_UNKNOWN, 6, ""), "gain only")
	assert.Equal(t, "PHG0008", phg_data_extension(G_UNKNOWN, G_UNKNOWN, G_UNKNOWN, "N"), "direction only")
}

func Test_EncodePosition_partially_specified_phg(t *testing.T) {
	var result = EncodePosition(false, false, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		50, G_UNKNOWN, G_UNKNOWN, "", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&PHG7000", result)

	// Compressed positions carry the same three values as a radio range.

	result = EncodePosition(false, true, 42+34.61/60, -(71 + 26.47/60), 0, G_UNKNOWN, 'D', '&',
		50, G_UNKNOWN, G_UNKNOWN, "", G_UNKNOWN, 0, 0, 0, 0, "")
	assert.NotContains(t, result, "\x00", "compressed radio range")
	assert.Equal(t, EncodePosition(false, true, 42+34.61/60, -(71+26.47/60), 0, G_UNKNOWN, 'D', '&',
		50, 0, 0, "", G_UNKNOWN, 0, 0, 0, 0, ""), result,
		"G_UNKNOWN height/gain should encode as the unspecified defaults")
}
