// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package nodeevents carries what happens on a packet node - links and
// circuits coming and going, routes changing, stations heard - to whoever is
// watching: an MQTT publisher, say.
//
// Publishing never blocks: the node publishes from the goroutine everything
// else of its runs on, and a watcher that falls behind loses events rather
// than holding the node up.
package nodeevents

import (
	"sync"
	"time"
)

// Kind says what an Event is about.
type Kind string

const (
	LinkUp        Kind = "link/up"        // An AX.25 link to or from the node is up.
	LinkDown      Kind = "link/down"      // An AX.25 link has gone.
	CircuitUp     Kind = "circuit/up"     // A NET/ROM circuit is up.
	CircuitDown   Kind = "circuit/down"   // A NET/ROM circuit has gone.
	RoutesChanged Kind = "routes/changed" // The NET/ROM routing table changed.
	Heard         Kind = "heard"          // A station was heard, for the first time in a while.
)

// Event is something that happened on the node.  Which fields are set depends
// on its Kind.
type Event struct {
	Time time.Time `json:"time"`
	Kind Kind      `json:"kind"`
	Node string    `json:"node"` // The node's callsign.

	Port   *int   `json:"port,omitempty"`   // The channel, for a link or a station heard.
	Local  string `json:"local,omitempty"`  // Our end of a link or circuit.
	Remote string `json:"remote,omitempty"` // The far end.
	User   string `json:"user,omitempty"`   // Whose session a circuit carries.

	Incoming bool   `json:"incoming,omitempty"` // Whether the far end opened it.
	Role     string `json:"role,omitempty"`     // For a link: "neighbour", "user" or "downlink".
	Error    string `json:"error,omitempty"`    // Why a link or circuit went, if not in an orderly way.

	Destinations int `json:"destinations,omitempty"` // For routes: how many nodes are known.
	Neighbours   int `json:"neighbours,omitempty"`   // For routes: how many neighbours.
}

// Bus hands each Event published to every subscriber.  It is safe for
// concurrent use.
type Bus struct {
	mu     sync.Mutex
	subs   map[*subscription]bool
	onDrop func()
}

type subscription struct {
	ch chan Event
}

// NewBus returns a Bus that calls onDrop, if it is not nil, each time a
// subscriber misses an event for being too far behind.
func NewBus(onDrop func()) *Bus {
	var b = new(Bus)
	b.subs = make(map[*subscription]bool)
	b.onDrop = onDrop

	return b
}

// Publish hands e to every subscriber with room for it.  A nil Bus publishes
// nothing.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for s := range b.subs {
		select {
		case s.ch <- e:
		default:
			if b.onDrop != nil {
				b.onDrop()
			}
		}
	}
}

// Subscribe returns a channel of the events published from now on, holding up
// to buffer of them, and a function that ends the subscription and closes the
// channel.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	var s = new(subscription)
	s.ch = make(chan Event, buffer)

	b.mu.Lock()
	b.subs[s] = true
	b.mu.Unlock()

	var once sync.Once

	return s.ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, s)
			b.mu.Unlock()
			close(s.ch)
		})
	}
}
