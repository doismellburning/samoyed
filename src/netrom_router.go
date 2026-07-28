// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"sync"
)

// netromObsCountInit is the obsolescence count given to a route each time it
// is heard. The count is decremented once per NODES broadcast interval by
// tick() and the route is dropped when it reaches zero, so a route survives
// this many of our own broadcast cycles after the last time we heard it.
//
// Counting down on our own cycle rather than on each received broadcast is
// what makes a neighbour going off the air actually expire: a silent
// neighbour sends nothing to age its routes with.
const netromObsCountInit = 6

// netromRouteEntry holds a single entry in the NET/ROM routing table.
type netromRouteEntry struct {
	dstCallsign string
	dstAlias    string
	neighbor    string // next-hop AX.25 callsign (immediate radio neighbor).
	quality     byte   // effective quality 0–255; higher is better.
	obsCount    int    // cycles this route survives without being heard again.
}

// netromRouter is a thread-safe Bellman-Ford distance-vector routing table.
type netromRouter struct {
	mu     sync.RWMutex
	routes map[string]*netromRouteEntry // keyed by upper-cased dstCallsign.
}

func newNetromRouter() *netromRouter {
	var r = new(netromRouter)
	r.routes = make(map[string]*netromRouteEntry)

	return r
}

// update processes one NODES broadcast received from fromNeighbor.
// localQuality is the quality of the link to fromNeighbor. Routes named in
// the broadcast have their obsolescence count refreshed; routes that are not
// are left alone and expire through tick().
func (r *netromRouter) update(b *netromRoutingBroadcast, fromNeighbor string, localQuality byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range b.entries {
		// Effective quality: bottleneck of our link to the neighbor and
		// the neighbor's quality to the destination.
		r.learnLocked(e.dstCallsign, strings.TrimRight(string(e.dstAlias[:]), " "),
			fromNeighbor, min(localQuality, e.quality))
	}

	// Hearing a broadcast from fromNeighbor is itself proof that it is
	// directly reachable, whether or not it advertised itself. Without this
	// two adjacent nodes never learn a route to each other.
	r.learnLocked(fromNeighbor, strings.TrimRight(string(b.srcAlias[:]), " "),
		fromNeighbor, localQuality)
}

// learnLocked records a route to dst via neighbor at the given quality,
// replacing an existing route only if the new one is at least as good.
// Must be called with r.mu held.
func (r *netromRouter) learnLocked(dstCallsign, dstAlias, neighbor string, quality byte) {
	var dst = strings.ToUpper(dstCallsign)

	var existing, ok = r.routes[dst]
	switch {
	case !ok || quality > existing.quality:
		r.routes[dst] = &netromRouteEntry{
			dstCallsign: dstCallsign,
			dstAlias:    dstAlias,
			neighbor:    neighbor,
			quality:     quality,
			obsCount:    netromObsCountInit,
		}
	case existing.neighbor == neighbor:
		// Same neighbor: refresh quality and obsolescence count.
		existing.quality = quality
		existing.obsCount = netromObsCountInit
		if dstAlias != "" {
			existing.dstAlias = dstAlias
		}
	default:
		// A worse route via a different neighbor: ignore it, but the route
		// we are keeping was not heard here, so leave its count alone.
	}
}

// tick decrements every route's obsolescence count and removes those that
// reach zero. Call it once per NODES broadcast interval.
func (r *netromRouter) tick() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for dst, entry := range r.routes {
		entry.obsCount--
		if entry.obsCount <= 0 {
			delete(r.routes, dst)
		}
	}
}

// processNodes handles one received NODES broadcast.
func (r *netromRouter) processNodes(b *netromRoutingBroadcast, fromNeighbor string, localQuality byte) {
	r.update(b, fromNeighbor, localQuality)
}

// lookup returns the best route to dst, or false if no route is known.
func (r *netromRouter) lookup(dst string) (*netromRouteEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var entry, ok = r.routes[strings.ToUpper(dst)]
	if !ok {
		return nil, false
	}
	// Return a copy so the caller is not affected by concurrent updates.
	var result = *entry

	return &result, true
}

// snapshot returns all current routing table entries, for building NODES broadcasts.
func (r *netromRouter) snapshot() []netromRouteEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result = make([]netromRouteEntry, 0, len(r.routes))
	for _, e := range r.routes {
		result = append(result, *e)
	}

	return result
}
