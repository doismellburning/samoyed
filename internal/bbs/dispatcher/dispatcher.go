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
	Start(ctx context.Context) []action.Action
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

	// Busy, if given, is the partners forwarding now, so that one calling
	// in while this BBS is already forwarding with it is turned away.
	Busy *Busy
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

	var _, startErr = perform(s, m.Start(ctx))
	if startErr != nil {
		return startErr
	}

	var reader = lines.NewReader(s, d.cfg.MaxLineLen)

	var line, readErr = d.readLine(ctx, reader)
	if readErr != nil {
		return d.ended(ctx, s, readErr)
	}

	if sid.IsSID(strings.TrimSpace(line)) {
		return d.forward(ctx, s, reader, peer, strings.TrimSpace(line))
	}

	var done, err = perform(s, m.Line(ctx, line))
	if err != nil || done {
		return err
	}

	return d.ended(ctx, s, d.loop(ctx, s, reader, m))
}

// forward hands a session that opened with a SID to the partner's
// forwarding machine, unless it isn't a partner or is already forwarding.
func (d *Dispatcher) forward(ctx context.Context, s transport.Session, reader *lines.Reader, peer transport.Peer, line string) error {
	var forwarder, ok = d.forwarder(peer, line).Get()
	if !ok {
		return lines.Write(s, peer.Call+" isn't a forwarding partner of this BBS.  73.")
	}

	if d.cfg.Busy != nil {
		if !d.cfg.Busy.TryAcquire(peer.Call) {
			return lines.Write(s, "Already forwarding with "+peer.Call+", so disconnecting.  73.")
		}

		defer d.cfg.Busy.Release(peer.Call)
	}

	logrus.WithField("station", peer.Call).Info("Forwarding mail")

	return d.ended(ctx, s, d.Drive(ctx, s, reader, forwarder))
}

// Drive runs m over a session: its Start, and then each line the station
// sends, until m disconnects, which is a nil error.  The station leaving
// first is io.EOF, saying nothing for IdleTimeout is
// context.DeadlineExceeded, and anything else is as the session failed.
// Serve uses it once it knows what the session is; a session this BBS
// opened is driven with it from the start, reading through reader.
func (d *Dispatcher) Drive(ctx context.Context, s transport.Session, reader *lines.Reader, m Machine) error {
	var done, err = perform(s, m.Start(ctx))
	if err != nil || done {
		return err
	}

	return d.loop(ctx, s, reader, m)
}

// loop passes each line the station sends to m, and does what it says,
// until m disconnects or the session ends.
func (d *Dispatcher) loop(ctx context.Context, s transport.Session, reader *lines.Reader, m Machine) error {
	for {
		var line, readErr = d.readLine(ctx, reader)
		if readErr != nil {
			return readErr
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
