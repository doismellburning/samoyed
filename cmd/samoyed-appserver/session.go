// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/sirupsen/logrus"
)

// byeDrainTimeout caps how long "bye" waits for the other station to
// acknowledge the farewell before disconnecting regardless.
const byeDrainTimeout = 10 * time.Second

// maxLineLen is the longest command line a station may send.  Anything
// longer ends its session rather than being buffered for ever.
const maxLineLen = 256

// link is what a session needs of its AX.25 link.  *agwclient.Link is one.
type link interface {
	ReadContext(ctx context.Context, p []byte) (int, error)
	Write(p []byte) (int, error)
	Drain(ctx context.Context) error
	Close() error
	Remote() string
	Port() int
}

// appServer is the table of stations connected now, which "who" lists, and
// what serves them.
type appServer struct {
	byeDrainTimeout time.Duration

	mu       sync.Mutex
	sessions map[*session]struct{}
}

func newAppServer() *appServer {
	var srv = new(appServer)

	srv.byeDrainTimeout = byeDrainTimeout
	srv.sessions = make(map[*session]struct{})

	return srv
}

// serve takes each link accept hands it, and serves it on a goroutine of its
// own, until ctx is cancelled.  Each station has its own goroutine, so one
// waiting - for a timing test, say, or a farewell to be acknowledged - holds
// up nobody else.
func (srv *appServer) serve(ctx context.Context, accept func(context.Context) (*agwclient.Link, error)) error {
	var sessions sync.WaitGroup
	defer sessions.Wait()

	for {
		var l, err = accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}

		sessions.Go(func() { srv.handle(ctx, l) })
	}
}

// handle serves one station until it disconnects.
func (srv *appServer) handle(ctx context.Context, l link) {
	var s = srv.add(l)
	defer srv.remove(s)

	var entry = logrus.WithFields(logrus.Fields{"channel": l.Port(), "station": l.Remote()})

	entry.Info("Begin session")

	var err = s.run(ctx)

	entry.WithError(err).Info("End session")
}

func (srv *appServer) add(l link) *session {
	var s = new(session)

	s.srv = srv
	s.link = l
	s.loginTime = time.Now()

	srv.mu.Lock()
	defer srv.mu.Unlock()

	srv.sessions[s] = struct{}{}

	return s
}

func (srv *appServer) remove(s *session) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	delete(srv.sessions, s)
}

// sortedSessions returns the current sessions in a stable order, for display purposes.
func (srv *appServer) sortedSessions() []*session {
	srv.mu.Lock()

	var sessions = make([]*session, 0, len(srv.sessions))

	for s := range srv.sessions {
		sessions = append(sessions, s)
	}

	srv.mu.Unlock()

	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].link.Port() != sessions[j].link.Port() {
			return sessions[i].link.Port() < sessions[j].link.Port()
		}

		return sessions[i].link.Remote() < sessions[j].link.Remote()
	})

	return sessions
}

// session is one connected station.
type session struct {
	srv       *appServer
	link      link
	loginTime time.Time // Time when connection established.
}

// run greets the station, then reads its commands, a line at a time, and
// answers each until it says "bye" or goes away.
func (s *session) run(ctx context.Context) error {
	if !s.send("Welcome!  Type ? for list of commands or HELP <command> for details.") {
		return errors.New("could not send greeting")
	}

	var scanner = bufio.NewScanner(contextReader{ctx: ctx, link: s.link})

	scanner.Buffer(make([]byte, 0, maxLineLen), maxLineLen)
	scanner.Split(newLineSplitter())

	for scanner.Scan() {
		var line = strings.TrimSpace(scanner.Text())

		logrus.WithFields(logrus.Fields{"channel": s.link.Port(), "station": s.link.Remote(), "line": line}).Debug("Received")

		if !s.command(ctx, line) {
			return nil
		}
	}

	var err = scanner.Err()
	if err == nil {
		return io.EOF // The station disconnected.
	}

	return err
}

// command runs one line from the station.  It reports whether the session
// carries on.
func (s *session) command(ctx context.Context, line string) bool {
	var name, rest, _ = strings.Cut(line, " ")

	if name == "" {
		return s.send("Type ? for list of commands or HELP <command> for details.")
	}

	var cmd = lookupCommand(strings.ToLower(name))
	if cmd == nil {
		return s.send("Invalid command. Type ? for list of commands or HELP <command> for details.")
	}

	return cmd.handler(ctx, s, rest)
}

// send sends one connected-mode frame per line, each ending with the carriage
// return AX.25 terminals expect.  It reports whether it could.
func (s *session) send(lines ...string) bool {
	for _, line := range lines {
		var _, err = s.link.Write([]byte(line + "\r"))
		if err != nil {
			logrus.WithError(err).WithField("station", s.link.Remote()).Debug("Could not send")

			return false
		}
	}

	return true
}

// contextReader reads from a link, giving up when ctx is cancelled.
type contextReader struct {
	ctx  context.Context //nolint:containedctx // Adapts ReadContext to io.Reader for bufio.Scanner
	link link
}

func (r contextReader) Read(p []byte) (int, error) {
	return r.link.ReadContext(r.ctx, p)
}

// newLineSplitter returns a bufio.SplitFunc for lines ending in CR, LF or
// CRLF.  A terminal ends a line with CR, and some send LF after it; when that
// LF arrives on its own, after the CR has already ended a line, it isn't
// taken as an empty line of its own.
func newLineSplitter() bufio.SplitFunc {
	var afterCR = false

	return func(data []byte, atEOF bool) (int, []byte, error) {
		if afterCR && len(data) > 0 && data[0] == '\n' {
			afterCR = false

			return 1, nil, nil
		}

		var i = bytes.IndexAny(data, "\r\n")
		if i >= 0 {
			afterCR = data[i] == '\r'

			var advance = i + 1
			if afterCR && advance < len(data) && data[advance] == '\n' {
				advance++
				afterCR = false
			}

			return advance, data[:i], nil
		}

		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}

		return 0, nil, nil
	}
}
