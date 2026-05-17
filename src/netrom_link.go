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
	mu          sync.Mutex
	localIdx    byte   // our circuit index (used in frames we send to the remote).
	localID     byte   // our circuit ID.
	remoteIdx   byte   // remote's circuit index (used in frames remote sends to us).
	remoteID    byte   // remote's circuit ID.
	remoteNode  string // remote NET/ROM node callsign.
	localNode   string // our node callsign.
	remoteCall  string // originating user/callsign (for display and AGW callbacks).
	localCall   string
	channel     int
	client      int
	state       netromCircuitState
	vs          byte // V(S): next send sequence number.
	vr          byte // V(R): next expected receive sequence number.
	va          byte // V(A): last acknowledged send sequence number.
	window      byte
	t1          *time.Timer
	rc          int // retry counter.
	sendQueue   [][]byte
	outstanding [256][]byte // unacknowledged outbound frames indexed by N(S).
	recvBuf     []byte      // partial reassembly buffer for MORE frames.
	mgr         *netromLinkManager
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
	return &netromLinkManager{
		mu:        sync.Mutex{},
		circuits:  nil,
		nodeCall:  nodeCall,
		nodeAlias: nodeAlias,
		nextIdx:   1,
		nextID:    1,
	}
}

// allocCircuit creates and registers a new circuit, returning it locked.
func (m *netromLinkManager) allocCircuit() *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	var c = new(netromCircuit)
	c.localIdx = m.nextIdx
	c.localID = m.nextID
	c.window = NETROM_WINDOW_DEFAULT
	c.state = nrStateDisconnected
	c.mgr = m

	m.nextIdx++
	if m.nextIdx == 0 {
		m.nextIdx = 1
	}
	m.nextID++
	if m.nextID == 0 {
		m.nextID = 1
	}

	m.circuits = append(m.circuits, c)
	return c
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

