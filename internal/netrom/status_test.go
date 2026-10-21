// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeStatusEmpty(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	var s = a.Status()
	assert.Equal(t, 0, s.Channel)
	assert.Equal(t, "Q1TEST-7", s.Callsign)
	assert.Equal(t, "QNODEA", s.Alias)
	assert.Empty(t, s.Neighbours)
	assert.Empty(t, s.Destinations)
	assert.Empty(t, s.Circuits)
}

func TestNodeStatus(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, b.Register(0, 1, "Q2TEST-7"))
	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM))
	require.True(t, a.Send(0, 0, "Q1TEST", "QNODEB", []byte("hello")))

	var s = a.Status()
	assert.Equal(t, []NeighbourStatus{{Channel: 0, Callsign: "Q2TEST-7", Link: "up", Quality: int(DefaultQuality), Failures: 0, Failing: false}}, s.Neighbours)
	assert.Equal(t, []DestinationStatus{{
		Callsign: "Q2TEST-7",
		Alias:    "QNODEB",
		Routes:   []RouteStatus{{Channel: 0, Neighbour: "Q2TEST-7", Quality: int(DefaultQuality), Obsolescence: obsolescenceInit}},
	}}, s.Destinations)
	assert.Equal(t, []CircuitStatus{{
		Channel:     0,
		OwnCall:     "Q1TEST",
		RemoteCall:  "QNODEB",
		RemoteNode:  "Q2TEST-7",
		User:        "Q1TEST",
		Incoming:    false,
		State:       "connecting",
		Outstanding: 1,
	}}, s.Circuits)

	exchange(t, a, b)

	s = a.Status()
	require.Len(t, s.Circuits, 1)
	assert.Equal(t, "connected", s.Circuits[0].State)

	var bs = b.Status()
	require.Len(t, bs.Circuits, 1)
	assert.Equal(t, "Q2TEST-7", bs.Circuits[0].OwnCall)
	assert.Equal(t, "Q1TEST", bs.Circuits[0].RemoteCall)
	assert.True(t, bs.Circuits[0].Incoming)

	require.True(t, a.Disconnect(0, 0, "Q1TEST", "QNODEB"))
	exchange(t, a, b)

	assert.Empty(t, a.Status().Circuits, "a closed circuit is not shown")
}

// The same callsign on two channels is two neighbours, in callsign order
// then channel, and a route says which of them it goes through.
func TestNodeStatusNeighbourChannels(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.sendToNeighbour(neighbour{channel: 3, callsign: "Q2TEST-7"}, []byte("one"))
	a.router.heardNodes(neighbour{channel: 1, callsign: "Q2TEST-7"}, nodesFrom("QNODEB"))

	var s = a.Status()
	assert.Equal(t, []NeighbourStatus{
		{Channel: 1, Callsign: "Q2TEST-7", Link: "down", Quality: int(DefaultQuality), Failures: 0, Failing: false},
		{Channel: 3, Callsign: "Q2TEST-7", Link: "connecting", Quality: int(DefaultQuality), Failures: 0, Failing: false},
	}, s.Neighbours)

	require.Len(t, s.Destinations, 1)
	assert.Equal(t, []RouteStatus{{Channel: 1, Neighbour: "Q2TEST-7", Quality: int(DefaultQuality), Obsolescence: obsolescenceInit}}, s.Destinations[0].Routes)
}

// A neighbour with a link on its way, and no routes, is still a neighbour.
func TestNodeStatusConnectingNeighbour(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("one"))

	assert.Equal(t, []NeighbourStatus{{Channel: 0, Callsign: "Q2TEST-7", Link: "connecting", Quality: int(DefaultQuality), Failures: 0, Failing: false}}, a.Status().Neighbours)
}

// A neighbour whose link keeps failing is shown as failing, even once its
// routes have aged away.
func TestNodeStatusFailingNeighbour(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)

	for range obsolescenceInit {
		a.router.age()
	}

	var s = a.Status()
	assert.Empty(t, s.Destinations)
	assert.Equal(t, []NeighbourStatus{{Channel: 0, Callsign: "Q2TEST-7", Link: "down", Quality: int(DefaultQuality), Failures: 2, Failing: true}}, s.Neighbours)
}
