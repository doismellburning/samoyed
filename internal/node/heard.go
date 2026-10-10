// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"slices"
	"time"
)

// HeardStation is a station the node has heard directly.
type HeardStation struct {
	Port   int
	Call   string
	Last   time.Time
	Frames int
}

// HeardList remembers the stations heard on each port, as the node's MH
// command lists them.  It is not safe for concurrent use.
type HeardList struct {
	max      int
	stations map[heardKey]*HeardStation
}

type heardKey struct {
	port int
	call string
}

// NewHeardList returns a list remembering up to limit stations, forgetting
// the one heard longest ago to make room for another.
func NewHeardList(limit int) *HeardList {
	var h = new(HeardList)
	h.max = limit
	h.stations = make(map[heardKey]*HeardStation)

	return h
}

// heardQuiet is how long a station must go unheard to count as newly heard
// when it is heard again.
const heardQuiet = 15 * time.Minute

// Heard notes a frame from call on port at now, and says whether the station
// is newly heard: never before, or not for a while.
func (h *HeardList) Heard(port int, call string, now time.Time) bool {
	var key = heardKey{port: port, call: call}

	var s, ok = h.stations[key]
	var fresh = !ok || now.Sub(s.Last) >= heardQuiet

	if !ok {
		if len(h.stations) >= h.max {
			h.forgetOldest()
		}

		s = new(HeardStation)
		s.Port = port
		s.Call = call
		h.stations[key] = s
	}

	s.Last = now
	s.Frames++

	return fresh
}

func (h *HeardList) forgetOldest() {
	var oldest heardKey

	var found = false

	for k, s := range h.stations {
		if !found || s.Last.Before(h.stations[oldest].Last) {
			oldest = k
			found = true
		}
	}

	delete(h.stations, oldest)
}

// Stations returns the stations heard on port, or on every port for a
// negative one, most recent first.
func (h *HeardList) Stations(port int) []HeardStation {
	var list []HeardStation

	for _, s := range h.stations {
		if port < 0 || s.Port == port {
			list = append(list, *s)
		}
	}

	slices.SortFunc(list, func(a, b HeardStation) int { return b.Last.Compare(a.Last) })

	return list
}
