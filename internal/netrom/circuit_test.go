// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

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

// fakeClock runs timers when a test says time has passed.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	clock *fakeClock
	at    time.Duration
	f     func()
	done  bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()

	var wasPending = !t.done
	t.done = true

	return wasPending
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer { //nolint:ireturn // implementing the interface
	c.mu.Lock()
	defer c.mu.Unlock()

	var t = &fakeTimer{clock: c, at: c.now + d, f: f, done: false}
	c.timers = append(c.timers, t)

	return t
}

// advance runs, in order, every timer due within d.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	var target = c.now + d

	for {
		var next *fakeTimer

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

// fakeOwner records what a circuit manager reports, and holds the
// frames it sends until the test delivers them.
type fakeOwner struct {
	mu sync.Mutex

	refuse  bool
	client  int
	ownCall string

	outbox      []*Frame
	established []circuitID
	incoming    []bool
	terminated  []circuitID
	timeouts    []bool
	data        [][]byte
}

func (o *fakeOwner) sendLayer3(f *Frame) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.outbox = append(o.outbox, f)
}

func (o *fakeOwner) acceptCircuit(string, string) (int, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.client, o.ownCall, !o.refuse
}

func (o *fakeOwner) circuitEstablished(c circuitID, incoming bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.established = append(o.established, c)
	o.incoming = append(o.incoming, incoming)
}

func (o *fakeOwner) circuitTerminated(c circuitID, timeout bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.terminated = append(o.terminated, c)
	o.timeouts = append(o.timeouts, timeout)
}

func (o *fakeOwner) circuitData(_ circuitID, data []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.data = append(o.data, slices.Clone(data))
}

// take empties the outbox.
func (o *fakeOwner) take() []*Frame {
	o.mu.Lock()
	defer o.mu.Unlock()

	var out = o.outbox
	o.outbox = nil

	return out
}

func (o *fakeOwner) received() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()

	return bytes.Join(o.data, nil)
}

// pair is two nodes' circuit managers, A (Q1TEST-7) and B (Q2TEST-7),
// with the frames between them passed by hand.
type pair struct {
	t      *testing.T
	clock  *fakeClock
	a, b   *circuitManager
	oa, ob *fakeOwner
}

func newPair(t *testing.T) *pair {
	t.Helper()

	var p = new(pair)
	p.t = t
	p.clock = new(fakeClock)

	p.oa = new(fakeOwner)
	p.oa.client = 0
	p.oa.ownCall = "Q1TEST-7"

	p.ob = new(fakeOwner)
	p.ob.client = 1
	p.ob.ownCall = "Q2TEST-7"

	p.a = newCircuitManager("Q1TEST-7", 16, p.oa, p.clock)
	p.b = newCircuitManager("Q2TEST-7", 16, p.ob, p.clock)

	return p
}

// wire re-encodes and decodes each frame, as the air would.
func (p *pair) wire(f *Frame) *Frame {
	p.t.Helper()

	var b, err = f.encode()
	require.NoError(p.t, err)

	var got, decodeErr = DecodeFrame(b)
	require.NoError(p.t, decodeErr)

	return got
}

// pump delivers frames each way until neither side has anything more to
// say.
func (p *pair) pump() {
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

// userCircuit() is the circuit A's client opens to B.
func userCircuit() circuitID {
	return circuitID{client: 0, ownCall: "Q1TEST", remoteCall: "Q2TEST-7"}
}

// connected is a pair with a circuit from A's client to B's.
func connectedPair(t *testing.T) *pair {
	t.Helper()

	var p = newPair(t)

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))
	p.pump()

	require.Len(t, p.oa.established, 1)
	require.Len(t, p.ob.established, 1)

	return p
}

func opcodes(frames []*Frame) []Opcode {
	var out []Opcode
	for _, f := range frames {
		out = append(out, f.opcode)
	}

	return out
}

