// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dispatcher

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/memt"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echo is a shell that answers each line by repeating it, and says bye to BYE.
type echo struct{ peer transport.Peer }

func (e *echo) Start() []action.Action { return []action.Action{action.Prompt{Text: "de Q1BBS>"}} }

func (e *echo) Line(_ context.Context, line string) []action.Action {
	if line == "BYE" {
		return []action.Action{action.Output{Lines: []string{"73"}}, action.Disconnect{}}
	}

	return []action.Action{action.Output{Lines: []string{e.peer.Call + " said " + line}}, action.Prompt{Text: "de Q1BBS>"}}
}

func testConfig() Config {
	var cfg = new(Config)

	cfg.Banner = []string{"Welcome to Q1BBS."}
	cfg.SID = "[SAMBBS-0.0-H$]"
	cfg.IdleTimeout = 5 * time.Second
	cfg.MaxLineLen = 32

	return *cfg
}

// start serves a session from Q2TEST with cfg, and returns the station's end
// of it, a reader of what the BBS sends, and Serve's result to come.
func start(t *testing.T, cfg Config) (net.Conn, *lines.Reader, <-chan error) {
	t.Helper()

	var s, station = memt.Pair(transport.Peer{Call: "Q2TEST", Kind: transport.Memory, Authenticated: false})

	t.Cleanup(func() { _ = station.Close() })

	var d = New(cfg, func(peer transport.Peer) *echo { return &echo{peer: peer} })
	var result = make(chan error, 1)

	go func() { result <- d.Serve(t.Context(), s) }()

	return station, lines.NewReader(station, 1024), result
}

func expect(t *testing.T, r *lines.Reader, want ...string) {
	t.Helper()

	for _, w := range want {
		var ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)

		var got, err = r.ReadLine(ctx)

		cancel()
		require.NoError(t, err, "waiting for %q", w)
		assert.Equal(t, w, got)
	}
}

func say(t *testing.T, station net.Conn, line string) {
	t.Helper()

	var _, err = station.Write([]byte(line + "\r"))
	require.NoError(t, err)
}

func result(t *testing.T, ch <-chan error) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")

		return nil
	}
}

func TestASession(t *testing.T) {
	t.Parallel()

	var station, r, done = start(t, testConfig())

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "L")
	expect(t, r, "Q2TEST said L", "de Q1BBS>")

	say(t, station, "BYE")
	expect(t, r, "73")

	require.NoError(t, result(t, done))

	var _, err = r.ReadLine(t.Context())
	require.ErrorIs(t, err, io.EOF, "the session is closed")
}

func TestASIDIsTurnedAway(t *testing.T) {
	t.Parallel()

	var station, r, done = start(t, testConfig())

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "[FBB-7.0-ABFHM$]")
	expect(t, r, "Q2TEST isn't a forwarding partner of this BBS.  73.")

	require.NoError(t, result(t, done))
}

// forwarding is a forwarding machine that says what SID the partner opened
// with, and hangs up when it says *** DONE.
type forwarding struct{ theirs sid.SID }

func (f *forwarding) Start() []action.Action {
	return []action.Action{action.Output{Lines: []string{"forwarding with " + f.theirs.Name}}}
}

func (f *forwarding) Line(_ context.Context, line string) []action.Action {
	if line == "*** DONE" {
		return []action.Action{action.Disconnect{}}
	}

	return []action.Action{action.Output{Lines: []string{"forwarding got " + line}}}
}

func TestAPartnersSIDStartsForwarding(t *testing.T) {
	t.Parallel()

	var cfg = testConfig()
	var asked []string

	cfg.Forwarder = func(peer transport.Peer, theirs sid.SID) maybe.Maybe[Machine] {
		asked = append(asked, peer.Call)

		return maybe.Just[Machine](&forwarding{theirs: theirs})
	}

	var station, r, done = start(t, cfg)

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, " [FBB-7.0-ABFHM$] ")
	expect(t, r, "forwarding with FBB")

	say(t, station, "F>")
	expect(t, r, "forwarding got F>")

	say(t, station, "*** DONE")
	require.NoError(t, result(t, done))
	assert.Equal(t, []string{"Q2TEST"}, asked)
}

func TestAStrangersSIDIsTurnedAway(t *testing.T) {
	t.Parallel()

	var cfg = testConfig()

	cfg.Forwarder = func(transport.Peer, sid.SID) maybe.Maybe[Machine] { return maybe.Nothing[Machine]() }

	var station, r, done = start(t, cfg)

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "[FBB-7.0-ABFHM$]")
	expect(t, r, "Q2TEST isn't a forwarding partner of this BBS.  73.")

	require.NoError(t, result(t, done))
}

func TestASIDLaterIsJustALine(t *testing.T) {
	t.Parallel()

	var station, r, _ = start(t, testConfig())

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "L")
	expect(t, r, "Q2TEST said L", "de Q1BBS>")

	say(t, station, "[FBB-7.0-ABFHM$]")
	expect(t, r, "Q2TEST said [FBB-7.0-ABFHM$]")
}

