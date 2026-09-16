// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"sync"
	"testing"
)

// Test callsigns. Named constants (rather than repeated literals) to keep
// golangci-lint's goconst check happy given how many tests in this file need
// them.
const (
	testNodeCallQ1 = "Q1TEST"
	testNodeCallQ2 = "Q2TEST"
	testNodeCallQ3 = "Q3TEST"
	testNodeCallQ4 = "Q4TEST"
)

// registerTestCallsign registers callsign on channel for the given AGW
// client, as an application using the AGW 'X' command would, and restores the
// registration list afterwards.
func registerTestCallsign(t *testing.T, channel int, callsign string, client int) {
	t.Helper()

	reg_callsign_list = nil
	t.Cleanup(func() { reg_callsign_list = nil })

	var regE = new(dlq_item_t)
	regE._type = DLQ_REGISTER_CALLSIGN
	regE._chan = channel
	regE.addrs[0] = callsign
	regE.client = client
	dl_register_callsign(regE)
}

func TestNetromLinkManagerAllocCircuit(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c1 = mgr.allocCircuit()
	var c2 = mgr.allocCircuit()

	if c1.localIdx == c2.localIdx && c1.localID == c2.localID {
		t.Error("allocCircuit should produce distinct circuit identifiers")
	}
	if c1.localIdx == 0 || c2.localIdx == 0 {
		t.Error("circuit index should be non-zero")
	}
}

func TestNetromLinkManagerFindByLocal(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	mgr.publishCircuit(c)
	var idx = c.localIdx
	var id = c.localID

	var found = mgr.findByLocal(idx, id)
	if found != c {
		t.Error("findByLocal should return the allocated circuit")
	}

	var notFound = mgr.findByLocal(0xff, 0xff)
	if notFound != nil {
		t.Error("findByLocal should return nil for unknown index/ID")
	}
}

func TestNetromLinkManagerFindByRemote(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	c.remoteNode = testNodeCallQ2
	c.remoteIdx = 0x10
	c.remoteID = 0x20
	mgr.publishCircuit(c)

	var found = mgr.findByRemote(testNodeCallQ2, 0x10, 0x20)
	if found != c {
		t.Error("findByRemote should return the circuit matching remote node and IDs")
	}

	var notFound = mgr.findByRemote(testNodeCallQ3, 0x10, 0x20)
	if notFound != nil {
		t.Error("findByRemote should return nil for wrong remote node")
	}
}

func TestNetromLinkManagerRemoveCircuit(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	mgr.publishCircuit(c)
	var idx = c.localIdx
	var id = c.localID

	mgr.removeCircuit(c)

	var notFound = mgr.findByLocal(idx, id)
	if notFound != nil {
		t.Error("circuit should not be findable after removal")
	}
}

func TestNetromCircuitWindowOpen(t *testing.T) {
	var c = new(netromCircuit)
	c.window = 4
	c.vs = 0
	c.va = 0

	if !c.windowOpen() {
		t.Error("window should be open when no frames are outstanding")
	}

	c.vs = 4
	if c.windowOpen() {
		t.Error("window should be closed when outstanding == window size")
	}

	c.vs = 3
	if !c.windowOpen() {
		t.Error("window should be open when outstanding < window size")
	}
}

func TestNetromCircuitWindowWrapAround(t *testing.T) {
	var c = new(netromCircuit)
	c.window = 4
	c.va = 253
	c.vs = 1 // wrapped around: in-flight = 256 - 253 + 1 = 4.

	if c.windowOpen() {
		t.Error("window should be closed at window size with wrap-around")
	}

	c.vs = 0 // in-flight = 256 - 253 = 3.
	if !c.windowOpen() {
		t.Error("window should be open when in-flight < window size with wrap-around")
	}
}

func TestNetromCircuitInitialState(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var c = mgr.allocCircuit()

	if c.state != nrStateDisconnected {
		t.Errorf("new circuit should be in disconnected state, got %d", c.state)
	}
	if c.window != NETROM_WINDOW_DEFAULT {
		t.Errorf("window: got %d, want %d", c.window, NETROM_WINDOW_DEFAULT)
	}
}

