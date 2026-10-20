// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"math"
	"strconv"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/nodemap"
)

// reportedPorts is the IDs of the ports the node map is told of.  Anything
// else the map is told that names a port must name one of these, or it names
// a port the map has never heard of.
func reportedPorts(ports []nodemap.Port) map[string]bool {
	var ids = make(map[string]bool, len(ports))
	for _, port := range ports {
		ids[port.ID] = true
	}

	return ids
}

// netromNeighbours is, for the node map, the NET/ROM node's links to its
// neighbours that are up, as status has them at the time, over whichever of
// ports the node runs on.
func netromNeighbours(status func() netrom.Status, ports []nodemap.Port) func() []nodemap.NeighbourLink {
	var reported = reportedPorts(ports)

	return func() []nodemap.NeighbourLink {
		var s = status()

		var port = strconv.Itoa(s.Channel)
		if !reported[port] {
			return nil
		}

		var links []nodemap.NeighbourLink

		for _, nb := range s.Neighbours {
			if nb.Link != "up" {
				continue
			}

			var quality = maybe.Nothing[int32]()
			if nb.Quality >= 0 && nb.Quality <= math.MaxUint8 {
				quality = maybe.Just(int32(nb.Quality))
			}

			links = append(links, nodemap.NeighbourLink{Node: nb.Callsign, Port: port, Quality: quality, Type: nodemap.NeighbourNetROM})
		}

		return links
	}
}
