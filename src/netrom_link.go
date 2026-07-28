// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"sync"
	"time"
)

// netromMaxRetry is the maximum number of retransmission attempts before giving up.
const netromMaxRetry = 10

// netromT1Duration is the default retransmission timeout.
const netromT1Duration = 30 * time.Second

// netromMaxReassembly bounds the MORE-fragment reassembly buffer. A peer that
// never clears the MORE bit would otherwise grow it without limit, and
// server_rec_conn_data cannot pass more than this to a client anyway.
const netromMaxReassembly = AX25_MAX_INFO_LEN

// netromCircuitState is the state of a single NET/ROM transport circuit.
type netromCircuitState int

const (
	nrStateDisconnected       netromCircuitState = 0
	nrStateAwaitingConnection netromCircuitState = 1
	nrStateConnected          netromCircuitState = 2
	nrStateAwaitingRelease    netromCircuitState = 3
)

// netromCircuit holds the state for one NET/ROM transport circuit.
type netromCircuit struct {
	mu           sync.Mutex
	localIdx     byte   // our circuit index (used in frames we send to the remote).
	localID      byte   // our circuit ID.
	remoteIdx    byte   // remote's circuit index (used in frames remote sends to us).
	remoteID     byte   // remote's circuit ID.
	remoteNode   string // remote NET/ROM node callsign.
	nextHop      string // immediate AX.25 neighbor to use for netromTx on this circuit.
	localNode    string // our node callsign.
	remoteCall   string // originating user/callsign (for display and AGW callbacks).
	localCall    string
	channel      int
	client       int
	state        netromCircuitState
	vs           byte // V(S): next send sequence number.
	vr           byte // V(R): next expected receive sequence number.
	va           byte // V(A): last acknowledged send sequence number.
	window       byte
	t1           *time.Timer
	rc           int // retry counter.
	sendQueue    []netromSendItem
	outstanding  [256][]byte // unacknowledged outbound frames indexed by N(S).
	recvBuf      []byte      // partial reassembly buffer for MORE frames.
	recvOverflow bool        // reassembly exceeded netromMaxReassembly; discard until the final fragment.
	mgr          *netromLinkManager
}

// netromSendItem is one fragment waiting to be sent, along with whether more
// of the same application message follow it.
type netromSendItem struct {
	data []byte
	more bool
}

// netromLinkManager manages all active NET/ROM transport circuits.
type netromLinkManager struct {
	mu        sync.Mutex
	circuits  []*netromCircuit
	nextIdx   byte
	nextID    byte
	nodeCall  string
	nodeAlias string
}

func newNetromLinkManager(nodeCall, nodeAlias string) *netromLinkManager {
	var m = new(netromLinkManager)
	m.nodeCall = nodeCall
	m.nodeAlias = nodeAlias
	m.nextIdx = 1
	m.nextID = 1

	return m
}

// allocCircuit reserves a fresh local idx/id and returns a new circuit that is
// not yet visible to lookups. Call publishCircuit once its identity fields
// (channel, localCall, remoteCall, remoteNode, remoteIdx, remoteID) are set,
// so that findByLocal/findByCallsigns/findByRemote never observe a partially
// initialised circuit.
//
// The index counts every allocation and the ID counts index wraps, so the ID
// distinguishes successive incarnations of the same index — which is the
// whole point of carrying both in the protocol. A pair still held by a live
// circuit is skipped, so a frame for an old session can never be delivered to
// a new one. Returns nil if every pair is in use.
func (m *netromLinkManager) allocCircuit() *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	var idx, id, ok = m.nextFreeIDLocked()
	if !ok {
		return nil
	}

	var c = new(netromCircuit)
	c.localIdx = idx
	c.localID = id
	c.window = NETROM_WINDOW_DEFAULT
	c.state = nrStateDisconnected
	c.mgr = m

	return c
}

// nextFreeIDLocked advances the allocation counters until they name an
// idx/id pair no live circuit holds. Must be called with m.mu held.
func (m *netromLinkManager) nextFreeIDLocked() (byte, byte, bool) {
	// 0 is skipped for both, so the space is 255*255 pairs.
	for range 255 * 255 {
		var idx, id = m.nextIdx, m.nextID

		m.nextIdx++
		if m.nextIdx == 0 {
			m.nextIdx = 1
			m.nextID++
			if m.nextID == 0 {
				m.nextID = 1
			}
		}

		var inUse = false
		for _, c := range m.circuits {
			if c.localIdx == idx && c.localID == id {
				inUse = true

				break
			}
		}
		if !inUse {
			return idx, id, true
		}
	}

	return 0, 0, false
}

