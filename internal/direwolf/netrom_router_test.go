// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nodesFrom(alias string, entries ...netromNodesEntry) *netromNodes {
	var n = new(netromNodes)
	n.alias = alias
	n.entries = entries

	return n
}

func entry(callsign string, alias string, neighbour string, quality byte) netromNodesEntry {
	return netromNodesEntry{callsign: callsign, alias: alias, neighbour: neighbour, quality: quality}
}

func TestNetromPathQuality(t *testing.T) {
	assert.Equal(t, byte(150), netromPathQuality(192, 200))
	assert.Equal(t, byte(254), netromPathQuality(255, 255))
	assert.Equal(t, byte(0), netromPathQuality(0, 255))
	assert.Equal(t, byte(144), netromPathQuality(192, 192), "each hop costs something")
}

// Hearing a broadcast makes its sender a route in its own right, at the
// quality we give links, and each thing it advertises a route through it.
func TestNetromRouterLearnsFromBroadcast(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 192, 10)

	r.heardNodes("Q2TEST-7", nodesFrom("QNODEB", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200)))

	var hop, ok = r.nextHop("Q2TEST-7")
	require.True(t, ok)
	assert.Equal(t, "Q2TEST-7", hop)

	hop, ok = r.nextHop("Q3TEST-7")
	require.True(t, ok)
	assert.Equal(t, "Q2TEST-7", hop)

	assert.Equal(t, []netromNodesEntry{
		entry("Q2TEST-7", "QNODEB", "Q2TEST-7", 192),
		entry("Q3TEST-7", "QNODEC", "Q2TEST-7", 150),
	}, r.broadcastEntries())
}

func TestNetromRouterIgnores(t *testing.T) {
	var testCases = []struct {
		name  string
		entry netromNodesEntry
	}{
		{"a route to ourselves", entry("Q1TEST-7", "QNODEA", "Q2TEST-7", 255)},
		{"a route the neighbour has by way of us", entry("Q3TEST-7", "QNODEC", "Q1TEST-7", 255)},
		{"a route below the minimum quality", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 10)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var r = newNetromRouter("Q1TEST-7", 192, 10)

			r.heardNodes("Q2TEST-7", nodesFrom("QNODEB", tc.entry))

			var _, ok = r.nextHop(tc.entry.callsign)
			assert.False(t, ok, "should not have a route to %s", tc.entry.callsign)
		})
	}
}

// The minimum is applied to the quality the route will have for us, not to
// what the neighbour advertised.
func TestNetromRouterMinimumQuality(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 192, 150)

	r.heardNodes("Q2TEST-7", nodesFrom("QNODEB",
		entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200), // 150 for us: kept.
		entry("Q4TEST-7", "QNODED", "Q4TEST-7", 199), // 149 for us: dropped.
	))

	var _, ok = r.nextHop("Q3TEST-7")
	assert.True(t, ok)

	_, ok = r.nextHop("Q4TEST-7")
	assert.False(t, ok)
}

// Our own broadcasts, looping back from a digipeater or another port, are not
// a neighbour.
func TestNetromRouterIgnoresItsOwnBroadcast(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 192, 10)

	r.heardNodes("Q1TEST-7", nodesFrom("QNODEA", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200)))

	assert.Empty(t, r.broadcastEntries())
}

// A route that is not refreshed goes stale on our own clock: it stops being
// advertised once it is on its way out, and goes altogether at the end.
func TestNetromRouterAgesRoutes(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 192, 10)

	r.heardNodes("Q2TEST-7", nodesFrom("QNODEB"))

	for range netromObsolescenceInit - netromMinBroadcastObsolescence {
		r.age()
	}

	assert.Len(t, r.broadcastEntries(), 1, "still advertised at the minimum")

	r.age()
	assert.Empty(t, r.broadcastEntries(), "not advertised once below the minimum")

	var _, ok = r.nextHop("Q2TEST-7")
	assert.True(t, ok, "but still usable")

	for range netromMinBroadcastObsolescence - 1 {
		r.age()
	}

	_, ok = r.nextHop("Q2TEST-7")
	assert.False(t, ok, "gone once the count runs out")

	r.heardNodes("Q2TEST-7", nodesFrom("QNODEB"))
	_, ok = r.nextHop("Q2TEST-7")
	assert.True(t, ok, "and back when heard again")
}

