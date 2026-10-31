// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agwt carries BBS sessions over AX.25 links, through an AGW server
// such as samoyed-direwolf: each station that connects to the BBS's callsign
// is a session.
package agwt

import (
	"context"
	"time"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/sirupsen/logrus"
)

// DefaultDrainTimeout is how long closing a session waits for what was last
// sent to be acknowledged.
const DefaultDrainTimeout = 30 * time.Second

// Session is a BBS session over an AX.25 link.  It reads and writes through
// the link, ReadContext and all, so a line reader can give up on it.
type Session struct {
	*agwclient.Link

	drainTimeout time.Duration
}

// NewSession makes a session of link, which waits up to drainTimeout when
// closed for what was last sent to be acknowledged.
func NewSession(link *agwclient.Link, drainTimeout time.Duration) *Session {
	var s = new(Session)

	s.Link = link
	s.drainTimeout = drainTimeout

	return s
}

// Peer is the station at the far end.  Its callsign is only its own word,
// so it is never authenticated.
func (s *Session) Peer() transport.Peer {
	return transport.Peer{Call: s.Remote(), Kind: transport.AGW, Authenticated: false}
}

// Close hangs up, once what was last sent - a farewell, or the end of a long
// message - has been acknowledged, or once drainTimeout has passed, whichever
// is first.  Hanging up straight away would throw away whatever the TNC
// still had to send.
func (s *Session) Close() error {
	var ctx, cancel = context.WithTimeout(context.Background(), s.drainTimeout)
	defer cancel()

	var drainErr = s.Drain(ctx)
	if drainErr != nil {
		logrus.WithError(drainErr).WithField("station", s.Remote()).Debug("Hanging up without everything acknowledged")
	}

	return s.Link.Close()
}

// Listener hands out a session for each station that connects.
type Listener struct {
	accept       func(ctx context.Context) (*agwclient.Link, error)
	drainTimeout time.Duration
}

// NewListener makes a Listener taking its links from accept, normally an
// agwclient.Client's Accept.
func NewListener(accept func(ctx context.Context) (*agwclient.Link, error), drainTimeout time.Duration) *Listener {
	var l = new(Listener)

	l.accept = accept
	l.drainTimeout = drainTimeout

	return l
}

// Accept waits for the next station to connect, until ctx is cancelled.
func (l *Listener) Accept(ctx context.Context) (*Session, error) {
	var link, err = l.accept(ctx)
	if err != nil {
		return nil, err
	}

	return NewSession(link, l.drainTimeout), nil
}
