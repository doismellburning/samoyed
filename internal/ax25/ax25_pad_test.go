// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package ax25

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ax25_unwrap_third_party(t *testing.T) {
	// Taken from the original comments for the function:
	// Example: Input:      A>B,C:}D>E,F:info
	// Output:     D>E,F:info
	// (Except because we're using FormatAddrs, the info part isn't shown)
	var pp = FromText("A>B,C:}D>E,F:info", true)
	var pp2 = pp.UnwrapThirdParty()
	var addrs = pp2.FormatAddrs()
	assert.Equal(t, "D>E,F:", addrs)
}

func Test_ax25_set_info(t *testing.T) {
	var p = FromText("D>E,F:info", true)
	var initialInfo = p.Info()
	assert.Equal(t, "info", string(initialInfo)) // Make sure I set this up right!

	var s = "badger"
	p.SetInfo([]byte(s))

	// Check info updated
	var newInfo = p.Info()
	assert.Equal(t, s, string(newInfo))

	// Make sure we didn't break stuff along the way
	assert.Equal(t, "D>E,F:", p.FormatAddrs())
}

func Test_ax25_parse_addr_strictness(t *testing.T) {
	// Lower case, a trailing "*" and an over-long address are each accepted
	// or rejected depending on the strictness asked for.  The decode_aprs
	// utility uses AddrStrictLowerCaseWarning so that a packet captured from
	// somewhere such as aprs.fi still gets explained rather than discarded.
	var testCases = []struct {
		addr       string
		strictness AddrStrictness
		ok         bool
	}{
		{"Q1TEST-1", AddrLenient, true},
		{"Q1TEST-1", AddrStrict, true},
		{"Q1TEST-1", AddrStrictNoStar, true},
		{"Q1TEST-1", AddrStrictLowerCaseWarning, true},

		// Lower case, as a q-construct or otherwise.
		{"qAR", AddrLenient, true},
		{"qAR", AddrStrict, false},
		{"qAR", AddrStrictNoStar, false},
		{"qAR", AddrStrictLowerCaseWarning, true},
		{"q1test", AddrLenient, true},
		{"q1test", AddrStrict, false},
		{"q1test", AddrStrictNoStar, false},
		{"q1test", AddrStrictLowerCaseWarning, true},

		// "Has been repeated" flag.
		{"Q1TEST-1*", AddrStrict, true},
		{"Q1TEST-1*", AddrStrictNoStar, false},
		{"Q1TEST-1*", AddrStrictLowerCaseWarning, true},

		// Longer than 6 characters is for an APRS-IS server only.
		{"Q1TESTLONG", AddrLenient, true},
		{"Q1TESTLONG", AddrStrict, false},
		{"Q1TESTLONG", AddrStrictNoStar, false},
		{"Q1TESTLONG", AddrStrictLowerCaseWarning, false},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s/%d", tc.addr, tc.strictness), func(t *testing.T) {
			var _, _, _, ok = ParseAddr(Source, tc.addr, tc.strictness)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func Test_ax25_frame_with_no_pid(t *testing.T) {
	// The shortest frame FromFrame accepts is two addresses plus a control
	// byte: no PID and no information part.  Everything that reads the
	// information part has to cope with an offset that lands past the end of
	// the frame rather than slicing off it.
	var frame = []byte("000000000000010")
	require.Len(t, frame, MinPacketLen)

	var alevel ALevel

	var pp = FromFrame(frame, alevel)
	require.NotNil(t, pp)
	assert.Equal(t, 2, pp.num_addr)

	assert.Empty(t, pp.Info())
	assert.Equal(t, 0, pp.NumInfo())
	assert.Equal(t, byte(' '), pp.DTI())
	assert.False(t, pp.IsAPRS())

	// The rest of the receive path's getters should survive it too.
	assert.NotPanics(t, func() {
		pp.FormatAddrs()
		pp.FormatViaPath()
		pp.FrameType()
		pp.DedupeCRC()
	})
}

func TestMustFromText(t *testing.T) {
	var pp = MustFromText("Q1TEST>APDW17,WIDE1-1:>Testing")

	assert.Equal(t, "Q1TEST>APDW17,WIDE1-1:", pp.FormatAddrs())
	assert.Equal(t, ">Testing", string(pp.Info()))

	assert.PanicsWithValue(t, "not an AX.25 packet: Q1TEST", func() { MustFromText("Q1TEST") })
}