// publishCircuit makes c visible to findByLocal/findByCallsigns/findByRemote.
func (m *netromLinkManager) publishCircuit(c *netromCircuit) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.circuits = append(m.circuits, c)
}

// setRemote updates a circuit's remote idx/id, which findByRemote uses as
// lookup keys, under the manager lock rather than c.mu so that finder reads
// and this write are synchronized by the same mutex.
func (m *netromLinkManager) setRemote(c *netromCircuit, idx, id byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c.remoteIdx = idx
	c.remoteID = id
}

func (m *netromLinkManager) findByLocal(idx, id byte) *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.circuits {
		if c.localIdx == idx && c.localID == id {
			return c
		}
	}

	return nil
}

// findByCallsigns returns the circuit this client owns between localCall and
// remoteCall on channel, or nil. Matching on the client too keeps one AGW
// client's commands from reaching another's circuit — and keeps a plain
// AX.25 disconnect from being mistaken for a NET/ROM one.
func (m *netromLinkManager) findByCallsigns(channel int, localCall, remoteCall string, client int) *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.circuits {
		if c.channel == channel && c.client == client &&
			c.localCall == localCall && c.remoteCall == remoteCall {
			return c
		}
	}

	return nil
}

func (m *netromLinkManager) findByRemote(remoteNode string, idx, id byte) *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.circuits {
		if c.remoteNode == remoteNode && c.remoteIdx == idx && c.remoteID == id {
			return c
		}
	}

	return nil
}

func (m *netromLinkManager) removeCircuit(c *netromCircuit) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, existing := range m.circuits {
		if existing == c {
			m.circuits = append(m.circuits[:i], m.circuits[i+1:]...)

			return
		}
	}
}

// connectRequest initiates an outbound NET/ROM connection. localCall is the
// callsign the requesting client uses as its own; it is what the client will
// name in subsequent AGW commands for this circuit, and what the far end sees
// as the originating user.
func (m *netromLinkManager) connectRequest(channel, client int, localCall, dstNode string, router *netromRouter) {
	var route, ok = router.lookup(dstNode)
	if !ok {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: no route to %s\n", dstNode)
		server_link_terminated(channel, client, dstNode, localCall, false)

		return
	}

	var c = m.allocCircuit()
	if c == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: no free circuit for connection to %s\n", dstNode)
		server_link_terminated(channel, client, dstNode, localCall, false)

		return
	}

	c.mu.Lock()
	c.localNode = m.nodeCall
	c.remoteNode = dstNode
	c.nextHop = route.neighbor
	c.localCall = localCall
	c.remoteCall = dstNode
	c.channel = channel
	c.client = client
	c.state = nrStateAwaitingConnection
	c.window = NETROM_WINDOW_DEFAULT
	c.mu.Unlock()

	m.publishCircuit(c)

	var payload = netromBuildConnect(
		dstNode, m.nodeCall, netromConfigTTL(),
		0, 0, // remote ckt fields – 0 because remote doesn't have a circuit yet.
		c.localIdx, c.localID,
		localCall, m.nodeAlias,
		dstNode, "",
		c.window,
	)
	netromTx(channel, route.neighbor, payload)

	c.mu.Lock()
	// The CONNECT ACK may already have arrived and transitioned the circuit
	// while netromTx was in flight; only arm T1 if we're still waiting.
	if c.state == nrStateAwaitingConnection {
		c.startT1()
	}
	c.mu.Unlock()
}

// dataRequest sends connected data on a circuit identified by localIdx/localID.
func (m *netromLinkManager) dataRequest(localIdx, localID byte, data []byte) {
	var c = m.findByLocal(localIdx, localID)
	if c == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: dataRequest: no circuit for idx=0x%02x id=0x%02x\n", localIdx, localID)

		return
	}

	if len(data) > netromMaxReassembly {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: dataRequest: %d bytes exceeds the %d byte maximum; not sent.\n",
			len(data), netromMaxReassembly)

		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nrStateConnected {
		return
	}

	// Split into fragments the far end can accept, with MORE set on every
	// one but the last so it can reassemble them.
	for start := 0; start < len(data); start += NETROM_MAX_INFO {
		var end = min(start+NETROM_MAX_INFO, len(data))
		var item = netromSendItem{data: data[start:end], more: end < len(data)}
		c.sendQueue = append(c.sendQueue, item)
	}
	c.trySend()
}

