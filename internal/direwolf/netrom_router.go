// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

const (
	// A destination keeps its three best routes, as Linux does.
	netromMaxRoutes = 3

	// A route starts with this obsolescence count and loses one each time
	// we broadcast, so one not refreshed by a neighbour's broadcast lasts
	// this many of our intervals.  Only routes still at or above
	// netromMinBroadcastObsolescence are passed on to our neighbours, so a
	// route on its way out is not advertised to them as though it were
	// sound.
	netromObsolescenceInit         = 6
	netromMinBroadcastObsolescence = 4

	// After this many failed attempts to reach a neighbour, its routes are
	// passed over until it is heard from again.
	netromNeighbourFailLimit = 2

	// A NODES broadcast is something anybody on frequency can send, so the
	// table is bounded: once it holds this many destinations, broadcasts
	// can refresh or improve what is there but not add to it.
	netromMaxDestinations = 1000
)

// netromRoute is one way of reaching a destination: through a neighbour, at a
// quality, for as long as its obsolescence count lasts.
type netromRoute struct {
	neighbour    string
	quality      byte
	obsolescence int
}

// netromDestination is a node we can reach, with its routes best first.
type netromDestination struct {
	callsign string
	alias    string
	routes   []netromRoute
}

// netromRouter is a node's routing table, learned from the NODES broadcasts
// its neighbours send.  It follows the rules the Linux netromd daemon
// applies, which are what keep a NET/ROM network's routes from going round
// in circles:
//
//   - The neighbour a broadcast came from is reachable directly, at the
//     quality configured for our link to it.
//   - A destination it advertises is reachable through it, at a quality that
//     is the product of the two - (link quality * advertised quality + 128) /
//     256 - so every hop makes a route worse and a long way round loses to a
//     short one.
//   - A destination the neighbour reaches through *us* is ignored, since
//     going to it by way of that neighbour would only bring the traffic
//     back.  Without that, two neighbours keep a destination alive between
//     them long after it has gone, each learning it afresh from the other.
//   - A route below the minimum quality is not worth having.
//   - Routes age on our own broadcast clock, not on what we hear, so a
//     neighbour that falls silent takes its routes with it.
//
// It is safe for concurrent use.
type netromRouter struct {
	mu sync.Mutex

	myCall      string
	linkQuality byte // What we assume for a link to any neighbour we hear.
	minQuality  byte

	destinations map[string]*netromDestination
	failures     map[string]int // Consecutive failures per neighbour.
}

func newNetromRouter(myCall string, linkQuality byte, minQuality byte) *netromRouter {
	var r = new(netromRouter)
	r.myCall = myCall
	r.linkQuality = linkQuality
	r.minQuality = minQuality
	r.destinations = make(map[string]*netromDestination)
	r.failures = make(map[string]int)

	return r
}

// netromPathQuality is the quality of reaching a destination advertised at
// advertised over a link of linkQuality, as netromd calculates it.
func netromPathQuality(linkQuality byte, advertised byte) byte {
	return byte((int(linkQuality)*int(advertised) + 128) / 256)
}

// heardNodes updates the table from a NODES broadcast that neighbour sent.
func (r *netromRouter) heardNodes(neighbour string, n *netromNodes) {
	if strings.EqualFold(neighbour, r.myCall) {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.failures, neighbour)

	r.addRoute(neighbour, n.alias, neighbour, r.linkQuality)

	for _, e := range n.entries {
		switch {
		case strings.EqualFold(e.callsign, r.myCall):
			// A route to ourselves is no use to us.
		case strings.EqualFold(e.callsign, neighbour):
			// The neighbour itself is reachable directly, as above.
		case strings.EqualFold(e.neighbour, r.myCall):
			// Split horizon: the neighbour's way there is through us.
		default:
			var quality = netromPathQuality(r.linkQuality, e.quality)
			if quality >= r.minQuality && quality > 0 {
				r.addRoute(e.callsign, e.alias, neighbour, quality)
			}
		}
	}
}

