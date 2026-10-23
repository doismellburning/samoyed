// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package xid

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// A set of parameters nobody has filled in should offer nothing beyond the two
// groups Encode always sends.  While the optional fields were ints holding
// G_UNKNOWN for "not specified", the zero value of Param said instead
// that every one of them had been negotiated to zero, and a caller that built
// the struct without going through initiate_negotiation would transmit a
// zero-length I field, a zero-frame window, a zero acknowledge timer and zero
// retries as though they had been asked for.
func TestXIDEncodeZeroValueOmitsOptionalParameters(t *testing.T) {
	t.Parallel()

	var param Param

	var info = Encode(&param, ax25.CRCmd)

	// Format Indicator, Group Identifier, two group length bytes, then only
	// Classes of Procedures (4 bytes) and HDLC Optional Functions (5 bytes).
	assert.Len(t, info, 4+4+5)
	assert.Equal(t, byte(4+5), info[3], "group length should cover only the two mandatory groups")

	assert.NotContains(t, info[4:], byte(PI_I_Field_Length_Rx))
	assert.NotContains(t, info[4:], byte(PI_Window_Size_Rx))
	assert.NotContains(t, info[4:], byte(PI_Ack_Timer))
	assert.NotContains(t, info[4:], byte(PI_Retries))

	// And it round-trips back to "not specified" rather than to zeroes.
	var parsed, _, status = Parse(info)
	assert.True(t, status)
	assert.Equal(t, maybe.Nothing[int](), parsed.IFieldLengthRx)
	assert.Equal(t, maybe.Nothing[int](), parsed.WindowSizeRx)
	assert.Equal(t, maybe.Nothing[int](), parsed.AckTimer)
	assert.Equal(t, maybe.Nothing[int](), parsed.Retries)
}

// An XID's info field comes off the air, and Parse used to index into it
// wherever its header and group length said there would be something, so a
// short or truncated one - a single byte would do - took the program down.
// The monitor display parses every XID frame it hears, so this was in reach of
// anyone on frequency, connected or not.  What was whole before the info field
// ran out is kept, as for a parameter with a bad length.
func TestXIDParseTruncatedInfo(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var param, _, status = Parse(tc.info)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, maybe.Nothing[int](), param.AckTimer)
			assert.Equal(t, maybe.Nothing[int](), param.WindowSizeRx)
		})
	}

	// Parameters that fit are kept even when a later one does not.
	var info = []byte{FI_Format_Indicator, GI_Group_Identifier, 0, 9, PI_Window_Size_Rx, 1, 4, PI_Ack_Timer, 2, 0x0b}

	var param, _, status = Parse(info)
	assert.True(t, status)
	assert.Equal(t, maybe.Just(4), param.WindowSizeRx)
	assert.Equal(t, maybe.Nothing[int](), param.AckTimer)
}

// LinBPQ with L2Compress enabled ends its XID command with parameter 16, a
// flag asking for its own L2 compression, which has no value and so a length
// of 0.  Parse insisted every parameter be 1 to 4 bytes long, so it logged an
// error on every connection from such a node and gave up on whatever followed.
// A parameter we don't decode is skipped whatever its length; the ones we do
// decode are still held to 1 to 4 bytes.
func TestXIDParseSkipsUnknownParameters(t *testing.T) {
	t.Parallel()

	// As built by L2SENDXID in LinBPQ's L2Code.c.
	var bpq = []byte{
		FI_Format_Indicator, GI_Group_Identifier, 0x00, 0x12,
		PI_Classes_of_Procedures, 2, 0x00, 0x21,
		PI_HDLC_Optional_Functions, 3, 0x22, 0xa4, 0x86,
		PI_I_Field_Length_Rx, 2, 0x08, 0x00,
		PI_Window_Size_Rx, 1, 7,
		16, 0,
	}

	var param, desc, status = Parse(bpq)
	assert.True(t, status)
	assert.Equal(t, "REJ modulo-8 I-Field-Length-Rx=256 Window-Size-Rx=7 ", desc)
	assert.Equal(t, maybe.Just(256), param.IFieldLengthRx)
	assert.Equal(t, maybe.Just(7), param.WindowSizeRx)

	for _, tc := range []struct {
		name    string
		info    []byte
		retries maybe.Maybe[int]
	}{
		{
			"unknown, no value",
			[]byte{FI_Format_Indicator, GI_Group_Identifier, 0, 5, 16, 0, PI_Retries, 1, 6},
			maybe.Just(6),
		},
		{
			"unknown, longer than 4 bytes",
			[]byte{FI_Format_Indicator, GI_Group_Identifier, 0, 10, 99, 5, 1, 2, 3, 4, 5, PI_Retries, 1, 6},
			maybe.Just(6),
		},
		{
			"known, no value",
			[]byte{FI_Format_Indicator, GI_Group_Identifier, 0, 5, PI_Ack_Timer, 0, PI_Retries, 1, 6},
			maybe.Nothing[int](),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var param, _, status = Parse(tc.info)
			assert.True(t, status)
			assert.Equal(t, tc.retries, param.Retries)
		})
	}
}
