// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"slices"
	"strings"
)

// MaxRoutes is how many routes a destination keeps: the best few, so that
// when one fails there is another to fall back on.
const MaxRoutes = 3

// NeighbourKey names a neighbour: the same station heard on two ports is two
// neighbours, since a route through one is not a route through the other.
type NeighbourKey struct {
	Port int
	Call string
}

// Neighbour is a node heard directly, which packets can be handed to.
type Neighbour struct {
	NeighbourKey

	Alias   string
	Quality int  // The quality of the link to it, 0 to 255.
	Locked  bool // Configured, rather than learned: never aged out.
}

// Route is one way of reaching a destination: through a neighbour, at a
// quality, until it has gone unrefreshed for long enough.
type Route struct {
	Neighbour    NeighbourKey
	Quality      int
	Obsolescence int  // Counts down once per broadcast interval; gone at zero.
	Locked       bool // A route to a locked neighbour itself, which never ages.
}

// Destination is a node the table knows a way to.
type Destination struct {
	Call   string
	Alias  string
	Routes []Route // Best first.
}

// Best returns the destination's best route.
func (d *Destination) Best() (Route, bool) {
	if len(d.Routes) == 0 {
		var none Route

		return none, false
	}

	return d.Routes[0], true
}

// TableConfig is how a Table judges and ages what it learns.
type TableConfig struct {
	MyCall string // This node; routes back through it are ignored.

	// MinQuality is the least quality worth keeping a route at.
	MinQuality int

	// ObsolescenceInit is what a route's obsolescence count starts at each
	// time a broadcast refreshes it.
	ObsolescenceInit int
}

// Table is the routing table NODES broadcasts build.
type Table struct {
	cfg          TableConfig
	neighbours   map[NeighbourKey]*Neighbour
	destinations map[string]*Destination
}

// NewTable returns an empty table.
func NewTable(cfg TableConfig) *Table {
	var t = new(Table)
	t.cfg = cfg
	t.neighbours = make(map[NeighbourKey]*Neighbour)
	t.destinations = make(map[string]*Destination)

	return t
}

// LockNeighbour configures a neighbour at a fixed quality, which a broadcast
// from it does not override and which never ages out.
func (t *Table) LockNeighbour(key NeighbourKey, alias string, quality int) {
	var n = t.neighbour(key)
	n.Locked = true
	n.Quality = quality

	if alias != "" {
		n.Alias = alias
	}

	var d = t.destination(key.Call)
	if alias != "" {
		d.Alias = alias
	}

	t.addRoute(d, Route{Neighbour: key, Quality: quality, Obsolescence: t.cfg.ObsolescenceInit, Locked: true})
}

// Neighbour returns the neighbour named by key.
func (t *Table) Neighbour(key NeighbourKey) (Neighbour, bool) {
	var n, ok = t.neighbours[key]
	if !ok {
		var none Neighbour

		return none, false
	}

	return *n, true
}

// Heard takes in a NODES broadcast nb, heard from the neighbour key over a
// port whose quality is portQuality, and reports whether the table changed.
func (t *Table) Heard(key NeighbourKey, portQuality int, nb NodesBroadcast) bool {
	if key.Call == t.cfg.MyCall {
		return false
	}

	var n = t.neighbour(key)
	n.Alias = nb.Alias

	if !n.Locked {
		n.Quality = portQuality
	}

	var changed = false

	var self = t.destination(key.Call)
	self.Alias = nb.Alias

	if t.addRoute(self, Route{Neighbour: key, Quality: n.Quality, Obsolescence: t.cfg.ObsolescenceInit, Locked: n.Locked}) {
		changed = true
	}

	for _, e := range nb.Entries {
		if e.Call == t.cfg.MyCall || e.BestNeighbour == t.cfg.MyCall || e.Call == key.Call {
			continue // Routes back through us are no routes at all.
		}

		var quality = (n.Quality*e.Quality + 128) / 256
		if quality < t.cfg.MinQuality || quality == 0 {
			continue
		}

		var d = t.destination(e.Call)
		if e.Alias != "" {
			d.Alias = e.Alias
		}

		if t.addRoute(d, Route{Neighbour: key, Quality: quality, Obsolescence: t.cfg.ObsolescenceInit, Locked: false}) {
			changed = true
		}
	}

	t.prune()

	return changed
}

// Age counts every unlocked route one interval nearer obsolescence, dropping
// those that get there, and reports whether anything was dropped.
func (t *Table) Age() bool {
	var changed = false

	for _, d := range t.destinations {
		for i := range d.Routes {
			if !d.Routes[i].Locked {
				d.Routes[i].Obsolescence--
			}
		}

		var before = len(d.Routes)

		d.Routes = slices.DeleteFunc(d.Routes, func(r Route) bool {
			return !r.Locked && r.Obsolescence <= 0
		})

		if len(d.Routes) != before {
			changed = true
		}
	}

	t.prune()

	return changed
}

