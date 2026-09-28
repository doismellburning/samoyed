// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNetromClock runs timers when a test says time has passed.
type fakeNetromClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeNetromTimer
}

type fakeNetromTimer struct {
	clock *fakeNetromClock
	at    time.Duration
	f     func()
	done  bool
}

func (t *fakeNetromTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	var wasPending = !t.done
	t.done = true

	return wasPending
}

func (c *fakeNetromClock) AfterFunc(d time.Duration, f func()) netromTimer { //nolint:ireturn // implementing the interface
	c.mu.Lock()
	defer c.mu.Unlock()

	var t = &fakeNetromTimer{clock: c, at: c.now + d, f: f, done: false}
	c.timers = append(c.timers, t)

	return t
}

// advance runs, in order, every timer due within d.
func (c *fakeNetromClock) advance(d time.Duration) {
	c.mu.Lock()
	var target = c.now + d

	for {
		var next *fakeNetromTimer

		for _, t := range c.timers {
			if !t.done && t.at <= target && (next == nil || t.at < next.at) {
				next = t
			}
		}

		if next == nil {
			break
		}

		next.done = true
		c.now = next.at

		c.mu.Unlock()
		next.f()
		c.mu.Lock()
	}

	c.now = target
	c.mu.Unlock()
}

// fakeNetromOwner records what a circuit manager reports, and holds the
// frames it sends until the test delivers them.
type fakeNetromOwner struct {
	mu sync.Mutex

	refuse  bool
	client  int
	ownCall string

	outbox      []*netromFrame
	established []netromCircuitID
	incoming    []bool
	terminated  []netromCircuitID
	timeouts    []bool
	data        [][]byte
}

func (o *fakeNetromOwner) sendLayer3(f *netromFrame) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.outbox = append(o.outbox, f)
}

func (o *fakeNetromOwner) acceptCircuit(string, string) (int, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.client, o.ownCall, !o.refuse
}

func (o *fakeNetromOwner) circuitEstablished(c netromCircuitID, incoming bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.established = append(o.established, c)
	o.incoming = append(o.incoming, incoming)
}

func (o *fakeNetromOwner) circuitTerminated(c netromCircuitID, timeout bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.terminated = append(o.terminated, c)
	o.timeouts = append(o.timeouts, timeout)
}

func (o *fakeNetromOwner) circuitData(_ netromCircuitID, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.data = append(o.data, slices.Clone(data))
}

// take empties the outbox.
func (o *fakeNetromOwner) take() []*netromFrame {
	o.mu.Lock()
	defer o.mu.Unlock()

	var out = o.outbox
	o.outbox = nil

	return out
}

func (o *fakeNetromOwner) received() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()

	return bytes.Join(o.data, nil)
}

// netromPair is two nodes' circuit managers, A (Q1TEST-7) and B (Q2TEST-7),
// with the frames between them passed by hand.
type netromPair struct {
	t      *testing.T
	clock  *fakeNetromClock
	a, b   *netromCircuitManager
	oa, ob *fakeNetromOwner
}

func newNetromPair(t *testing.T) *netromPair {
	t.Helper()

	var p = new(netromPair)
	p.t = t
	p.clock = new(fakeNetromClock)

	p.oa = new(fakeNetromOwner)
	p.oa.client = 0
	p.oa.ownCall = "Q1TEST-7"

	p.ob = new(fakeNetromOwner)
	p.ob.client = 1
	p.ob.ownCall = "Q2TEST-7"

	p.a = newNetromCircuitManager("Q1TEST-7", 16, p.oa, p.clock)
	p.b = newNetromCircuitManager("Q2TEST-7", 16, p.ob, p.clock)

	return p
}

// wire re-encodes and decodes each frame, as the air would.
func (p *netromPair) wire(f *netromFrame) *netromFrame {
	p.t.Helper()

	var b, err = f.encode()
	require.NoError(p.t, err)

	var got, decodeErr = decodeNetromFrame(b)
	require.NoError(p.t, decodeErr)

	return got
}

// pump delivers frames each way until neither side has anything more to
// say.
func (p *netromPair) pump() {
	p.t.Helper()

	for range 100 {
		var fromA, fromB = p.oa.take(), p.ob.take()
		if len(fromA) == 0 && len(fromB) == 0 {
			return
		}

		for _, f := range fromA {
			p.b.rx(p.wire(f))
		}

		for _, f := range fromB {
			p.a.rx(p.wire(f))
		}
	}

	p.t.Fatal("the two ends never went quiet")
}

