// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// NET/ROM transport circuits.
//
// A circuit is a connection between two NET/ROM nodes, end to end across
// however many hops lie between them, carrying one user's data in order and
// without loss.  Each end names it by an index and an ID of its own choosing;
// every frame carries the recipient's pair, which is how it finds its circuit.
//
// The state machine, its timers and its windowing follow the Linux NET/ROM
// transport (net/netrom/nr_in.c, nr_out.c, nr_timer.c), which is what the
// other implementations on the air interoperate with:
//
//   - Connecting (Linux state 1): a CONNECT REQUEST has gone out, and is
//     repeated every T1 until a CONNECT ACKNOWLEDGE comes back, or N2
//     repeats have gone unanswered.
//   - Connected (state 3): INFO frames flow both ways within a window of
//     unacknowledged frames, each carrying the sender's receive sequence
//     number as an acknowledgement, with INFO ACKNOWLEDGE for when there is
//     nothing to send; out-of-order frames are held for resequencing; NAK
//     asks for a resend and CHOKE for a pause; MORE marks a message split
//     over several frames.
//   - Disconnecting (state 2): a DISCONNECT REQUEST has gone out, and is
//     repeated every T1 until acknowledged or N2 repeats have gone
//     unanswered.

import (
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	netromDefaultWindow = 4
	netromDefaultT1     = 120 * time.Second // Retransmission.
	netromDefaultT2     = 5 * time.Second   // Acknowledgement delay.
	netromDefaultT4     = 180 * time.Second // How long to respect a CHOKE.
	netromDefaultN2     = 3                 // Retries.

	// netromMaxInfo is the most data one INFO frame carries: what is left of
	// a 256-byte AX.25 information field after the NET/ROM headers, as
	// Linux has it.
	netromMaxInfo = 236

	// A reassembled message is delivered in pieces no bigger than an AGW
	// client can be handed at once, so a peer that never clears MORE cannot
	// grow the buffer without limit.
	netromMaxReassembly = ax25.MaxInfoLen

	// Every circuit costs memory and timers, and a CONNECT REQUEST is cheap
	// to send, so there is a limit.
	netromMaxCircuits = 256
)

var (
	errNetromTooManyCircuits = errors.New("too many NET/ROM circuits")
	errNetromNoCircuit       = errors.New("no such NET/ROM circuit")
	errNetromCircuitExists   = errors.New("the client already has that NET/ROM circuit")
)

type netromCircuitState int

const (
	netromClosed netromCircuitState = iota
	netromConnecting
	netromDisconnecting
	netromConnected
)

// netromTimer is a pending timer.
type netromTimer interface {
	Stop() bool
}

// netromClock starts timers, so that tests can drive time themselves.
type netromClock interface {
	AfterFunc(d time.Duration, f func()) netromTimer
}

type realNetromClock struct{}

func (realNetromClock) AfterFunc(d time.Duration, f func()) netromTimer { //nolint:ireturn // a *time.Timer, behind the interface tests replace it with
	return time.AfterFunc(d, f)
}

// netromCircuitOwner is what the circuits report to and ask of: the node
// they belong to.  Apart from acceptCircuit, which the decision to answer a
// CONNECT REQUEST waits on, its methods are called after the circuit
// manager's lock has been released, in the order the events happened.
type netromCircuitOwner interface {
	// sendLayer3 routes a frame towards its destination node.
	sendLayer3(f *netromFrame)

	// acceptCircuit decides whether to answer a CONNECT REQUEST addressed to
	// this node, and if so which client to hand the circuit to and the
	// callsign that client knows this end by.
	acceptCircuit(user string, originNode string) (client int, ownCall string, ok bool)

	circuitEstablished(c netromCircuitID, incoming bool)
	circuitTerminated(c netromCircuitID, timeout bool)
	circuitData(c netromCircuitID, data []byte)
}

// netromCircuitID is how a circuit is known to its client: the client, the
// callsign it uses for this end, and the one it uses for the other.
type netromCircuitID struct {
	client     int
	ownCall    string
	remoteCall string
}

// netromSegment is a piece of a message, as it goes in one INFO frame.
type netromSegment struct {
	data []byte
	more bool
}

