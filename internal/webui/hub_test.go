// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEpoch() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// newTestHub returns a Hub whose clock is under the test's control.
func newTestHub() (*Hub, *time.Time) {
	var now = testEpoch()
	var h = NewHub()
	h.now = func() time.Time { return now }

	return h, &now
}

func rx(station string) Packet {
	var p Packet
	p.Direction = Received
	p.Station = station
	p.Monitor = station + ">APRS:test"

	return p
}

func TestNilHubIsANoOp(t *testing.T) {
	var h *Hub

	h.AddChannel(0, "radio")
	h.Publish(rx("Q1TEST"))

	assert.Empty(t, h.Packets())
	assert.Empty(t, h.Stations())
	assert.Empty(t, h.Channels())
}

func TestPacketsWrapOldestFirst(t *testing.T) {
	var h, _ = newTestHub()
	h.maxPackets = 3

	for _, s := range []string{"Q1TEST", "Q2TEST", "Q3TEST", "Q4TEST", "Q5TEST"} {
		h.Publish(rx(s))
	}

	var got []string
	for _, p := range h.Packets() {
		got = append(got, p.Station)
	}

	assert.Equal(t, []string{"Q3TEST", "Q4TEST", "Q5TEST"}, got)
}

func TestPublishFillsInTime(t *testing.T) {
	var h, _ = newTestHub()

	h.Publish(rx("Q1TEST"))

	assert.Equal(t, testEpoch(), h.Packets()[0].Time)
}

func TestStationKeepsEarlierPositionSymbolAndComment(t *testing.T) {
	var h, now = newTestHub()

	var first = rx("Q1TEST")
	first.Position = maybe.Just(Position{Lat: 51.5, Lon: -0.1})
	first.Symbol = "/>"
	first.Comment = "mobile"
	h.Publish(first)

	*now = now.Add(time.Minute)
	h.Publish(rx("Q1TEST")) // A status, say: no position, symbol or comment.

	var stations = h.Stations()
	require.Len(t, stations, 1)

	var s = stations[0]
	assert.Equal(t, maybe.Just(Position{Lat: 51.5, Lon: -0.1}), s.Position)
	assert.Equal(t, "/>", s.Symbol)
	assert.Equal(t, "mobile", s.Comment)
	assert.Equal(t, 2, s.Count)
	assert.Equal(t, testEpoch(), s.FirstSeen)
	assert.Equal(t, testEpoch().Add(time.Minute), s.LastHeard)
}

func TestTransmittedAndAnonymousPacketsMakeNoStation(t *testing.T) {
	var h, _ = newTestHub()

	var tx = rx("Q1TEST")
	tx.Direction = Transmitted
	h.Publish(tx)
	h.Publish(rx(""))

	assert.Empty(t, h.Stations())
	assert.Len(t, h.Packets(), 2)
}

func TestStationsMostRecentFirstAndExpire(t *testing.T) {
	var h, now = newTestHub()

	h.Publish(rx("Q1TEST"))
	*now = now.Add(time.Hour)
	h.Publish(rx("Q2TEST"))

	var stations = h.Stations()
	require.Len(t, stations, 2)
	assert.Equal(t, "Q2TEST", stations[0].Name)
	assert.Equal(t, "Q1TEST", stations[1].Name)

	*now = now.Add(DefaultStationMaxAge - time.Minute)

	stations = h.Stations()
	require.Len(t, stations, 1)
	assert.Equal(t, "Q2TEST", stations[0].Name)
}

func TestStationTableEvictsLeastRecentlyHeard(t *testing.T) {
	var h, now = newTestHub()
	h.maxStations = 2

	h.Publish(rx("Q1TEST"))
	*now = now.Add(time.Second)
	h.Publish(rx("Q2TEST"))
	*now = now.Add(time.Second)
	h.Publish(rx("Q1TEST"))
	*now = now.Add(time.Second)
	h.Publish(rx("Q3TEST"))

	var names []string
	for _, s := range h.Stations() {
		names = append(names, s.Name)
	}

	assert.Equal(t, []string{"Q3TEST", "Q1TEST"}, names)
}

func TestChannelCounts(t *testing.T) {
	var h, _ = newTestHub()

	h.AddChannel(1, "aprs-is")
	h.AddChannel(0, "radio")

	h.Publish(rx("Q1TEST"))
	h.Publish(rx("Q2TEST"))

	var tx = rx("")
	tx.Direction = Transmitted
	h.Publish(tx)

	assert.Equal(t, []Channel{
		{Number: 0, Description: "radio", Received: 2, Transmitted: 1},
		{Number: 1, Description: "aprs-is", Received: 0, Transmitted: 0},
	}, h.Channels())
}

