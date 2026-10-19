// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"cmp"
	"maps"
	"slices"
)

// Status is what a node is, and what it knows, at a moment: for showing to
// someone, not for acting on.
type Status struct {
	Channel  int
	Callsign string
	Alias    string

	Neighbours   []NeighbourStatus   // In callsign order.
	Destinations []DestinationStatus // In callsign order.
	Circuits     []CircuitStatus     // In order of their own callsign, then the far end's.
}

// NeighbourStatus is a neighbour: a node we have a link to, or a route
// through.
type NeighbourStatus struct {
	Callsign string

	// Link is "up", "connecting" or "down".
	Link string

	// Failures is how many times in a row a link to it has failed.  Once
	// that reaches neighbourFailLimit, Failing is set and routes through it
	// are passed over until it is heard from again.
	Failures int
	Failing  bool
}

// DestinationStatus is a node in the routing table, with its routes best
// first.
type DestinationStatus struct {
	Callsign string
	Alias    string
	Routes   []RouteStatus
}

// RouteStatus is one way of reaching a destination.
type RouteStatus struct {
	Neighbour    string
	Quality      int
	Obsolescence int
}

// CircuitStatus is a client's circuit.
type CircuitStatus struct {
	OwnCall    string // What the client calls this end.
	RemoteCall string // What it calls the far end.
	RemoteNode string // The node at the far end.
	User       string // Whose circuit it is, as the CONNECT REQUEST said.
	Incoming   bool

	// State is "connecting", "connected" or "disconnecting".
	State string

	// Outstanding is how many segments are still to go out or be
	// acknowledged.
	Outstanding int
}

func (s linkState) String() string {
	switch s {
	case neighbourUp:
		return "up"
	case neighbourConnecting:
		return "connecting"
	case neighbourDown:
	}

	return "down"
}

func (s circuitState) String() string {
	switch s {
	case stateConnecting:
		return "connecting"
	case stateConnected:
		return "connected"
	case stateDisconnecting:
		return "disconnecting"
	case stateClosed:
	}

	return "closed"
}

// Status is the node as it is now.
func (n *Node) Status() Status {
	var s Status
	s.Channel = n.cfg.Channel
	s.Callsign = n.cfg.Callsign
	s.Alias = n.cfg.Alias

	var failures map[string]int

	s.Destinations, failures = n.router.status()
	s.Circuits = n.circuits.status()

	var links = make(map[string]linkState)

	n.mu.Lock()
	for call, l := range n.links {
		links[call] = l.state
	}
	n.mu.Unlock()

	// A neighbour is anything with a link, or a route through it, or a
	// failure count - each of which can outlast the others.
	var neighbours = make(map[string]bool)
	for call := range links {
		neighbours[call] = true
	}

	for call := range failures {
		neighbours[call] = true
	}

	for _, d := range s.Destinations {
		for _, rt := range d.Routes {
			neighbours[rt.Neighbour] = true
		}
	}

	for _, call := range slices.Sorted(maps.Keys(neighbours)) {
		var nb NeighbourStatus
		nb.Callsign = call
		nb.Link = links[call].String()
		nb.Failures = failures[call]
		nb.Failing = nb.Failures >= neighbourFailLimit
		s.Neighbours = append(s.Neighbours, nb)
	}

	return s
}

// status is the routing table, in callsign order, and the neighbours'
// failure counts.
func (r *router) status() ([]DestinationStatus, map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var destinations = make([]DestinationStatus, 0, len(r.destinations))

	for _, d := range r.destinations {
		var ds DestinationStatus
		ds.Callsign = d.callsign
		ds.Alias = d.alias

		for _, rt := range d.routes {
			ds.Routes = append(ds.Routes, RouteStatus{
				Neighbour:    rt.neighbour,
				Quality:      int(rt.quality),
				Obsolescence: rt.obsolescence,
			})
		}

		destinations = append(destinations, ds)
	}

	slices.SortFunc(destinations, func(a, b DestinationStatus) int { return cmp.Compare(a.Callsign, b.Callsign) })

	return destinations, maps.Clone(r.failures)
}

// status is the circuits that are open or on their way, for showing.
func (m *circuitManager) status() []CircuitStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var circuits = make([]CircuitStatus, 0, len(m.circuits))

	for _, c := range m.circuits {
		if c.state == stateClosed {
			continue
		}

		circuits = append(circuits, CircuitStatus{
			OwnCall:     c.id.ownCall,
			RemoteCall:  c.id.remoteCall,
			RemoteNode:  c.remoteNode,
			User:        c.user,
			Incoming:    c.incoming,
			State:       c.state.String(),
			Outstanding: len(c.sendQueue) + len(c.ackQueue),
		})
	}

	slices.SortFunc(circuits, func(a, b CircuitStatus) int {
		return cmp.Or(cmp.Compare(a.OwnCall, b.OwnCall), cmp.Compare(a.RemoteCall, b.RemoteCall))
	})

	return circuits
}