// netromCircuitTimer is one of a circuit's timers.  Each start or stop makes
// a new generation, so that an expiry that was already on its way when the
// timer was stopped or restarted finds itself out of date and does nothing.
type netromCircuitTimer struct {
	timer      netromTimer
	generation uint64
	running    bool
}

func (t *netromCircuitTimer) stop() {
	if t.timer != nil {
		t.timer.Stop()
	}

	t.generation++
	t.running = false
}

type netromCircuit struct {
	id netromCircuitID

	remoteNode string // The node at the far end.
	user       string // Whose circuit it is, as the CONNECT REQUEST says.
	incoming   bool

	myIndex, myID     byte
	yourIndex, yourID byte

	state  netromCircuitState
	window byte
	t1     time.Duration

	// Sequence numbers, modulo 256 as bytes are: vs is the next to send, va
	// the oldest not yet acknowledged, vr the next expected, and vl the vr
	// we last told the other end about.
	vs, va, vr, vl byte

	ackPending bool // We owe the other end an acknowledgement.
	peerBusy   bool // It has asked us to pause.
	bpq        bool // It spoke the BPQ extensions, so we answer in kind.
	n2count    int

	sendQueue []netromSegment       // Not yet sent.
	ackQueue  []netromSegment       // Sent, not yet acknowledged; the first is va.
	reseq     map[byte]*netromFrame // Received ahead of vr.
	message   []byte                // Reassembly of a message split by MORE.
	t1Timer   netromCircuitTimer    // Retransmission.
	t2Timer   netromCircuitTimer    // Delayed acknowledgement.
	t4Timer   netromCircuitTimer    // Peer busy.
}

// netromCircuitManager holds a node's circuits.  It is safe for concurrent
// use.
type netromCircuitManager struct {
	mu sync.Mutex

	myCall string
	ttl    byte
	owner  netromCircuitOwner
	clock  netromClock

	window byte
	t1     time.Duration
	t2     time.Duration
	t4     time.Duration
	n2     int

	circuits    map[uint16]*netromCircuit // By myIndex<<8 | myID.
	nextCircuit uint16

	// effects are the owner calls made while the lock is held, run once it
	// is released.
	effects []func()
}

func newNetromCircuitManager(myCall string, ttl byte, owner netromCircuitOwner, clock netromClock) *netromCircuitManager {
	var m = new(netromCircuitManager)
	m.myCall = myCall
	m.ttl = ttl
	m.owner = owner
	m.clock = clock
	m.window = netromDefaultWindow
	m.t1 = netromDefaultT1
	m.t2 = netromDefaultT2
	m.t4 = netromDefaultT4
	m.n2 = netromDefaultN2
	m.circuits = make(map[uint16]*netromCircuit)

	return m
}

// locked runs fn with the lock held, then the owner calls it gave rise to.
func (m *netromCircuitManager) locked(fn func()) {
	m.mu.Lock()
	fn()

	var effects = m.effects
	m.effects = nil
	m.mu.Unlock()

	for _, effect := range effects {
		effect()
	}
}

func (m *netromCircuitManager) later(effect func()) {
	m.effects = append(m.effects, effect)
}

// connect opens a circuit for a client to remoteNode.  The client knows this
// end as id.ownCall, which is also the user the CONNECT REQUEST names, and
// the far end as id.remoteCall.  Data may be sent on it straight away; it
// waits until the circuit is up.
func (m *netromCircuitManager) connect(id netromCircuitID, remoteNode string) error {
	var err error

	m.locked(func() {
		// A client names its circuits by their callsigns, so a second one
		// by the same names would leave it unable to say which it meant.
		if m.byID(id) != nil {
			err = errNetromCircuitExists

			return
		}

		var c *netromCircuit

		c, err = m.newCircuit()
		if err != nil {
			return
		}

		c.id = id
		c.user = id.ownCall
		c.remoteNode = remoteNode
		c.state = netromConnecting
		c.window = m.window
		m.sendConnReq(c)
		m.startT1(c)
	})

	return err
}

// send queues data on the client's circuit, split into as many INFO frames
// as it takes.
func (m *netromCircuitManager) send(id netromCircuitID, data []byte) error {
	var err error

	m.locked(func() {
		var c = m.byID(id)
		if c == nil || c.state == netromDisconnecting {
			err = errNetromNoCircuit

			return
		}

		for len(data) > 0 {
			var n = min(len(data), netromMaxInfo)
			c.sendQueue = append(c.sendQueue, netromSegment{data: slices.Clone(data[:n]), more: n < len(data)})
			data = data[n:]
		}

		m.kick(c)
	})

	return err
}