// disconnectRequest initiates a disconnect on a circuit.
func (m *netromLinkManager) disconnectRequest(localIdx, localID byte) {
	var c = m.findByLocal(localIdx, localID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == nrStateDisconnected || c.state == nrStateAwaitingRelease {
		return
	}

	c.stopT1()
	c.state = nrStateAwaitingRelease
	c.rc = 0
	c.sendDisconnect()
	c.startT1()
}

// rxFrame is the main entry point for received NET/ROM transport frames.
// fromNeighbor is the immediate AX.25 source of the frame (the radio
// neighbor that sent it to us, which may be a digipeater hop away from the
// frame's ultimate NET/ROM origin).
func (m *netromLinkManager) rxFrame(fromChan int, fromNeighbor string, f *netromTransportFrame) {
	switch f.opcode {
	case netromOpcodeConnect:
		m.rxConnect(fromChan, fromNeighbor, f)
	case netromOpcodeConnAck:
		m.rxConnAck(f)
	case netromOpcodeDisconnect:
		m.rxDisconnect(f)
	case netromOpcodeDiscAck:
		m.rxDiscAck(f)
	case netromOpcodeInfo:
		m.rxInfo(f)
	case netromOpcodeInfoAck:
		m.rxInfoAck(f)
	}
}

// refuseConnect answers a CONNECT REQUEST we cannot accept with a CHOKE'd
// CONNECT ACK, which the originator reads as "connection refused".
func (m *netromLinkManager) refuseConnect(fromChan int, fromNeighbor string, f *netromTransportFrame) {
	var payload = netromBuildConnAck(
		f.net.src, m.nodeCall, netromConfigTTL(),
		f.origIdx, f.origID,
		0, 0, // no circuit accepted.
		NETROM_WINDOW_DEFAULT,
		true,
	)
	netromTx(fromChan, fromNeighbor, payload)
}

func (m *netromLinkManager) rxConnect(fromChan int, fromNeighbor string, f *netromTransportFrame) {
	// A retransmitted CONNECT REQUEST (e.g. our CONNECT ACK was lost) should
	// not allocate a second circuit for the same remote circuit; just resend
	// the ACK for the circuit we already have.
	if existing := m.findByRemote(f.net.src, f.origIdx, f.origID); existing != nil {
		existing.mu.Lock()
		var ackPayload = netromBuildConnAck(
			f.net.src, m.nodeCall, netromConfigTTL(),
			f.origIdx, f.origID,
			existing.localIdx, existing.localID,
			existing.window,
			false,
		)
		var nextHop = existing.nextHop
		existing.mu.Unlock()
		netromTx(fromChan, nextHop, ackPayload)

		return
	}

	// Nobody is listening on the called callsign, so there is nothing to
	// connect to. Refusing says so; accepting would leave the far end
	// believing it had a session while we dropped its data on the floor,
	// and would leak a circuit that nothing ever tears down.
	var client = find_registered_client(fromChan, f.dstCallsign)
	if client < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: CONNECT REQUEST for %s from %s refused: no client registered for that callsign.\n",
			f.dstCallsign, f.origCallsign)
		m.refuseConnect(fromChan, fromNeighbor, f)

		return
	}

	// Allocate a circuit for the incoming connection.
	var c = m.allocCircuit()
	if c == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: CONNECT REQUEST for %s refused: no free circuit.\n", f.dstCallsign)
		m.refuseConnect(fromChan, fromNeighbor, f)

		return
	}

	c.mu.Lock()
	c.localNode = m.nodeCall
	c.remoteNode = f.net.src
	c.nextHop = fromNeighbor
	c.localCall = f.dstCallsign
	c.remoteCall = f.origCallsign
	c.channel = fromChan
	c.client = client
	c.remoteIdx = f.origIdx
	c.remoteID = f.origID
	if f.windowSize > 0 {
		c.window = f.windowSize
	}
	c.state = nrStateConnected
	c.mu.Unlock()

	m.publishCircuit(c)

	// Send CONNECT ACK to the immediate neighbor that forwarded this request,
	// not the (possibly multi-hop-distant) NET/ROM origin callsign.
	var payload = netromBuildConnAck(
		f.net.src, m.nodeCall, netromConfigTTL(),
		f.origIdx, f.origID,
		c.localIdx, c.localID,
		c.window,
		false,
	)
	netromTx(fromChan, fromNeighbor, payload)

	// Notify the AGW client that registered this callsign; a request with no
	// owning client was refused above, so there is always one here.
	server_link_established(fromChan, c.client, c.remoteCall, c.localCall, true)
}

