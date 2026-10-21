// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

// NET/ROM transport circuits.
//
// A circuit is a connection between two NET/ROM nodes, end to end across
// however many hops lie between them, carrying one user's data in order and
// without loss.  Each end names it by an index and an ID of its own choosing;
// every frame carries the recipient's pair, which is how it finds its circuit.
//
// A circuit is in one of three states while it is open:
//
//   - Connecting: a CONNECT REQUEST has gone out, and is repeated every T1
//     until a CONNECT ACKNOWLEDGE comes back, or N2 repeats have gone
//     unanswered.
//   - Connected: INFO frames flow both ways within a window of unacknowledged
//     frames, each carrying the sender's receive sequence number as an
//     acknowledgement, with INFO ACKNOWLEDGE for when there is nothing to
//     send; out-of-order frames are held for resequencing; NAK asks for a
//     resend and CHOKE for a pause; MORE marks a message split over several
//     frames.
//   - Disconnecting: a DISCONNECT REQUEST has gone out, and is repeated every
//     T1 until acknowledged or N2 repeats have gone unanswered.

import (
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	defaultWindow = 4
	defaultT1     = 120 * time.Second // Retransmission.
	defaultT2     = 5 * time.Second   // Acknowledgement delay.
	defaultT4     = 180 * time.Second // How long to respect a CHOKE.
	defaultN2     = 3                 // Retries.

	// maxInfo is the most data one INFO frame carries: what is left of
	// a 256-byte AX.25 information field after the NET/ROM headers.
	maxInfo = 236

	// A reassembled message is delivered in pieces no bigger than an AGW
	// client can be handed at once, so a peer that never clears MORE cannot
	// grow the buffer without limit.
	maxReassembly = ax25.MaxInfoLen

	// Every circuit costs memory and timers, and a CONNECT REQUEST is cheap
	// to send, so there is a limit.
	maxCircuits = 256
)

var (
	errTooManyCircuits       = errors.New("too many NET/ROM circuits")
	errNoCircuit             = errors.New("no such NET/ROM circuit")
	errCircuitExists         = errors.New("the client already has that NET/ROM circuit")
	errCircuitOnOtherChannel = errors.New("the client has a NET/ROM circuit by those callsigns on another channel")
)

type circuitState int

const (
	stateClosed circuitState = iota
	stateConnecting
	stateDisconnecting
	stateConnected
)

// Timer is a pending timer.
type Timer interface {
	Stop() bool
}

// Clock starts timers, so that tests can drive time themselves.
type Clock interface {
	AfterFunc(d time.Duration, f func()) Timer
}

type RealClock struct{}

func (RealClock) AfterFunc(d time.Duration, f func()) Timer { //nolint:ireturn // a *time.Timer, behind the interface tests replace it with
	return time.AfterFunc(d, f)
}

// circuitOwner is what the circuits report to and ask of: the node
// they belong to.  Apart from acceptCircuit, which the decision to answer a
// CONNECT REQUEST waits on, its methods are called after the circuit
// manager's lock has been released, in the order the events happened.
type circuitOwner interface {
	// sendLayer3 routes a frame towards its destination node.
	sendLayer3(f *Frame)

	// acceptCircuit decides whether to answer a CONNECT REQUEST addressed to
	// this node, and if so which client to hand the circuit to, on which
	// channel, and the callsign it knows this end by.
	acceptCircuit(user string, originNode string) (to listener, ownCall string, ok bool)

	circuitEstablished(c circuitID, incoming bool)
	circuitTerminated(c circuitID, timeout bool)
	circuitData(c circuitID, data []byte)
}

// circuitID is how a circuit is known to its client: the channel the
// client has it on, the client, the callsign it uses for this end, and the
// one it uses for the other.  The channel is only what the client asked on,
// or registered on to take incoming circuits, since a circuit is not on a
// channel of its own: its frames go wherever the route to its far end does.
type circuitID struct {
	channel    int
	client     int
	ownCall    string
	remoteCall string
}

// segment is a piece of a message, as it goes in one INFO frame.
type segment struct {
	data []byte
	more bool
}

// circuitTimer is one of a circuit's timers.  Each start or stop makes
// a new generation, so that an expiry that was already on its way when the
// timer was stopped or restarted finds itself out of date and does nothing.
type circuitTimer struct {
	timer      Timer
	generation uint64
	running    bool
}

func (t *circuitTimer) stop() {
	if t.timer != nil {
		t.timer.Stop()
	}

	t.generation++
	t.running = false
}