// disconnect closes the client's circuit.  What has not been acknowledged
// yet is abandoned, as Linux does.
func (m *netromCircuitManager) disconnect(id netromCircuitID) error {
	var err error

	m.locked(func() {
		var c = m.byID(id)
		if c == nil {
			err = errNetromNoCircuit

			return
		}

		m.startDisconnect(c)
	})

	return err
}

// dropClient closes every circuit of a client that has gone away, without
// reporting back to it.
func (m *netromCircuitManager) dropClient(client int) {
	m.locked(func() {
		for _, c := range m.circuits {
			if c.id.client != client {
				continue
			}

			c.id.client = -1

			m.startDisconnect(c)
		}
	})
}

// startDisconnect begins closing c: a circuit that is up gets a DISCONNECT
// REQUEST; one still connecting is simply dropped.
func (m *netromCircuitManager) startDisconnect(c *netromCircuit) {
	switch c.state {
	case netromConnecting:
		m.close(c, false)
	case netromConnected:
		m.clearQueues(c)
		c.n2count = 0
		c.state = netromDisconnecting
		m.sendDiscReq(c)
		m.startT1(c)
		c.t2Timer.stop()
		c.t4Timer.stop()
	case netromDisconnecting, netromClosed:
	}
}

// rx handles a layer 3 frame addressed to this node.
func (m *netromCircuitManager) rx(f *netromFrame) {
	m.locked(func() {
		var c *netromCircuit

		switch {
		case f.index == 0 && f.id == 0:
			// Not a circuit of ours, unless it is a refusal of one, which
			// names it by the other end's pair.
			if f.opcode == netromOpConnAck && f.has(netromFlagChoke) {
				c = m.byPeer(f.peerIndex(), f.peerID(), f.origin)
			}
		case f.opcode == netromOpConnReq:
			c = m.byPeer(f.index, f.id, f.origin)
			if c == nil {
				m.incoming(f)

				return
			}
		default:
			c = m.circuits[uint16(f.index)<<8|uint16(f.id)]
		}

		if c == nil {
			return
		}

		switch c.state {
		case netromConnecting:
			m.rxConnecting(c, f)
		case netromDisconnecting:
			m.rxDisconnecting(c, f)
		case netromConnected:
			m.rxConnected(c, f)
		case netromClosed:
		}

		m.kick(c)
	})
}

// incoming answers a CONNECT REQUEST for a new circuit.
func (m *netromCircuitManager) incoming(f *netromFrame) {
	var client, ownCall, ok = m.owner.acceptCircuit(f.user, f.origin)
	if !ok {
		m.sendRefusal(f)

		return
	}

	var id = netromCircuitID{client: client, ownCall: ownCall, remoteCall: f.user}

	// The client would know this circuit by the same names as one it has
	// already.  A new request from the same user means the far end has lost
	// that one - its node restarted, say, or our DISCONNECT ACKNOWLEDGE went
	// astray - so it is the one to go, rather than leave the client's
	// commands going to either at random.
	if stale := m.byID(id); stale != nil {
		m.close(stale, false)
	}

	var c, err = m.newCircuit()
	if err != nil {
		m.sendRefusal(f)

		return
	}

	c.id = id
	c.user = f.user
	c.remoteNode = f.origin
	c.incoming = true
	c.yourIndex = f.index
	c.yourID = f.id

	c.window = m.window
	if f.window > 0 && f.window < c.window {
		c.window = f.window
	}

	if timeout, ok := f.timeout.Get(); ok {
		c.bpq = true

		if d := time.Duration(timeout) * time.Second; d > 0 && d < c.t1 {
			c.t1 = d
		}
	}

	m.sendConnAck(c)

	c.state = netromConnected

	m.later(func() { m.owner.circuitEstablished(id, true) })
}

