// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package memt is an in-memory transport, for tests: a session whose far end
// is a net.Conn the test drives as the station would.
package memt

import (
	"net"

	"github.com/doismellburning/samoyed/internal/bbs/transport"
)

// Session is the BBS's end of an in-memory session.  Being a net.Conn
// underneath, it takes read deadlines, so a line reader can give up on it.
type Session struct {
	net.Conn

	peer transport.Peer
}

// Peer is who is at the far end.
func (s *Session) Peer() transport.Peer { return s.peer }

// Pair returns a session as the BBS would see it, from peer, and the far end
// of it for the test to play the station with.
func Pair(peer transport.Peer) (*Session, net.Conn) {
	var bbs, station = net.Pipe()

	var s = new(Session)

	s.Conn = bbs
	s.peer = peer

	return s, station
}
