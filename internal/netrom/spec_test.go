// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

// Test cases taken clause by clause from "The NET/ROM Protocol" (Software
// 2000), https://wiki.oarc.uk/_media/packet:thenetromprotocol.pdf, covering
// what the other tests here leave out.  Each quotes the clause it checks.
// Where Samoyed departs from the protocol on purpose, as
// docs/source/glossary/netrom.rst lists, the test checks what Samoyed does.

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "Each frame consists of a standard AX.25 link header, followed by a
// 15-byte network header and a 5-byte transport header."
func TestSpecHeaderLengths(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 15, networkHeaderLen)
	assert.Equal(t, 5, transportHeaderLen)

	var b, err = testFrame(OpDiscReq).encode()
	require.NoError(t, err)
	assert.Len(t, b, 20, "a DISCONNECT REQUEST is the headers and nothing more")
}

// "Because AX.25 frames are limited to 256 bytes and the combined network
// and transport header overhead totals 20 bytes, the maximum size of the
// information field is 236 bytes."
func TestSpecMaximumInformationField(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 236, maxInfo)
	assert.Equal(t, 256, MaxFrame)
}

// The bytes the layouts mark "(Unused)" carry nothing, so whatever a sender
// leaves in them changes nothing about the frame's meaning.
func TestSpecUnusedBytesAreIgnored(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		name   string
		opcode Opcode
		tail   []byte // From the circuit index on, with 0xAA, 0x55 in the unused bytes.
	}{
		{"CONNECT REQUEST", OpConnReq, concat([]byte{0x01, 0x02, 0xAA, 0x55, 0x01, 0x04}, q1testAddr(), q1test7Addr())},
		{"DISCONNECT REQUEST", OpDiscReq, []byte{0x01, 0x02, 0xAA, 0x55, 0x03}},
		{"DISCONNECT ACKNOWLEDGE", OpDiscAck, []byte{0x01, 0x02, 0xAA, 0x55, 0x04}},
		{"INFORMATION ACKNOWLEDGE", OpInfoAck, []byte{0x01, 0x02, 0xAA, 0x09, 0x06}}, // Only the TX sequence is unused.
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f, err = DecodeFrame(concat(networkHeader(), tc.tail))
			require.NoError(t, err)
			assert.Equal(t, tc.opcode, f.opcode)
			assert.Equal(t, byte(0x01), f.index)
			assert.Equal(t, byte(0x02), f.id)
			assert.Zero(t, f.flags)
		})
	}
}

// The opcode byte is "Choke Flag, NAK Flag, More-Follows Flag, Resrvd,
// Opcode": the opcode is the low nibble, and the reserved bit is no part
// of it.
func TestSpecOpcodeByteLayout(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		name   string
		b      byte
		opcode Opcode
		flags  byte
	}{
		{"choke is bit 7", 0x86, OpInfoAck, FlagChoke},
		{"NAK is bit 6", 0x46, OpInfoAck, FlagNAK},
		{"more-follows is bit 5", 0x25, OpInfo, FlagMore},
		{"the reserved bit leaves the opcode alone", 0x15, OpInfo, 0x10},
		{"all four", 0xF5, OpInfo, 0xF0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f, err = DecodeFrame(concat(networkHeader(), []byte{0x01, 0x02, 0x00, 0x00, tc.b}))
			require.NoError(t, err)
			assert.Equal(t, tc.opcode, f.opcode)
			assert.Equal(t, tc.flags, f.flags)
		})
	}
}