// TestNetromLinkManagerConcurrentFindAndSetRemoteNoRace exercises findByRemote
// (reader) and setRemote (writer) concurrently on the same published circuit.
// Both must be synchronized under the manager lock so that `go test -race`
// stays clean; run with -race to actually catch a regression.
func TestNetromLinkManagerConcurrentFindAndSetRemoteNoRace(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	c.remoteNode = testNodeCallQ2
	mgr.publishCircuit(c)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			mgr.setRemote(c, 0x10, 0x20)
		}()
		go func() {
			defer wg.Done()
			mgr.findByRemote(testNodeCallQ2, 0x10, 0x20)
		}()
	}
	wg.Wait()
}

// TestNetromRxConnectDedupesRetransmit verifies that a retransmitted CONNECT
// REQUEST for a circuit we already accepted resends the CONNECT ACK rather
// than allocating a second, duplicate circuit.
func TestNetromRxConnectDedupesRetransmit(t *testing.T) {
	registerTestCallsign(t, 0, testNodeCallQ1, 1)

	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var payload = netromBuildConnect(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, testNodeCallQ2, "QNODE2", testNodeCallQ1, "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, testNodeCallQ3, f)
	mgr.rxConnect(0, testNodeCallQ3, f) // retransmit of the same CONNECT REQUEST.

	if len(mgr.circuits) != 1 {
		t.Errorf("expected exactly one circuit after a retransmitted CONNECT REQUEST, got %d", len(mgr.circuits))
	}
}

// TestNetromRxConnectUsesRegisteredClient verifies that an inbound circuit is
// wired up to whichever AGW client has registered the destination callsign,
// instead of being permanently orphaned with client == -1.
func TestNetromRxConnectUsesRegisteredClient(t *testing.T) {
	registerTestCallsign(t, 0, testNodeCallQ1, 1)

	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var payload = netromBuildConnect(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, testNodeCallQ2, "QNODE2", testNodeCallQ1, "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, testNodeCallQ3, f)

	if len(mgr.circuits) != 1 {
		t.Fatalf("expected one circuit, got %d", len(mgr.circuits))
	}
	if mgr.circuits[0].client != 1 {
		t.Errorf("expected inbound circuit to be assigned to registered client 1, got %d", mgr.circuits[0].client)
	}
}

// TestNetromRxConnectUnregisteredCallsignRefused verifies that a CONNECT
// REQUEST for a callsign nobody has registered is refused outright. Accepting
// it used to leave the far end believing it had a session while we discarded
// its data, and leaked a circuit that nothing ever removed.
func TestNetromRxConnectUnregisteredCallsignRefused(t *testing.T) {
	reg_callsign_list = nil
	t.Cleanup(func() { reg_callsign_list = nil })

	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var payload = netromBuildConnect(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, testNodeCallQ2, "QNODE2", testNodeCallQ1, "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, testNodeCallQ3, f)

	if len(mgr.circuits) != 0 {
		t.Errorf("expected no circuit for an unregistered callsign, got %d", len(mgr.circuits))
	}
}

// TestNetromConnectRequestStoresNextHopNeighbor verifies that outbound
// circuits remember the routed next-hop AX.25 neighbor (not the ultimate
// NET/ROM destination), so that ongoing traffic on a multi-hop circuit goes
// to the correct immediate neighbor rather than being addressed directly to
// a station that may not be reachable in one radio hop.
func TestNetromConnectRequestStoresNextHopNeighbor(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var router = newNetromRouter()
	router.routes[testNodeCallQ3] = &netromRouteEntry{
		dstCallsign: testNodeCallQ3,
		dstAlias:    "QNODE3",
		neighbor:    testNodeCallQ2, // one radio hop away; Q3TEST itself is two hops.
		quality:     200,
		obsCount:    netromObsCountInit,
	}

	mgr.connectRequest(0, 1, testNodeCallQ1, testNodeCallQ3, router)

	if len(mgr.circuits) != 1 {
		t.Fatalf("expected one circuit, got %d", len(mgr.circuits))
	}
	var c = mgr.circuits[0]
	if c.remoteNode != testNodeCallQ3 {
		t.Errorf("expected remoteNode Q3TEST, got %s", c.remoteNode)
	}
	if c.nextHop != testNodeCallQ2 {
		t.Errorf("expected nextHop to be the routed neighbor Q2TEST, not the ultimate destination; got %s", c.nextHop)
	}
}

// TestNetromRxConnectStoresNextHopFromImmediateSender verifies that an
// inbound circuit's nextHop is the AX.25 station that actually forwarded the
// CONNECT REQUEST to us, not the (possibly multi-hop-distant) NET/ROM
// originator callsign in the frame's network header.
func TestNetromRxConnectStoresNextHopFromImmediateSender(t *testing.T) {
	registerTestCallsign(t, 0, testNodeCallQ1, 1)

	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var payload = netromBuildConnect(testNodeCallQ1, testNodeCallQ3, NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, testNodeCallQ3, "QNODE3", testNodeCallQ1, "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	// Q3TEST is the NET/ROM originator, but Q2TEST is the digipeater/neighbor
	// that actually transmitted this frame to us over the radio.
	mgr.rxConnect(0, testNodeCallQ2, f)

	if len(mgr.circuits) != 1 {
		t.Fatalf("expected one circuit, got %d", len(mgr.circuits))
	}
	var c = mgr.circuits[0]
	if c.remoteNode != testNodeCallQ3 {
		t.Errorf("expected remoteNode Q3TEST, got %s", c.remoteNode)
	}
	if c.nextHop != testNodeCallQ2 {
		t.Errorf("expected nextHop to be the immediate sender Q2TEST, not the NET/ROM originator; got %s", c.nextHop)
	}
}

// TestNetromT1ExpiredNoOpOnAlreadyDisconnectedCircuit guards against a stale
// T1 callback (time.AfterFunc's Stop() does not prevent an already-started
// callback from running) reprocessing a circuit that another goroutine has
// already torn down: it must not increment the retry counter, re-notify the
// AGW client, or attempt to remove the circuit a second time.
func TestNetromT1ExpiredNoOpOnAlreadyDisconnectedCircuit(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var c = mgr.allocCircuit()
	c.remoteNode = testNodeCallQ2
	c.nextHop = testNodeCallQ2
	c.client = -1
	c.state = nrStateDisconnected
	c.rc = netromMaxRetry // would exceed the limit if incremented.
	mgr.publishCircuit(c)

	c.t1Expired()

	if c.rc != netromMaxRetry {
		t.Errorf("expected rc to stay at %d for an already-disconnected circuit, got %d", netromMaxRetry, c.rc)
	}
	if mgr.findByLocal(c.localIdx, c.localID) == nil {
		t.Error("t1Expired should not remove a circuit that is already disconnected")
	}
}

// TestNetromInitPreservesExplicitZeroQuality verifies that QUALITY 0,
// explicitly configured, is not silently overwritten by the default (192) —
// only an unconfigured quality should fall back to the default.
func TestNetromInitPreservesExplicitZeroQuality(t *testing.T) {
	netromInitTestSetup(t, 0, MEDIUM_RADIO)

	var config = netromInitTestConfig(0)
	config.quality = 0
	config.qualitySet = true

	netrom_init(config)

	if saveNetromConfig == nil {
		t.Fatal("expected netrom_init to accept a radio channel")
	}

	if config.quality != 0 {
		t.Errorf("expected explicit QUALITY 0 to be preserved, got %d", config.quality)
	}
}

// TestNetromInitDefaultsUnsetQuality verifies that an unconfigured quality
// (qualitySet == false) still falls back to the default of 192.
func TestNetromInitDefaultsUnsetQuality(t *testing.T) {
	netromInitTestSetup(t, 0, MEDIUM_RADIO)

	var config = netromInitTestConfig(0)

	netrom_init(config)

	if saveNetromConfig == nil {
		t.Fatal("expected netrom_init to accept a radio channel")
	}

	if config.quality != 192 {
		t.Errorf("expected default quality 192 for unset QUALITY, got %d", config.quality)
	}
}

// TestNetromRxConnAckTransitionsToConnected verifies the happy path of an
// outbound circuit receiving a (non-CHOKE) CONNECT ACK: it adopts the
// remote's accepted idx/id and window, and moves to nrStateConnected.
func TestNetromRxConnAckTransitionsToConnected(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	c.localNode = testNodeCallQ1
	c.remoteNode = testNodeCallQ2
	c.nextHop = testNodeCallQ2
	c.state = nrStateAwaitingConnection
	mgr.publishCircuit(c)

	var payload = netromBuildConnAck(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT, c.localIdx, c.localID, 0x33, 0x44, NETROM_WINDOW_DEFAULT, false)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT ACK: %v", err)
	}

	mgr.rxConnAck(f)

	if c.state != nrStateConnected {
		t.Errorf("expected state nrStateConnected, got %d", c.state)
	}
	if c.remoteIdx != 0x33 || c.remoteID != 0x44 {
		t.Errorf("expected remoteIdx/ID 0x33/0x44, got 0x%02x/0x%02x", c.remoteIdx, c.remoteID)
	}
}

// TestNetromRxConnAckChokeRemovesCircuit verifies that a CHOKE'd CONNECT ACK
// (connection refused) tears the circuit down rather than treating it as
// connected.
func TestNetromRxConnAckChokeRemovesCircuit(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var c = mgr.allocCircuit()
	c.localNode = testNodeCallQ1
	c.remoteNode = testNodeCallQ2
	c.nextHop = testNodeCallQ2
	c.client = -1
	c.state = nrStateAwaitingConnection
	mgr.publishCircuit(c)

	var payload = netromBuildConnAck(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT, c.localIdx, c.localID, 0x33, 0x44, NETROM_WINDOW_DEFAULT, true)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT ACK: %v", err)
	}

	mgr.rxConnAck(f)

	if c.state != nrStateDisconnected {
		t.Errorf("expected state nrStateDisconnected after CHOKE, got %d", c.state)
	}
	if mgr.findByLocal(c.localIdx, c.localID) != nil {
		t.Error("expected circuit to be removed from the manager after a CHOKE'd CONNECT ACK")
	}
}

// netromConnectedTestCircuit returns a published, connected circuit on a
// fresh manager, ready for data to be sent or received on it.
func netromConnectedTestCircuit(t *testing.T) (*netromLinkManager, *netromCircuit) {
	t.Helper()

	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var c = mgr.allocCircuit()
	if c == nil {
		t.Fatal("allocCircuit returned no circuit")
	}
	c.localNode = testNodeCallQ1
	c.remoteNode = testNodeCallQ2
	c.nextHop = testNodeCallQ2
	c.localCall = testNodeCallQ1
	c.remoteCall = testNodeCallQ2
	c.client = -1
	c.state = nrStateConnected
	mgr.publishCircuit(c)

	return mgr, c
}

// TestNetromConnectRequestUsesClientCallsign is the regression test for
// outbound circuits that no AGW command could ever address: connectRequest
// used to record this node's callsign as the circuit's local callsign, while
// the AGW 'D' and 'd' handlers look a circuit up by the callsign the client
// gave as its own. Unless the two happened to be identical nothing matched,
// and every byte the client sent was dropped.
func TestNetromConnectRequestUsesClientCallsign(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var router = newNetromRouter()
	router.routes[testNodeCallQ3] = &netromRouteEntry{
		dstCallsign: testNodeCallQ3,
		dstAlias:    "QNODE3",
		neighbor:    testNodeCallQ2,
		quality:     200,
		obsCount:    netromObsCountInit,
	}

	// The client calls out as Q4TEST, which is not this node's callsign.
	mgr.connectRequest(0, 7, testNodeCallQ4, testNodeCallQ3, router)

	var c = mgr.findByCallsigns(0, testNodeCallQ4, testNodeCallQ3, 7)
	if c == nil {
		t.Fatal("the circuit cannot be found by the callsigns the client will use for it")
	}
	if c.localCall != testNodeCallQ4 {
		t.Errorf("localCall: got %q, want the client's own callsign %q", c.localCall, testNodeCallQ4)
	}
}

// TestNetromConnectRequestOriginatesFromClientCallsign checks that the far
// end is told which user is calling, not merely which node relayed it.
func TestNetromConnectRequestOriginatesFromClientCallsign(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")
	var router = newNetromRouter()
	router.routes[testNodeCallQ3] = &netromRouteEntry{
		dstCallsign: testNodeCallQ3,
		dstAlias:    "QNODE3",
		neighbor:    testNodeCallQ2,
		quality:     200,
		obsCount:    netromObsCountInit,
	}

	mgr.connectRequest(0, 7, testNodeCallQ4, testNodeCallQ3, router)

	var c = mgr.findByCallsigns(0, testNodeCallQ4, testNodeCallQ3, 7)
	if c == nil {
		t.Fatal("expected a circuit for the connect request")
	}

	// Rebuild what a T1 retransmission would send and check the originating
	// callsign it carries.
	c.mu.Lock()
	var payload = netromBuildConnect(
		c.remoteNode, c.localNode, netromConfigTTL(),
		0, 0,
		c.localIdx, c.localID,
		c.localCall, c.mgr.nodeAlias,
		c.remoteNode, "",
		c.window,
	)
	c.mu.Unlock()

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse rebuilt CONNECT REQUEST: %v", err)
	}
	if f.origCallsign != testNodeCallQ4 {
		t.Errorf("origCallsign: got %q, want the calling user %q", f.origCallsign, testNodeCallQ4)
	}
}

// TestNetromDataRequestFragments is the regression test for silent data loss:
// NETROM_MAX_INFO went unused and the MORE bit was never set on transmit, so
// anything larger than one frame was handed whole to the AX.25 layer, which
// truncates past its own limit and would in any case have been rejected by a
// peer expecting NET/ROM-sized fragments.
func TestNetromDataRequestFragments(t *testing.T) {
	var mgr, c = netromConnectedTestCircuit(t)

	// Three fragments' worth, which fits the default window.
	var data = make([]byte, NETROM_MAX_INFO*2+50)
	for i := range data {
		data[i] = byte(i)
	}

	mgr.dataRequest(c.localIdx, c.localID, data)

	c.mu.Lock()
	var sent = int(c.vs)
	c.mu.Unlock()

	if sent != 3 {
		t.Fatalf("expected 3 fragments to be sent, got %d", sent)
	}

	var reassembled []byte
	for i := range sent {
		var f, err = netromParseTransportFrame(c.outstanding[i])
		if err != nil {
			t.Fatalf("fragment %d does not parse: %v", i, err)
		}
		if len(f.info) > NETROM_MAX_INFO {
			t.Errorf("fragment %d is %d bytes, over the %d byte maximum", i, len(f.info), NETROM_MAX_INFO)
		}

		var more = f.flags&netromFlagMore != 0
		var wantMore = i < sent-1
		if more != wantMore {
			t.Errorf("fragment %d: MORE is %v, want %v", i, more, wantMore)
		}
		reassembled = append(reassembled, f.info...)
	}

	if string(reassembled) != string(data) {
		t.Error("fragments do not reassemble into the original data")
	}
}

// TestNetromDataRequestSingleFrameHasNoMoreBit checks that fragmentation does
// not put a MORE bit on a message that fits in one frame.
func TestNetromDataRequestSingleFrameHasNoMoreBit(t *testing.T) {
	var mgr, c = netromConnectedTestCircuit(t)

	mgr.dataRequest(c.localIdx, c.localID, []byte("short message"))

	var f, err = netromParseTransportFrame(c.outstanding[0])
	if err != nil {
		t.Fatalf("frame does not parse: %v", err)
	}
	if f.flags&netromFlagMore != 0 {
		t.Error("a message that fits one frame should not have the MORE bit set")
	}
}

// TestNetromReassemblyIsBounded is the regression test for an unbounded
// buffer: a peer that never clears the MORE bit could make us accumulate
// fragments until we ran out of memory.
func TestNetromReassemblyIsBounded(t *testing.T) {
	var mgr, c = netromConnectedTestCircuit(t)

	var fragment = make([]byte, NETROM_MAX_INFO)

	// Far more fragments than the cap allows.
	for range (netromMaxReassembly / NETROM_MAX_INFO) + 5 {
		c.mu.Lock()
		var seq = c.vr
		c.mu.Unlock()

		var payload = netromBuildInfo(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT,
			c.localIdx, c.localID, seq, 0, false, false, true, fragment)
		var f, err = netromParseTransportFrame(payload)
		if err != nil {
			t.Fatalf("failed to parse test INFO: %v", err)
		}
		mgr.rxInfo(f)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.recvBuf) > netromMaxReassembly {
		t.Errorf("reassembly buffer grew to %d bytes, over the %d byte cap", len(c.recvBuf), netromMaxReassembly)
	}
	if !c.recvOverflow {
		t.Error("expected the circuit to be marked as having overflowed reassembly")
	}
}

// TestNetromReassemblyRecoversAfterOverflow checks that a circuit that gave
// up on one oversized message can still receive the next one.
func TestNetromReassemblyRecoversAfterOverflow(t *testing.T) {
	var mgr, c = netromConnectedTestCircuit(t)

	c.mu.Lock()
	c.recvOverflow = true
	var seq = c.vr
	c.mu.Unlock()

	// The final fragment of the abandoned message.
	var payload = netromBuildInfo(testNodeCallQ1, testNodeCallQ2, NETROM_TTL_DEFAULT,
		c.localIdx, c.localID, seq, 0, false, false, false, []byte("tail"))
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test INFO: %v", err)
	}
	mgr.rxInfo(f)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.recvOverflow {
		t.Error("overflow state should be cleared once the message ends")
	}
	if len(c.recvBuf) != 0 {
		t.Errorf("reassembly buffer should be empty for the next message, holds %d bytes", len(c.recvBuf))
	}
}