var userCircuit = netromCircuitID{client: 0, ownCall: "Q1TEST", remoteCall: "Q2TEST-7"} //nolint:gochecknoglobals

// connected is a pair with a circuit from A's client to B's.
func connectedNetromPair(t *testing.T) *netromPair {
	t.Helper()

	var p = newNetromPair(t)

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))
	p.pump()

	require.Len(t, p.oa.established, 1)
	require.Len(t, p.ob.established, 1)

	return p
}

func opcodes(frames []*netromFrame) []netromOpcode {
	var out []netromOpcode
	for _, f := range frames {
		out = append(out, f.opcode)
	}

	return out
}

func TestNetromCircuitConnects(t *testing.T) {
	var p = newNetromPair(t)

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))

	var sent = p.oa.take()
	require.Len(t, sent, 1)

	var req = sent[0]
	assert.Equal(t, netromOpConnReq, req.opcode)
	assert.Equal(t, "Q1TEST-7", req.origin)
	assert.Equal(t, "Q2TEST-7", req.destination)
	assert.Equal(t, "Q1TEST", req.user)
	assert.Equal(t, "Q1TEST-7", req.originNode)
	assert.Equal(t, byte(netromDefaultWindow), req.window)
	assert.NotEqual(t, byte(0), req.index)
	assert.NotEqual(t, byte(0), req.id)

	p.b.rx(p.wire(req))

	var acks = p.ob.take()
	require.Len(t, acks, 1)

	var ack = acks[0]
	assert.Equal(t, netromOpConnAck, ack.opcode)
	assert.False(t, ack.has(netromFlagChoke))
	assert.Equal(t, req.index, ack.index, "the acknowledgement names the requester's circuit")
	assert.Equal(t, req.id, ack.id)
	assert.True(t, ack.ackTTL.IsJust(), "a request with the BPQ timeout gets the BPQ TTL back")

	p.a.rx(p.wire(ack))

	assert.Equal(t, []netromCircuitID{userCircuit}, p.oa.established)
	assert.Equal(t, []bool{false}, p.oa.incoming)
	assert.Equal(t, []netromCircuitID{{client: 1, ownCall: "Q2TEST-7", remoteCall: "Q1TEST"}}, p.ob.established)
	assert.Equal(t, []bool{true}, p.ob.incoming)
}

func TestNetromCircuitRefused(t *testing.T) {
	var p = newNetromPair(t)
	p.ob.refuse = true

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))
	p.pump()

	assert.Empty(t, p.oa.established)
	assert.Equal(t, []netromCircuitID{userCircuit}, p.oa.terminated)
	assert.Equal(t, []bool{false}, p.oa.timeouts, "a refusal is not a timeout")
	assert.Empty(t, p.a.circuits)
	assert.Empty(t, p.b.circuits)
}

func TestNetromCircuitCarriesDataBothWays(t *testing.T) {
	var p = connectedNetromPair(t)

	require.NoError(t, p.a.send(userCircuit, []byte("from A")))
	require.NoError(t, p.b.send(p.ob.established[0], []byte("from B")))
	p.pump()

	assert.Equal(t, []byte("from A"), p.ob.received())
	assert.Equal(t, []byte("from B"), p.oa.received())
}

// The review of the first implementation found acknowledgements carried on
// INFO frames ignored, so that with traffic both ways the window filled after
// four frames and everything stopped.
func TestNetromCircuitTakesAcknowledgementsFromInfo(t *testing.T) {
	var p = connectedNetromPair(t)

	for range 6 {
		require.NoError(t, p.a.send(userCircuit, []byte("x")))
	}

	var sent = p.oa.take()
	require.Len(t, sent, netromDefaultWindow, "a window's worth goes out")

	var c = p.a.byID(userCircuit)

	// B answers with data of its own, acknowledging all four.
	var reply = new(netromFrame)
	reply.origin = "Q2TEST-7"
	reply.destination = "Q1TEST-7"
	reply.ttl = 16
	reply.index = c.myIndex
	reply.id = c.myID
	reply.opcode = netromOpInfo
	reply.txSeq = 0
	reply.rxSeq = 4
	reply.info = []byte("y")

	p.a.rx(p.wire(reply))

	assert.Equal(t, byte(4), c.va, "the INFO acknowledged everything outstanding")

	var more = p.oa.take()
	assert.Equal(t, []netromOpcode{netromOpInfo, netromOpInfo}, opcodes(more), "so the rest goes out")
	assert.Equal(t, byte(1), more[0].rxSeq, "carrying the acknowledgement of B's frame")
}