type circuit struct {
	id circuitID

	remoteNode string // The node at the far end.
	user       string // Whose circuit it is, as the CONNECT REQUEST says.
	incoming   bool

	myIndex, myID     byte
	yourIndex, yourID byte

	state  circuitState
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

	sendQueue []segment       // Not yet sent.
	ackQueue  []segment       // Sent, not yet acknowledged; the first is va.
	reseq     map[byte]*Frame // Received ahead of vr.
	message   []byte          // Reassembly of a message split by MORE.
	t1Timer   circuitTimer    // Retransmission.
	t2Timer   circuitTimer    // Delayed acknowledgement.
	t4Timer   circuitTimer    // Peer busy.
}

// circuitManager holds a node's circuits.  It is safe for concurrent
// use.
type circuitManager struct {
	mu sync.Mutex

	myCall string
	ttl    byte
	owner  circuitOwner
	clock  Clock

	window byte
	t1     time.Duration
	t2     time.Duration
	t4     time.Duration
	n2     int

	circuits    map[uint16]*circuit // By myIndex<<8 | myID.
	nextCircuit uint16

	// effects are the owner calls made while the lock is held, waiting to
	// be run once it is released, in the order they were made; draining
	// says some goroutine is already running them.
	// is released.
	effects  []func()
	draining bool
}

func newCircuitManager(myCall string, ttl byte, owner circuitOwner, clock Clock) *circuitManager {
	var m = new(circuitManager)
	m.myCall = myCall
	m.ttl = ttl
	m.owner = owner
	m.clock = clock
	m.window = defaultWindow
	m.t1 = defaultT1
	m.t2 = defaultT2
	m.t4 = defaultT4
	m.n2 = defaultN2
	m.circuits = make(map[uint16]*circuit)

	return m
}

// locked runs fn with the lock held, then the owner calls it gave rise to.
//
// The calls are run one goroutine at a time, oldest first, whichever
// goroutine made them: the receive path and the timers can both be making
// them, and the owner must not hear of a circuit's data after hearing it has
// gone.  A goroutine that finds another already running them leaves its own
// to that one.
func (m *circuitManager) locked(fn func()) {
	m.mu.Lock()
	fn()

	if m.draining {
		m.mu.Unlock()

		return
	}

	m.draining = true

	for len(m.effects) > 0 {
		var effects = m.effects
		m.effects = nil
		m.mu.Unlock()

		for _, effect := range effects {
			effect()
		}

		m.mu.Lock()
	}

	m.draining = false
	m.mu.Unlock()
}

func (m *circuitManager) later(effect func()) {
	m.effects = append(m.effects, effect)
}

// connect opens a circuit for a client to remoteNode.  The client knows this
// end as id.ownCall, which is also the user the CONNECT REQUEST names, and
// the far end as id.remoteCall.  Data may be sent on it straight away; it
// waits until the circuit is up.
func (m *circuitManager) connect(id circuitID, remoteNode string) error {
	var err error

	m.locked(func() {
		// A client names its circuits by their callsigns, so a second one
		// by the same names would leave it unable to say which it meant.
		if m.byID(id) != nil {
			err = errCircuitExists

			return
		}

		// Nor can it have one by the same names on another channel: to the
		// far end the two would be the same user at the same node, and it
		// would take the second for the first's replacement.
		if m.byNames(id) != nil {
			err = errCircuitOnOtherChannel

			return
		}

		var c *circuit

		c, err = m.newCircuit()
		if err != nil {
			return
		}

		c.id = id
		c.user = id.ownCall
		c.remoteNode = remoteNode
		c.state = stateConnecting
		c.window = m.window
		m.sendConnReq(c)
		m.startT1(c)
	})

	return err
}

// send queues data on the client's circuit, split into as many INFO frames
// as it takes.
func (m *circuitManager) send(id circuitID, data []byte) error {
	var err error

	m.locked(func() {
		var c = m.byID(id)
		if c == nil || c.state == stateDisconnecting {
			err = errNoCircuit

			return
		}

		for len(data) > 0 {
			var n = min(len(data), maxInfo)
			c.sendQueue = append(c.sendQueue, segment{data: slices.Clone(data[:n]), more: n < len(data)})
			data = data[n:]
		}

		m.kick(c)
	})

	return err
}

// outstanding is how many of the client's segments on its circuit are
// still to go out or be acknowledged, and whether it has the circuit.
func (m *circuitManager) outstanding(id circuitID) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var c = m.byID(id)
	if c == nil {
		return 0, false
	}

	return len(c.sendQueue) + len(c.ackQueue), true
}

