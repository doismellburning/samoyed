// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
