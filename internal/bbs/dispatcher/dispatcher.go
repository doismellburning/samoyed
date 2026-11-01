// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dispatcher runs a session: it greets the station, sends the BBS's
// SID, and then passes each line to the user shell and its answers back.  A
// station whose first line is a SID is another BBS come to forward mail: a
// partner's session is handed to its forwarding machine, and anyone else is
// turned away.
package dispatcher

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// Machine is what a session drives: the user shell, or a forwarding
// exchange.
type Machine interface {
	Start() []action.Action
	Line(ctx context.Context, line string) []action.Action
}

// Config is how sessions are run.
type Config struct {
	// Banner is sent first, a line at a time.
	Banner []string

	// SID is the BBS's system ID, such as "[SAMBBS-0.0-H$]", sent after the
	// banner as FBB-style BBSes do.
	SID string

	// IdleTimeout is how long a station may say nothing before it is
	// disconnected.
	IdleTimeout time.Duration

	// MaxLineLen is the longest line a station may send.
	MaxLineLen int

	// Forwarder, given the station and the SID it opened with, is the
	// machine to forward mail with it, if it is a partner.  Left nil, no
	// station is.
	Forwarder func(peer transport.Peer, theirs sid.SID) maybe.Maybe[Machine]
}

// Dispatcher runs sessions.
type Dispatcher struct {
	cfg        Config
	newMachine func(peer transport.Peer) Machine
}

// New makes a Dispatcher that runs each session with the shell newMachine
// makes for its peer.
func New[M Machine](cfg Config, newMachine func(peer transport.Peer) M) *Dispatcher {
	var d = new(Dispatcher)

	d.cfg = cfg
	d.newMachine = func(peer transport.Peer) Machine { return newMachine(peer) }

	return d
}

// Serve runs one session until the station leaves, says bye, stays idle too
// long, or ctx is cancelled, and then closes it.
func (d *Dispatcher) Serve(ctx context.Context, s transport.Session) error {
	defer func() { _ = s.Close() }()

	var peer = s.Peer()
	var entry = logrus.WithFields(logrus.Fields{"station": peer.Call, "kind": peer.Kind})

	entry.Info("Begin session")

	var err = d.serve(ctx, s, peer)

	entry.WithError(err).Info("End session")

	return err
}

func (d *Dispatcher) serve(ctx context.Context, s transport.Session, peer transport.Peer) error {
	var m = d.newMachine(peer)

	var greeting = append(append([]string{}, d.cfg.Banner...), d.cfg.SID)

	var writeErr = lines.Write(s, greeting...)
	if writeErr != nil {
		return writeErr
	}

	var _, startErr = perform(s, m.Start())
	if startErr != nil {
		return startErr
	}

	var reader = lines.NewReader(s, d.cfg.MaxLineLen)

	for first := true; ; first = false {
		var line, readErr = d.readLine(ctx, reader)
		if readErr != nil {
			return d.ended(ctx, s, readErr)
		}

		if first && sid.IsSID(strings.TrimSpace(line)) {
			var forwarder, ok = d.forwarder(peer, strings.TrimSpace(line)).Get()
			if !ok {
				return lines.Write(s, peer.Call+" isn't a forwarding partner of this BBS.  73.")
			}

			logrus.WithField("station", peer.Call).Info("Forwarding mail")

			m = forwarder

			var done, err = perform(s, m.Start())
			if err != nil || done {
				return err
			}

			continue
		}

		var done, err = perform(s, m.Line(ctx, line))
		if err != nil || done {
			return err
		}
	}
}

// forwarder is the machine to forward mail with peer, which opened with
// line, its SID, if it is a partner.
func (d *Dispatcher) forwarder(peer transport.Peer, line string) maybe.Maybe[Machine] {
	var theirs, err = sid.Parse(line)
	if err != nil || d.cfg.Forwarder == nil {
		return maybe.Nothing[Machine]()
	}

	return d.cfg.Forwarder(peer, theirs)
}

// readLine reads the station's next line, giving up after IdleTimeout.
func (d *Dispatcher) readLine(ctx context.Context, reader *lines.Reader) (string, error) {
	var idleCtx, cancel = context.WithTimeout(ctx, d.cfg.IdleTimeout)
	defer cancel()

	return reader.ReadLine(idleCtx)
}

// ended says why a session ended, telling the station if it is still there
// to be told.  The station leaving, or the BBS shutting down, is no error.
func (d *Dispatcher) ended(ctx context.Context, s transport.Session, err error) error {
	switch {
	case errors.Is(err, io.EOF), ctx.Err() != nil:
		return nil //nolint:nilerr // The station leaving, or the BBS stopping, is no failure.
	case errors.Is(err, context.DeadlineExceeded):
		_ = lines.Write(s, "Nothing heard for "+d.cfg.IdleTimeout.String()+", so disconnecting.  73.")

		return nil
	case errors.Is(err, lines.ErrLineTooLong):
		_ = lines.Write(s, "That line was too long, so disconnecting.  73.")

		return err
	default:
		return err
	}
}

// perform carries out a machine's actions, reporting whether one ended the
// session.
func perform(s transport.Session, actions []action.Action) (bool, error) {
	for _, next := range actions {
		var err error

		switch a := next.(type) {
		case action.Output:
			err = lines.Write(s, a.Lines...)
		case action.Prompt:
			err = lines.Write(s, a.Text)
		case action.Disconnect:
			return true, nil
		}

		if err != nil {
			return false, err
		}
	}

	return false, nil
}
