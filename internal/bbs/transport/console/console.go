// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package console is a session on the BBS's own terminal: what is typed on
// stdin, as a given callsign, with the BBS's output on stdout.  It is how a
// sysop uses the BBS locally, and how the BBS can be tried without a radio.
package console

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/doismellburning/samoyed/internal/bbs/transport"
)

// ErrClosed says the session has been closed.
var ErrClosed = errors.New("console session closed")

// Session is a console session.
type Session struct {
	in   io.Reader
	out  io.Writer
	peer transport.Peer

	mu     sync.Mutex
	closed bool
}

// New makes a console session reading in and writing out, as call.  The
// console is trusted: whoever has a shell on the BBS's machine runs the BBS.
func New(in io.Reader, out io.Writer, call string) *Session {
	var s = new(Session)

	s.in = in
	s.out = out
	s.peer.Call = strings.ToUpper(strings.TrimSpace(call))
	s.peer.Kind = transport.Console
	s.peer.Authenticated = true

	return s
}

// Peer is who is at the console.
func (s *Session) Peer() transport.Peer { return s.peer }

// Read reads what is typed.  It returns io.EOF once the input ends.
func (s *Session) Read(p []byte) (int, error) {
	if s.isClosed() {
		return 0, ErrClosed
	}

	return s.in.Read(p)
}

// Write writes the BBS's output.  The BBS ends its lines with CR, as packet
// terminals expect; a console wants LF, so each CR becomes one.
func (s *Session) Write(p []byte) (int, error) {
	if s.isClosed() {
		return 0, ErrClosed
	}

	var _, err = s.out.Write(bytes.ReplaceAll(p, []byte("\r"), []byte("\n")))
	if err != nil {
		return 0, err
	}

	return len(p), nil
}

// Close ends the session.  It leaves the terminal itself open: it isn't the
// session's to close.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closed = true

	return nil
}

func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}
