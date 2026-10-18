// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package webui

import (
	"net/http"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
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

	// IGate is the IGate's connection to APRS-IS, or Nothing on a station
	// that isn't an IGate.
	IGate maybe.Maybe[IGateStatus] `json:"igate,omitzero"`
}

// IGateStatus is an IGate's connection to its APRS-IS server, and how much has
// passed over it.
type IGateStatus struct {
	Server string `json:"server"` // host:port

	// Connected says there is a connection to the server, and LoggedIn
	// that the login on it has been sent, so packets are being passed up.
	Connected bool `json:"connected"`
	LoggedIn  bool `json:"loggedIn"`

	// ConnectedAt is when the most recent connection was made, Nothing
	// if none ever has been.
	ConnectedAt maybe.Maybe[time.Time] `json:"connectedAt,omitzero"`

	UplinkPackets   int `json:"uplinkPackets"`   // Passed up to the server.
	UplinkBytes     int `json:"uplinkBytes"`     // Sent to the server, logins and heartbeats included.
	DownlinkPackets int `json:"downlinkPackets"` // Received from the server.
	DownlinkBytes   int `json:"downlinkBytes"`   // Received from the server, everything included.
	RFPackets       int `json:"rfPackets"`       // Gated from the server to radio.
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
