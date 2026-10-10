// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"errors"
	"fmt"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// Why a circuit closed, other than by an orderly disconnect.
var (
	ErrNoRoute = errors.New("netrom: no route to destination")
	ErrRefused = errors.New("netrom: connection refused")
	ErrTimeout = errors.New("netrom: circuit timed out")
	ErrReset   = errors.New("netrom: circuit reset by the far end")
	ErrIdle    = errors.New("netrom: circuit idle too long")
	ErrClosed  = errors.New("netrom: circuit closed")
)

// CircuitState is where a circuit is in its life.
type CircuitState int

const (
	CircuitConnecting    CircuitState = iota // Connect request sent, waiting for the ack.
	CircuitConnected                         // Carrying data.
	CircuitDisconnecting                     // Disconnect request sent, waiting for the ack.
	CircuitClosed                            // Done with.
)

func (s CircuitState) String() string {
	switch s {
	case CircuitConnecting:
		return "connecting"
	case CircuitConnected:
		return "connected"
	case CircuitDisconnecting:
		return "disconnecting"
	default:
		return "closed"
	}
}

// CircuitHandler is told what happens on a circuit.  It is called on the
// Router's goroutine, so it must not block, and must not call back into the
// Router other than through the Circuit's own methods.
type CircuitHandler interface {
	// Connected says the circuit is up: for an outgoing one, the far end
	// accepted it; for an incoming one, it has just been accepted.
	Connected(c *Circuit)

	// Received hands over data the far end sent, in order.
	Received(c *Circuit, data []byte)

	// Closed says the circuit is gone: err is nil for an orderly disconnect.
	Closed(c *Circuit, err error)
}

// segment is one information packet's worth of a write.
type segment struct {
	data []byte
	more bool
}

type sentSegment struct {
	segment

	seq byte
}

// Circuit is one NET/ROM transport connection.  Its methods must be called on
// the Router's goroutine.
type Circuit struct {
	r       *Router
	handler CircuitHandler

	myIndex, myID     byte
	yourIndex, yourID byte

	local    string // Our node address for this circuit.
	remote   string // The far end's node.
	user     string // Whose session it is.
	userNode string // The node the user is on, from the connect request.
	incoming bool

	state   CircuitState
	window  int
	timeout time.Duration

	vs, va, vr, vl byte // Next to send, oldest unacked, next expected, last acked to the far end.

	queue   []segment     // Not yet sent.
	unacked []sentSegment // Sent, not yet acknowledged, oldest first.
	reseq   map[byte]segment
	partial []byte // A message arriving in more than one packet.

	peerBusy   bool
	busyUntil  time.Time
	ackDue     time.Time // Zero for no acknowledgement waiting to go.
	nakSent    bool
	retries    int
	retryDue   time.Time
	lastActive time.Time
	started    time.Time

	sentBytes, receivedBytes int
}

// CircuitInfo is a snapshot of a circuit, for showing.
type CircuitInfo struct {
	ID             string // This node's circuit index and ID, which Router.CloseCircuit takes.
	Local, Remote  string
	User, UserNode string
	Incoming       bool
	State          CircuitState
	Window         int
	Unacked        int
	Queued         int
	Started        time.Time
	Sent, Received int
}

// Info returns a snapshot of c.
func (c *Circuit) Info() CircuitInfo {
	return CircuitInfo{
		ID:       circuitID(c.myIndex, c.myID),
		Local:    c.local,
		Remote:   c.remote,
		User:     c.user,
		UserNode: c.userNode,
		Incoming: c.incoming,
		State:    c.state,
		Window:   c.window,
		Unacked:  len(c.unacked),
		Queued:   len(c.queue),
		Started:  c.started,
		Sent:     c.sentBytes,
		Received: c.receivedBytes,
	}
}

func circuitID(index byte, id byte) string {
	return fmt.Sprintf("%02X%02X", index, id)
}

// Remote returns the far end's node.
func (c *Circuit) Remote() string { return c.remote }

// User returns whose session the circuit carries.
func (c *Circuit) User() string { return c.user }

// UserNode returns the node the user is on.
func (c *Circuit) UserNode() string { return c.userNode }

// State returns where the circuit is in its life.
func (c *Circuit) State() CircuitState { return c.state }

// SetHandler changes who is told what happens on c.
func (c *Circuit) SetHandler(h CircuitHandler) { c.handler = h }

// Write queues data to go to the far end, in as many information packets as
// it takes.
func (c *Circuit) Write(data []byte) error {
	if c.state == CircuitClosed || c.state == CircuitDisconnecting {
		return ErrClosed
	}

	for len(data) > 0 {
		var n = min(len(data), MaxInfoLen)
		c.queue = append(c.queue, segment{data: append([]byte(nil), data[:n]...), more: n < len(data)})
		data = data[n:]
	}

	c.lastActive = c.r.now
	c.kick()
	c.r.deliverLooped()

	return nil
}