// Failed drops every unlocked route through the neighbour key, which could not
// be reached; its next broadcast brings them back.
func (t *Table) Failed(key NeighbourKey) {
	for _, d := range t.destinations {
		d.Routes = slices.DeleteFunc(d.Routes, func(r Route) bool {
			return r.Neighbour == key && !r.Locked
		})
	}

	t.prune()
}

// Advertise returns the destinations to put in this node's NODES broadcast:
// each one's best route, if it has not gone unrefreshed for too long.
func (t *Table) Advertise(minObsolescence int) []NodesEntry {
	var entries []NodesEntry

	for _, d := range t.Destinations() {
		var best, ok = d.Best()
		if !ok || best.Quality == 0 || (!best.Locked && best.Obsolescence < minObsolescence) {
			continue
		}

		entries = append(entries, NodesEntry{
			Call:          d.Call,
			Alias:         d.Alias,
			BestNeighbour: best.Neighbour.Call,
			Quality:       best.Quality,
		})
	}

	return entries
}

// Lookup finds a destination by callsign or alias.
func (t *Table) Lookup(name string) (Destination, bool) {
	var upper = strings.ToUpper(strings.TrimSpace(name))

	var call, err = NormaliseCall(upper)
	if err == nil {
		if d, ok := t.destinations[call]; ok {
			return copyDestination(d), true
		}
	}

	for _, d := range t.Destinations() {
		if d.Alias != "" && strings.EqualFold(d.Alias, upper) {
			return d, true
		}
	}

	var none Destination

	return none, false
}

// Destinations returns a copy of every destination, sorted by alias then
// callsign, as node software lists them.
func (t *Table) Destinations() []Destination {
	var ds = make([]Destination, 0, len(t.destinations))
	for _, d := range t.destinations {
		ds = append(ds, copyDestination(d))
	}

	slices.SortFunc(ds, func(a, b Destination) int {
		if c := strings.Compare(a.Alias, b.Alias); c != 0 {
			return c
		}

		return strings.Compare(a.Call, b.Call)
	})

	return ds
}

// Neighbours returns a copy of every neighbour, sorted by port then callsign.
func (t *Table) Neighbours() []Neighbour {
	var ns = make([]Neighbour, 0, len(t.neighbours))
	for _, n := range t.neighbours {
		ns = append(ns, *n)
	}

	slices.SortFunc(ns, func(a, b Neighbour) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}

		return strings.Compare(a.Call, b.Call)
	})

	return ns
}

// RoutesVia counts the destinations routed through the neighbour key.
func (t *Table) RoutesVia(key NeighbourKey) int {
	var count = 0

	for _, d := range t.destinations {
		for _, r := range d.Routes {
			if r.Neighbour == key {
				count++
			}
		}
	}

	return count
}

func copyDestination(d *Destination) Destination {
	var c = *d
	c.Routes = slices.Clone(d.Routes)

	return c
}

func (t *Table) neighbour(key NeighbourKey) *Neighbour {
	var n, ok = t.neighbours[key]
	if !ok {
		n = new(Neighbour)
		n.NeighbourKey = key
		t.neighbours[key] = n
	}

	return n
}

func (t *Table) destination(call string) *Destination {
	var d, ok = t.destinations[call]
	if !ok {
		d = new(Destination)
		d.Call = call
		t.destinations[call] = d
	}

	return d
}

// addRoute puts r in d's routes, replacing any through the same neighbour, and
// keeps only the best MaxRoutes.  It reports whether the best route changed.
func (t *Table) addRoute(d *Destination, r Route) bool {
	var before, hadBest = d.Best()

	var i = slices.IndexFunc(d.Routes, func(old Route) bool { return old.Neighbour == r.Neighbour })
	if i >= 0 {
		if d.Routes[i].Locked && !r.Locked {
			// A broadcast refreshes a locked route's count, never its quality.
			d.Routes[i].Obsolescence = r.Obsolescence

			return false
		}

		d.Routes[i] = r
	} else {
		d.Routes = append(d.Routes, r)
	}

	slices.SortStableFunc(d.Routes, func(a, b Route) int { return b.Quality - a.Quality })

	if len(d.Routes) > MaxRoutes {
		d.Routes = d.Routes[:MaxRoutes]
	}

	var after, _ = d.Best()

	return !hadBest || before.Neighbour != after.Neighbour || before.Quality != after.Quality
}

// prune drops destinations with no routes left, and learned neighbours no
// route goes through.
func (t *Table) prune() {
	var used = make(map[NeighbourKey]bool)

	for call, d := range t.destinations {
		if len(d.Routes) == 0 {
			delete(t.destinations, call)

			continue
		}

		for _, r := range d.Routes {
			used[r.Neighbour] = true
		}
	}

	for key, n := range t.neighbours {
		if !n.Locked && !used[key] {
			delete(t.neighbours, key)
		}
	}
}
