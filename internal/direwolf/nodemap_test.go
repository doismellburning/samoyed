// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/nodemap"
	"github.com/stretchr/testify/assert"
)

// netromStatus is a NET/ROM node's status on channel, with neighbours.
func netromStatus(channel int, neighbours ...netrom.NeighbourStatus) func() netrom.Status {
	return func() netrom.Status {
		return netrom.Status{Channel: channel, Callsign: "Q1TEST-7", Alias: "QNODEA", Neighbours: neighbours, Destinations: nil, Circuits: nil}
	}
}

func neighbour(callsign string, link string) netrom.NeighbourStatus {
	return netrom.NeighbourStatus{Callsign: callsign, Link: link, Quality: 192, Failures: 0, Failing: false}
}

func TestNetROMNeighbours(t *testing.T) {
	t.Parallel()

	var ports = []nodemap.Port{mapPortFor("0", nodemap.LinkRF), mapPortFor("10", nodemap.LinkInternet)}

	t.Run("lists the links that are up", func(t *testing.T) {
		t.Parallel()

		var status = netromStatus(10, neighbour("Q2TEST-7", "up"), neighbour("Q3TEST-7", "connecting"), neighbour("Q4TEST-7", "down"), neighbour("Q5TEST-7", "up"))

		assert.Equal(t, []nodemap.NeighbourLink{
			{Node: "Q2TEST-7", Port: "10", Quality: maybe.Just[int32](192), Type: nodemap.NeighbourNetROM},
			{Node: "Q5TEST-7", Port: "10", Quality: maybe.Just[int32](192), Type: nodemap.NeighbourNetROM},
		}, netromNeighbours(status, ports)())
	})

	t.Run("lists none on a port the map is not told of", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, netromNeighbours(netromStatus(1, neighbour("Q2TEST-7", "up")), ports)())
	})

	t.Run("asks again for each report", func(t *testing.T) {
		t.Parallel()

		var up = false

		var neighbours = netromNeighbours(func() netrom.Status {
			if up {
				return netromStatus(0, neighbour("Q2TEST-7", "up"))()
			}

			return netromStatus(0)()
		}, ports)

		assert.Empty(t, neighbours())

		up = true

		assert.Len(t, neighbours(), 1)
	})
}
