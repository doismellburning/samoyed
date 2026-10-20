// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mheard

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hear records monitor as heard on channel, as the receive path would.
func hear(t *testing.T, mdb *DB, channel int, monitor string) {
	t.Helper()

	var pp = ax25.FromText(monitor, true)
	require.NotNil(t, pp)

	mdb.SaveRF(channel, pp, maybe.Nothing[float64](), maybe.Nothing[float64]())
}

// heardAt makes callsign heard over the radio at when, first and last.
func heardAt(mdb *DB, callsign string, when time.Time) {
	mdb.mu.Lock()
	defer mdb.mu.Unlock()

	mdb.db[callsign].firstHeardRF = when
	mdb.db[callsign].lastHeardRF = when
}

func TestHeardRF(t *testing.T) {
	t.Parallel()

	var mdb = New(0)
	var now = time.Now()

	hear(t, mdb, 0, "Q1TEST>APDW17:one")
	hear(t, mdb, 1, "Q2TEST-1>APDW17,Q9TEST*:two")
	hear(t, mdb, 0, "Q3TEST>APDW17:three")

	heardAt(mdb, "Q1TEST", now.Add(-time.Hour))
	heardAt(mdb, "Q2TEST-1", now.Add(-time.Minute))
	heardAt(mdb, "Q3TEST", now.Add(-time.Minute))

	// Heard only from APRS-IS, so not listed.
	mdb.SaveIS("Q4TEST>APDW17,TCPIP*,qAC,T2TEST:four")

	assert.Equal(t, []Heard{
		{Callsign: "Q2TEST-1", Channel: 1, Hops: 1, Packets: 1, FirstHeard: now.Add(-time.Minute), LastHeard: now.Add(-time.Minute), Lat: maybe.Nothing[float64](), Lon: maybe.Nothing[float64]()},
		{Callsign: "Q3TEST", Channel: 0, Hops: 0, Packets: 1, FirstHeard: now.Add(-time.Minute), LastHeard: now.Add(-time.Minute), Lat: maybe.Nothing[float64](), Lon: maybe.Nothing[float64]()},
		{Callsign: "Q1TEST", Channel: 0, Hops: 0, Packets: 1, FirstHeard: now.Add(-time.Hour), LastHeard: now.Add(-time.Hour), Lat: maybe.Nothing[float64](), Lon: maybe.Nothing[float64]()},
	}, mdb.HeardRF(10))

	assert.Len(t, mdb.HeardRF(2), 2)
	assert.Equal(t, "Q2TEST-1", mdb.HeardRF(1)[0].Callsign)
	assert.Empty(t, mdb.HeardRF(0))
	assert.Empty(t, mdb.HeardRF(-1))
}

func TestHeardRFCountsAndRemembers(t *testing.T) {
	t.Parallel()

	var mdb = New(0)

	var before = time.Now()

	// Heard from APRS-IS first, which is neither counted nor taken as
	// first heard over the radio.
	mdb.SaveIS("Q1TEST>APDW17,TCPIP*,qAC,T2TEST:zero")

	hear(t, mdb, 0, "Q1TEST>APDW17:one")

	var first = mdb.HeardRF(1)[0].FirstHeard
	assert.False(t, first.Before(before))

	var pp = ax25.FromText("Q1TEST>APDW17:two", true)
	require.NotNil(t, pp)
	mdb.SaveRF(1, pp, maybe.Just(51.5), maybe.Just(-1.0))

	var got = mdb.HeardRF(1)[0]
	assert.Equal(t, 2, got.Packets)
	assert.Equal(t, first, got.FirstHeard, "first heard is kept")
	assert.False(t, got.LastHeard.Before(first))
	assert.Equal(t, 1, got.Channel)
	assert.Equal(t, maybe.Just(51.5), got.Lat)
	assert.Equal(t, maybe.Just(-1.0), got.Lon)
}

func TestHeardRFEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, New(0).HeardRF(10))
}