// TestNetromAllocCircuitIDDistinguishesIncarnations is the regression test
// for a circuit ID that carried no information: the index and the ID were
// incremented together, so the ID was always equal to the index and could not
// tell one use of an index from the next.
func TestNetromAllocCircuitIDDistinguishesIncarnations(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var differs = false
	for range 300 {
		var c = mgr.allocCircuit()
		if c == nil {
			t.Fatal("allocCircuit ran out of circuits unexpectedly")
		}
		if c.localIdx != c.localID {
			differs = true
		}
	}

	if !differs {
		t.Error("the circuit ID never differed from the index, so it cannot distinguish incarnations of an index")
	}
}

// TestNetromAllocCircuitSkipsLiveIdentifiers checks that allocation never
// hands out an idx/id pair a live circuit still holds, which would let frames
// for the old session be delivered to the new one.
func TestNetromAllocCircuitSkipsLiveIdentifiers(t *testing.T) {
	var mgr = newNetromLinkManager(testNodeCallQ1, "QNODE1")

	var first = mgr.allocCircuit()
	if first == nil {
		t.Fatal("allocCircuit returned no circuit")
	}
	mgr.publishCircuit(first)

	// Wind the counters back to where they were, as a wrap eventually does.
	mgr.mu.Lock()
	mgr.nextIdx = first.localIdx
	mgr.nextID = first.localID
	mgr.mu.Unlock()

	var second = mgr.allocCircuit()
	if second == nil {
		t.Fatal("allocCircuit returned no circuit")
	}

	if second.localIdx == first.localIdx && second.localID == first.localID {
		t.Error("allocCircuit handed out an idx/id pair that a live circuit still holds")
	}
}
