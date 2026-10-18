// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"net/http"
	"time"
)

// Status is what the station says about itself, as opposed to what it has
// heard: what it is running and since when.
type Status struct {
	Version string    `json:"version"`
	Started time.Time `json:"started"`

	// UptimeSeconds is filled in by the handler from Started, so a page
	// can show how long the station has been up without trusting its
	// browser's clock to agree with the station's.
	UptimeSeconds int64 `json:"uptimeSeconds"`
}

// StatusFunc reports the station's current Status.  It is called for each
// request, from the server's goroutines, so must be safe for concurrent use.
type StatusFunc func() Status

func serveStatus(w http.ResponseWriter, r *http.Request, status StatusFunc, now func() time.Time) {
	if status == nil {
		http.NotFound(w, r)

		return
	}

	var s = status()
	if !s.Started.IsZero() {
		s.UptimeSeconds = int64(now().Sub(s.Started).Seconds())
	}

	writeJSON(w, s)
}