// Data a client sends before the far end has answered waits for it, rather
// than being dropped.
func TestNetromCircuitQueuesDataBeforeConnected(t *testing.T) {
	var p = newNetromPair(t)

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))
	require.NoError(t, p.a.send(userCircuit, []byte("early")))

	var sent = p.oa.take()
	assert.Equal(t, []netromOpcode{netromOpConnReq}, opcodes(sent), "nothing but the request yet")

	p.b.rx(p.wire(sent[0]))
	p.pump()

	assert.Equal(t, []byte("early"), p.ob.received())
}

func TestNetromCircuitSplitsAndReassembles(t *testing.T) {
	var p = connectedNetromPair(t)

	var message = bytes.Repeat([]byte("0123456789"), 60)
	require.NoError(t, p.a.send(userCircuit, message))

	var sent = p.oa.take()
	require.Len(t, sent, 3)
	assert.Len(t, sent[0].info, netromMaxInfo)
	assert.True(t, sent[0].has(netromFlagMore))
	assert.True(t, sent[1].has(netromFlagMore))
	assert.False(t, sent[2].has(netromFlagMore))

	for _, f := range sent {
		p.b.rx(p.wire(f))
	}

	require.Len(t, p.ob.data, 1, "delivered as the one message it was sent as")
	assert.Equal(t, message, p.ob.data[0])
}

// A peer that never clears MORE cannot make us hold its data for ever.
func TestNetromCircuitBoundsReassembly(t *testing.T) {
	var p = connectedNetromPair(t)

	var c = p.b.byID(p.ob.established[0])

	var chunk = bytes.Repeat([]byte{'z'}, netromMaxInfo)
	for ns := range 12 {
		var f = p.b.frame(c, netromOpInfo)
		f.index, f.id = c.myIndex, c.myID
		f.txSeq = byte(ns)
		f.flags = netromFlagMore
		f.info = chunk
		c.vl = byte(ns) // Keep the window open for the test.
		p.b.rx(f)
	}

	require.NotEmpty(t, p.ob.data)

	for _, piece := range p.ob.data {
		assert.LessOrEqual(t, len(piece), netromMaxReassembly)
	}

	assert.Less(t, len(c.message), netromMaxReassembly)
}

func TestNetromCircuitResequences(t *testing.T) {
	var p = connectedNetromPair(t)

	for _, s := range []string{"one ", "two ", "three"} {
		require.NoError(t, p.a.send(userCircuit, []byte(s)))
	}

	var sent = p.oa.take()
	require.Len(t, sent, 3)

	p.b.rx(p.wire(sent[1]))
	p.b.rx(p.wire(sent[2]))
	assert.Empty(t, p.ob.data, "nothing delivered past the gap")

	p.b.rx(p.wire(sent[0]))
	assert.Equal(t, []byte("one two three"), p.ob.received())
}

func TestNetromCircuitResendsOnNAK(t *testing.T) {
	var p = connectedNetromPair(t)

	require.NoError(t, p.a.send(userCircuit, []byte("lost")))
	require.NoError(t, p.a.send(userCircuit, []byte("arrived")))

	var sent = p.oa.take()
	require.Len(t, sent, 2)

	// The first is lost; the second arrives and B, holding it, asks again.
	p.b.rx(p.wire(sent[1]))
	p.clock.advance(netromDefaultT2)

	var nak = p.ob.take()
	require.Len(t, nak, 1)
	assert.Equal(t, netromOpInfoAck, nak[0].opcode)
	assert.True(t, nak[0].has(netromFlagNAK))

	p.a.rx(p.wire(nak[0]))

	var resent = p.oa.take()
	require.Len(t, resent, 1)
	assert.Equal(t, byte(0), resent[0].txSeq)
	assert.Equal(t, []byte("lost"), resent[0].info)

	p.b.rx(p.wire(resent[0]))
	assert.Equal(t, []byte("lostarrived"), p.ob.received())
}

func TestNetromCircuitRespectsChoke(t *testing.T) {
	var p = connectedNetromPair(t)

	var c = p.a.byID(userCircuit)

	var choke = p.a.frame(c, netromOpInfoAck)
	choke.origin, choke.destination = "Q2TEST-7", "Q1TEST-7"
	choke.index, choke.id = c.myIndex, c.myID
	choke.flags = netromFlagChoke
	p.a.rx(p.wire(choke))

	require.NoError(t, p.a.send(userCircuit, []byte("wait")))
	assert.Empty(t, p.oa.take(), "nothing sent while choked")

	p.clock.advance(netromDefaultT4)
	assert.Equal(t, []netromOpcode{netromOpInfo}, opcodes(p.oa.take()), "until T4 runs out")
}

