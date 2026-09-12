// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test that decode_aprs does not panic on an AX.25 UI frame with an empty
// information field, as sent by linbpq ID broadcasts (issue #504).
func Test_decode_aprs_empty_info(t *testing.T) {
	DeviceIDDataInstance = NewDeviceIDData()

	var pp = AX25FromText("Q1TEST>ID:", true)
	assert.NotNil(t, pp)

	// Must not panic, and must return a populated struct.
	var A = DecodeAPRS(pp, true, "")
	assert.NotNil(t, A)
	assert.Equal(t, "AX.25 UI frame with empty information field", A.g_data_type_desc)
	assert.Equal(t, "Q1TEST", A.g_src)
	assert.Equal(t, "ID", A.g_dest)
}