func (m *netromLinkManager) findByCallsigns(channel int, localCall, remoteCall string) *netromCircuit {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, c := range m.circuits {
		if c.channel == channel && c.localCall == localCall && c.remoteCall == remoteCall {
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

// connectRequest initiates an outbound NET/ROM connection.
func (m *netromLinkManager) connectRequest(channel, client int, dstNode string, router *netromRouter) {
	var route, ok = router.lookup(dstNode)
	if !ok {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("NET/ROM: no route to %s\n", dstNode)
		return
	}

	var c = m.allocCircuit()
	c.mu.Lock()
	c.localNode = m.nodeCall
	c.remoteNode = dstNode
	c.localCall = m.nodeCall
	c.remoteCall = dstNode
	c.channel = channel
	c.client = client
	c.state = nrStateAwaitingConnection
	c.window = NETROM_WINDOW_DEFAULT
	c.mu.Unlock()

	var payload = netromBuildConnect(
		dstNode, m.nodeCall, netromConfigTTL(),
		0, 0, // remote ckt fields – 0 because remote doesn't have a circuit yet.
		c.localIdx, c.localID,
		m.nodeCall, m.nodeAlias,
		dstNode, "",
		c.window,
	)
	netromTx(channel, route.neighbor, payload)

	c.mu.Lock()
	c.startT1()
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

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state != nrStateConnected {
		return
	}

	c.sendQueue = append(c.sendQueue, data)
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
func (m *netromLinkManager) rxFrame(fromChan int, f *netromTransportFrame) {
	switch f.opcode {
	case netromOpcodeConnect:
		m.rxConnect(fromChan, f)
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

func (m *netromLinkManager) rxConnect(fromChan int, f *netromTransportFrame) {
	// Allocate a circuit for the incoming connection.
	var c = m.allocCircuit()
	c.mu.Lock()
	c.localNode = m.nodeCall
	c.remoteNode = f.net.src
	c.localCall = f.dstCallsign
	c.remoteCall = f.origCallsign
	c.channel = fromChan
	c.client = -1 // will be set when an application registers for this callsign.
	c.remoteIdx = f.origIdx
	c.remoteID = f.origID
	if f.windowSize > 0 {
		c.window = f.windowSize
	}
	c.state = nrStateConnected
	c.mu.Unlock()

	// Send CONNECT ACK.
	var payload = netromBuildConnAck(
		f.net.src, m.nodeCall, netromConfigTTL(),
		f.origIdx, f.origID,
		c.localIdx, c.localID,
		c.window,
		false,
	)
	netromTx(fromChan, f.net.src, payload)

	// Notify the server/application layer if an AGW client owns this callsign.
	if c.client >= 0 {
		server_link_established(fromChan, c.client, c.remoteCall, c.localCall, true)
	}
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
	c.remoteIdx = f.acceptIdx
	c.remoteID = f.acceptID
	if f.windowSize > 0 {
		c.window = f.windowSize
	}

	if f.flags&netromFlagChoke != 0 {
		// Connection refused.
		c.state = nrStateDisconnected
		server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, false)
		m.removeCircuit(c)
		return
	}

	c.state = nrStateConnected
	c.rc = 0
	server_link_established(c.channel, c.client, c.remoteCall, c.localCall, false)
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
	netromTx(c.channel, c.remoteNode, discAck)

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
	server_link_terminated(c.channel, c.client, c.remoteCall, c.localCall, false)
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
		netromTx(c.channel, c.remoteNode, ack)
		return
	}

	c.vr++

	// Reassemble fragmented messages (MORE bit).
	var data = f.info
	if f.flags&netromFlagMore != 0 {
		c.recvBuf = append(c.recvBuf, data...)
		// Send INFO ACK for the fragment.
		var ack = netromBuildInfoAck(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr, false, false)
		netromTx(c.channel, c.remoteNode, ack)
		return
	}

	if len(c.recvBuf) > 0 {
		c.recvBuf = append(c.recvBuf, data...)
		data = c.recvBuf
		c.recvBuf = nil
	}

	// Deliver to application.
	if c.client >= 0 {
		server_rec_conn_data(c.channel, c.client, c.remoteCall, c.localCall, AX25_PID_NETROM, data)
	}

	// Send INFO ACK.
	var ack = netromBuildInfoAck(c.remoteNode, c.localNode, netromConfigTTL(), c.remoteIdx, c.remoteID, c.vr, false, false)
	netromTx(c.channel, c.remoteNode, ack)
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

	// Advance V(A) to N(R), clearing outstanding frames.
	for c.va != f.rxSeq {
		c.outstanding[c.va] = nil
		c.va++
	}

	c.rc = 0
	if c.va == c.vs {
		c.stopT1()
	}

	c.trySend()
}

// trySend transmits queued frames while the window allows. Must be called with c.mu held.
func (c *netromCircuit) trySend() {
	for len(c.sendQueue) > 0 && c.windowOpen() {
		var data = c.sendQueue[0]
		c.sendQueue = c.sendQueue[1:]

		var payload = netromBuildInfo(
			c.remoteNode, c.localNode, netromConfigTTL(),
			c.remoteIdx, c.remoteID,
			c.vs, c.vr,
			false, false, false,
			data,
		)
		c.outstanding[c.vs] = payload
		c.vs++
		netromTx(c.channel, c.remoteNode, payload)

		if c.t1 == nil {
			c.startT1()
		}
	}
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
	netromTx(c.channel, c.remoteNode, payload)
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

// t1Expired handles a T1 retransmission timeout.
func (c *netromCircuit) t1Expired() {
	c.mu.Lock()
	defer c.mu.Unlock()

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
			c.localNode, c.mgr.nodeAlias,
			c.remoteNode, "",
			c.window,
		)
		netromTx(c.channel, c.remoteNode, payload)

	case nrStateConnected:
		// Retransmit oldest unacknowledged frame.
		if c.outstanding[c.va] != nil {
			netromTx(c.channel, c.remoteNode, c.outstanding[c.va])
		}

	case nrStateAwaitingRelease:
		c.sendDisconnect()

	case nrStateDisconnected:
		// Nothing to retransmit; do not re-arm the timer.
		return
	}

	c.startT1()
}