func TestNetromCircuitDelaysAcknowledgement(t *testing.T) {
	var p = connectedNetromPair(t)

	require.NoError(t, p.a.send(userCircuit, []byte("x")))
	p.b.rx(p.wire(p.oa.take()[0]))

	assert.Empty(t, p.ob.take(), "no acknowledgement straight away")

	p.clock.advance(netromDefaultT2)

	var ack = p.ob.take()
	require.Len(t, ack, 1)
	assert.Equal(t, netromOpInfoAck, ack[0].opcode)
	assert.Equal(t, byte(1), ack[0].rxSeq)
}

// A full window is acknowledged at once, rather than leaving the sender
// stopped until T2.
func TestNetromCircuitAcknowledgesAFullWindowAtOnce(t *testing.T) {
	var p = connectedNetromPair(t)

	for range netromDefaultWindow {
		require.NoError(t, p.a.send(userCircuit, []byte("x")))
	}

	for _, f := range p.oa.take() {
		p.b.rx(p.wire(f))
	}

	var ack = p.ob.take()
	require.Len(t, ack, 1)
	assert.Equal(t, byte(netromDefaultWindow), ack[0].rxSeq)
}

func TestNetromCircuitRetransmits(t *testing.T) {
	var p = connectedNetromPair(t)

	require.NoError(t, p.a.send(userCircuit, []byte("again")))
	var first = p.oa.take()
	require.Len(t, first, 1)

	p.clock.advance(netromDefaultT1)

	var again = p.oa.take()
	require.Len(t, again, 1)
	assert.Equal(t, first[0].txSeq, again[0].txSeq)
	assert.Equal(t, first[0].info, again[0].info)

	p.clock.advance(netromDefaultN2 * netromDefaultT1)
	assert.Equal(t, []netromCircuitID{userCircuit}, p.oa.terminated)
	assert.Equal(t, []bool{true}, p.oa.timeouts)
}

func TestNetromCircuitConnectTimesOut(t *testing.T) {
	var p = newNetromPair(t)

	var id = netromCircuitID{client: 0, ownCall: "Q1TEST", remoteCall: "Q9TEST"}
	require.NoError(t, p.a.connect(id, "Q9TEST"))

	var req = p.oa.take()
	require.Len(t, req, 1)
	assert.Equal(t, "Q9TEST", req[0].destination, "to a node nobody answers for")

	p.clock.advance(netromDefaultN2 * netromDefaultT1)
	assert.Len(t, p.oa.take(), netromDefaultN2, "the request is repeated")
	assert.Empty(t, p.oa.terminated)

	p.clock.advance(netromDefaultT1)
	assert.Equal(t, []netromCircuitID{id}, p.oa.terminated)
	assert.Equal(t, []bool{true}, p.oa.timeouts)
	assert.Empty(t, p.a.circuits)
}

func TestNetromCircuitDisconnects(t *testing.T) {
	var p = connectedNetromPair(t)

	require.NoError(t, p.a.disconnect(userCircuit))
	assert.Equal(t, []netromOpcode{netromOpDiscReq}, opcodes(p.oa.outbox))

	p.pump()

	assert.Equal(t, []netromCircuitID{userCircuit}, p.oa.terminated)
	assert.Len(t, p.ob.terminated, 1)
	assert.Empty(t, p.a.circuits)
	assert.Empty(t, p.b.circuits)
}

// The review of the first implementation found a client's circuits left open
// when the client went away, to be handed to whichever client next got its
// number.
func TestNetromCircuitDropClient(t *testing.T) {
	var p = connectedNetromPair(t)

	p.a.dropClient(userCircuit.client)
	assert.Equal(t, []netromOpcode{netromOpDiscReq}, opcodes(p.oa.outbox))

	p.pump()

	assert.Empty(t, p.oa.terminated, "nobody left to tell")
	assert.Empty(t, p.a.circuits)
	assert.Len(t, p.ob.terminated, 1, "the far end is told")
}

// A CONNECT REQUEST repeated because our acknowledgement was lost gets the
// acknowledgement again, not a second circuit.
func TestNetromCircuitDuplicateRequest(t *testing.T) {
	var p = newNetromPair(t)

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))

	var req = p.oa.take()[0]
	p.b.rx(p.wire(req))
	p.ob.take()

	p.b.rx(p.wire(req))
	assert.Equal(t, []netromOpcode{netromOpConnAck}, opcodes(p.ob.take()))
	assert.Len(t, p.b.circuits, 1)
	assert.Len(t, p.ob.established, 1)
}

