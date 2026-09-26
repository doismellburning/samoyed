package direwolf

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
	// (Except because we're using AX25FormatAddrs, the info part isn't shown)
	var pp = AX25FromText("A>B,C:}D>E,F:info", true)
	var pp2 = ax25_unwrap_third_party(pp)
	var addrs = AX25FormatAddrs(pp2)
	assert.Equal(t, "D>E,F:", addrs)
}

func Test_ax25_set_info(t *testing.T) {
	var p = AX25FromText("D>E,F:info", true)
	var initialInfo = AX25GetInfo(p)
	assert.Equal(t, "info", string(initialInfo)) // Make sure I set this up right!

	var s = "badger"
	ax25_set_info(p, []byte(s))

	// Check info updated
	var newInfo = AX25GetInfo(p)
	assert.Equal(t, s, string(newInfo))

	// Make sure we didn't break stuff along the way
	assert.Equal(t, "D>E,F:", AX25FormatAddrs(p))
}

func Test_ax25_parse_addr_strictness(t *testing.T) {
	// Lower case, a trailing "*" and an over-long address are each accepted
	// or rejected depending on the strictness asked for.  The decode_aprs
	// utility uses addrStrictLowerCaseWarning so that a packet captured from
	// somewhere such as aprs.fi still gets explained rather than discarded.
	var testCases = []struct {
		addr       string
		strictness addrStrictness
		ok         bool
	}{
		{"Q1TEST-1", addrLenient, true},
		{"Q1TEST-1", addrStrict, true},
		{"Q1TEST-1", addrStrictNoStar, true},
		{"Q1TEST-1", addrStrictLowerCaseWarning, true},

		// Lower case, as a q-construct or otherwise.
		{"qAR", addrLenient, true},
		{"qAR", addrStrict, false},
		{"qAR", addrStrictNoStar, false},
		{"qAR", addrStrictLowerCaseWarning, true},
		{"q1test", addrLenient, true},
		{"q1test", addrStrict, false},
		{"q1test", addrStrictNoStar, false},
		{"q1test", addrStrictLowerCaseWarning, true},

		// "Has been repeated" flag.
		{"Q1TEST-1*", addrStrict, true},
		{"Q1TEST-1*", addrStrictNoStar, false},
		{"Q1TEST-1*", addrStrictLowerCaseWarning, true},

		// Longer than 6 characters is for an APRS-IS server only.
		{"Q1TESTLONG", addrLenient, true},
		{"Q1TESTLONG", addrStrict, false},
		{"Q1TESTLONG", addrStrictNoStar, false},
		{"Q1TESTLONG", addrStrictLowerCaseWarning, false},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s/%d", tc.addr, tc.strictness), func(t *testing.T) {
			var _, _, _, ok = ax25_parse_addr(AX25_SOURCE, tc.addr, tc.strictness)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func Test_ax25_frame_with_no_pid(t *testing.T) {
	// The shortest frame AX25FromFrame accepts is two addresses plus a control
	// byte: no PID and no information part.  Everything that reads the
	// information part has to cope with an offset that lands past the end of
	// the frame rather than slicing off it.
	var frame = []byte("000000000000010")
	require.Len(t, frame, AX25_MIN_PACKET_LEN)

	var alevel ALevel

	var pp = AX25FromFrame(frame, alevel)
	require.NotNil(t, pp)
	assert.Equal(t, 2, pp.num_addr)

	assert.Empty(t, AX25GetInfo(pp))
	assert.Equal(t, 0, ax25_get_num_info(pp))
	assert.Equal(t, byte(' '), ax25_get_dti(pp))
	assert.False(t, ax25_is_aprs(pp))

	// The rest of the receive path's getters should survive it too.
	assert.NotPanics(t, func() {
		AX25FormatAddrs(pp)
		ax25_format_via_path(pp)
		ax25_frame_type(pp)
		ax25_dedupe_crc(pp)
	})
}

func TestMustAX25FromText(t *testing.T) {
	var pp = MustAX25FromText("Q1TEST>APDW17,WIDE1-1:>Testing")

	assert.Equal(t, "Q1TEST>APDW17,WIDE1-1:", AX25FormatAddrs(pp))
	assert.Equal(t, ">Testing", string(AX25GetInfo(pp)))

	assert.PanicsWithValue(t, "not an AX.25 packet: Q1TEST", func() { MustAX25FromText("Q1TEST") })
}
