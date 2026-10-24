// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"cmp"
	"slices"
	"strings"
	"sync"
)

const (
	// A destination keeps its three best routes.
	maxRoutes = 3

	// A route starts with this obsolescence count and loses one each time
	// we broadcast, so one not refreshed by a neighbour's broadcast lasts
	// this many of our intervals.  Only routes still at or above
	// minBroadcastObsolescence are passed on to our neighbours, so a
	// route on its way out is not advertised to them as though it were
	// sound.
	obsolescenceInit         = 6
	minBroadcastObsolescence = 4

	// After this many failed attempts to reach a neighbour, its routes are
	// passed over until it is heard from again.
	neighbourFailLimit = 2

	// A NODES broadcast is something anybody on frequency can send, so the
	// table is bounded: once it holds this many destinations, broadcasts
	// can refresh or improve what is there but not add to it.
	maxDestinations = 1000
)

// neighbour is a node we hear directly: a callsign on a channel.  The same
// callsign heard on two channels is two neighbours, as it is to BPQ and to
// Linux, each a way of its own to whatever it can reach.
type neighbour struct {
	channel  int
	callsign string
}

// routeEntry is one way of reaching a destination: through a neighbour, at a
// quality, for as long as its obsolescence count lasts.
type routeEntry struct {
	neighbour    neighbour
	quality      byte
	obsolescence int
}

// destination is a node we can reach, with its routes best first.
type destination struct {
	callsign string
	alias    string
	routes   []routeEntry
}

// qualityByChannel is what a node assumes for its link to a neighbour it
// hears on channel.
type qualityByChannel func(channel int) byte

// router is a node's routing table, learned from the NODES broadcasts
// its neighbours send.  These rules are what keep a NET/ROM network's routes
// from going round in circles:
//
//   - The neighbour a broadcast came from is reachable directly, at the
//     quality configured for our link to it - which is for the channel it was
//     heard on, so that a fast channel can be preferred to a slow one.
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
type router struct {
	mu sync.Mutex

	myCall      string
	linkQuality qualityByChannel
	minQuality  byte

	destinations map[string]*destination
	failures     map[neighbour]int // Consecutive failures per neighbour.
}

func newRouter(myCall string, linkQuality qualityByChannel, minQuality byte) *router {
	var r = new(router)
	r.myCall = myCall
	r.linkQuality = linkQuality
	r.minQuality = minQuality
	r.destinations = make(map[string]*destination)
	r.failures = make(map[neighbour]int)

	return r
}

// pathQuality is the quality of reaching a destination advertised at
// advertised over a link of linkQuality.
func pathQuality(linkQuality byte, advertised byte) byte {
	// At most (255 * 255 + 128) / 256 = 254, so the mask loses nothing.
	return byte(((int(linkQuality)*int(advertised) + 128) / 256) & 0xFF)
}

// heardNodes updates the table from a NODES broadcast that from sent.
func (r *router) heardNodes(from neighbour, n *Nodes) {
	if strings.EqualFold(from.callsign, r.myCall) {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.failures, from)

	var linkQuality = r.linkQuality(from.channel)

	r.addRoute(from.callsign, n.alias, from, linkQuality)

	for _, e := range n.entries {
		switch {
		case strings.EqualFold(e.callsign, r.myCall):
			// A route to ourselves is no use to us.
		case strings.EqualFold(e.callsign, from.callsign):
			// The neighbour itself is reachable directly, as above.
		case strings.EqualFold(e.neighbour, r.myCall):
			// Split horizon: the neighbour's way there is through us.  A
			// broadcast names no channel, so this is through us on any
			// of them, which is near enough: a neighbour reaching us on
			// one channel through us on another is going a long way
			// round.
		default:
			var quality = pathQuality(linkQuality, e.quality)
			if quality >= r.minQuality && quality > 0 {
				r.addRoute(e.callsign, e.alias, from, quality)
			}
		}
	}
}