// disconnect closes the client's circuit.  What has not been acknowledged
// yet is abandoned.
func (m *circuitManager) disconnect(id circuitID) error {
	var err error

	m.locked(func() {
		var c = m.byID(id)
		if c == nil {
			err = errNoCircuit

			return
		}

		m.startDisconnect(c)
	})

	return err
}

// dropClient closes every circuit of a client that has gone away, without
// reporting back to it.
func (m *circuitManager) dropClient(client int) {
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
func (m *circuitManager) startDisconnect(c *circuit) {
	switch c.state {
	case stateConnecting:
		m.close(c, false)
	case stateConnected:
		m.clearQueues(c)
		c.n2count = 0
		c.state = stateDisconnecting
		m.sendDiscReq(c)
		m.startT1(c)
		c.t2Timer.stop()
		c.t4Timer.stop()
	case stateDisconnecting, stateClosed:
	}
}

// rx handles a layer 3 frame addressed to this node.
func (m *circuitManager) rx(f *Frame) {
	m.locked(func() {
		var c *circuit

		switch {
		case f.index == 0 && f.id == 0:
			// Not a circuit of ours, unless it is a refusal of one, which
			// names it by the other end's pair.
			if f.opcode == OpConnAck && f.has(FlagChoke) {
				c = m.byPeer(f.peerIndex(), f.peerID(), f.origin)
			}
		case f.opcode == OpConnReq:
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
		case stateConnecting:
			m.rxConnecting(c, f)
		case stateDisconnecting:
			m.rxDisconnecting(c, f)
		case stateConnected:
			m.rxConnected(c, f)
		case stateClosed:
		}

		m.kick(c)
	})
}

// incoming answers a CONNECT REQUEST for a new circuit.
func (m *circuitManager) incoming(f *Frame) {
	var to, ownCall, ok = m.owner.acceptCircuit(f.user, f.origin)
	if !ok {
		m.sendRefusal(f)

		return
	}

	var id = circuitID{channel: to.channel, client: to.client, ownCall: ownCall, remoteCall: f.user}

	// The client would know this circuit by the same names as one it has
	// already.  A new request from the same user at the same node means
	// that node has lost the old one - it restarted, say, or our DISCONNECT
	// ACKNOWLEDGE went astray - so the old one goes, rather than leave the
	// client's commands going to either at random.  From another node it is
	// someone else, or a second session, and the client could not tell the
	// two apart, so the new one is refused.  Whatever channel the old one is
	// on, the client knows the two by the same callsigns.
	if existing := m.byNames(id); existing != nil {
		if existing.remoteNode != f.origin {
			m.sendRefusal(f)

			return
		}

		m.close(existing, false)
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

	c.state = stateConnected

	m.later(func() { m.owner.circuitEstablished(id, true) })
}

func (m *circuitManager) rxConnecting(c *circuit, f *Frame) {
	if f.opcode != OpConnAck {
		return
	}

	if f.has(FlagChoke) {
		m.close(c, false)

		return
	}

	c.t1Timer.stop()
	c.yourIndex = f.peerIndex()
	c.yourID = f.peerID()
	c.vs, c.va, c.vr, c.vl = 0, 0, 0, 0
	c.n2count = 0
	c.state = stateConnected

	if f.window > 0 && f.window < c.window {
		c.window = f.window
	}

	var id = c.id
	m.later(func() { m.owner.circuitEstablished(id, false) })
}

func (m *circuitManager) rxDisconnecting(c *circuit, f *Frame) {
	switch {
	case f.opcode == OpDiscReq:
		m.sendDiscAck(c)
		m.close(c, false)
	case f.opcode == OpDiscAck,
		f.opcode == OpConnAck && f.has(FlagChoke):
		m.close(c, false)
	}
}

func (m *circuitManager) rxConnected(c *circuit, f *Frame) {
	switch f.opcode {
	case OpConnReq:
		// Our CONNECT ACKNOWLEDGE went astray; say it again.
		m.sendConnAck(c)

	case OpDiscReq:
		m.sendDiscAck(c)
		m.close(c, false)

	case OpDiscAck:
		m.close(c, false)

	case OpConnAck:
		if f.has(FlagChoke) {
			m.close(c, false)
		}

	case OpInfoAck:
		m.rxAck(c, f)

	case OpInfo:
		m.rxAck(c, f)
		m.rxInfo(c, f)

	case OpProtoExt:
	}
}

// rxAck acts on the flags and receive sequence number that INFO and INFO
// ACKNOWLEDGE both carry.
func (m *circuitManager) rxAck(c *circuit, f *Frame) {
	if f.has(FlagChoke) {
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
	case f.has(FlagNAK):
		c.framesAcked(f.rxSeq)
		m.sendNAKResponse(c)
	case c.peerBusy:
		c.framesAcked(f.rxSeq)
	default:
		m.checkIFramesAcked(c, f.rxSeq)
	}
}

// rxInfo takes an INFO frame's data, in sequence.
func (m *circuitManager) rxInfo(c *circuit, f *Frame) {
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

		m.deliver(c, next.info, next.has(FlagMore))
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
// come, but never more than maxReassembly at once.
func (m *circuitManager) deliver(c *circuit, data []byte, more bool) {
	c.message = append(c.message, data...)

	for len(c.message) >= maxReassembly {
		var piece = c.message[:maxReassembly]
		c.message = slices.Clone(c.message[maxReassembly:])
		m.deliverPiece(c, piece)
	}

	if !more && len(c.message) > 0 {
		var piece = c.message
		c.message = nil
		m.deliverPiece(c, piece)
	}
}

func (m *circuitManager) deliverPiece(c *circuit, data []byte) {
	if c.id.client < 0 {
		return
	}

	var id = c.id
	m.later(func() { m.owner.circuitData(id, data) })
}

// kick sends what the window allows.
func (m *circuitManager) kick(c *circuit) {
	if c.state != stateConnected || c.peerBusy || len(c.sendQueue) == 0 {
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
func (m *circuitManager) checkIFramesAcked(c *circuit, nr byte) {
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

func (c *circuit) framesAcked(nr byte) {
	for len(c.ackQueue) > 0 && c.va != nr {
		c.ackQueue = c.ackQueue[1:]
		c.va++
	}
}

// validNR reports whether nr acknowledges something between va and vs.
func (c *circuit) validNR(nr byte) bool {
	return nr-c.va <= c.vs-c.va
}

// inRxWindow reports whether ns is one we are ready to take: from vr up to,
// but not including, the edge of the window we last advertised.
func (c *circuit) inRxWindow(ns byte) bool {
	return ns-c.vr < c.vl+c.window-c.vr
}

func (m *circuitManager) t1Expired(c *circuit) {
	if c.n2count >= m.n2 {
		m.close(c, true)

		return
	}

	c.n2count++

	switch c.state {
	case stateConnecting:
		m.sendConnReq(c)
	case stateDisconnecting:
		m.sendDiscReq(c)
	case stateConnected:
		// Everything unacknowledged goes again, from va.
		c.sendQueue = append(c.ackQueue, c.sendQueue...)
		c.ackQueue = nil
		c.vs = c.va
		m.kick(c)
	case stateClosed:
		return
	}

	m.startT1(c)
}

func (m *circuitManager) t2Expired(c *circuit) {
	if c.ackPending {
		m.sendEnquiryResponse(c)
	}
}

func (m *circuitManager) t4Expired(c *circuit) {
	c.peerBusy = false
	m.kick(c)
}

// close ends c and tells its client.
func (m *circuitManager) close(c *circuit, timeout bool) {
	c.t1Timer.stop()
	c.t2Timer.stop()
	c.t4Timer.stop()
	m.clearQueues(c)
	c.state = stateClosed

	delete(m.circuits, uint16(c.myIndex)<<8|uint16(c.myID))

	if c.id.client < 0 {
		return
	}

	var id = c.id
	m.later(func() { m.owner.circuitTerminated(id, timeout) })
}

func (m *circuitManager) clearQueues(c *circuit) {
	c.sendQueue = nil
	c.ackQueue = nil
	c.reseq = make(map[byte]*Frame)
	c.message = nil
}

// newCircuit allocates a circuit with an index and ID pair not in use,
// counting through them and never using zero for either.
func (m *circuitManager) newCircuit() (*circuit, error) {
	if len(m.circuits) >= maxCircuits {
		return nil, errTooManyCircuits
	}

	for {
		m.nextCircuit++

		var index, id = byte((m.nextCircuit >> 8) & 0xFF), byte(m.nextCircuit & 0xFF)
		if index == 0 || id == 0 {
			continue
		}

		if _, taken := m.circuits[m.nextCircuit]; taken {
			continue
		}

		var c = new(circuit)
		c.myIndex = index
		c.myID = id
		c.t1 = m.t1
		c.reseq = make(map[byte]*Frame)
		m.circuits[m.nextCircuit] = c

		return c, nil
	}
}

func (m *circuitManager) byID(id circuitID) *circuit {
	for _, c := range m.circuits {
		if c.id == id && c.state != stateClosed {
			return c
		}
	}

	return nil
}

// byNames is the client's open circuit by id's callsigns, whatever channel
// it is on.
func (m *circuitManager) byNames(id circuitID) *circuit {
	for _, c := range m.circuits {
		if c.id.client == id.client && c.id.ownCall == id.ownCall && c.id.remoteCall == id.remoteCall && c.state != stateClosed {
			return c
		}
	}

	return nil
}

func (m *circuitManager) byPeer(index byte, id byte, node string) *circuit {
	for _, c := range m.circuits {
		if c.yourIndex == index && c.yourID == id && c.remoteNode == node && c.state != stateClosed {
			return c
		}
	}

	return nil
}

// Timers.

func (m *circuitManager) startTimer(c *circuit, t *circuitTimer, d time.Duration, expired func(*circuit)) {
	t.stop()
	t.running = true

	var generation = t.generation

	t.timer = m.clock.AfterFunc(d, func() {
		m.locked(func() {
			if t.generation != generation || !t.running || c.state == stateClosed {
				return
			}

			t.running = false

			expired(c)
		})
	})
}

func (m *circuitManager) startT1(c *circuit) {
	m.startTimer(c, &c.t1Timer, c.t1, m.t1Expired)
}

func (m *circuitManager) startT2(c *circuit) {
	m.startTimer(c, &c.t2Timer, m.t2, m.t2Expired)
}

func (m *circuitManager) startT4(c *circuit) {
	m.startTimer(c, &c.t4Timer, m.t4, m.t4Expired)
}

// Frames.

// frame starts a frame for c with the network header filled in.
func (m *circuitManager) frame(c *circuit, opcode Opcode) *Frame {
	var f = new(Frame)
	f.origin = m.myCall
	f.destination = c.remoteNode
	f.ttl = m.ttl
	f.index = c.yourIndex
	f.id = c.yourID
	f.opcode = opcode

	return f
}

func (m *circuitManager) send3(f *Frame) {
	m.later(func() { m.owner.sendLayer3(f) })
}

func (m *circuitManager) sendConnReq(c *circuit) {
	var f = m.frame(c, OpConnReq)
	f.index = c.myIndex
	f.id = c.myID
	f.window = c.window
	f.user = c.user
	f.originNode = m.myCall
	f.timeout = maybe.Just(int(c.t1 / time.Second))
	m.send3(f)
}

func (m *circuitManager) sendConnAck(c *circuit) {
	var f = m.frame(c, OpConnAck)
	f.txSeq = c.myIndex
	f.rxSeq = c.myID
	f.window = c.window

	if c.bpq {
		f.ackTTL = maybe.Just(m.ttl)
	}

	m.send3(f)
}

// sendRefusal refuses a CONNECT REQUEST, naming the requester's circuit.
func (m *circuitManager) sendRefusal(req *Frame) {
	var f = new(Frame)
	f.origin = m.myCall
	f.destination = req.origin
	f.ttl = m.ttl
	f.index = req.index
	f.id = req.id
	f.opcode = OpConnAck
	f.flags = FlagChoke
	m.send3(f)
}

func (m *circuitManager) sendDiscReq(c *circuit) {
	m.send3(m.frame(c, OpDiscReq))
}

func (m *circuitManager) sendDiscAck(c *circuit) {
	m.send3(m.frame(c, OpDiscAck))
}

func (m *circuitManager) sendInfo(c *circuit, seg segment, ns byte) {
	var f = m.frame(c, OpInfo)
	f.txSeq = ns
	f.rxSeq = c.vr
	f.info = seg.data

	if seg.more {
		f.flags |= FlagMore
	}

	m.send3(f)
}

// sendEnquiryResponse acknowledges what has arrived, asking for a resend
// when there is a gap before something held for resequencing.
func (m *circuitManager) sendEnquiryResponse(c *circuit) {
	var f = m.frame(c, OpInfoAck)
	f.rxSeq = c.vr

	if len(c.reseq) > 0 {
		f.flags |= FlagNAK
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
func (m *circuitManager) sendNAKResponse(c *circuit) {
	if len(c.ackQueue) == 0 {
		return
	}

	m.sendInfo(c, c.ackQueue[0], c.va)

	c.vl = c.vr
	c.ackPending = false
	c.t2Timer.stop()
	m.startT1(c)
}
