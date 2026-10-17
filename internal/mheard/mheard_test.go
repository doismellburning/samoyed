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

// heardAt makes callsign last heard over the radio at when.
func heardAt(mdb *DB, callsign string, when time.Time) {
	mdb.mu.Lock()
	defer mdb.mu.Unlock()

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
		{Callsign: "Q2TEST-1", Channel: 1, Hops: 1, LastHeard: now.Add(-time.Minute)},
		{Callsign: "Q3TEST", Channel: 0, Hops: 0, LastHeard: now.Add(-time.Minute)},
		{Callsign: "Q1TEST", Channel: 0, Hops: 0, LastHeard: now.Add(-time.Hour)},
	}, mdb.HeardRF(10))

	assert.Len(t, mdb.HeardRF(2), 2)
	assert.Equal(t, "Q2TEST-1", mdb.HeardRF(1)[0].Callsign)
	assert.Empty(t, mdb.HeardRF(0))
	assert.Empty(t, mdb.HeardRF(-1))
}

func TestHeardRFEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, New(0).HeardRF(10))
}