// addRoute records that callsign is reachable through neighbour at quality,
// refreshing the route if there is one through that neighbour already.
// r.mu must be held.
func (r *netromRouter) addRoute(callsign string, alias string, neighbour string, quality byte) {
	var d, ok = r.destinations[callsign]
	if !ok {
		if len(r.destinations) >= netromMaxDestinations {
			return
		}

		d = new(netromDestination)
		d.callsign = callsign
		r.destinations[callsign] = d
	}

	if alias != "" {
		d.alias = alias
	}

	var route = netromRoute{neighbour: neighbour, quality: quality, obsolescence: netromObsolescenceInit}

	var i = slices.IndexFunc(d.routes, func(rt netromRoute) bool { return rt.neighbour == neighbour })

	switch {
	case i >= 0:
		d.routes[i] = route
	case len(d.routes) < netromMaxRoutes:
		d.routes = append(d.routes, route)
	default:
		// Full: the new one replaces the worst, if it is better.
		var worst = len(d.routes) - 1
		if quality <= d.routes[worst].quality {
			return
		}

		d.routes[worst] = route
	}

	slices.SortStableFunc(d.routes, func(a, b netromRoute) int { return cmp.Compare(b.quality, a.quality) })
}

// age takes one from every route's obsolescence count, dropping those that
// reach zero and destinations left with no routes.  It is called each time we
// broadcast.
func (r *netromRouter) age() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for callsign, d := range r.destinations {
		d.routes = slices.DeleteFunc(d.routes, func(rt netromRoute) bool { return rt.obsolescence <= 1 })

		for i := range d.routes {
			d.routes[i].obsolescence--
		}

		if len(d.routes) == 0 {
			delete(r.destinations, callsign)
		}
	}
}

// neighbourFailed notes that a link to neighbour could not be made or was
// lost.  Once it has failed netromNeighbourFailLimit times in a row, routes
// through it are passed over until it is heard from again.
func (r *netromRouter) neighbourFailed(neighbour string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failures[neighbour]++
}

// neighbourHeard notes that neighbour is evidently working again.
func (r *netromRouter) neighbourHeard(neighbour string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.failures, neighbour)
}

// resolve turns what someone asked to connect to - a node's callsign or its
// alias - into the callsign of a destination in the table.
func (r *netromRouter) resolve(name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var call, err = normaliseNetromCallsign(name)
	if err == nil {
		if _, ok := r.destinations[call]; ok {
			return call, true
		}
	}

	for _, d := range r.destinations {
		if d.alias != "" && strings.EqualFold(d.alias, name) {
			return d.callsign, true
		}
	}

	return "", false
}

// nextHop returns the neighbour to send traffic for callsign to: that of its
// best route through a neighbour that has not been failing.
func (r *netromRouter) nextHop(callsign string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var d, ok = r.destinations[callsign]
	if !ok {
		return "", false
	}

	for _, rt := range d.routes {
		if r.failures[rt.neighbour] < netromNeighbourFailLimit {
			return rt.neighbour, true
		}
	}

	return "", false
}

// broadcastEntries is what we tell our neighbours we can reach: each
// destination's best route, unless it is on its way out.  They are in
// callsign order, so that a broadcast is the same from one time to the next
// for the same table.
func (r *netromRouter) broadcastEntries() []netromNodesEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	var entries = make([]netromNodesEntry, 0, len(r.destinations))

	for _, d := range r.destinations {
		var best = d.routes[0]
		if best.obsolescence < netromMinBroadcastObsolescence || best.quality == 0 {
			continue
		}

		entries = append(entries, netromNodesEntry{
			callsign:  d.callsign,
			alias:     d.alias,
			neighbour: best.neighbour,
			quality:   best.quality,
		})
	}

	slices.SortFunc(entries, func(a, b netromNodesEntry) int { return strings.Compare(a.callsign, b.callsign) })

	return entries
}
