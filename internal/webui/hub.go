// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package webui serves a read-only web dashboard and APRS map of what a
// station is hearing and sending.
//
// Everything here is push-based, like internal/metrics: the code that receives
// and transmits frames hands each one to a Hub, which keeps a little recent
// history for a page that has just loaded and streams new arrivals to the pages
// already open.  This package has no knowledge of, or dependency on, the code
// reporting into it; NewPacket turns an AX.25 frame and its APRS decode into
// the Packet a Hub takes.
package webui

import (
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	// DefaultPacketHistory is how many recent packets a Hub keeps for a newly
	// loaded page.
	DefaultPacketHistory = 200

	// DefaultStationMaxAge is how long a station stays on the map and in the
	// heard list after it was last heard.
	DefaultStationMaxAge = 2 * time.Hour

	// DefaultMaxStations bounds the station table, so an APRS-IS feed with a
	// generous filter can't grow it without limit.  The least recently heard
	// station makes way for a new one.
	DefaultMaxStations = 5000

	// subscriberBuffer is how many events a page may fall behind by before
	// it is cut off.  Its browser reconnects and re-fetches the snapshot, so
	// cutting off loses nothing but a moment's liveness, whereas waiting for
	// it would hold up the receive path.
	subscriberBuffer = 64

	// prunePeriod is how often expired stations are swept out as packets
	// arrive.
	prunePeriod = time.Minute
)

// Direction says whether a packet was received or transmitted.
type Direction string

// The directions a Packet can have.
const (
	Received    Direction = "rx"
	Transmitted Direction = "tx"
)

// Position is a latitude and longitude in decimal degrees, negative for south
// and west.
type Position struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Packet is one frame received or transmitted, with whatever an APRS decode
// made of it.  Every text field came off the air or the internet and must be
// treated as untrusted.
type Packet struct {
	Time      time.Time
	Channel   int
	Direction Direction
	Via       string // Where a received packet came from: "radio", "aprs-is", "dtmf" or "network".

	// Station is the callsign, or object or item name, that the packet is
	// about - the key it is filed under in the station table.  Empty for a
	// packet that isn't about anyone in particular.
	Station string

	Monitor     string // The whole packet in monitor format, "SRC>DST,PATH:info".
	Description string // What kind of APRS packet it is, if it is one.

	Position maybe.Maybe[Position]
	Symbol   string // APRS symbol table and code, two characters, or empty.
	Comment  string

	AudioLevel maybe.Maybe[int]
}

// packetJSON is Packet as a browser sees it: an absent value is left out.
type packetJSON struct {
	Time        time.Time `json:"time"`
	Channel     int       `json:"channel"`
	Direction   Direction `json:"dir"`
	Via         string    `json:"via,omitempty"`
	Station     string    `json:"station,omitempty"`
	Monitor     string    `json:"monitor"`
	Description string    `json:"description,omitempty"`
	Position    *Position `json:"position,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	Comment     string    `json:"comment,omitempty"`
	AudioLevel  *int      `json:"audioLevel,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (p Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(packetJSON{
		Time:        p.Time,
		Channel:     p.Channel,
		Direction:   p.Direction,
		Via:         p.Via,
		Station:     p.Station,
		Monitor:     p.Monitor,
		Description: p.Description,
		Position:    toPointer(p.Position),
		Symbol:      p.Symbol,
		Comment:     p.Comment,
		AudioLevel:  toPointer(p.AudioLevel),
	})
}

// Station is what the Hub remembers about one station it has heard.
type Station struct {
	Name      string
	Channel   int // Most recent channel it was heard on.
	Via       string
	FirstSeen time.Time
	LastHeard time.Time
	Count     int

	// The most recent of each of these that the station has sent; a packet
	// without one doesn't erase what an earlier one said.
	Position maybe.Maybe[Position]
	Symbol   string
	Comment  string
}