// The regression the review of the first implementation turned up: two
// neighbouring nodes kept a vanished destination alive between them for ever,
// each re-learning it from the other's broadcast at undiminished quality.
// Here Q3TEST-7 is heard once by A and then never again, while A and B go on
// broadcasting to each other; it must drop out of both tables within two
// route lifetimes - A's own, then that of the copy B learned from A.  (Quality
// falling with every hop would kill such a loop eventually on its own, after
// many more rounds of traffic going round it; split horizon keeps it from
// forming at all.)
func TestNetromRoutersDoNotKeepADeadDestinationAlive(t *testing.T) {
	var a = newNetromRouter("Q1TEST-7", 192, 10)
	var b = newNetromRouter("Q2TEST-7", 192, 10)

	a.heardNodes("Q3TEST-7", nodesFrom("QNODEC"))

	var exchange = func() {
		b.heardNodes("Q1TEST-7", nodesFrom("QNODEA", a.broadcastEntries()...))
		a.heardNodes("Q2TEST-7", nodesFrom("QNODEB", b.broadcastEntries()...))
		a.age()
		b.age()
	}

	exchange()

	var hop, ok = b.nextHop("Q3TEST-7")
	require.True(t, ok, "B learns C through A")
	assert.Equal(t, "Q1TEST-7", hop)

	for range 2 * netromObsolescenceInit {
		exchange()
	}

	_, ok = a.nextHop("Q3TEST-7")
	assert.False(t, ok, "A should have forgotten C")

	_, ok = b.nextHop("Q3TEST-7")
	assert.False(t, ok, "B should have forgotten C")

	_, ok = b.nextHop("Q1TEST-7")
	assert.True(t, ok, "while A and B, still hearing each other, keep each other")
}

// A destination keeps its best three routes, best first; a fourth replaces
// the worst only if it is better.
func TestNetromRouterKeepsTheBestRoutes(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 255, 10)

	for i, q := range []byte{100, 200, 150} {
		r.heardNodes(fmt.Sprintf("Q%dTEST", i+2), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", q)))
	}

	var hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, "Q3TEST", hop, "the best route is used")

	r.heardNodes("Q5TEST", nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 50)))
	assert.Len(t, r.destinations["Q9TEST"].routes, netromMaxRoutes)
	assert.NotContains(t, neighboursOf(r, "Q9TEST"), "Q5TEST", "a worse fourth is not kept")

	r.heardNodes("Q6TEST", nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 250)))
	assert.Equal(t, []string{"Q6TEST", "Q3TEST", "Q4TEST"}, neighboursOf(r, "Q9TEST"), "a better one displaces the worst")
}

// A neighbour's worse report replaces its earlier one rather than being
// ignored: its route really has got worse.
func TestNetromRouterUpdatesARoute(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 255, 10)

	r.heardNodes("Q2TEST", nodesFrom("", entry("Q9TEST", "", "Q9TEST", 200)))
	r.heardNodes("Q2TEST", nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	require.Len(t, r.destinations["Q9TEST"].routes, 1)
	assert.Equal(t, byte(100), r.destinations["Q9TEST"].routes[0].quality)
}

func TestNetromRouterPassesOverAFailingNeighbour(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 255, 10)

	r.heardNodes("Q2TEST", nodesFrom("", entry("Q9TEST", "", "Q9TEST", 200)))
	r.heardNodes("Q3TEST", nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	r.neighbourFailed("Q2TEST")

	var hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, "Q2TEST", hop, "one failure is not enough")

	r.neighbourFailed("Q2TEST")

	hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, "Q3TEST", hop)

	r.neighbourHeard("Q2TEST")

	hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, "Q2TEST", hop)
}

func TestNetromRouterResolves(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 192, 10)

	r.heardNodes("Q2TEST-7", nodesFrom("QNODEB"))

	for _, name := range []string{"Q2TEST-7", "q2test-7", "QNODEB", "qnodeb"} {
		var call, ok = r.resolve(name)
		assert.True(t, ok, name)
		assert.Equal(t, "Q2TEST-7", call, name)
	}

	var _, ok = r.resolve("QNODEX")
	assert.False(t, ok)
}

// Broadcasts are the one thing anybody can send us, so they cannot grow the
// table without limit.
func TestNetromRouterIsBounded(t *testing.T) {
	var r = newNetromRouter("Q1TEST-7", 255, 10)

	for i := range netromMaxDestinations + 10 {
		r.heardNodes(fmt.Sprintf("Q%dTEST-%d", i%10, i%16), nodesFrom("", entry(fmt.Sprintf("Q%05d", i), "", "Q9TEST", 200)))
	}

	assert.LessOrEqual(t, len(r.destinations), netromMaxDestinations)
}

func neighboursOf(r *netromRouter, callsign string) []string {
	var out []string
	for _, rt := range r.destinations[callsign].routes {
		out = append(out, rt.neighbour)
	}

	return out
}