// "NET/ROM automatically fragments and reassembles any user supplied
// link-layer information frames that exceed 236 bytes", and "If the
// more-follows flag is set ... the information is a fragment of a long
// information frame".  Exactly 236 bytes is not fragmented; 237 is.
func TestSpecFragmentationBoundary(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		size  int
		sizes []int
	}{
		{1, []int{1}},
		{maxInfo, []int{maxInfo}},
		{maxInfo + 1, []int{maxInfo, 1}},
		{2 * maxInfo, []int{maxInfo, maxInfo}},
	}

	for _, tc := range testCases {
		t.Run(strconv.Itoa(tc.size), func(t *testing.T) {
			t.Parallel()

			var p = connectedPair(t)

			var message = bytes.Repeat([]byte{'x'}, tc.size)
			require.NoError(t, p.a.send(userCircuit(), message))

			var sent = p.oa.take()
			require.Len(t, sent, len(tc.sizes))

			for i, f := range sent {
				assert.Len(t, f.info, tc.sizes[i])
				assert.Equal(t, i < len(sent)-1, f.has(FlagMore), "more-follows on every fragment but the last")
			}

			for _, f := range sent {
				p.b.rx(p.wire(f))
			}

			require.Len(t, p.ob.data, 1)
			assert.Equal(t, message, p.ob.data[0])
		})
	}
}

// "The accepted window size indicates the negotiated size of this circuit,
// and will never exceed the proposed window size of the Connect Request."
func TestSpecAcceptedWindowNeverExceedsProposed(t *testing.T) {
	t.Parallel()

	for _, proposed := range []byte{1, 2, defaultWindow, defaultWindow + 1, 127} {
		t.Run(strconv.FormatUint(uint64(proposed), 10), func(t *testing.T) {
			t.Parallel()

			var p = newPair(t)

			require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))

			var req = p.oa.take()[0]
			req.window = proposed
			p.b.rx(p.wire(req))

			var ack = p.ob.take()
			require.Len(t, ack, 1)
			assert.Equal(t, OpConnAck, ack[0].opcode)
			assert.False(t, ack[0].has(FlagChoke))
			assert.LessOrEqual(t, ack[0].window, proposed)
			assert.Equal(t, min(proposed, defaultWindow), ack[0].window)
		})
	}
}

// "Message sequence numbers are 8-bit fields": a circuit carries on past
// 255 frames, the numbers wrapping round, without losing or reordering
// anything.
func TestSpecSequenceNumbersWrap(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var want []byte

	for i := range 300 {
		var data = []byte{byte(i & 0xFF)}
		want = append(want, data...)

		require.NoError(t, p.a.send(userCircuit(), data))
		p.pump()
		p.clock.advance(defaultT2)
		p.pump()
	}

	assert.Equal(t, want, p.ob.received())
}

// "If the choke flag is set (bit 7 of the opcode byte), it indicates that
// this node cannot accept any more Information messages until further
// notice."  That holds on an INFORMATION message, not only on an
// INFORMATION ACKNOWLEDGE.
func TestSpecChokeOnInformation(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var cb = p.b.byID(p.ob.established[0])

	var f = p.b.frame(cb, OpInfo)
	f.flags = FlagChoke
	f.info = []byte("from B, but send me nothing")
	p.a.rx(p.wire(f))
	p.oa.take()

	assert.Equal(t, []byte("from B, but send me nothing"), p.oa.received(), "the information itself is still taken")

	require.NoError(t, p.a.send(userCircuit(), []byte("wait")))
	assert.NotContains(t, opcodes(p.oa.take()), OpInfo, "nothing sent while choked")
}

// "Up to eleven destinations are packed into each UI-frame, and the node
// broadcasts as many such frames as required to send its entire routing
// table."
func TestSpecNodesElevenPerFrame(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		entries  int
		perFrame []int
	}{
		{0, []int{0}},
		{11, []int{11}},
		{12, []int{11, 1}},
		{22, []int{11, 11}},
		{33, []int{11, 11, 11}},
	}

	for _, tc := range testCases {
		t.Run(strconv.Itoa(tc.entries), func(t *testing.T) {
			t.Parallel()

			var entries = make([]NodesEntry, tc.entries)
			for i := range entries {
				entries[i] = entry(fmt.Sprintf("Q%dTEST", i%10), "QNODE", "Q2TEST-7", byte(i))
			}

			var frames, err = EncodeNodes("QNODEA", entries)
			require.NoError(t, err)
			require.Len(t, frames, len(tc.perFrame))

			for i, frame := range frames {
				assert.Len(t, frame, 1+6+21*tc.perFrame[i], "signature, alias, then 21 bytes an entry")
				assert.Equal(t, byte(0xFF), frame[0], "the signature")
			}
		})
	}
}