type stationJSON struct {
	Name      string    `json:"name"`
	Channel   int       `json:"channel"`
	Via       string    `json:"via,omitempty"`
	FirstSeen time.Time `json:"firstSeen"`
	LastHeard time.Time `json:"lastHeard"`
	Count     int       `json:"count"`
	Position  *Position `json:"position,omitempty"`
	Symbol    string    `json:"symbol,omitempty"`
	Comment   string    `json:"comment,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (s Station) MarshalJSON() ([]byte, error) {
	return json.Marshal(stationJSON{
		Name:      s.Name,
		Channel:   s.Channel,
		Via:       s.Via,
		FirstSeen: s.FirstSeen,
		LastHeard: s.LastHeard,
		Count:     s.Count,
		Position:  toPointer(s.Position),
		Symbol:    s.Symbol,
		Comment:   s.Comment,
	})
}

// Channel describes one configured channel and how much has passed over it.
type Channel struct {
	Number      int    `json:"number"`
	Description string `json:"description"` // e.g. "radio", "aprs-is", "network".
	Received    int    `json:"rx"`
	Transmitted int    `json:"tx"`
}

// event is one message for the pages that are open, encoded once for all of
// them.
type event struct {
	name string
	data []byte
}

type subscriber struct {
	events chan event
}

// Hub holds the recent history and fans each new packet out to the open pages.
// All methods are safe for concurrent use, and are no-ops on a nil *Hub, so a
// caller can report into one unconditionally whether or not the web interface
// is enabled.
type Hub struct {
	mu sync.Mutex

	packets    []Packet // Ring buffer of recent packets.
	nextPacket int      // Where the next packet goes once packets is full.
	maxPackets int

	stations      map[string]*Station
	maxStations   int
	stationMaxAge time.Duration
	lastPrune     time.Time

	channels map[int]*Channel

	subscribers map[*subscriber]struct{}

	now func() time.Time
}

// NewHub returns an empty Hub with the default limits.
func NewHub() *Hub {
	var h = new(Hub)
	h.maxPackets = DefaultPacketHistory
	h.packets = make([]Packet, 0, h.maxPackets)
	h.stations = make(map[string]*Station)
	h.maxStations = DefaultMaxStations
	h.stationMaxAge = DefaultStationMaxAge
	h.channels = make(map[int]*Channel)
	h.subscribers = make(map[*subscriber]struct{})
	h.now = time.Now

	return h
}

// AddChannel declares a channel, so it shows up before anything has been heard
// on it.
func (h *Hub) AddChannel(number int, description string) {
	if h == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.channel(number).Description = description
}

// Publish records a packet and sends it to every open page.  A received packet
// with a Station also updates that station's entry.  A zero Time is filled in
// with the current time.
func (h *Hub) Publish(p Packet) {
	if h == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var now = h.now()
	if p.Time.IsZero() {
		p.Time = now
	}

	if len(h.packets) < h.maxPackets {
		h.packets = append(h.packets, p)
	} else {
		h.packets[h.nextPacket] = p
		h.nextPacket = (h.nextPacket + 1) % h.maxPackets
	}

	var ch = h.channel(p.Channel)
	if p.Direction == Transmitted {
		ch.Transmitted++
	} else {
		ch.Received++
	}

	h.broadcast("packet", p)

	if p.Direction == Received && p.Station != "" {
		h.broadcast("station", *h.updateStation(p))
	}

	if now.Sub(h.lastPrune) >= prunePeriod {
		h.pruneStations(now)
		h.lastPrune = now
	}
}

// Packets returns the recent packets, oldest first.
func (h *Hub) Packets() []Packet {
	if h == nil {
		return []Packet{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var out = make([]Packet, 0, len(h.packets))
	out = append(out, h.packets[h.nextPacket:]...)
	out = append(out, h.packets[:h.nextPacket]...)

	return out
}

// Stations returns the stations heard within the age limit, most recently
// heard first.
func (h *Hub) Stations() []Station {
	if h == nil {
		return []Station{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.pruneStations(h.now())

	var out = make([]Station, 0, len(h.stations))
	for _, s := range h.stations {
		out = append(out, *s)
	}

	slices.SortFunc(out, func(a, b Station) int {
		return b.LastHeard.Compare(a.LastHeard)
	})

	return out
}

// Channels returns the channels in number order.
func (h *Hub) Channels() []Channel {
	if h == nil {
		return []Channel{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var out = make([]Channel, 0, len(h.channels))
	for _, n := range slices.Sorted(maps.Keys(h.channels)) {
		out = append(out, *h.channels[n])
	}

	return out
}

// subscribe registers a new open page.  Its channel is closed when the Hub
// cuts it off for falling behind; it must call unsubscribe once it is done
// either way.
func (h *Hub) subscribe() *subscriber {
	var s = new(subscriber)
	s.events = make(chan event, subscriberBuffer)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.subscribers[s] = struct{}{}

	return s
}

func (h *Hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.subscribers[s]; ok {
		delete(h.subscribers, s)
		close(s.events)
	}
}

// broadcast sends an event to every subscriber, cutting off any that has
// fallen too far behind to take it.  The caller holds h.mu.
func (h *Hub) broadcast(name string, v any) {
	if len(h.subscribers) == 0 {
		return
	}

	data, err := json.Marshal(v) // := for errchkjson, which doesn't follow var.
	if err != nil {
		// Nothing we build can fail to marshal, so this is a bug, but
		// not one worth taking the station down for.
		return
	}

	var e = event{name: name, data: data}

	for s := range h.subscribers {
		select {
		case s.events <- e:
		default:
			delete(h.subscribers, s)
			close(s.events)
		}
	}
}

// channel returns the entry for channel n, creating it if need be.  The caller
// holds h.mu.
func (h *Hub) channel(n int) *Channel {
	var ch, ok = h.channels[n]
	if !ok {
		ch = new(Channel)
		ch.Number = n
		h.channels[n] = ch
	}

	return ch
}

// updateStation files a received packet under its station.  The caller holds
// h.mu.
func (h *Hub) updateStation(p Packet) *Station {
	var s, ok = h.stations[p.Station]
	if !ok {
		if len(h.stations) >= h.maxStations {
			h.evictOldestStation()
		}

		s = new(Station)
		s.Name = p.Station
		s.FirstSeen = p.Time
		h.stations[p.Station] = s
	}

	s.Channel = p.Channel
	s.Via = p.Via
	s.LastHeard = p.Time
	s.Count++
	s.Position = p.Position.Or(s.Position)

	if p.Symbol != "" {
		s.Symbol = p.Symbol
	}

	if p.Comment != "" {
		s.Comment = p.Comment
	}

	return s
}

// The caller holds h.mu.
func (h *Hub) evictOldestStation() {
	var oldest *Station
	for _, s := range h.stations {
		if oldest == nil || s.LastHeard.Before(oldest.LastHeard) {
			oldest = s
		}
	}

	if oldest != nil {
		delete(h.stations, oldest.Name)
	}
}

// The caller holds h.mu.
func (h *Hub) pruneStations(now time.Time) {
	maps.DeleteFunc(h.stations, func(_ string, s *Station) bool {
		return now.Sub(s.LastHeard) > h.stationMaxAge
	})
}

func toPointer[T any](m maybe.Maybe[T]) *T {
	if v, ok := m.Get(); ok {
		return &v
	}

	return nil
}
