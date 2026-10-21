// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nodesFrom(alias string, entries ...NodesEntry) *Nodes {
	var n = new(Nodes)
	n.alias = alias
	n.entries = entries

	return n
}

func entry(callsign string, alias string, neighbour string, quality byte) NodesEntry {
	return NodesEntry{callsign: callsign, alias: alias, neighbour: neighbour, quality: quality}
}

// nbr is the neighbour call on channel 0.
func nbr(call string) neighbour {
	return neighbour{channel: 0, callsign: call}
}

func TestNetromPathQuality(t *testing.T) {
	t.Parallel()

	assert.Equal(t, byte(150), pathQuality(192, 200))
	assert.Equal(t, byte(254), pathQuality(255, 255))
	assert.Equal(t, byte(0), pathQuality(0, 255))
	assert.Equal(t, byte(144), pathQuality(192, 192), "each hop costs something")
}

// Hearing a broadcast makes its sender a route in its own right, at the
// quality we give links, and each thing it advertises a route through it.
func TestNetromRouterLearnsFromBroadcast(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 10)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200)))

	var hop, ok = r.nextHop("Q2TEST-7")
	require.True(t, ok)
	assert.Equal(t, nbr("Q2TEST-7"), hop)

	hop, ok = r.nextHop("Q3TEST-7")
	require.True(t, ok)
	assert.Equal(t, nbr("Q2TEST-7"), hop)

	assert.Equal(t, []NodesEntry{
		entry("Q2TEST-7", "QNODEB", "Q2TEST-7", 192),
		entry("Q3TEST-7", "QNODEC", "Q2TEST-7", 150),
	}, r.broadcastEntries())
}

func TestNetromRouterIgnores(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		name  string
		entry NodesEntry
	}{
		{"a route to ourselves", entry("Q1TEST-7", "QNODEA", "Q2TEST-7", 255)},
		{"a route the neighbour has by way of us", entry("Q3TEST-7", "QNODEC", "Q1TEST-7", 255)},
		{"a route below the minimum quality", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 10)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var r = newRouter("Q1TEST-7", 192, 10)

			r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB", tc.entry))

			var _, ok = r.nextHop(tc.entry.callsign)
			assert.False(t, ok, "should not have a route to %s", tc.entry.callsign)
		})
	}
}

// The minimum is applied to the quality the route will have for us, not to
// what the neighbour advertised.
func TestNetromRouterMinimumQuality(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 150)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB",
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
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 10)

	r.heardNodes(nbr("Q1TEST-7"), nodesFrom("QNODEA", entry("Q3TEST-7", "QNODEC", "Q3TEST-7", 200)))

	assert.Empty(t, r.broadcastEntries())
}

// A route that is not refreshed goes stale on our own clock: it stops being
// advertised once it is on its way out, and goes altogether at the end.
func TestNetromRouterAgesRoutes(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 10)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB"))

	for range obsolescenceInit - minBroadcastObsolescence {
		r.age()
	}

	assert.Len(t, r.broadcastEntries(), 1, "still advertised at the minimum")

	r.age()
	assert.Empty(t, r.broadcastEntries(), "not advertised once below the minimum")

	var _, ok = r.nextHop("Q2TEST-7")
	assert.True(t, ok, "but still usable")

	for range minBroadcastObsolescence - 1 {
		r.age()
	}

	_, ok = r.nextHop("Q2TEST-7")
	assert.False(t, ok, "gone once the count runs out")

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB"))
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
	t.Parallel()

	var a = newRouter("Q1TEST-7", 192, 10)
	var b = newRouter("Q2TEST-7", 192, 10)

	a.heardNodes(nbr("Q3TEST-7"), nodesFrom("QNODEC"))

	var exchange = func() {
		b.heardNodes(nbr("Q1TEST-7"), nodesFrom("QNODEA", a.broadcastEntries()...))
		a.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB", b.broadcastEntries()...))
		a.age()
		b.age()
	}

	exchange()

	var hop, ok = b.nextHop("Q3TEST-7")
	require.True(t, ok, "B learns C through A")
	assert.Equal(t, nbr("Q1TEST-7"), hop)

	for range 2 * obsolescenceInit {
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
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	for i, q := range []byte{100, 200, 150} {
		r.heardNodes(nbr(fmt.Sprintf("Q%dTEST", i+2)), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", q)))
	}

	var hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, nbr("Q3TEST"), hop, "the best route is used")

	r.heardNodes(nbr("Q5TEST"), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 50)))
	assert.Len(t, r.destinations["Q9TEST"].routes, maxRoutes)
	assert.NotContains(t, neighboursOf(r, "Q9TEST"), "Q5TEST", "a worse fourth is not kept")

	r.heardNodes(nbr("Q6TEST"), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 250)))
	assert.Equal(t, []string{"Q6TEST", "Q3TEST", "Q4TEST"}, neighboursOf(r, "Q9TEST"), "a better one displaces the worst")
}

