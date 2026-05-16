// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
)

func makeTestBroadcast(srcAlias string, entries []netromNodesEntry) *netromRoutingBroadcast {
	var bc = new(netromRoutingBroadcast)
	bc.srcAlias = netromPadAlias(srcAlias)
	bc.entries = entries
	return bc
}

func TestNetromRouterBasicUpdate(t *testing.T) {
	var r = newNetromRouter()

	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
	})
	r.processNodes(bc, "Q2TEST", 180)

	var entry, ok = r.lookup("Q3TEST")
	if !ok {
		t.Fatal("expected route to Q3TEST")
	}
	if entry.neighbor != "Q2TEST" {
		t.Errorf("neighbor: got %q, want %q", entry.neighbor, "Q2TEST")
	}
	// effective quality = min(180, 200) = 180
	if entry.quality != 180 {
		t.Errorf("quality: got %d, want 180", entry.quality)
	}
}

func TestNetromRouterBestPathSelected(t *testing.T) {
	var r = newNetromRouter()

	// Neighbor A advertises Q3TEST with quality 100, our link to A is 200.
	var bcA = makeTestBroadcast("QNODEA", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q3TEST", quality: 100},
	})
	r.update(bcA, "Q2TEST-1", 200)

	// Neighbor B advertises Q3TEST with quality 220, our link to B is 240 → eq=220.
	var bcB = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q3TEST", quality: 220},
	})
	r.update(bcB, "Q2TEST-2", 240)

	var entry, ok = r.lookup("Q3TEST")
	if !ok {
		t.Fatal("expected route to Q3TEST")
	}
	if entry.neighbor != "Q2TEST-2" {
		t.Errorf("expected better path via Q2TEST-2, got %q", entry.neighbor)
	}
	if entry.quality != 220 {
		t.Errorf("quality: got %d, want 220", entry.quality)
	}
}

func TestNetromRouterAgingAndPrune(t *testing.T) {
	var r = newNetromRouter()

	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
	})
	r.processNodes(bc, "Q2TEST", 200)

	// Confirm route exists.
	if _, ok := r.lookup("Q3TEST"); !ok {
		t.Fatal("expected route to Q3TEST after first broadcast")
	}

	// Broadcast netromObsCountPrune-1 more times without Q3TEST; route should survive.
	var emptyBc = makeTestBroadcast("QNODEB", nil)
	for range netromObsCountPrune - 1 {
		r.processNodes(emptyBc, "Q2TEST", 200)
	}
	if _, ok := r.lookup("Q3TEST"); !ok {
		t.Fatal("route should still exist after fewer than prune threshold broadcasts")
	}

	// One more broadcast without Q3TEST tips obsCount to prune threshold → should be removed.
	r.processNodes(emptyBc, "Q2TEST", 200)
	if _, ok := r.lookup("Q3TEST"); ok {
		t.Error("route should have been pruned after reaching obsCount threshold")
	}
}

func TestNetromRouterLookupMissing(t *testing.T) {
	var r = newNetromRouter()
	if _, ok := r.lookup("QMISSING"); ok {
		t.Error("expected no route for unknown destination")
	}
}

func TestNetromRouterCaseNormalization(t *testing.T) {
	var r = newNetromRouter()
	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 150},
	})
	r.processNodes(bc, "Q2TEST", 200)

	// Lookup should work regardless of case.
	if _, ok := r.lookup("q3test"); !ok {
		t.Error("case-insensitive lookup failed")
	}
	if _, ok := r.lookup("Q3TEST"); !ok {
		t.Error("upper-case lookup failed")
	}
}

func TestNetromRouterSnapshot(t *testing.T) {
	var r = newNetromRouter()
	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
		{dstCallsign: "Q4TEST", dstAlias: netromPadAlias("QNODED"), neighbor: "Q2TEST", quality: 180},
	})
	r.processNodes(bc, "Q2TEST", 200)

	var snap = r.snapshot()
	if len(snap) != 2 {
		t.Errorf("snapshot len: got %d, want 2", len(snap))
	}
}