func TestNetromCircuitConnects(t *testing.T) {
	t.Parallel()

	var p = newPair(t)

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))

	var sent = p.oa.take()
	require.Len(t, sent, 1)

	var req = sent[0]
	assert.Equal(t, OpConnReq, req.opcode)
	assert.Equal(t, "Q1TEST-7", req.origin)
	assert.Equal(t, "Q2TEST-7", req.destination)
	assert.Equal(t, "Q1TEST", req.user)
	assert.Equal(t, "Q1TEST-7", req.originNode)
	assert.Equal(t, byte(defaultWindow), req.window)
	assert.NotEqual(t, byte(0), req.index)
	assert.NotEqual(t, byte(0), req.id)

	p.b.rx(p.wire(req))

	var acks = p.ob.take()
	require.Len(t, acks, 1)

	var ack = acks[0]
	assert.Equal(t, OpConnAck, ack.opcode)
	assert.False(t, ack.has(FlagChoke))
	assert.Equal(t, req.index, ack.index, "the acknowledgement names the requester's circuit")
	assert.Equal(t, req.id, ack.id)
	assert.True(t, ack.ackTTL.IsJust(), "a request with the BPQ timeout gets the BPQ TTL back")

	p.a.rx(p.wire(ack))

	assert.Equal(t, []circuitID{userCircuit()}, p.oa.established)
	assert.Equal(t, []bool{false}, p.oa.incoming)
	assert.Equal(t, []circuitID{{client: 1, ownCall: "Q2TEST-7", remoteCall: "Q1TEST"}}, p.ob.established)
	assert.Equal(t, []bool{true}, p.ob.incoming)
}

func TestNetromCircuitRefused(t *testing.T) {
	t.Parallel()

	var p = newPair(t)
	p.ob.refuse = true

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))
	p.pump()

	assert.Empty(t, p.oa.established)
	assert.Equal(t, []circuitID{userCircuit()}, p.oa.terminated)
	assert.Equal(t, []bool{false}, p.oa.timeouts, "a refusal is not a timeout")
	assert.Empty(t, p.a.circuits)
	assert.Empty(t, p.b.circuits)
}

func TestNetromCircuitCarriesDataBothWays(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	require.NoError(t, p.a.send(userCircuit(), []byte("from A")))
	require.NoError(t, p.b.send(p.ob.established[0], []byte("from B")))
	p.pump()

	assert.Equal(t, []byte("from A"), p.ob.received())
	assert.Equal(t, []byte("from B"), p.oa.received())
}

// The review of the first implementation found acknowledgements carried on
// INFO frames ignored, so that with traffic both ways the window filled after
// four frames and everything stopped.
func TestNetromCircuitTakesAcknowledgementsFromInfo(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	for range 6 {
		require.NoError(t, p.a.send(userCircuit(), []byte("x")))
	}

	var sent = p.oa.take()
	require.Len(t, sent, defaultWindow, "a window's worth goes out")

	var c = p.a.byID(userCircuit())

	// B answers with data of its own, acknowledging all four.
	var reply = new(Frame)
	reply.origin = "Q2TEST-7"
	reply.destination = "Q1TEST-7"
	reply.ttl = 16
	reply.index = c.myIndex
	reply.id = c.myID
	reply.opcode = OpInfo
	reply.txSeq = 0
	reply.rxSeq = 4
	reply.info = []byte("y")

	p.a.rx(p.wire(reply))

	assert.Equal(t, byte(4), c.va, "the INFO acknowledged everything outstanding")

	var more = p.oa.take()
	assert.Equal(t, []Opcode{OpInfo, OpInfo}, opcodes(more), "so the rest goes out")
	assert.Equal(t, byte(1), more[0].rxSeq, "carrying the acknowledgement of B's frame")
}

// Data a client sends before the far end has answered waits for it, rather
// than being dropped.
func TestNetromCircuitQueuesDataBeforeConnected(t *testing.T) {
	t.Parallel()

	var p = newPair(t)

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))
	require.NoError(t, p.a.send(userCircuit(), []byte("early")))

	var sent = p.oa.take()
	assert.Equal(t, []Opcode{OpConnReq}, opcodes(sent), "nothing but the request yet")

	p.b.rx(p.wire(sent[0]))
	p.pump()

	assert.Equal(t, []byte("early"), p.ob.received())
}

func TestNetromCircuitSplitsAndReassembles(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var message = bytes.Repeat([]byte("0123456789"), 60)
	require.NoError(t, p.a.send(userCircuit(), message))

	var sent = p.oa.take()
	require.Len(t, sent, 3)
	assert.Len(t, sent[0].info, maxInfo)
	assert.True(t, sent[0].has(FlagMore))
	assert.True(t, sent[1].has(FlagMore))
	assert.False(t, sent[2].has(FlagMore))

	for _, f := range sent {
		p.b.rx(p.wire(f))
	}

	require.Len(t, p.ob.data, 1, "delivered as the one message it was sent as")
	assert.Equal(t, message, p.ob.data[0])
}