// A neighbour's worse report replaces its earlier one rather than being
// ignored: its route really has got worse.
func TestNetromRouterUpdatesARoute(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 200)))
	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	require.Len(t, r.destinations["Q9TEST"].routes, 1)
	assert.Equal(t, byte(100), r.destinations["Q9TEST"].routes[0].quality)
}

func TestNetromRouterPassesOverAFailingNeighbour(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	r.heardNodes(nbr("Q2TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 200)))
	r.heardNodes(nbr("Q3TEST"), nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	r.neighbourFailed(nbr("Q2TEST"))

	var hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, nbr("Q2TEST"), hop, "one failure is not enough")

	r.neighbourFailed(nbr("Q2TEST"))

	hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, nbr("Q3TEST"), hop)

	r.neighbourHeard(nbr("Q2TEST"))

	hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, nbr("Q2TEST"), hop)
}

// A callsign heard on two channels is two neighbours, each a route of its
// own, and failing on one channel does not pass over the other.
func TestNetromRouterNeighbourIsCallsignOnAChannel(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	var on0 = neighbour{channel: 0, callsign: "Q2TEST"}
	var on1 = neighbour{channel: 1, callsign: "Q2TEST"}

	r.heardNodes(on0, nodesFrom("", entry("Q9TEST", "", "Q9TEST", 200)))
	r.heardNodes(on1, nodesFrom("", entry("Q9TEST", "", "Q9TEST", 100)))

	require.Len(t, r.destinations["Q9TEST"].routes, 2)

	var hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, on0, hop, "the better of the two")

	r.neighbourFailed(on0)
	r.neighbourFailed(on0)

	hop, _ = r.nextHop("Q9TEST")
	assert.Equal(t, on1, hop, "the same callsign on the other channel")
}

func TestNetromRouterResolves(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 192, 10)

	r.heardNodes(nbr("Q2TEST-7"), nodesFrom("QNODEB"))

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
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	for i := range maxDestinations + 10 {
		r.heardNodes(nbr(fmt.Sprintf("Q%dTEST-%d", i%10, i%16)), nodesFrom("", entry(fmt.Sprintf("Q%05d", i), "", "Q9TEST", 200)))
	}

	assert.LessOrEqual(t, len(r.destinations), maxDestinations)
}

func neighboursOf(r *router, callsign string) []string {
	var out []string
	for _, rt := range r.destinations[callsign].routes {
		out = append(out, rt.neighbour.callsign)
	}

	return out
}

// A destination whose best route is on its way out, or through a neighbour
// that keeps failing, is still advertised by a sound second route, as it
// would be used: not left out, nor advertised by a route we would not use.
func TestRouterAdvertisesTheRouteItWouldUse(t *testing.T) {
	t.Parallel()

	var r = newRouter("Q1TEST-7", 255, 10)

	r.heardNodes(nbr("Q4TEST"), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 200)))

	for range obsolescenceInit - minBroadcastObsolescence + 1 {
		r.age()
	}

	r.heardNodes(nbr("Q5TEST"), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 150)))

	var advertised = func() string {
		for _, e := range r.broadcastEntries() {
			if e.callsign == "Q9TEST" {
				return e.neighbour
			}
		}

		return ""
	}

	assert.Equal(t, "Q5TEST", advertised(), "the fresh route, while the better one ages")

	r.heardNodes(nbr("Q4TEST"), nodesFrom("", entry("Q9TEST", "QNODEZ", "Q9TEST", 200)))
	assert.Equal(t, "Q4TEST", advertised(), "the better one, once refreshed")

	r.neighbourFailed(nbr("Q4TEST"))
	r.neighbourFailed(nbr("Q4TEST"))
	assert.Equal(t, "Q5TEST", advertised(), "not through a neighbour that keeps failing")
}