// addRoute records that callsign is reachable through neighbour at quality,
// refreshing the route if there is one through that neighbour already.
// r.mu must be held.
func (r *router) addRoute(callsign string, alias string, neighbour neighbour, quality byte) {
	var d, ok = r.destinations[callsign]
	if !ok {
		if len(r.destinations) >= maxDestinations {
			return
		}

		d = new(destination)
		d.callsign = callsign
		r.destinations[callsign] = d
	}

	if alias != "" {
		d.alias = alias
	}

	var route = routeEntry{neighbour: neighbour, quality: quality, obsolescence: obsolescenceInit}

	var i = slices.IndexFunc(d.routes, func(rt routeEntry) bool { return rt.neighbour == neighbour })

	switch {
	case i >= 0:
		d.routes[i] = route
	case len(d.routes) < maxRoutes:
		d.routes = append(d.routes, route)
	default:
		// Full: the new one replaces the worst, if it is better.
		var worst = len(d.routes) - 1
		if quality <= d.routes[worst].quality {
			return
		}

		d.routes[worst] = route
	}

	slices.SortStableFunc(d.routes, func(a, b routeEntry) int { return cmp.Compare(b.quality, a.quality) })
}

// heardFrom notes that a layer 3 frame from origin arrived through
// neighbour, which is evidently a way back to it.  It keeps a route through
// that neighbour fresh, or else adds one at quality 0: used if there is
// nothing better, but never advertised, and never displacing one learned from
// a broadcast.
func (r *router) heardFrom(origin string, neighbour neighbour) {
	if strings.EqualFold(origin, r.myCall) {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if d, ok := r.destinations[origin]; ok {
		var i = slices.IndexFunc(d.routes, func(rt routeEntry) bool { return rt.neighbour == neighbour })
		if i >= 0 {
			d.routes[i].obsolescence = obsolescenceInit

			return
		}
	}

	r.addRoute(origin, "", neighbour, 0)
}

// age takes one from every route's obsolescence count, dropping those that
// reach zero and destinations left with no routes.  It is called each time we
// broadcast.
func (r *router) age() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for callsign, d := range r.destinations {
		d.routes = slices.DeleteFunc(d.routes, func(rt routeEntry) bool { return rt.obsolescence <= 1 })

		for i := range d.routes {
			d.routes[i].obsolescence--
		}

		if len(d.routes) == 0 {
			delete(r.destinations, callsign)
		}
	}
}

// neighbourFailed notes that a link to neighbour could not be made or was
// lost.  Once it has failed neighbourFailLimit times in a row, routes
// through it are passed over until it is heard from again.
func (r *router) neighbourFailed(neighbour neighbour) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.failures[neighbour]++
}

// neighbourHeard notes that neighbour is evidently working again.
func (r *router) neighbourHeard(neighbour neighbour) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.failures, neighbour)
}

// resolve turns what someone asked to connect to - a node's callsign or its
// alias - into the callsign of a destination in the table.
func (r *router) resolve(name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var call, err = NormaliseCallsign(name)
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
func (r *router) nextHop(callsign string) (neighbour, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var d, ok = r.destinations[callsign]
	if !ok {
		var none neighbour

		return none, false
	}

	for _, rt := range d.routes {
		if r.failures[rt.neighbour] < neighbourFailLimit {
			return rt.neighbour, true
		}
	}

	var none neighbour

	return none, false
}

// broadcastEntries is what we tell our neighbours we can reach: for each
// destination, its best route that is neither on its way out nor through a
// neighbour we are passing over as failing - what we would use ourselves.
// They are in callsign order, so that a broadcast is the same from one time
// to the next for the same table.
func (r *router) broadcastEntries() []NodesEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	var entries = make([]NodesEntry, 0, len(r.destinations))

	for _, d := range r.destinations {
		var i = slices.IndexFunc(d.routes, func(rt routeEntry) bool {
			return rt.obsolescence >= minBroadcastObsolescence && rt.quality > 0 &&
				r.failures[rt.neighbour] < neighbourFailLimit
		})
		if i < 0 {
			continue
		}

		var best = d.routes[i]

		entries = append(entries, NodesEntry{
			callsign:  d.callsign,
			alias:     d.alias,
			neighbour: best.neighbour.callsign,
			quality:   best.quality,
		})
	}

	slices.SortFunc(entries, func(a, b NodesEntry) int { return strings.Compare(a.callsign, b.callsign) })

	return entries
}