// A peer that never clears MORE cannot make us hold its data for ever.
func TestNetromCircuitBoundsReassembly(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var c = p.b.byID(p.ob.established[0])

	var chunk = bytes.Repeat([]byte{'z'}, maxInfo)

	for ns := range 12 {
		var f = p.b.frame(c, OpInfo)
		f.index, f.id = c.myIndex, c.myID
		f.txSeq = byte(ns)
		f.flags = FlagMore
		f.info = chunk
		c.vl = byte(ns) // Keep the window open for the test.

		p.b.rx(f)
	}

	require.NotEmpty(t, p.ob.data)

	for _, piece := range p.ob.data {
		assert.LessOrEqual(t, len(piece), maxReassembly)
	}

	assert.Less(t, len(c.message), maxReassembly)
}

func TestNetromCircuitResequences(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	for _, s := range []string{"one ", "two ", "three"} {
		require.NoError(t, p.a.send(userCircuit(), []byte(s)))
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
	t.Parallel()

	var p = connectedPair(t)

	require.NoError(t, p.a.send(userCircuit(), []byte("lost")))
	require.NoError(t, p.a.send(userCircuit(), []byte("arrived")))

	var sent = p.oa.take()
	require.Len(t, sent, 2)

	// The first is lost; the second arrives and B, holding it, asks again.
	p.b.rx(p.wire(sent[1]))
	p.clock.advance(defaultT2)

	var nak = p.ob.take()
	require.Len(t, nak, 1)
	assert.Equal(t, OpInfoAck, nak[0].opcode)
	assert.True(t, nak[0].has(FlagNAK))

	p.a.rx(p.wire(nak[0]))

	var resent = p.oa.take()
	require.Len(t, resent, 1)
	assert.Equal(t, byte(0), resent[0].txSeq)
	assert.Equal(t, []byte("lost"), resent[0].info)

	p.b.rx(p.wire(resent[0]))
	assert.Equal(t, []byte("lostarrived"), p.ob.received())
}

func TestNetromCircuitRespectsChoke(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var c = p.a.byID(userCircuit())

	var choke = p.a.frame(c, OpInfoAck)
	choke.origin, choke.destination = "Q2TEST-7", "Q1TEST-7"
	choke.index, choke.id = c.myIndex, c.myID
	choke.flags = FlagChoke
	p.a.rx(p.wire(choke))

	require.NoError(t, p.a.send(userCircuit(), []byte("wait")))
	assert.Empty(t, p.oa.take(), "nothing sent while choked")

	p.clock.advance(defaultT4)
	assert.Equal(t, []Opcode{OpInfo}, opcodes(p.oa.take()), "until T4 runs out")
}

func TestNetromCircuitDelaysAcknowledgement(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	require.NoError(t, p.a.send(userCircuit(), []byte("x")))
	p.b.rx(p.wire(p.oa.take()[0]))

	assert.Empty(t, p.ob.take(), "no acknowledgement straight away")

	p.clock.advance(defaultT2)

	var ack = p.ob.take()
	require.Len(t, ack, 1)
	assert.Equal(t, OpInfoAck, ack[0].opcode)
	assert.Equal(t, byte(1), ack[0].rxSeq)
}

// A full window is acknowledged at once, rather than leaving the sender
// stopped until T2.
func TestNetromCircuitAcknowledgesAFullWindowAtOnce(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	for range defaultWindow {
		require.NoError(t, p.a.send(userCircuit(), []byte("x")))
	}

	for _, f := range p.oa.take() {
		p.b.rx(p.wire(f))
	}

	var ack = p.ob.take()
	require.Len(t, ack, 1)
	assert.Equal(t, byte(defaultWindow), ack[0].rxSeq)
}

func TestNetromCircuitRetransmits(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	require.NoError(t, p.a.send(userCircuit(), []byte("again")))
	var first = p.oa.take()
	require.Len(t, first, 1)

	p.clock.advance(defaultT1)

	var again = p.oa.take()
	require.Len(t, again, 1)
	assert.Equal(t, first[0].txSeq, again[0].txSeq)
	assert.Equal(t, first[0].info, again[0].info)

	p.clock.advance(defaultN2 * defaultT1)
	assert.Equal(t, []circuitID{userCircuit()}, p.oa.terminated)
	assert.Equal(t, []bool{true}, p.oa.timeouts)
}

func TestNetromCircuitConnectTimesOut(t *testing.T) {
	t.Parallel()

	var p = newPair(t)

	var id = circuitID{client: 0, ownCall: "Q1TEST", remoteCall: "Q9TEST"}
	require.NoError(t, p.a.connect(id, "Q9TEST"))

	var req = p.oa.take()
	require.Len(t, req, 1)
	assert.Equal(t, "Q9TEST", req[0].destination, "to a node nobody answers for")

	p.clock.advance(defaultN2 * defaultT1)
	assert.Len(t, p.oa.take(), defaultN2, "the request is repeated")
	assert.Empty(t, p.oa.terminated)

	p.clock.advance(defaultT1)
	assert.Equal(t, []circuitID{id}, p.oa.terminated)
	assert.Equal(t, []bool{true}, p.oa.timeouts)
	assert.Empty(t, p.a.circuits)
}

func TestNetromCircuitDisconnects(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	require.NoError(t, p.a.disconnect(userCircuit()))
	assert.Equal(t, []Opcode{OpDiscReq}, opcodes(p.oa.outbox))

	p.pump()

	assert.Equal(t, []circuitID{userCircuit()}, p.oa.terminated)
	assert.Len(t, p.ob.terminated, 1)
	assert.Empty(t, p.a.circuits)
	assert.Empty(t, p.b.circuits)
}

// The review of the first implementation found a client's circuits left open
// when the client went away, to be handed to whichever client next got its
// number.
func TestNetromCircuitDropClient(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	p.a.dropClient(userCircuit().client)
	assert.Equal(t, []Opcode{OpDiscReq}, opcodes(p.oa.outbox))

	p.pump()

	assert.Empty(t, p.oa.terminated, "nobody left to tell")
	assert.Empty(t, p.a.circuits)
	assert.Len(t, p.ob.terminated, 1, "the far end is told")
}

// A CONNECT REQUEST repeated because our acknowledgement was lost gets the
// acknowledgement again, not a second circuit.
func TestNetromCircuitDuplicateRequest(t *testing.T) {
	t.Parallel()

	var p = newPair(t)

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))

	var req = p.oa.take()[0]
	p.b.rx(p.wire(req))
	p.ob.take()

	p.b.rx(p.wire(req))
	assert.Equal(t, []Opcode{OpConnAck}, opcodes(p.ob.take()))
	assert.Len(t, p.b.circuits, 1)
	assert.Len(t, p.ob.established, 1)
}

