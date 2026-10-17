// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ax25

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_ax25_unwrap_third_party(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
			t.Parallel()

			var _, _, _, ok = ParseAddr(Source, tc.addr, tc.strictness)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func Test_ax25_frame_with_no_pid(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	var pp = MustFromText("Q1TEST>APDW17,WIDE1-1:>Testing")

	assert.Equal(t, "Q1TEST>APDW17,WIDE1-1:", pp.FormatAddrs())
	assert.Equal(t, ">Testing", string(pp.Info()))

	assert.PanicsWithValue(t, "not an AX.25 packet: Q1TEST", func() { MustFromText("Q1TEST") })
}

func Test_printableAddress(t *testing.T) {
	t.Parallel()

	var field = []byte{'Q' << 1, '1' << 1, 'T' << 1, 0x07 << 1, ' ' << 1, ' ' << 1}

	assert.Equal(t, "Q1T.  ", printableAddress(field))
}

// fullLengthFrame returns a received APRS frame from Q1TEST to APDW17 via the
// given digipeaters, padded out with information to MaxPacketLen.
func fullLengthFrame(t *testing.T, via string) *Packet {
	t.Helper()

	var frame = MustFromText("Q1TEST>APDW17," + via + ":>").FrameData()
	frame = append(frame, bytes.Repeat([]byte{'x'}, MaxPacketLen-len(frame))...)

	var pp = FromFrame(frame, ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, pp)

	return pp
}

func TestInsertAddrFullLengthFrame(t *testing.T) {
	t.Parallel()

	// The frame is already as long as a frame can be, so there is no room for
	// another address.  A digipeater inserting its own call for tracing used
	// to run off the end of the frame buffer.
	var pp = fullLengthFrame(t, "WIDE2-2")

	assert.False(t, pp.InsertAddr(Repeater1, "Q2TEST"))
	assert.Equal(t, MaxPacketLen, pp.FrameLen())
	assert.Equal(t, "Q1TEST>APDW17,WIDE2-2:", pp.FormatAddrs())
	assert.NotPanics(t, func() { pp.Pack() })

	var short = MustFromText("Q1TEST>APDW17,WIDE2-2:>")
	assert.True(t, short.InsertAddr(Repeater1, "Q2TEST"))
	assert.Equal(t, "Q1TEST>APDW17,Q2TEST,WIDE2-2:", short.FormatAddrs())
}

func TestParseAddrLenientAlphanumericSSID(t *testing.T) {
	t.Parallel()

	// APRS-IS has stations, D-STAR gateways for instance, whose "SSID" is a
	// letter.  Dire Wolf read it with atoi, which makes it 0, and kept the
	// packet; strict checking still rejects it.
	var testCases = []struct {
		addr string
		call string
		ssid int
	}{
		{"Q1TEST-B", "Q1TEST", 0},
		{"Q1TEST-1A", "Q1TEST", 1},
		{"Q1TEST-12", "Q1TEST", 12},
	}

	for _, tc := range testCases {
		t.Run(tc.addr, func(t *testing.T) {
			t.Parallel()

			var call, ssid, _, ok = ParseAddr(Source, tc.addr, AddrLenient)
			require.True(t, ok)
			assert.Equal(t, tc.call, call)
			assert.Equal(t, tc.ssid, ssid)

			_, _, _, ok = ParseAddr(Source, tc.addr, AddrStrict)
			assert.Equal(t, tc.addr == "Q1TEST-12", ok)
		})
	}

	var pp = FromText("Q1TEST-B>APDW17,TCPIP,Q2TEST-2*:>hello", false)
	require.NotNil(t, pp)
	assert.Equal(t, "Q1TEST>APDW17,TCPIP,Q2TEST-2*:", pp.FormatAddrs())
}

func TestParseAddrRejectsNonASCII(t *testing.T) {
	t.Parallel()

	// An address field holds 7-bit ASCII, so a letter or digit from beyond
	// it can't be represented: it used to be accepted and then mangled,
	// "QÄ1" going out as "QD 1".
	for _, addr := range []string{"QÄ1", "Q²1", "Q1Ⅻ", "Q1TEST-¹", "Q1TEST-٣"} {
		for _, strictness := range []AddrStrictness{AddrLenient, AddrStrict, AddrStrictNoStar, AddrStrictLowerCaseWarning} {
			var _, _, _, ok = ParseAddr(Source, addr, strictness)
			assert.False(t, ok, "%q accepted with strictness %d", addr, strictness)
		}

		assert.Nil(t, FromText(addr+">APDW17:>hello", true), addr)
	}
}

func TestSafeString(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		name      string
		info      []byte
		utf8      string
		asciiOnly string
	}{
		{"plain", []byte("hello"), "hello", "hello"},
		{"control", []byte("a\rb"), "a<0x0d>b", "a<0x0d>b"},
		{"trailing space", []byte("a "), "a<0x20>", "a<0x20>"},
		{"not UTF-8", []byte{0xfe, 'a', 0xff, 0x80}, "<0xfe>a<0xff><0x80>", "<0xfe>a<0xff><0x80>"},
		{"thorn", []byte("þ"), "þ", "<0xc3><0xbe>"},
		{"euro", []byte("€"), "€", "<0xe2><0x82><0xac>"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.utf8, safeString(tc.info, false))
			assert.Equal(t, tc.asciiOnly, safeString(tc.info, true))

			// FromText reads the <0xNN> notation back as the bytes it stands for.
			for _, s := range []string{tc.utf8, tc.asciiOnly} {
				var pp = FromText("Q1TEST>APDW17:"+s, true)
				require.NotNil(t, pp)
				assert.Equal(t, tc.info, pp.Info(), s)
			}
		})
	}
}