func (m *netromCircuitManager) rxConnecting(c *netromCircuit, f *netromFrame) {
	if f.opcode != netromOpConnAck {
		return
	}

	if f.has(netromFlagChoke) {
		m.close(c, false)

		return
	}

	c.t1Timer.stop()
	c.yourIndex = f.peerIndex()
	c.yourID = f.peerID()
	c.vs, c.va, c.vr, c.vl = 0, 0, 0, 0
	c.n2count = 0
	c.state = netromConnected

	if f.window > 0 && f.window < c.window {
		c.window = f.window
	}

	var id = c.id
	m.later(func() { m.owner.circuitEstablished(id, false) })
}

func (m *netromCircuitManager) rxDisconnecting(c *netromCircuit, f *netromFrame) {
	switch {
	case f.opcode == netromOpDiscReq:
		m.sendDiscAck(c)
		m.close(c, false)
	case f.opcode == netromOpDiscAck,
		f.opcode == netromOpConnAck && f.has(netromFlagChoke):
		m.close(c, false)
	}
}

func (m *netromCircuitManager) rxConnected(c *netromCircuit, f *netromFrame) {
	switch f.opcode {
	case netromOpConnReq:
		// Our CONNECT ACKNOWLEDGE went astray; say it again.
		m.sendConnAck(c)

	case netromOpDiscReq:
		m.sendDiscAck(c)
		m.close(c, false)

	case netromOpDiscAck:
		m.close(c, false)

	case netromOpConnAck:
		if f.has(netromFlagChoke) {
			m.close(c, false)
		}

	case netromOpInfoAck:
		m.rxAck(c, f)

	case netromOpInfo:
		m.rxAck(c, f)
		m.rxInfo(c, f)

	case netromOpProtoExt:
	}
}

// rxAck acts on the flags and receive sequence number that INFO and INFO
// ACKNOWLEDGE both carry.
func (m *netromCircuitManager) rxAck(c *netromCircuit, f *netromFrame) {
	if f.has(netromFlagChoke) {
		c.peerBusy = true
		m.startT4(c)
	} else {
		c.peerBusy = false
		c.t4Timer.stop()
	}

	if !c.validNR(f.rxSeq) {
		return
	}

	switch {
	case f.has(netromFlagNAK):
		c.framesAcked(f.rxSeq)
		m.sendNAKResponse(c)
	case c.peerBusy:
		c.framesAcked(f.rxSeq)
	default:
		m.checkIFramesAcked(c, f.rxSeq)
	}
}

// rxInfo takes an INFO frame's data, in sequence.
func (m *netromCircuitManager) rxInfo(c *netromCircuit, f *netromFrame) {
	if c.inRxWindow(f.txSeq) {
		c.reseq[f.txSeq] = f
	}

	for {
		var next, ok = c.reseq[c.vr]
		if !ok {
			break
		}

		delete(c.reseq, c.vr)
		c.vr++

		m.deliver(c, next.info, next.has(netromFlagMore))
	}

	if c.vl+c.window == c.vr {
		// The other end's window is full: acknowledge now rather than make
		// it wait.
		m.sendEnquiryResponse(c)
	} else if !c.ackPending {
		c.ackPending = true
		m.startT2(c)
	}
}

// deliver passes data up, holding it while more of the same message is to
// come, but never more than netromMaxReassembly at once.
func (m *netromCircuitManager) deliver(c *netromCircuit, data []byte, more bool) {
	c.message = append(c.message, data...)

	for len(c.message) >= netromMaxReassembly {
		var piece = c.message[:netromMaxReassembly]
		c.message = slices.Clone(c.message[netromMaxReassembly:])
		m.deliverPiece(c, piece)
	}

	if !more && len(c.message) > 0 {
		var piece = c.message
		c.message = nil
		m.deliverPiece(c, piece)
	}
}

func (m *netromCircuitManager) deliverPiece(c *netromCircuit, data []byte) {
	if c.id.client < 0 {
		return
	}

	var id = c.id
	m.later(func() { m.owner.circuitData(id, data) })
}

// kick sends what the window allows.
func (m *netromCircuitManager) kick(c *netromCircuit) {
	if c.state != netromConnected || c.peerBusy || len(c.sendQueue) == 0 {
		return
	}

	var end = c.va + c.window
	if c.vs == end {
		return
	}

	for c.vs != end && len(c.sendQueue) > 0 {
		var seg = c.sendQueue[0]
		c.sendQueue = c.sendQueue[1:]

		m.sendInfo(c, seg, c.vs)
		c.ackQueue = append(c.ackQueue, seg)
		c.vs++
	}

	c.vl = c.vr
	c.ackPending = false
	c.t2Timer.stop()

	if !c.t1Timer.running {
		m.startT1(c)
	}
}

