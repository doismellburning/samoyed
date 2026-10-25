// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"fmt"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rip is a routing information packet as figure 1 of the specification lays
// one out, options and their terminator included.
func rip(tb testing.TB, callsign string, hops byte, targetTime int, options ...[]byte) []byte {
	tb.Helper()

	var b, err = appendCall(nil, callsign)
	require.NoError(tb, err)

	b = append(b, hops, byte((targetTime>>8)&0xff), byte(targetTime&0xff))

	for _, o := range options {
		b = append(b, o...)
	}

	return append(b, 0)
}

// rifOption is an option as figure 2 of the specification lays one out: a
// length counting itself and the type, the type, then the value.
func rifOption(code byte, value ...byte) []byte {
	return append([]byte{byte((len(value) + rifOptionHeader) & 0xff), code}, value...)
}

func rif(rips ...[]byte) []byte {
	var b = []byte{rifSignature}
	for _, r := range rips {
		b = append(b, r...)
	}

	return b
}

// rttProbe is an RTT probe carrying text, padded with spaces as the
// specification has it.
func rttProbe(tb testing.TB, text string) []byte {
	tb.Helper()

	var f = testFrame(OpInfo)
	f.destination = RTTCallsign
	f.ttl = 2
	f.index, f.id = 0, 0
	f.info = fmt.Appendf(nil, "%-236s", text)

	var b, err = f.encode()
	require.NoError(tb, err)

	return b
}

func TestINP3Monitor(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		name string
		info []byte
		want string
	}{
		{
			"a route with an alias",
			rif(rip(t, "Q1TEST-7", 1, 25, rifOption(rifOptAlias, []byte("QNODEA")...))),
			"INP3 RIF: Q1TEST-7 (QNODEA) hops=1 tt=250ms",
		},
		{
			"a route unreachable and one with an IP address and an unknown option",
			rif(
				rip(t, "Q2TEST", 3, rifHorizon),
				rip(t, "Q3TEST-1", 2, 0x0100,
					rifOption(rifOptIPv4, 44, 1, 2, 3, 32),
					rifOption(0x12, 0x01, 0x02),
				),
			),
			"INP3 RIF: Q2TEST hops=3 unreachable, Q3TEST-1 hops=2 tt=2560ms ip=44.1.2.3/32 opt12=0102",
		},
		{
			"no routes",
			rif(),
			"INP3 RIF",
		},
		{
			"an RTT probe with flags",
			rttProbe(t, "1234 5678 $N $IX"),
			"INP3 RTT from Q1TEST-7 ttl=2: flags=$N $IX",
		},
		{
			"an RTT probe without",
			rttProbe(t, "1234 $ 5678"),
			"INP3 RTT from Q1TEST-7 ttl=2",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var text, ok = Describe(monitorPacket(t, "Q2TEST-7", false, ax25.PIDNetROM, tc.info))
			require.True(t, ok)
			assert.Equal(t, tc.want, text)
		})
	}
}

// A routing information frame that does not decode is left to be shown as
// it is, rather than described in part.
func TestINP3MonitorLeavesMalformedRIFAlone(t *testing.T) {
	t.Parallel()

	var r = rip(t, "Q1TEST", 1, 25, rifOption(rifOptAlias, []byte("QNODEA")...))

	var badCall = rif(r)
	badCall[1] = 0xff

	var testCases = []struct {
		name string
		info []byte
	}{
		{"a RIP cut short", rif(r[:ripLen-1])},
		{"options without their terminator", rif(r[:len(r)-1])},
		{"an option of length 1", rif(r[:ripLen], []byte{1})},
		{"an option of length 1 with a byte after it", rif(r[:ripLen], []byte{1, 0})},
		{"an option longer than the frame", rif(r[:ripLen], []byte{9, 0, 'Q'})},
		{"a destination that is no callsign", badCall},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var _, err = DecodeRIF(tc.info)
			require.ErrorIs(t, err, errRIF)

			var _, ok = Describe(monitorPacket(t, "Q2TEST-7", false, ax25.PIDNetROM, tc.info))
			assert.False(t, ok)
		})
	}
}

// A NODES broadcast opens with 0xFF too, and is still one.
func TestINP3MonitorKeepsNodes(t *testing.T) {
	t.Parallel()

	var frames, err = EncodeNodes("QNODEA", []NodesEntry{
		{callsign: "Q2TEST-7", alias: "QNODEB", neighbour: "Q2TEST-7", quality: 192},
	})
	require.NoError(t, err)

	var text, ok = Describe(monitorPacket(t, NodesCallsign, true, ax25.PIDNetROM, frames[0]))
	require.True(t, ok)
	assert.Equal(t, "NET/ROM NODES from QNODEA: Q2TEST-7 (QNODEB) via Q2TEST-7 q=192", text)
}