func TestNothingReadPastEndOfFrame(t *testing.T) {
	t.Parallel()

	var addrs = MustFromText("Q1TEST>APDW17,WIDE2-2:>").FrameData()[:21]

	// All address and no control byte.  This used to come out as an I frame,
	// from the zero byte just past the end.
	var noControl = FromFrame(addrs, ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, noControl)
	assert.Equal(t, 3, noControl.NumAddr())
	assert.Equal(t, -1, noControl.Control())
	assert.Equal(t, FrameNotAX25, noControl.FrameTypeOnly())
	assert.Equal(t, -1, noControl.PID())

	// A UI frame that stops before its PID.
	var noPID = FromFrame(append(slices.Clone(addrs), UIFrame), ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, noPID)
	assert.Equal(t, FrameTypeUUI, noPID.FrameTypeOnly())
	assert.Equal(t, -1, noPID.PID())
	assert.False(t, noPID.IsAPRS())

	// An I frame with no information part must not have its modulo guessed
	// from whatever is left in the buffer after it.  Here that is a 0xf0,
	// which is what the guess looks for.
	var iFrame = FromFrame(append(slices.Clone(addrs), 0x00, PIDNoLayer3, PIDNoLayer3), ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, iFrame)
	iFrame.SetInfo(nil)
	assert.Equal(t, FrameTypeI, iFrame.FrameTypeOnly())
	assert.Equal(t, ModuloUnknown, iFrame.Modulo())
}

func TestSetInfoMissingPID(t *testing.T) {
	t.Parallel()

	// A UI frame that stops before its PID.  The information part used to be
	// written a byte past the end, with the frame length not counting the
	// gap, so the frame lost its last byte and gained a stray one.
	var frame = append(slices.Clone(MustFromText("Q1TEST>APDW17:>").FrameData()[:14]), UIFrame)
	var pp = FromFrame(frame, ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, pp)

	pp.SetInfo([]byte("hello"))
	assert.Equal(t, "hello", string(pp.Info()))
	assert.Equal(t, PIDNoLayer3, pp.PID())
	assert.Equal(t, 14+1+1+5, pp.FrameLen())
}

func TestSetInfoNotAX25(t *testing.T) {
	t.Parallel()

	// No address ends, so this isn't AX.25 and the whole frame is its
	// information part, for setting as for getting.
	var pp = FromFrame(bytes.Repeat([]byte{'x' &^ 1}, 20), ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, pp)
	require.Equal(t, 0, pp.NumAddr())

	pp.SetInfo([]byte("xyz"))
	assert.Equal(t, "xyz", string(pp.FrameData()))
	assert.Equal(t, "xyz", string(pp.Info()))
}

func TestDupLeavesQueue(t *testing.T) {
	t.Parallel()

	// A copy is a new packet, in no queue: it must not still point at the
	// original's next one, or adding it to a queue would bring that along.
	var pp = MustFromText("Q1TEST>APDW17:>hello")
	pp.SetNext(MustFromText("Q2TEST>APDW17:>hello"))

	var dup = pp.Dup()
	assert.Nil(t, dup.Next())
	assert.NotNil(t, pp.Next())
	assert.Equal(t, pp.FrameData(), dup.FrameData())
}