// checkIFramesAcked handles an acknowledgement of nr in the ordinary way:
// everything outstanding acknowledged stops the retransmission timer, and
// progress short of that restarts it.
func (m *netromCircuitManager) checkIFramesAcked(c *netromCircuit, nr byte) {
	switch {
	case c.vs == nr:
		c.framesAcked(nr)
		c.t1Timer.stop()
		c.n2count = 0
	case c.va != nr:
		c.framesAcked(nr)
		m.startT1(c)
	}
}

func (c *netromCircuit) framesAcked(nr byte) {
	for len(c.ackQueue) > 0 && c.va != nr {
		c.ackQueue = c.ackQueue[1:]
		c.va++
	}
}

// validNR reports whether nr acknowledges something between va and vs.
func (c *netromCircuit) validNR(nr byte) bool {
	return nr-c.va <= c.vs-c.va
}

// inRxWindow reports whether ns is one we are ready to take: from vr up to,
// but not including, the edge of the window we last advertised.
func (c *netromCircuit) inRxWindow(ns byte) bool {
	return ns-c.vr < c.vl+c.window-c.vr
}

func (m *netromCircuitManager) t1Expired(c *netromCircuit) {
	if c.n2count >= m.n2 {
		m.close(c, true)

		return
	}

	c.n2count++

	switch c.state {
	case netromConnecting:
		m.sendConnReq(c)
	case netromDisconnecting:
		m.sendDiscReq(c)
	case netromConnected:
		// Everything unacknowledged goes again, from va.
		c.sendQueue = append(c.ackQueue, c.sendQueue...)
		c.ackQueue = nil
		c.vs = c.va
		m.kick(c)
	case netromClosed:
		return
	}

	m.startT1(c)
}

func (m *netromCircuitManager) t2Expired(c *netromCircuit) {
	if c.ackPending {
		m.sendEnquiryResponse(c)
	}
}

func (m *netromCircuitManager) t4Expired(c *netromCircuit) {
	c.peerBusy = false
	m.kick(c)
}

// close ends c and tells its client.
func (m *netromCircuitManager) close(c *netromCircuit, timeout bool) {
	c.t1Timer.stop()
	c.t2Timer.stop()
	c.t4Timer.stop()
	m.clearQueues(c)
	c.state = netromClosed

	delete(m.circuits, uint16(c.myIndex)<<8|uint16(c.myID))

	if c.id.client < 0 {
		return
	}

	var id = c.id
	m.later(func() { m.owner.circuitTerminated(id, timeout) })
}

func (m *netromCircuitManager) clearQueues(c *netromCircuit) {
	c.sendQueue = nil
	c.ackQueue = nil
	c.reseq = make(map[byte]*netromFrame)
	c.message = nil
}

// newCircuit allocates a circuit with an index and ID pair not in use,
// counting through them as Linux does and never using zero for either.
func (m *netromCircuitManager) newCircuit() (*netromCircuit, error) {
	if len(m.circuits) >= netromMaxCircuits {
		return nil, errNetromTooManyCircuits
	}

	for {
		m.nextCircuit++

		var index, id = byte(m.nextCircuit >> 8), byte(m.nextCircuit)
		if index == 0 || id == 0 {
			continue
		}

		if _, taken := m.circuits[m.nextCircuit]; taken {
			continue
		}

		var c = new(netromCircuit)
		c.myIndex = index
		c.myID = id
		c.t1 = m.t1
		c.reseq = make(map[byte]*netromFrame)
		m.circuits[m.nextCircuit] = c

		return c, nil
	}
}

func (m *netromCircuitManager) byID(id netromCircuitID) *netromCircuit {
	for _, c := range m.circuits {
		if c.id == id && c.state != netromClosed {
			return c
		}
	}

	return nil
}

func (m *netromCircuitManager) byPeer(index byte, id byte, node string) *netromCircuit {
	for _, c := range m.circuits {
		if c.yourIndex == index && c.yourID == id && c.remoteNode == node && c.state != netromClosed {
			return c
		}
	}

	return nil
}

// Timers.

