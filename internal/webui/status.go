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

	// NetROM is the NET/ROM node, or Nothing on a station that doesn't
	// run one.
	NetROM maybe.Maybe[NetROMStatus] `json:"netrom,omitzero"`

	// Queues is what is waiting to be sent on each radio channel, empty on
	// a station with none.
	Queues []QueueStatus `json:"queues,omitempty"`

	// Clients is the client applications attached over AGW and KISS TCP,
	// empty on a station with none.
	Clients []ClientStatus `json:"clients,omitempty"`
}

// ClientStatus is a client application attached to one of the station's
// ports.
type ClientStatus struct {
	Protocol string `json:"protocol"` // "agw" or "kiss".
	Port     int    `json:"port"`     // The station's port it is attached to.
	Address  string `json:"address"`  // Where it is connected from, host:port.

	// Channel is the radio channel a KISS port carries, or Nothing for a
	// port that carries them all.
	Channel maybe.Maybe[int] `json:"channel,omitzero"`

	// Raw and Monitor say whether an AGW client has asked to be sent
	// received frames, in raw and monitor form.
	Raw     bool `json:"raw,omitempty"`
	Monitor bool `json:"monitor,omitempty"`

	// LoggedIn says whether an AGW client has logged in, or is Nothing
	// where it needs no login: none is configured, or it connected from
	// the station's own machine.
	LoggedIn maybe.Maybe[bool] `json:"loggedIn,omitzero"`
}

// QueueStatus is how many packets are waiting to be sent on a radio channel,
// at each priority: High for digipeated and expedited frames, Low for
// everything else.
type QueueStatus struct {
	Channel int `json:"channel"`
	High    int `json:"high"`
	Low     int `json:"low"`
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

// NetROMStatus is a NET/ROM node: what it is, who its neighbours are, what
// it can reach and the circuits it has open.
type NetROMStatus struct {
	Channel  int    `json:"channel"`
	Callsign string `json:"callsign"`
	Alias    string `json:"alias"`

	Neighbours   []NetROMNeighbour   `json:"neighbours"`
	Destinations []NetROMDestination `json:"destinations"`
	Circuits     []NetROMCircuit     `json:"circuits"`
}

// NetROMNeighbour is a node the NET/ROM node has a link to, or a route
// through.
type NetROMNeighbour struct {
	Callsign string `json:"callsign"`
	Link     string `json:"link"` // "up", "connecting" or "down".

	// Failures is how many times in a row a link to it has failed, and
	// Failing that routes through it are being passed over for now.
	Failures int  `json:"failures"`
	Failing  bool `json:"failing"`
}

// NetROMDestination is a node in the routing table, with its routes best
// first.
type NetROMDestination struct {
	Callsign string        `json:"callsign"`
	Alias    string        `json:"alias"`
	Routes   []NetROMRoute `json:"routes"`
}

// NetROMRoute is one way of reaching a destination: through a neighbour, at
// a quality from 0 to 255, for as many more of the node's own broadcasts as
// its obsolescence count says.
type NetROMRoute struct {
	Neighbour    string `json:"neighbour"`
	Quality      int    `json:"quality"`
	Obsolescence int    `json:"obsolescence"`
}

// NetROMCircuit is a circuit a client has open through the node.
type NetROMCircuit struct {
	OwnCall     string `json:"ownCall"`    // What the client calls this end.
	RemoteCall  string `json:"remoteCall"` // What it calls the far end.
	RemoteNode  string `json:"remoteNode"` // The node at the far end.
	User        string `json:"user"`       // Whose circuit it is.
	Incoming    bool   `json:"incoming"`
	State       string `json:"state"`       // "connecting", "connected" or "disconnecting".
	Outstanding int    `json:"outstanding"` // Segments still to go out or be acknowledged.
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