// Close starts an orderly disconnect.
func (c *Circuit) Close() {
	switch c.state {
	case CircuitConnecting:
		// Nothing to say goodbye to yet; a late ack is ignored.
		c.finish(nil)
	case CircuitConnected:
		c.state = CircuitDisconnecting
		c.retries = 0
		c.retryDue = c.r.now.Add(c.timeout)
		c.sendControl(OpDisconnectRequest)
	case CircuitDisconnecting, CircuitClosed:
	}

	c.r.deliverLooped()
}

// kick sends whatever queued data the window allows.
func (c *Circuit) kick() {
	if c.state != CircuitConnected {
		return
	}

	for len(c.queue) > 0 && !c.peerBusy && len(c.unacked) < c.window {
		var s = sentSegment{segment: c.queue[0], seq: c.vs}
		c.queue = c.queue[1:]
		c.vs++

		if len(c.unacked) == 0 {
			c.retryDue = c.r.now.Add(c.timeout)
		}

		c.unacked = append(c.unacked, s)
		c.sendInfo(s)
	}
}

func (c *Circuit) sendInfo(s sentSegment) {
	var flags byte
	if s.more {
		flags |= FlagMore
	}

	c.sentBytes += len(s.data)
	c.acked()
	c.r.send(c.local, c.remote, transport(c.yourIndex, c.yourID, s.seq, c.vr, OpInfo, flags, s.data))
}

// sendControl sends one of the packets that carry no sequence numbers.
func (c *Circuit) sendControl(op Opcode) {
	c.r.send(c.local, c.remote, transport(c.yourIndex, c.yourID, 0, 0, op, 0, nil))
}

func (c *Circuit) sendInfoAck(flags byte) {
	c.acked()
	c.r.send(c.local, c.remote, transport(c.yourIndex, c.yourID, 0, c.vr, OpInfoAck, flags, nil))
}

// acked notes that the far end is about to be told everything up to vr.
func (c *Circuit) acked() {
	c.vl = c.vr
	c.ackDue = time.Time{}
}

func (c *Circuit) sendConnectRequest() {
	var cr = ConnectRequest{
		Window:  c.window,
		User:    c.user,
		Node:    c.userNode,
		Timeout: secondsOf(c.timeout),
	}

	var payload, err = cr.Encode()
	if err != nil {
		c.finish(err)

		return
	}

	c.r.send(c.local, c.remote, transport(c.myIndex, c.myID, 0, 0, OpConnectRequest, 0, payload))
}

// receive handles a packet for this circuit.
func (c *Circuit) receive(p Packet) {
	c.lastActive = c.r.now

	switch c.state {
	case CircuitConnecting:
		c.receiveConnecting(p)
	case CircuitDisconnecting:
		c.receiveDisconnecting(p)
	case CircuitConnected:
		c.receiveConnected(p)
	case CircuitClosed:
	}
}

func (c *Circuit) receiveConnecting(p Packet) {
	if p.Opcode != OpConnectAck {
		if p.Opcode == OpReset {
			c.finish(ErrReset)
		}

		return
	}

	if p.Flags&FlagChoke != 0 {
		c.finish(ErrRefused)

		return
	}

	c.yourIndex = p.TxSeq
	c.yourID = p.RxSeq

	if len(p.Payload) > 0 && p.Payload[0] > 0 {
		c.window = min(c.window, int(p.Payload[0]))
	}

	c.state = CircuitConnected
	c.retries = 0
	c.r.emit(circuitEvent(EventCircuitUp, c, nil))
	c.handler.Connected(c)
	c.kick()
}

func (c *Circuit) receiveDisconnecting(p Packet) {
	switch p.Opcode {
	case OpDisconnectRequest:
		c.sendControl(OpDisconnectAck)
		c.finish(nil)
	case OpDisconnectAck:
		c.finish(nil)
	case OpConnectAck:
		if p.Flags&FlagChoke != 0 {
			c.finish(ErrReset)
		}
	case OpReset:
		c.finish(ErrReset)
	case OpProtocolExtension, OpConnectRequest, OpInfo, OpInfoAck:
	}
}

func (c *Circuit) receiveConnected(p Packet) {
	switch p.Opcode {
	case OpConnectRequest:
		// Our ack went missing; say it again.
		c.r.sendConnectAck(c)
	case OpDisconnectRequest:
		c.sendControl(OpDisconnectAck)
		c.finish(nil)
	case OpDisconnectAck, OpReset:
		c.finish(ErrReset)
	case OpConnectAck:
		if p.Flags&FlagChoke != 0 {
			c.finish(ErrReset)
		}
	case OpInfoAck:
		c.receiveAck(p)
	case OpInfo:
		c.receiveAck(p)

		if c.state == CircuitConnected {
			c.receiveInfo(p)
		}
	case OpProtocolExtension:
	}

	c.kick()
}