// Rule 2: "If the UI-frame does not have the proper PID or signature byte,
// the frame is ignored."  Only 0xFF is the signature.
func TestSpecNodesSignature(t *testing.T) {
	t.Parallel()

	for sig := range 0xFF {
		var _, err = DecodeNodes(concat([]byte{byte(sig)}, []byte("QNODEA")))
		assert.Error(t, err, "0x%02x is not the signature", sig)
	}
}

// Rule 5: "routequality = [(broadcastquality x pathquality) + 128] / 256",
// for every quality either side might have.
func TestSpecRouteQualityFormula(t *testing.T) {
	t.Parallel()

	for broadcast := range 256 {
		for path := range 256 {
			var want = (broadcast*path + 128) / 256
			require.Equal(t, byte(want), pathQuality(byte(path), byte(broadcast)), //nolint:gosec // G115: want is at most 254
				"broadcast %d, path %d", broadcast, path)
		}
	}
}

// Rule 4: "A direct route is assumed to exist to the node that originated
// the broadcast.  The quality of this route is taken from the path quality
// in the neighbor-list entry" - however good or bad the routes it
// advertises.
func TestSpecDirectRouteTakesThePathQuality(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 77, 0)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 255)))

	require.Len(t, r.destinations["Q2TEST-7"].routes, 1)
	assert.Equal(t, byte(77), r.destinations["Q2TEST-7"].routes[0].quality)
}

// Rule 1 says a minimum quality of zero ignores every broadcast; Samoyed
// takes it to accept every route instead, as the glossary says.  Quality
// zero routes are still not kept, since rule 6 says they are "never
// included in outgoing auto-routing broadcasts" and Samoyed drops trivial
// loops.
func TestSpecMinimumQualityZero(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 0)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB",
		entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 1), // (192 + 128) / 256 = 1.
		entry("Q4TEST-7", "QNODED", "Q4TEST-7", 0),
	))

	var _, ok = r.nextHop("Q3TEST-7")
	assert.True(t, ok, "a route of quality 1 is kept")

	_, ok = r.nextHop("Q4TEST-7")
	assert.False(t, ok, "one of quality 0 is not")
}

// Rule 6: "Quality-zero routes are never included in outgoing auto-routing
// broadcasts."  Samoyed's only quality-zero routes are those learned from
// traffic.
func TestSpecQualityZeroRoutesAreNotBroadcast(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 10)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB"))
	r.heardFrom("Q9TEST", nbr("Q2TEST-7"))

	var hop, ok = r.nextHop("Q9TEST")
	require.True(t, ok, "usable")
	assert.Equal(t, nbr("Q2TEST-7"), hop)

	for _, e := range r.broadcastEntries() {
		assert.NotEqual(t, "Q9TEST", e.callsign, "but not advertised")
		assert.NotZero(t, e.quality)
	}
}

// Rule 9: "If the number of entries in the destination list is greater than
// or equal to the max destinations parameter, then no additional
// destinations will be added" - but those already there are still updated.
func TestSpecFullTableStillUpdates(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	for i := len(r.destinations); i < maxDestinations; i++ {
		r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry(fmt.Sprintf("Q%05d", i), "", "Q9TEST", 200)))
	}

	require.Len(t, r.destinations, maxDestinations)

	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q8TEST", "", "Q8TEST", 200)))
	assert.NotContains(t, r.destinations, "Q8TEST", "no new destination")

	r.heardNodes(nbr("Q3TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 250)))
	assert.Equal(t, []string{"Q3TEST", "Q2TEST"}, neighboursOf(r, "Q9TEST"), "but a known one gains a route")

	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 50)))
	assert.Equal(t, byte(50), r.destinations["Q9TEST"].routes[1].quality, "and a route is updated")
}

