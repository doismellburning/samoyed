// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
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

	var info = xid_encode(&param, ax25.CRCmd)

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
	assert.True(t, status)
	assert.Equal(t, maybe.Nothing[int](), parsed.IFieldLengthRx)
	assert.Equal(t, maybe.Nothing[int](), parsed.WindowSizeRx)
	assert.Equal(t, maybe.Nothing[int](), parsed.AckTimer)
	assert.Equal(t, maybe.Nothing[int](), parsed.Retries)
}

// An XID's info field comes off the air, and xid_parse used to index into it
// wherever its header and group length said there would be something, so a
// short or truncated one - a single byte would do - took the program down.
// The monitor display parses every XID frame it hears, so this was in reach of
// anyone on frequency, connected or not.  What was whole before the info field
// ran out is kept, as for a parameter with a bad length.
func TestXIDParseTruncatedInfo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		info   []byte
		status bool
	}{
		{"one byte, not a format indicator", []byte{0x00}, false},
		{"format indicator alone", []byte{FI_Format_Indicator}, false},
		{"no group length", []byte{FI_Format_Indicator, GI_Group_Identifier}, false},
		{"half a group length", []byte{FI_Format_Indicator, GI_Group_Identifier, 0}, false},
		{"group length claims more than there is", []byte{FI_Format_Indicator, GI_Group_Identifier, 0, 4}, true},
		{"parameter with no length", []byte{FI_Format_Indicator, GI_Group_Identifier, 0, 4, PI_Window_Size_Rx}, true},
		{"parameter value cut short", []byte{FI_Format_Indicator, GI_Group_Identifier, 0, 4, PI_Ack_Timer, 2, 0x0b}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var param, _, status = xid_parse(tc.info)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, maybe.Nothing[int](), param.AckTimer)
			assert.Equal(t, maybe.Nothing[int](), param.WindowSizeRx)
		})
	}

	// Parameters that fit are kept even when a later one does not.
	var info = []byte{FI_Format_Indicator, GI_Group_Identifier, 0, 9, PI_Window_Size_Rx, 1, 4, PI_Ack_Timer, 2, 0x0b}

	var param, _, status = xid_parse(info)
	assert.True(t, status)
	assert.Equal(t, maybe.Just(4), param.WindowSizeRx)
	assert.Equal(t, maybe.Nothing[int](), param.AckTimer)
}