// A client knows its circuits by their callsigns, so it cannot have two by
// the same ones: asking again for one it has is refused, leaving the one it
// has alone.
func TestNetromCircuitRefusesADuplicateConnect(t *testing.T) {
	var p = connectedNetromPair(t)

	require.ErrorIs(t, p.a.connect(userCircuit, "Q2TEST-7"), errNetromCircuitExists)
	assert.Len(t, p.a.circuits, 1)
	assert.Empty(t, p.oa.take(), "nothing is sent")
	assert.Empty(t, p.oa.terminated)
}

// When the far end loses a circuit without our hearing of it and its user
// connects again, the new circuit has the same names as the old one.  The
// old one goes, so that the client's commands reach the circuit that is
// actually there.
func TestNetromCircuitReplacesAStaleOne(t *testing.T) {
	var p = connectedNetromPair(t)

	var stale = p.b.byID(p.ob.established[0])

	// A forgets the circuit, as though it had restarted, and connects again.
	var restarted = newNetromCircuitManager("Q1TEST-7", 16, p.oa, p.clock)
	restarted.nextCircuit = 0x0200 // Not the numbers it used before.
	p.a = restarted

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))
	p.pump()

	require.Len(t, p.ob.established, 2)
	assert.Equal(t, p.ob.established[0], p.ob.established[1], "known by the same names")
	assert.Equal(t, []netromCircuitID{p.ob.established[0]}, p.ob.terminated, "the old one is closed")
	assert.Len(t, p.b.circuits, 1)
	assert.NotSame(t, stale, p.b.byID(p.ob.established[1]))

	require.NoError(t, p.b.send(p.ob.established[1], []byte("to the new one")))
	p.pump()
	assert.Equal(t, []byte("to the new one"), p.oa.received())
}

func TestNetromCircuitNegotiates(t *testing.T) {
	var p = newNetromPair(t)

	require.NoError(t, p.a.connect(userCircuit, "Q2TEST-7"))

	var req = p.oa.take()[0]
	req.window = 2
	req.timeout = maybe.Just(30)
	p.b.rx(p.wire(req))

	var c = p.b.byID(p.ob.established[0])
	assert.Equal(t, byte(2), c.window, "the smaller window")
	assert.Equal(t, 30*time.Second, c.t1, "the shorter timeout")

	var ack = p.ob.take()[0]
	assert.Equal(t, byte(2), ack.window)
}

func TestNetromCircuitNumbering(t *testing.T) {
	var m = newNetromCircuitManager("Q1TEST-7", 16, new(fakeNetromOwner), new(fakeNetromClock))

	var seen = make(map[uint16]bool)

	for range netromMaxCircuits {
		var c, err = m.newCircuit()
		require.NoError(t, err)
		assert.NotEqual(t, byte(0), c.myIndex)
		assert.NotEqual(t, byte(0), c.myID)

		var key = uint16(c.myIndex)<<8 | uint16(c.myID)
		assert.False(t, seen[key])
		seen[key] = true
	}

	var _, err = m.newCircuit()
	assert.ErrorIs(t, err, errNetromTooManyCircuits)
}

// Frames for circuits we do not have, and stray acknowledgements, are
// ignored.
func TestNetromCircuitIgnoresStrays(t *testing.T) {
	var p = connectedNetromPair(t)

	var stray = new(netromFrame)
	stray.origin, stray.destination, stray.ttl = "Q2TEST-7", "Q1TEST-7", 16
	stray.index, stray.id = 0x7F, 0x7F
	stray.opcode = netromOpInfo
	stray.info = []byte("?")

	assert.NotPanics(t, func() { p.a.rx(stray) })
	assert.Empty(t, p.oa.data)
	assert.Empty(t, p.oa.take())
}

func TestRealNetromClock(t *testing.T) {
	var fired = make(chan struct{})

	realNetromClock{}.AfterFunc(time.Millisecond, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the timer never fired")
	}

	var stopped = realNetromClock{}.AfterFunc(time.Hour, func() {})
	assert.True(t, stopped.Stop())
}

// Frames go out with the TTL the node is configured for.
func TestNetromCircuitUsesTheConfiguredTTL(t *testing.T) {
	var owner = new(fakeNetromOwner)
	var m = newNetromCircuitManager("Q1TEST-7", 7, owner, new(fakeNetromClock))

	require.NoError(t, m.connect(userCircuit, "Q2TEST-7"))

	var sent = owner.take()
	require.Len(t, sent, 1)
	assert.Equal(t, byte(7), sent[0].ttl)
}