// "Each route in the routing table has an obsolescence count which is
// initialized to the obsolescence count initializer parameter whenever the
// route is added or updated as the result of an auto-routing broadcast.
// The count is also reinitialized to this value whenever the route is
// actually used successfully.  The default initializer value is 6."
func TestSpecObsolescence(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 6, obsolescenceInit)

	t.Run("a route lasts six scans without a refresh", func(t *testing.T) {
		t.Parallel()

		var r = newRouter("Q1TEST-7", 192, 10)

		r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB"))

		for range obsolescenceInit - 1 {
			r.age()
		}

		var _, ok = r.nextHop("Q2TEST-7")
		require.True(t, ok, "still there after five")

		r.age()

		_, ok = r.nextHop("Q2TEST-7")
		assert.False(t, ok, "\"decremented to zero, the route is deleted\"")
		assert.NotContains(t, r.destinations, "Q2TEST-7")
	})

	t.Run("traffic over a route renews it", func(t *testing.T) {
		t.Parallel()

		var r = newRouter("Q1TEST-7", 192, 10)

		r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200)))

		for range obsolescenceInit - 1 {
			r.age()
		}

		r.heardFrom("Q3TEST-7", nbr("Q2TEST-7"))

		var route = r.destinations["Q3TEST-7"].routes[0]
		assert.Equal(t, obsolescenceInit, route.obsolescence)
		assert.Equal(t, byte(150), route.quality, "at the quality it had, not the 0 of a route learned from traffic")
	})
}

// The routing flow diagram asks "Is destination node equal to this node?"
// before it looks at the time to live, so a frame for us is taken however
// little time it has left.
func TestSpecFrameForUsIgnoresTimeToLive(t *testing.T) {
	t.Parallel()

	for _, ttl := range []byte{0, 1} {
		t.Run(strconv.FormatUint(uint64(ttl), 10), func(t *testing.T) {
			t.Parallel()

			var a, _ = joinedNodes(t)
			require.True(t, a.Register(0, 1, "Q1TEST-7"))

			var req = new(Frame)
			req.origin = "Q2TEST-7"
			req.destination = "Q1TEST-7"
			req.ttl = ttl
			req.index, req.id = 1, 1
			req.opcode = OpConnReq
			req.window = 4
			req.user = "Q3TEST"
			req.originNode = "Q2TEST-7"

			var raw, err = req.encode()
			require.NoError(t, err)

			a.RecConnData(0, testClient, "Q2TEST-7", "Q1TEST-7", ax25.PIDNetROM, raw)

			assert.Equal(t, []string{"Q3TEST"}, a.agw.established)
		})
	}
}

// The network header holds the "Callsign of Origin Node (AX.25 shifted
// format)" then the "Callsign of Destination Node (AX.25 shifted format
// w/EOA bit)": the end-of-address bit is set on the destination's SSID byte
// alone, as the Linux kernel sets it.  The callsigns a CONNECT REQUEST
// carries have no such mark.
func TestSpecEndOfAddressBit(t *testing.T) {
	t.Parallel()

	var req = testFrame(OpConnReq)
	req.window = 4
	req.user = "Q1TEST"
	req.originNode = "Q1TEST-7"

	var b, err = req.encode()
	require.NoError(t, err)

	assert.Equal(t, byte(0x6E), b[6], "origin: SSID 7, no end-of-address bit")
	assert.Equal(t, byte(0x6F), b[13], "destination: SSID 7 and the end-of-address bit")
	assert.Equal(t, byte(0x60), b[headerLen+1+6], "user: no end-of-address bit")
	assert.Equal(t, byte(0x6E), b[headerLen+1+13], "originating node: no end-of-address bit")
}
