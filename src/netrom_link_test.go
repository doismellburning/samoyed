// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"sync"
	"testing"
)

func TestNetromLinkManagerAllocCircuit(t *testing.T) {
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

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
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

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
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

	var c = mgr.allocCircuit()
	c.remoteNode = "Q2TEST"
	c.remoteIdx = 0x10
	c.remoteID = 0x20
	mgr.publishCircuit(c)

	var found = mgr.findByRemote("Q2TEST", 0x10, 0x20)
	if found != c {
		t.Error("findByRemote should return the circuit matching remote node and IDs")
	}

	var notFound = mgr.findByRemote("Q3TEST", 0x10, 0x20)
	if notFound != nil {
		t.Error("findByRemote should return nil for wrong remote node")
	}
}

func TestNetromLinkManagerRemoveCircuit(t *testing.T) {
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

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
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")
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
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

	var c = mgr.allocCircuit()
	c.remoteNode = "Q2TEST"
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
			mgr.findByRemote("Q2TEST", 0x10, 0x20)
		}()
	}
	wg.Wait()
}

// TestNetromRxConnectDedupesRetransmit verifies that a retransmitted CONNECT
// REQUEST for a circuit we already accepted resends the CONNECT ACK rather
// than allocating a second, duplicate circuit.
func TestNetromRxConnectDedupesRetransmit(t *testing.T) {
	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")

	var payload = netromBuildConnect("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, "Q2TEST", "QNODE2", "Q1TEST", "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, f)
	mgr.rxConnect(0, f) // retransmit of the same CONNECT REQUEST.

	if len(mgr.circuits) != 1 {
		t.Errorf("expected exactly one circuit after a retransmitted CONNECT REQUEST, got %d", len(mgr.circuits))
	}
}

// TestNetromRxConnectUsesRegisteredClient verifies that an inbound circuit is
// wired up to whichever AGW client has registered the destination callsign,
// instead of being permanently orphaned with client == -1.
func TestNetromRxConnectUsesRegisteredClient(t *testing.T) {
	reg_callsign_list = nil
	t.Cleanup(func() { reg_callsign_list = nil })

	var regE = new(dlq_item_t)
	regE._type = DLQ_REGISTER_CALLSIGN
	regE._chan = 0
	regE.addrs[0] = "Q1TEST"
	regE.client = 1
	dl_register_callsign(regE)

	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")
	var payload = netromBuildConnect("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, "Q2TEST", "QNODE2", "Q1TEST", "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, f)

	if len(mgr.circuits) != 1 {
		t.Fatalf("expected one circuit, got %d", len(mgr.circuits))
	}
	if mgr.circuits[0].client != 1 {
		t.Errorf("expected inbound circuit to be assigned to registered client 1, got %d", mgr.circuits[0].client)
	}
}

// TestNetromRxConnectUnregisteredCallsignStaysUnowned verifies that an
// inbound circuit for a callsign nobody has registered is left with
// client == -1 (no application to deliver data to), rather than panicking or
// guessing a client.
func TestNetromRxConnectUnregisteredCallsignStaysUnowned(t *testing.T) {
	reg_callsign_list = nil
	t.Cleanup(func() { reg_callsign_list = nil })

	var mgr = newNetromLinkManager("Q1TEST", "QNODE1")
	var payload = netromBuildConnect("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0, 0, 0x11, 0x22, "Q2TEST", "QNODE2", "Q1TEST", "QNODE1", NETROM_WINDOW_DEFAULT)
	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("failed to parse test CONNECT REQUEST: %v", err)
	}

	mgr.rxConnect(0, f)

	if len(mgr.circuits) != 1 {
		t.Fatalf("expected one circuit, got %d", len(mgr.circuits))
	}
	if mgr.circuits[0].client != -1 {
		t.Errorf("expected unregistered inbound circuit to have client == -1, got %d", mgr.circuits[0].client)
	}
}
