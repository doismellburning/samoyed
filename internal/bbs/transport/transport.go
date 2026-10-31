// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package transport is what the BBS talks to stations over, whatever carries
// the conversation: an AX.25 link through an AGW server, the console, or, in
// tests, a pipe.  The rest of the BBS sees only a Session and who is at the
// far end of it.
package transport

import (
	"context"
	"io"
)

// Kind is what carries a session.
type Kind int

const (
	// Console is the BBS's own terminal.
	Console Kind = iota + 1

	// AGW is an AX.25 link through an AGW server.
	AGW

	// Memory is an in-process pipe, for tests.
	Memory
)

func (k Kind) String() string {
	switch k {
	case Console:
		return "console"
	case AGW:
		return "AGW"
	case Memory:
		return "memory"
	default:
		return "unknown"
	}
}

// Peer is who is at the far end of a session.
type Peer struct {
	// Call is the station's callsign: the AX.25 source address, or for the
	// console, whoever it was started as.
	Call string

	// Kind is what carries the session.
	Kind Kind

	// Authenticated says the station's identity has been checked by
	// something more than its own word.  An AX.25 callsign is only ever the
	// station's word, so a session over AGW never is; the console is, since
	// whoever has a shell on the BBS's machine runs the BBS.
	Authenticated bool
}

// Session is one conversation with a station.
type Session interface {
	io.ReadWriteCloser

	// Peer is who is at the far end.
	Peer() Peer
}

// Listener hands out incoming sessions.
type Listener interface {
	// Accept waits for the next session, until ctx is cancelled.
	Accept(ctx context.Context) (Session, error)
}