// A client knows its circuits by their callsigns, so it cannot have two by
// the same ones: asking again for one it has is refused, leaving the one it
// has alone.
func TestNetromCircuitRefusesADuplicateConnect(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	require.ErrorIs(t, p.a.connect(userCircuit(), "Q2TEST-7"), errCircuitExists)
	assert.Len(t, p.a.circuits, 1)
	assert.Empty(t, p.oa.take(), "nothing is sent")
	assert.Empty(t, p.oa.terminated)
}

// When the far end loses a circuit without our hearing of it and its user
// connects again, the new circuit has the same names as the old one.  The
// old one goes, so that the client's commands reach the circuit that is
// actually there.
func TestNetromCircuitReplacesAStaleOne(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var stale = p.b.byID(p.ob.established[0])

	// A forgets the circuit, as though it had restarted, and connects again.
	var restarted = newCircuitManager("Q1TEST-7", 16, p.oa, p.clock)
	restarted.nextCircuit = 0x0200 // Not the numbers it used before.
	p.a = restarted

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))
	p.pump()

	require.Len(t, p.ob.established, 2)
	assert.Equal(t, p.ob.established[0], p.ob.established[1], "known by the same names")
	assert.Equal(t, []circuitID{p.ob.established[0]}, p.ob.terminated, "the old one is closed")
	assert.Len(t, p.b.circuits, 1)
	assert.NotSame(t, stale, p.b.byID(p.ob.established[1]))

	require.NoError(t, p.b.send(p.ob.established[1], []byte("to the new one")))
	p.pump()
	assert.Equal(t, []byte("to the new one"), p.oa.received())
}

func TestNetromCircuitNegotiates(t *testing.T) {
	t.Parallel()

	var p = newPair(t)

	require.NoError(t, p.a.connect(userCircuit(), "Q2TEST-7"))

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
	t.Parallel()

	var m = newCircuitManager("Q1TEST-7", 16, new(fakeOwner), new(fakeClock))

	var seen = make(map[uint16]bool)

	for range maxCircuits {
		var c, err = m.newCircuit()
		require.NoError(t, err)
		assert.NotEqual(t, byte(0), c.myIndex)
		assert.NotEqual(t, byte(0), c.myID)

		var key = uint16(c.myIndex)<<8 | uint16(c.myID)
		assert.False(t, seen[key])
		seen[key] = true
	}

	var _, err = m.newCircuit()
	assert.ErrorIs(t, err, errTooManyCircuits)
}