// receiveAck takes in the flow control and acknowledgement every information
// packet and information ack carries.
func (c *Circuit) receiveAck(p Packet) {
	if p.Flags&FlagChoke != 0 {
		c.peerBusy = true
		c.busyUntil = c.r.now.Add(c.r.cfg.BusyDelay)
	} else {
		c.peerBusy = false
	}

	var nr = p.RxSeq
	if nr-c.va > c.vs-c.va {
		return // Acknowledges something never sent.
	}

	var count = int(nr - c.va)
	if count > 0 {
		c.unacked = c.unacked[count:]
		c.va = nr
		c.retries = 0
		c.retryDue = c.r.now.Add(c.timeout)
	}

	if p.Flags&FlagNAK != 0 && len(c.unacked) > 0 {
		c.sendInfo(c.unacked[0])
	}
}

func (c *Circuit) receiveInfo(p Packet) {
	var ns = p.TxSeq

	switch {
	case ns == c.vr:
		c.accept(segment{data: p.Payload, more: p.Flags&FlagMore != 0})
		c.vr++
		c.nakSent = false

		// Anything that arrived early and is now next.
		for {
			var s, ok = c.reseq[c.vr]
			if !ok {
				break
			}

			delete(c.reseq, c.vr)
			c.accept(s)
			c.vr++
		}
	case ns-c.vr < byte(c.window&0xff):
		// Early: keep it, and ask once for what is missing.
		if c.reseq == nil {
			c.reseq = make(map[byte]segment)
		}

		c.reseq[ns] = segment{data: p.Payload, more: p.Flags&FlagMore != 0}

		if !c.nakSent {
			c.nakSent = true
			c.sendInfoAck(FlagNAK)

			return
		}
	default:
		// A repeat of something already had: our ack was lost.
		c.sendInfoAck(0)

		return
	}

	if c.state != CircuitConnected {
		return
	}

	if c.vr-c.vl >= byte(c.window&0xff) {
		c.sendInfoAck(0) // The window is full; ack now.
	} else if c.ackDue.IsZero() {
		c.ackDue = c.r.now.Add(c.r.cfg.AckDelay)
	}
}

// accept takes in one in-order segment, handing a message over once its last
// segment is in.
func (c *Circuit) accept(s segment) {
	c.receivedBytes += len(s.data)
	c.partial = append(c.partial, s.data...)

	if s.more && len(c.partial) < maxReassembly {
		return
	}

	var data = c.partial
	c.partial = nil

	if len(data) > 0 {
		c.handler.Received(c, data)
	}
}

// maxReassembly is the most a message in several packets is allowed to grow to
// before it is handed over anyway, so a far end that never stops saying "more"
// cannot run us out of memory.
const maxReassembly = 64 * 1024

// tick runs the circuit's timers.
func (c *Circuit) tick() {
	var now = c.r.now

	switch c.state {
	case CircuitConnecting, CircuitDisconnecting:
		if now.Before(c.retryDue) {
			return
		}

		c.retries++
		if c.retries > c.r.cfg.Retries {
			c.finish(ErrTimeout)

			return
		}

		c.retryDue = now.Add(c.timeout)

		if c.state == CircuitConnecting {
			c.sendConnectRequest()
		} else {
			c.sendControl(OpDisconnectRequest)
		}
	case CircuitConnected:
		c.tickConnected(now)
	case CircuitClosed:
	}
}

func (c *Circuit) tickConnected(now time.Time) {
	if c.peerBusy && !now.Before(c.busyUntil) {
		c.peerBusy = false
		c.kick()
	}

	if len(c.unacked) > 0 && !now.Before(c.retryDue) {
		c.retries++
		if c.retries > c.r.cfg.Retries {
			c.sendControl(OpDisconnectRequest)
			c.finish(ErrTimeout)

			return
		}

		c.retryDue = now.Add(c.timeout)

		for _, s := range c.unacked {
			c.sendInfo(s)
		}
	}

	if !c.ackDue.IsZero() && !now.Before(c.ackDue) {
		c.sendInfoAck(0)
	}

	if c.r.cfg.IdleTimeout > 0 && now.Sub(c.lastActive) >= c.r.cfg.IdleTimeout {
		c.state = CircuitDisconnecting
		c.retries = 0
		c.retryDue = now.Add(c.timeout)
		c.sendControl(OpDisconnectRequest)
		c.handler.Closed(c, ErrIdle)
		c.handler = nopHandler{}
	}
}

// finish closes c for good and tells its handler why.
func (c *Circuit) finish(err error) {
	if c.state == CircuitClosed {
		return
	}

	c.state = CircuitClosed
	c.r.forget(c, err)
	c.handler.Closed(c, err)
}

// secondsOf returns d in whole seconds, as a connect request carries it.
func secondsOf(d time.Duration) maybe.Maybe[int] {
	return maybe.Just(min(int(d/time.Second), 0xffff))
}

type nopHandler struct{}

func (nopHandler) Connected(*Circuit)        {}
func (nopHandler) Received(*Circuit, []byte) {}
func (nopHandler) Closed(*Circuit, error)    {}