func TestSubscriberGetsPacketThenStation(t *testing.T) {
	var h, _ = newTestHub()
	var sub = h.subscribe()

	defer h.unsubscribe(sub)

	h.Publish(rx("Q1TEST"))

	assert.Equal(t, "packet", (<-sub.events).name)
	assert.Equal(t, "station", (<-sub.events).name)
}

func TestSlowSubscriberIsCutOffWithoutBlocking(t *testing.T) {
	var h, _ = newTestHub()
	var slow = h.subscribe()

	// Each received packet with a station is two events, so this
	// overflows the buffer.  If a full subscriber blocked Publish, the
	// test would hang here.
	for range subscriberBuffer {
		h.Publish(rx("Q1TEST"))
	}

	var n = 0
	for range slow.events {
		n++
	}

	assert.Equal(t, subscriberBuffer, n)

	// Unsubscribing one the Hub has already cut off mustn't close its
	// channel again.
	assert.NotPanics(t, func() { h.unsubscribe(slow) })
}

func TestPacketJSONOmitsAbsentValues(t *testing.T) {
	var p = rx("Q1TEST")
	p.Time = testEpoch()

	data, err := json.Marshal(p) // := for errchkjson, which doesn't follow var.
	require.NoError(t, err)
	assert.NotContains(t, string(data), "position")
	assert.NotContains(t, string(data), "audioLevel")

	p.Position = maybe.Just(Position{Lat: 1.5, Lon: -2.5})
	p.AudioLevel = maybe.Just(0)

	data, err = json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"position":{"lat":1.5,"lon":-2.5}`)
	assert.Contains(t, string(data), `"audioLevel":0`)
}

// The page reads these field names, so pin the whole shape: a rename here is a
// change to what the browser receives.
func TestJSONShape(t *testing.T) {
	var full = Packet{
		Time:        testEpoch(),
		Channel:     1,
		Direction:   Received,
		Via:         "radio",
		Station:     "Q1TEST",
		Monitor:     "Q1TEST-APRS:!",
		Description: "Position",
		Position:    maybe.Just(Position{Lat: 1.5, Lon: -2.5}),
		Symbol:      "/-",
		Comment:     "hi",
		AudioLevel:  maybe.Just(50),
	}

	var empty = Packet{
		Time:        testEpoch(),
		Channel:     0,
		Direction:   Transmitted,
		Via:         "",
		Station:     "",
		Monitor:     "",
		Description: "",
		Position:    maybe.Nothing[Position](),
		Symbol:      "",
		Comment:     "",
		AudioLevel:  maybe.Nothing[int](),
	}

	var station = Station{
		Name:      "Q1TEST",
		Channel:   1,
		Via:       "radio",
		FirstSeen: testEpoch(),
		LastHeard: testEpoch(),
		Count:     2,
		Position:  maybe.Just(Position{Lat: 1.5, Lon: -2.5}),
		Symbol:    "/-",
		Comment:   "hi",
	}

	var bare = Station{
		Name:      "Q2TEST",
		Channel:   0,
		Via:       "",
		FirstSeen: testEpoch(),
		LastHeard: testEpoch(),
		Count:     1,
		Position:  maybe.Nothing[Position](),
		Symbol:    "",
		Comment:   "",
	}

	var cases = []struct {
		name string
		v    any
		want string
	}{
		{"full packet", full, `{"time":"2026-01-02T03:04:05Z","channel":1,"dir":"rx","via":"radio","station":"Q1TEST",` +
			`"monitor":"Q1TEST-APRS:!","description":"Position","position":{"lat":1.5,"lon":-2.5},` +
			`"symbol":"/-","comment":"hi","audioLevel":50}`},
		{"empty packet", empty, `{"time":"2026-01-02T03:04:05Z","channel":0,"dir":"tx","monitor":""}`},
		{"full station", station, `{"name":"Q1TEST","channel":1,"via":"radio","firstSeen":"2026-01-02T03:04:05Z",` +
			`"lastHeard":"2026-01-02T03:04:05Z","count":2,"position":{"lat":1.5,"lon":-2.5},"symbol":"/-","comment":"hi"}`},
		{"bare station", bare, `{"name":"Q2TEST","channel":0,"firstSeen":"2026-01-02T03:04:05Z",` +
			`"lastHeard":"2026-01-02T03:04:05Z","count":1}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := json.Marshal(c.v) // := for errchkjson, which doesn't follow var.
			require.NoError(t, err)
			assert.JSONEq(t, c.want, string(data))
			assert.Equal(t, c.want, string(data), "field order")
		})
	}
}