// Frames for circuits we do not have, and stray acknowledgements, are
// ignored.
func TestNetromCircuitIgnoresStrays(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var stray = new(Frame)
	stray.origin, stray.destination, stray.ttl = "Q2TEST-7", "Q1TEST-7", 16
	stray.index, stray.id = 0x7F, 0x7F
	stray.opcode = OpInfo
	stray.info = []byte("?")

	assert.NotPanics(t, func() { p.a.rx(stray) })
	assert.Empty(t, p.oa.data)
	assert.Empty(t, p.oa.take())
}

func TestRealNetromClock(t *testing.T) {
	t.Parallel()

	var fired = make(chan struct{})

	RealClock{}.AfterFunc(time.Millisecond, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("the timer never fired")
	}

	var stopped = RealClock{}.AfterFunc(time.Hour, func() {})
	assert.True(t, stopped.Stop())
}

// Frames go out with the TTL the node is configured for.
func TestNetromCircuitUsesTheConfiguredTTL(t *testing.T) {
	t.Parallel()

	var owner = new(fakeOwner)
	var m = newCircuitManager("Q1TEST-7", 7, owner, new(fakeClock))

	require.NoError(t, m.connect(userCircuit(), "Q2TEST-7"))

	var sent = owner.take()
	require.Len(t, sent, 1)
	assert.Equal(t, byte(7), sent[0].ttl)
}

// A CONNECT REQUEST from another node, for a user whose name the client
// already knows a circuit by, is someone else - or a second session - and
// the client could not tell the two apart.  It is refused, and the circuit
// already there carries on.
func TestCircuitRefusesTheSameNamesFromAnotherNode(t *testing.T) {
	t.Parallel()

	var p = connectedPair(t)

	var req = new(Frame)
	req.origin = "Q3TEST-7"
	req.destination = "Q2TEST-7"
	req.ttl = 16
	req.index, req.id = 0x09, 0x09
	req.opcode = OpConnReq
	req.window = 4
	req.user = "Q1TEST"
	req.originNode = "Q3TEST-7"

	p.b.rx(p.wire(req))

	var replies = p.ob.take()
	require.Len(t, replies, 1)
	assert.Equal(t, OpConnAck, replies[0].opcode)
	assert.True(t, replies[0].has(FlagChoke), "refused")
	assert.Equal(t, "Q3TEST-7", replies[0].destination)

	assert.Empty(t, p.ob.terminated, "the circuit from A is untouched")
	assert.Len(t, p.b.circuits, 1)
}

// orderedOwner records the owner calls in the order they arrive, and can
// hold up the first sendLayer3 until told to go on.
type orderedOwner struct {
	fakeOwner

	mu      sync.Mutex
	events  []string
	holding chan struct{} // Closed once the first send is being held.
	release chan struct{}
	held    bool
}

func (o *orderedOwner) note(event string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.events = append(o.events, event)
}

func (o *orderedOwner) sendLayer3(f *Frame) {
	o.mu.Lock()
	var hold = !o.held
	o.held = true
	o.mu.Unlock()

	if hold {
		close(o.holding)
		<-o.release
	}

	o.note("send " + f.opcode.String())
}

func (o *orderedOwner) circuitTerminated(circuitID, bool) {
	o.note("terminated")
}

// Owner calls made on different goroutines - the receive path, a client's
// request, a timer - still reach the owner in the order the events happened:
// a client must not be told of a circuit's data, or of its frames going out,
// after being told it is gone.
func TestCircuitOwnerCallsKeepTheirOrderAcrossGoroutines(t *testing.T) {
	t.Parallel()

	var owner = new(orderedOwner)
	owner.holding = make(chan struct{})
	owner.release = make(chan struct{})

	var m = newCircuitManager("Q1TEST-7", 16, owner, new(fakeClock))

	var connected = make(chan error, 1)

	go func() { connected <- m.connect(userCircuit(), "Q2TEST-7") }()

	<-owner.holding // The CONNECT REQUEST is on its way out, held up in the owner.

	require.NoError(t, m.disconnect(userCircuit()))

	owner.mu.Lock()
	assert.Empty(t, owner.events, "the disconnect must wait its turn")
	owner.mu.Unlock()

	close(owner.release)
	require.NoError(t, <-connected)

	assert.Eventually(t, func() bool {
		owner.mu.Lock()
		defer owner.mu.Unlock()

		return len(owner.events) == 2
	}, 5*time.Second, time.Millisecond)

	owner.mu.Lock()
	assert.Equal(t, []string{"send CONN REQ", "terminated"}, owner.events)
	owner.mu.Unlock()
}
