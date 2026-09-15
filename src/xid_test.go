// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// A set of parameters nobody has filled in should offer nothing beyond the two
// groups xid_encode always sends.  While the optional fields were ints holding
// G_UNKNOWN for "not specified", the zero value of xid_param_s said instead
// that every one of them had been negotiated to zero, and a caller that built
// the struct without going through initiate_negotiation would transmit a
// zero-length I field, a zero-frame window, a zero acknowledge timer and zero
// retries as though they had been asked for.
func TestXIDEncodeZeroValueOmitsOptionalParameters(t *testing.T) {
	var param xid_param_s

	var info = xid_encode(&param, cr_cmd)

	// Format Indicator, Group Identifier, two group length bytes, then only
	// Classes of Procedures (4 bytes) and HDLC Optional Functions (5 bytes).
	assert.Len(t, info, 4+4+5)
	assert.Equal(t, byte(4+5), info[3], "group length should cover only the two mandatory groups")

	assert.NotContains(t, info[4:], byte(PI_I_Field_Length_Rx))
	assert.NotContains(t, info[4:], byte(PI_Window_Size_Rx))
	assert.NotContains(t, info[4:], byte(PI_Ack_Timer))
	assert.NotContains(t, info[4:], byte(PI_Retries))

	// And it round-trips back to "not specified" rather than to zeroes.
	var parsed, _, status = xid_parse(info)
	assert.Equal(t, 1, status)
	assert.Equal(t, maybe.Nothing[int](), parsed.i_field_length_rx)
	assert.Equal(t, maybe.Nothing[int](), parsed.window_size_rx)
	assert.Equal(t, maybe.Nothing[int](), parsed.ack_timer)
	assert.Equal(t, maybe.Nothing[int](), parsed.retries)
}
