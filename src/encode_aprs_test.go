// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// The callers of phg_data_extension and compressed_position only check that at
// least one of power, height and gain was specified, so the others arrive
// absent.  That used to be the G_UNKNOWN sentinel, which reached Sqrt/Log2, and
// the resulting NaN converted to a NUL byte in the middle of the transmitted
// packet.

func Test_phg_data_extension_partially_specified(t *testing.T) {
	var none = maybe.Nothing[int]()
	var some = maybe.Just[int]

	assert.Equal(t, "PHG7368", phg_data_extension(some(50), some(100), some(6), "N"), "all specified")

	assert.Equal(t, "PHG7000", phg_data_extension(some(50), none, none, ""), "power only")
	assert.Equal(t, "PHG0100", phg_data_extension(none, some(20), none, ""), "height only")
	assert.Equal(t, "PHG0060", phg_data_extension(none, none, some(6), ""), "gain only")
	assert.Equal(t, "PHG0008", phg_data_extension(none, none, none, "N"), "direction only")
}

func Test_EncodePosition_partially_specified_phg(t *testing.T) {
	var none = maybe.Nothing[int]()
	var some = maybe.Just[int]

	var result = EncodePosition(false, false, 42+34.61/60, -(71 + 26.47/60), 0, none, 'D', '&',
		some(50), none, none, "", none, some(0), 0, 0, 0, "")
	assert.Equal(t, "!4234.61ND07126.47W&PHG7000", result)

	// Compressed positions carry the same three values as a radio range.

	result = EncodePosition(false, true, 42+34.61/60, -(71 + 26.47/60), 0, none, 'D', '&',
		some(50), none, none, "", none, some(0), 0, 0, 0, "")
	assert.NotContains(t, result, "\x00", "compressed radio range")
	assert.Equal(t, EncodePosition(false, true, 42+34.61/60, -(71+26.47/60), 0, none, 'D', '&',
		some(50), some(0), some(0), "", none, some(0), 0, 0, 0, ""), result,
		"absent height/gain should encode as the unspecified defaults")
}
