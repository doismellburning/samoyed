// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
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
