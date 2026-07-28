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

	// The route survives netromObsCountInit-1 of our own broadcast cycles.
	for range netromObsCountInit - 1 {
		r.tick()
	}
	if _, ok := r.lookup("Q3TEST"); !ok {
		t.Fatal("route should still exist after fewer than netromObsCountInit cycles")
	}

	// One more cycle takes the obsolescence count to zero.
	r.tick()
	if _, ok := r.lookup("Q3TEST"); ok {
		t.Error("route should have been pruned once its obsolescence count reached zero")
	}
}

// TestNetromRouterHearingRouteResetsObsolescence checks that a route heard
// again gets a full lease of life rather than continuing to age out.
func TestNetromRouterHearingRouteResetsObsolescence(t *testing.T) {
	var r = newNetromRouter()

	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
	})
	r.processNodes(bc, "Q2TEST", 200)

	for range netromObsCountInit - 1 {
		r.tick()
	}

	// Heard again just before it would have expired.
	r.processNodes(bc, "Q2TEST", 200)

	for range netromObsCountInit - 1 {
		r.tick()
	}
	if _, ok := r.lookup("Q3TEST"); !ok {
		t.Error("hearing a route again should reset its obsolescence count")
	}
}

// TestNetromRouterPrunesRoutesViaSilentNeighbor is the regression test for a
// black hole: aging used to advance only when the neighbour a route was
// learned from sent another broadcast, so a neighbour that went off the air
// kept its routes — and their traffic — forever.
func TestNetromRouterPrunesRoutesViaSilentNeighbor(t *testing.T) {
	var r = newNetromRouter()

	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
	})
	r.processNodes(bc, "Q2TEST", 200)

	// Q2TEST now goes off the air: no further broadcasts from it, ever.
	// Only our own cycles pass.
	for range netromObsCountInit {
		r.tick()
	}

	if _, ok := r.lookup("Q3TEST"); ok {
		t.Error("route via a neighbour that stopped broadcasting should have been pruned")
	}
	if _, ok := r.lookup("Q2TEST"); ok {
		t.Error("route to a neighbour that stopped broadcasting should have been pruned")
	}
}

// TestNetromRouterLearnsRouteToBroadcastingNeighbor checks that simply
// hearing a neighbour's NODES broadcast is enough to route to it. Without
// this, two adjacent nodes that do not advertise themselves stay mutually
// unreachable no matter how well they hear each other.
func TestNetromRouterLearnsRouteToBroadcastingNeighbor(t *testing.T) {
	var r = newNetromRouter()

	// Q2TEST's broadcast mentions only a third node, not itself.
	var bc = makeTestBroadcast("QNODEB", []netromNodesEntry{
		{dstCallsign: "Q3TEST", dstAlias: netromPadAlias("QNODEC"), neighbor: "Q2TEST", quality: 200},
	})
	r.processNodes(bc, "Q2TEST", 190)

	var entry, ok = r.lookup("Q2TEST")
	if !ok {
		t.Fatal("expected a direct route to the broadcasting neighbour")
	}
	if entry.neighbor != "Q2TEST" {
		t.Errorf("neighbour: got %q, want the neighbour itself", entry.neighbor)
	}
	if entry.quality != 190 {
		t.Errorf("quality: got %d, want the link quality 190", entry.quality)
	}
	if entry.dstAlias != "QNODEB" {
		t.Errorf("alias: got %q, want the broadcast's source alias QNODEB", entry.dstAlias)
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

	// The two advertised destinations, plus the direct route to the
	// neighbour we heard the broadcast from.
	var snap = r.snapshot()
	if len(snap) != 3 {
		t.Errorf("snapshot len: got %d, want 3", len(snap))
	}
}