func TestTheStationLeaving(t *testing.T) {
	t.Parallel()

	var station, r, done = start(t, testConfig())

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")
	require.NoError(t, station.Close())

	require.NoError(t, result(t, done))
}

func TestIdleTimeout(t *testing.T) {
	t.Parallel()

	var cfg = testConfig()
	cfg.IdleTimeout = 50 * time.Millisecond

	var _, r, done = start(t, cfg)

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>", "Nothing heard for 50ms, so disconnecting.  73.")

	require.NoError(t, result(t, done))
}

func TestOverlongLine(t *testing.T) {
	t.Parallel()

	var station, r, done = start(t, testConfig())

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	go func() { _, _ = station.Write([]byte(strings.Repeat("x", 100))) }()

	expect(t, r, "That line was too long, so disconnecting.  73.")
	require.ErrorIs(t, result(t, done), lines.ErrLineTooLong)
}

func TestShuttingDown(t *testing.T) {
	t.Parallel()

	var s, station = memt.Pair(transport.Peer{Call: "Q2TEST", Kind: transport.Memory, Authenticated: false})

	t.Cleanup(func() { _ = station.Close() })

	var ctx, cancel = context.WithCancel(t.Context())
	var d = New(testConfig(), func(peer transport.Peer) *echo { return &echo{peer: peer} })
	var done = make(chan error, 1)

	go func() { done <- d.Serve(ctx, s) }()

	expect(t, lines.NewReader(station, 1024), "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")
	cancel()

	require.NoError(t, result(t, done))
}

func TestAStationGoneBeforeTheGreeting(t *testing.T) {
	t.Parallel()

	var s, station = memt.Pair(transport.Peer{Call: "Q2TEST", Kind: transport.Memory, Authenticated: false})

	require.NoError(t, station.Close())

	var d = New(testConfig(), func(peer transport.Peer) *echo { return &echo{peer: peer} })

	require.Error(t, d.Serve(t.Context(), s), "nobody there to greet")
}

func TestABusyPartnerIsTurnedAway(t *testing.T) {
	t.Parallel()

	var cfg = testConfig()

	cfg.Forwarder = func(_ transport.Peer, theirs sid.SID) maybe.Maybe[Machine] {
		return maybe.Just[Machine](&forwarding{theirs: theirs})
	}
	cfg.Busy = new(Busy)

	cfg.Busy.TryAcquire("Q2TEST")

	var station, r, done = start(t, cfg)

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "[FBB-7.0-ABFHM$]")
	expect(t, r, "Already forwarding with Q2TEST, so disconnecting.  73.")

	require.NoError(t, result(t, done))
}

func TestForwardingHoldsThePartnerBusy(t *testing.T) {
	t.Parallel()

	var cfg = testConfig()

	cfg.Forwarder = func(_ transport.Peer, theirs sid.SID) maybe.Maybe[Machine] {
		return maybe.Just[Machine](&forwarding{theirs: theirs})
	}
	cfg.Busy = new(Busy)

	var station, r, done = start(t, cfg)

	expect(t, r, "Welcome to Q1BBS.", "[SAMBBS-0.0-H$]", "de Q1BBS>")

	say(t, station, "[FBB-7.0-ABFHM$]")
	expect(t, r, "forwarding with FBB")
	assert.False(t, cfg.Busy.TryAcquire("Q2TEST"), "busy while forwarding")

	say(t, station, "*** DONE")
	require.NoError(t, result(t, done))
	assert.True(t, cfg.Busy.TryAcquire("Q2TEST"), "free once done")
}

func TestDrive(t *testing.T) {
	t.Parallel()

	var drive = func(t *testing.T) (net.Conn, *lines.Reader, <-chan error) {
		t.Helper()

		var s, station = memt.Pair(transport.Peer{Call: "Q3BBS", Kind: transport.Memory, Authenticated: false})

		t.Cleanup(func() { _ = station.Close() })

		var d = New(testConfig(), func(peer transport.Peer) *echo { return &echo{peer: peer} })
		var done = make(chan error, 1)

		go func() { done <- d.Drive(t.Context(), s, lines.NewReader(s, 64), &forwarding{theirs: sid.Ours(0, 0)}) }()

		return station, lines.NewReader(station, 1024), done
	}

	t.Run("until the machine disconnects", func(t *testing.T) {
		t.Parallel()

		var station, r, done = drive(t)

		expect(t, r, "forwarding with SAMBBS")
		say(t, station, "F>")
		expect(t, r, "forwarding got F>")
		say(t, station, "*** DONE")
		require.NoError(t, result(t, done))
	})

	t.Run("or the station leaves first", func(t *testing.T) {
		t.Parallel()

		var station, r, done = drive(t)

		expect(t, r, "forwarding with SAMBBS")
		require.NoError(t, station.Close())
		require.ErrorIs(t, result(t, done), io.EOF)
	})
}