func (m *netromLinkManager) rxConnAck(f *netromTransportFrame) {
	var c = m.findByLocal(f.cktIdx, f.cktID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nrStateAwaitingConnection {
		return
	}

	c.stopT1()
	m.setRemote(c, f.acceptIdx, f.acceptID)
	if f.windowSize > 0 {
		c.window = f.windowSize
	}

	if f.flags&netromFlagChoke != 0 {
		// Connection refused.
		c.state = nrStateDisconnected
		if c.client >= 0 {
			server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, false)
		}
		m.removeCircuit(c)

		return
	}

	c.state = nrStateConnected
	c.rc = 0
	if c.client >= 0 {
		server_link_established(c.channel, c.client, c.remoteCall, c.localCall, false)
	}
	c.trySend()
}

func (m *netromLinkManager) rxDisconnect(f *netromTransportFrame) {
	var c = m.findByLocal(f.cktIdx, f.cktID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopT1()

	var discAck = netromBuildDiscAck(
		c.remoteNode, c.localNode, netromConfigTTL(),
		c.remoteIdx, c.remoteID,
	)
	netromTx(c.channel, c.nextHop, discAck)

	var wasConnected = c.state == nrStateConnected || c.state == nrStateAwaitingConnection || c.state == nrStateAwaitingRelease
	c.state = nrStateDisconnected
	if wasConnected && c.client >= 0 {
		server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, false)
	}
	m.removeCircuit(c)
}

func (m *netromLinkManager) rxDiscAck(f *netromTransportFrame) {
	var c = m.findByLocal(f.cktIdx, f.cktID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.stopT1()
	c.state = nrStateDisconnected
	if c.client >= 0 {
		server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, false)
	}
	m.removeCircuit(c)
}

func (m *netromLinkManager) rxInfo(f *netromTransportFrame) {
	var c = m.findByLocal(f.cktIdx, f.cktID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nrStateConnected {
		return
	}

	// Validate sequence.
	if f.txSeq != c.vr {
		// Out-of-sequence: send NAK INFO ACK.
		var ack = netromBuildInfoAck(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr, false, true)
		netromTx(c.channel, c.nextHop, ack)

		return
	}

	c.vr++

	// Reassemble fragmented messages (MORE bit).
	var data = f.info
	if f.flags&netromFlagMore != 0 {
		c.appendFragment(data)
		// Send INFO ACK for the fragment. Keep acknowledging even a message
		// we have given up reassembling, so the peer is not left
		// retransmitting a fragment we will never accept.
		var ack = netromBuildInfoAck(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr, false, false)
		netromTx(c.channel, c.nextHop, ack)

		return
	}

	var overflowed = c.recvOverflow
	if len(c.recvBuf) > 0 || overflowed {
		c.appendFragment(data)
		data = c.recvBuf
		c.recvBuf = nil
		c.recvOverflow = false
	}

	// Deliver to application, unless the message grew past what we are
	// willing to hold and was discarded.
	if overflowed {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: reassembled message from %s exceeded %d bytes; discarded.\n",
			c.remoteCall, netromMaxReassembly)
	} else if c.client >= 0 {
		server_rec_conn_data(c.channel, c.client, c.remoteCall, c.localCall, AX25_PID_NETROM, data)
	}

	// Send INFO ACK.
	var ack = netromBuildInfoAck(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr, false, false)
	netromTx(c.channel, c.nextHop, ack)
}

func (m *netromLinkManager) rxInfoAck(f *netromTransportFrame) {
	var c = m.findByLocal(f.cktIdx, f.cktID)
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nrStateConnected {
		return
	}

	// Validate that N(R) acknowledges only frames we have actually sent.
	// toAck and inFlight are computed mod 256; if toAck > inFlight the ACK
	// refers to frames beyond V(S), which is invalid — discard silently.
	var toAck = int(f.rxSeq-c.va) & 0xff
	var inFlight = int(c.vs-c.va) & 0xff
	if toAck > inFlight {
		return
	}

	// Advance V(A) to N(R), clearing outstanding frames.
	for c.va != f.rxSeq {
		c.outstanding[c.va] = nil
		c.va++
	}

	c.rc = 0
	if c.va == c.vs {
		c.stopT1()
	}

	// NAK: fast-retransmit the oldest unacknowledged frame.
	if f.flags&netromFlagNAK != 0 && c.outstanding[c.va] != nil {
		netromTx(c.channel, c.nextHop, c.outstanding[c.va])
	}

	// CHOKE: remote is busy; do not send more data now.
	if f.flags&netromFlagChoke == 0 {
		c.trySend()
	}
}

