// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"sync"
)

// netromObsCountPrune is the obsolescence count at which a route is removed.
// Each NODES broadcast cycle that does not include a route increments its obsCount;
// once the count reaches this threshold the route is deleted.
const netromObsCountPrune = 6

// netromRouteEntry holds a single entry in the NET/ROM routing table.
type netromRouteEntry struct {
	dstCallsign string
	dstAlias    string
	neighbor    string // next-hop AX.25 callsign (immediate radio neighbor).
	quality     byte   // effective quality 0–255; higher is better.
	obsCount    int    // incremented each NODES cycle without this route.
}

// netromRouter is a thread-safe Bellman-Ford distance-vector routing table.
type netromRouter struct {
	mu     sync.RWMutex
	routes map[string]*netromRouteEntry // keyed by upper-cased dstCallsign.
}

func newNetromRouter() *netromRouter {
	return &netromRouter{
		mu:     sync.RWMutex{},
		routes: make(map[string]*netromRouteEntry),
	}
}

// update processes one NODES broadcast received from fromNeighbor.
// localQuality is the quality of the link to fromNeighbor.
// Existing routes not seen in this broadcast have their obsCount incremented;
// call age() after update() to prune stale routes.
func (r *netromRouter) update(b *netromRoutingBroadcast, fromNeighbor string, localQuality byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Mark all existing routes as not-yet-seen in this broadcast.
	var seen = make(map[string]bool, len(r.routes))

	for _, e := range b.entries {
		var dst = strings.ToUpper(e.dstCallsign)
		seen[dst] = true

		// Effective quality: bottleneck of our link to the neighbor and
		// the neighbor's quality to the destination.
		var eq = min(localQuality, e.quality)

		var existing, ok = r.routes[dst]
		if !ok || eq > existing.quality {
			r.routes[dst] = &netromRouteEntry{
				dstCallsign: e.dstCallsign,
				dstAlias:    strings.TrimRight(string(e.dstAlias[:]), " "),
				neighbor:    fromNeighbor,
				quality:     eq,
				obsCount:    0,
			}
		} else if existing.neighbor == fromNeighbor {
			// Same neighbor: update quality and reset obsCount.
			existing.quality = eq
			existing.obsCount = 0
		}
	}

	// Age only routes via fromNeighbor that were absent from this broadcast.
	// Routes learned from other neighbors are unaffected by a single neighbor's omission.
	for dst, entry := range r.routes {
		if !seen[dst] && entry.neighbor == fromNeighbor {
			entry.obsCount++
		}
	}
}

// age removes routes whose obsCount has reached the prune threshold.
// Call this after update() for each received NODES broadcast.
func (r *netromRouter) age() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for dst, entry := range r.routes {
		if entry.obsCount >= netromObsCountPrune {
			delete(r.routes, dst)
		}
	}
}

// processNodes is a convenience wrapper that calls update then age.
func (r *netromRouter) processNodes(b *netromRoutingBroadcast, fromNeighbor string, localQuality byte) {
	r.update(b, fromNeighbor, localQuality)
	r.age()
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
