// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/mheard"
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

// heardOn is a station heard on channel through hops digipeaters.
func heardOn(callsign string, channel int, hops int, when time.Time) mheard.Heard {
	return mheard.Heard{
		Callsign:   callsign,
		Channel:    channel,
		Hops:       hops,
		Packets:    3,
		FirstHeard: when.Add(-time.Hour),
		LastHeard:  when,
		Lat:        maybe.Nothing[float64](),
		Lon:        maybe.Nothing[float64](),
	}
}

func TestStationsHeard(t *testing.T) {
	t.Parallel()

	var ports = []nodemap.Port{mapPortFor("0", nodemap.LinkRF), mapPortFor("10", nodemap.LinkInternet)}
	var now = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

	t.Run("lists the stations heard directly on the ports the map is told of", func(t *testing.T) {
		t.Parallel()

		var located = heardOn("Q2TEST-7", 0, 0, now)
		located.Lat = maybe.Just(51.5)
		located.Lon = maybe.Just(-1.0)

		var heard = []mheard.Heard{
			located,
			heardOn("Q3TEST", 0, 1, now),  // Through a digipeater.
			heardOn("Q4TEST", 1, 0, now),  // On a port the map is not told of.
			heardOn("Q5TEST", 10, 0, now), // Missing its position.
		}

		var asked int

		var stations = stationsHeard(func(limit int) []mheard.Heard {
			asked = limit

			return heard
		}, ports)()

		assert.Positive(t, asked)
		assert.Equal(t, []nodemap.Heard{
			{
				Callsign:   "Q2TEST-7",
				Port:       "0",
				Packets:    maybe.Just[int64](3),
				FirstHeard: nodemap.Timestamp{Time: now.Add(-time.Hour)},
				LastHeard:  nodemap.Timestamp{Time: now},
				Coords:     maybe.Just(nodemap.LatLon{Lat: 51.5, Lon: -1}),
			},
			{
				Callsign:   "Q5TEST",
				Port:       "10",
				Packets:    maybe.Just[int64](3),
				FirstHeard: nodemap.Timestamp{Time: now.Add(-time.Hour)},
				LastHeard:  nodemap.Timestamp{Time: now},
				Coords:     maybe.Nothing[nodemap.LatLon](),
			},
		}, stations)
	})

	t.Run("lists at most so many, most recently heard first", func(t *testing.T) {
		t.Parallel()

		var heard []mheard.Heard
		for i := range mapHeardLimit + 10 {
			heard = append(heard, heardOn(fmt.Sprintf("Q%dTEST", i), 0, 0, now.Add(-time.Duration(i)*time.Minute)))
		}

		var stations = stationsHeard(func(int) []mheard.Heard { return heard }, ports)()

		assert.Len(t, stations, mapHeardLimit)
		assert.Equal(t, "Q0TEST", stations[0].Callsign)
	})
}