// trySend transmits queued frames while the window allows. Must be called with c.mu held.
func (c *netromCircuit) trySend() {
	for len(c.sendQueue) > 0 && c.windowOpen() {
		var item = c.sendQueue[0]
		c.sendQueue = c.sendQueue[1:]

		var payload = netromBuildInfo(
			c.remoteNode, c.localNode, netromConfigTTL(),
			c.remoteIdx, c.remoteID,
			c.vs, c.vr,
			false, false, item.more,
			item.data,
		)
		c.outstanding[c.vs] = payload
		c.vs++
		netromTx(c.channel, c.nextHop, payload)

		if c.t1 == nil {
			c.startT1()
		}
	}
}

// appendFragment adds a MORE fragment to the reassembly buffer, giving up on
// the message once it would exceed netromMaxReassembly rather than letting a
// peer that never clears the MORE bit grow the buffer without limit. Must be
// called with c.mu held.
func (c *netromCircuit) appendFragment(data []byte) {
	if c.recvOverflow {
		return
	}

	if len(c.recvBuf)+len(data) > netromMaxReassembly {
		c.recvOverflow = true
		c.recvBuf = nil

		return
	}

	c.recvBuf = append(c.recvBuf, data...)
}

// windowOpen reports whether there is room to send another frame. Must be called with c.mu held.
func (c *netromCircuit) windowOpen() bool {
	var inFlight = int(c.vs) - int(c.va)
	if inFlight < 0 {
		inFlight += 256
	}

	return inFlight < int(c.window)
}

// sendDisconnect transmits a DISCONNECT REQUEST. Must be called with c.mu held.
func (c *netromCircuit) sendDisconnect() {
	var payload = netromBuildDisconnect(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr)
	netromTx(c.channel, c.nextHop, payload)
}

// startT1 arms the retransmission timer. Must be called with c.mu held.
func (c *netromCircuit) startT1() {
	if c.t1 != nil {
		c.t1.Stop()
	}
	c.t1 = time.AfterFunc(netromT1Duration, func() { c.t1Expired() })
}

// stopT1 cancels the retransmission timer. Must be called with c.mu held.
func (c *netromCircuit) stopT1() {
	if c.t1 != nil {
		c.t1.Stop()
		c.t1 = nil
	}
}

// t1Expired handles a T1 retransmission timeout. Because time.AfterFunc does
// not guarantee stopT1's Stop() call prevents an already-started callback
// from running, this can still fire just after the circuit was torn down by
// another goroutine (e.g. a disconnect or CHOKE'd CONNECT ACK racing the
// timer). Bail out immediately in that case rather than incrementing rc and
// potentially re-notifying/re-removing an already-disconnected circuit.
func (c *netromCircuit) t1Expired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == nrStateDisconnected {
		return
	}

	c.rc++
	if c.rc > netromMaxRetry {
		c.state = nrStateDisconnected
		if c.client >= 0 {
			server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, true)
		}
		c.mgr.removeCircuit(c)

		return
	}

	switch c.state {
	case nrStateAwaitingConnection:
		// Retransmit CONNECT REQUEST.
		var payload = netromBuildConnect(
			c.remoteNode, c.localNode, netromConfigTTL(),
			0, 0,
			c.localIdx, c.localID,
			c.localCall, c.mgr.nodeAlias,
			c.remoteNode, "",
			c.window,
		)
		netromTx(c.channel, c.nextHop, payload)

	case nrStateConnected:
		// Retransmit oldest unacknowledged frame.
		if c.outstanding[c.va] != nil {
			netromTx(c.channel, c.nextHop, c.outstanding[c.va])
		}

	case nrStateAwaitingRelease:
		c.sendDisconnect()

	case nrStateDisconnected:
		// Unreachable: guarded by the early return above.
	}

	c.startT1()
}