func (m *netromCircuitManager) startTimer(c *netromCircuit, t *netromCircuitTimer, d time.Duration, expired func(*netromCircuit)) {
	t.stop()
	t.running = true

	var generation = t.generation

	t.timer = m.clock.AfterFunc(d, func() {
		m.locked(func() {
			if t.generation != generation || !t.running || c.state == netromClosed {
				return
			}

			t.running = false

			expired(c)
		})
	})
}

func (m *netromCircuitManager) startT1(c *netromCircuit) {
	m.startTimer(c, &c.t1Timer, c.t1, m.t1Expired)
}

func (m *netromCircuitManager) startT2(c *netromCircuit) {
	m.startTimer(c, &c.t2Timer, m.t2, m.t2Expired)
}

func (m *netromCircuitManager) startT4(c *netromCircuit) {
	m.startTimer(c, &c.t4Timer, m.t4, m.t4Expired)
}

// Frames.

// frame starts a frame for c with the network header filled in.
func (m *netromCircuitManager) frame(c *netromCircuit, opcode netromOpcode) *netromFrame {
	var f = new(netromFrame)
	f.origin = m.myCall
	f.destination = c.remoteNode
	f.ttl = m.ttl
	f.index = c.yourIndex
	f.id = c.yourID
	f.opcode = opcode

	return f
}

func (m *netromCircuitManager) send3(f *netromFrame) {
	m.later(func() { m.owner.sendLayer3(f) })
}

func (m *netromCircuitManager) sendConnReq(c *netromCircuit) {
	var f = m.frame(c, netromOpConnReq)
	f.index = c.myIndex
	f.id = c.myID
	f.window = c.window
	f.user = c.user
	f.originNode = m.myCall
	f.timeout = maybe.Just(int(c.t1 / time.Second))
	m.send3(f)
}

func (m *netromCircuitManager) sendConnAck(c *netromCircuit) {
	var f = m.frame(c, netromOpConnAck)
	f.txSeq = c.myIndex
	f.rxSeq = c.myID
	f.window = c.window

	if c.bpq {
		f.ackTTL = maybe.Just(m.ttl)
	}

	m.send3(f)
}

// sendRefusal refuses a CONNECT REQUEST, naming the requester's circuit as
// Linux does.
func (m *netromCircuitManager) sendRefusal(req *netromFrame) {
	var f = new(netromFrame)
	f.origin = m.myCall
	f.destination = req.origin
	f.ttl = m.ttl
	f.index = req.index
	f.id = req.id
	f.opcode = netromOpConnAck
	f.flags = netromFlagChoke
	m.send3(f)
}

func (m *netromCircuitManager) sendDiscReq(c *netromCircuit) {
	m.send3(m.frame(c, netromOpDiscReq))
}

func (m *netromCircuitManager) sendDiscAck(c *netromCircuit) {
	m.send3(m.frame(c, netromOpDiscAck))
}

func (m *netromCircuitManager) sendInfo(c *netromCircuit, seg netromSegment, ns byte) {
	var f = m.frame(c, netromOpInfo)
	f.txSeq = ns
	f.rxSeq = c.vr
	f.info = seg.data

	if seg.more {
		f.flags |= netromFlagMore
	}

	m.send3(f)
}

// sendEnquiryResponse acknowledges what has arrived, asking for a resend
// when there is a gap before something held for resequencing.
func (m *netromCircuitManager) sendEnquiryResponse(c *netromCircuit) {
	var f = m.frame(c, netromOpInfoAck)
	f.rxSeq = c.vr

	if len(c.reseq) > 0 {
		f.flags |= netromFlagNAK
	}

	m.send3(f)

	c.vl = c.vr
	c.ackPending = false
	c.t2Timer.stop()
}

// sendNAKResponse answers a NAK by sending the oldest unacknowledged frame
// again.
//
// Linux stops T1 here.  That leaves nothing to retry with if the resent frame
// is lost too and there is no new data to send, so the circuit sits there for
// good; restarting it instead costs at most a spare retransmission.
func (m *netromCircuitManager) sendNAKResponse(c *netromCircuit) {
	if len(c.ackQueue) == 0 {
		return
	}

	m.sendInfo(c, c.ackQueue[0], c.va)

	c.vl = c.vr
	c.ackPending = false
	c.t2Timer.stop()
	m.startT1(c)
}
