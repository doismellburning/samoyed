// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusRequest(t *testing.T, status StatusFunc, now time.Time) *httptest.ResponseRecorder {
	t.Helper()

	var w = httptest.NewRecorder()
	var r = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/status", nil)

	serveStatus(w, r, status, func() time.Time { return now })

	return w
}

// Uptime is worked out on the station, from its own clock.
func TestStatusUptimeIsFromTheStationsClock(t *testing.T) {
	t.Parallel()

	var started = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	var w = statusRequest(t, func() Status {
		var s Status
		s.Version = "2026.10.18"
		s.Started = started

		return s
	}, started.Add(25*time.Hour+30*time.Second))

	require.Equal(t, http.StatusOK, w.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, map[string]any{
		"version":       "2026.10.18",
		"started":       "2026-10-18T09:00:00Z",
		"uptimeSeconds": float64(25*60*60 + 30),
	}, got)
}

func TestStatusWithoutASourceIsNotFound(t *testing.T) {
	t.Parallel()

	assert.Equal(t, http.StatusNotFound, statusRequest(t, nil, time.Now()).Code)
}

// A station that isn't an IGate says nothing about one; one that is says how
// its connection stands.
func TestStatusIGate(t *testing.T) {
	t.Parallel()

	var connectedAt = time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)

	var igate IGateStatus
	igate.Server = "localhost:14580"
	igate.Connected = true
	igate.ConnectedAt = maybe.Just(connectedAt)
	igate.UplinkPackets = 1

	for _, tc := range []struct {
		name  string
		igate maybe.Maybe[IGateStatus]
		want  any
	}{
		{"none", maybe.Nothing[IGateStatus](), nil},
		{"connected", maybe.Just(igate), map[string]any{
			"server":          "localhost:14580",
			"connected":       true,
			"loggedIn":        false,
			"connectedAt":     "2026-10-18T09:00:00Z",
			"uplinkPackets":   float64(1),
			"uplinkBytes":     float64(0),
			"downlinkPackets": float64(0),
			"downlinkBytes":   float64(0),
			"rfPackets":       float64(0),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w = statusRequest(t, func() Status {
				var s Status
				s.IGate = tc.igate

				return s
			}, connectedAt)

			var got map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, tc.want, got["igate"])
		})
	}
}
