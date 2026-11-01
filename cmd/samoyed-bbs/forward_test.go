// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/forward/mbl"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/memt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bbs is one BBS of the two in a forwarding test.
type bbs struct {
	cfg   config.Config
	store *sqlite.Store
}

func newBBS(t *testing.T, yaml string) bbs {
	t.Helper()

	var cfg, cfgErr = config.Parse([]byte(yaml + "store: " + filepath.Join(t.TempDir(), "bbs.db") + "\n"))
	require.NoError(t, cfgErr)

	var store, storeErr = sqlite.Open(t.Context(), cfg.Store)
	require.NoError(t, storeErr)

	t.Cleanup(func() { _ = store.Close() })

	return bbs{cfg: cfg, store: store}
}

// send has a user of b send a message, as the shell would.
func (b bbs) send(t *testing.T, typ mail.Type, to string, title string) mail.Message {
	t.Helper()

	var address, err = mail.ParseAddress(to)
	require.NoError(t, err)

	var m = new(mail.NewMessage)

	m.Type = typ
	m.From = "Q9TEST"
	m.To = address
	m.Title = title
	m.Body = mail.Stamp([]byte(title+" body.\r"), b.cfg.Call, b.cfg.HRouteMaybe(), time.Now())

	var stored, acceptErr = newMailbox(b.cfg, b.store).Accept(t.Context(), *m, b.cfg.Call)
	require.NoError(t, acceptErr)

	return stored
}

func (b bbs) queueFor(t *testing.T, partner string) []int {
	t.Helper()

	var queue, err = b.store.QueueFor(t.Context(), partner)
	require.NoError(t, err)

	return queue
}

// byTitle is b's message titled title.
func (b bbs) byTitle(t *testing.T, title string) mail.Message {
	t.Helper()

	var all, err = b.store.List(t.Context(), everything())
	require.NoError(t, err)

	for _, s := range all {
		if s.Title == title {
			var found, getErr = b.store.Get(t.Context(), s.Number)
			require.NoError(t, getErr)

			var msg, _ = found.Get()

			return msg
		}
	}

	t.Fatalf("no message titled %q", title)

	var none = new(mail.Message)

	return *none
}

// everything lists every message that hasn't been killed.
func everything() mail.ListQuery {
	var q = new(mail.ListQuery)

	return *q
}

// call has caller connect to answerer over an in-memory link and forward
// with it, as caller's calling side, until one of them hangs up.
func call(t *testing.T, caller bbs, answerer bbs) {
	t.Helper()

	var ctx, cancel = context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var session, station = memt.Pair(transport.Peer{Call: caller.cfg.Call, Kind: transport.Memory, Authenticated: false})

	var served = make(chan error, 1)

	go func() { served <- newDispatcher(answerer.cfg, answerer.store).Serve(ctx, session) }()

	var m = mbl.NewCalling(answerer.cfg.Call, newMailbox(caller.cfg, caller.store), forwardConfig(caller.cfg))
	var reader = lines.NewReader(station, lines.DefaultMaxLen)

	var hungUp = false

	for !hungUp {
		var line, err = reader.ReadLine(ctx)
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		for _, next := range m.Line(ctx, line) {
			switch a := next.(type) {
			case action.Output:
				require.NoError(t, lines.Write(station, a.Lines...))
			case action.Prompt:
				require.NoError(t, lines.Write(station, a.Text))
			case action.Disconnect:
				hungUp = true
			}
		}
	}

	require.NoError(t, station.Close())
	require.NoError(t, <-served)
}

func TestForwardingBothWays(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\nhroute: \"#77.GBR.EURO\"\npartners:\n  - call: Q2BBS\n    covers: [\"*\"]\n    floods: [\"*\"]\n")
	var q2 = newBBS(t, "call: Q2BBS-1\nhroute: \"#12.GBR.EURO\"\npartners:\n  - call: Q1BBS\n    covers: [\"#77.GBR.EURO\"]\n    floods: [GBR]\n")

	var personal = q2.send(t, mail.Personal, "Q1TEST@Q1BBS.#77.GBR.EURO", "For Q1TEST")
	var bulletin = q2.send(t, mail.Bulletin, "ALL@GBR", "News")
	var reply = q1.send(t, mail.Personal, "Q8TEST@Q2BBS", "For Q8TEST")

	require.Len(t, q2.queueFor(t, "Q1BBS"), 2)
	require.Len(t, q1.queueFor(t, "Q2BBS"), 1)

	call(t, q2, q1)

	// Q1BBS has both of Q2BBS's messages, IDs kept, each with both BBSes'
	// R: lines, newest first.
	for _, sent := range []mail.Message{personal, bulletin} {
		var got = q1.byTitle(t, sent.Title)

		assert.Equal(t, sent.MID, got.MID)
		assert.Equal(t, sent.From, got.From)
		assert.Equal(t, sent.To, got.To)
		assert.Equal(t, []string{"Q1BBS", "Q2BBS"}, mail.Path(got.Body))
	}

	// Q2BBS has Q1BBS's, likewise.
	var got = q2.byTitle(t, reply.Title)

	assert.Equal(t, reply.MID, got.MID)
	assert.Equal(t, []string{"Q2BBS", "Q1BBS"}, mail.Path(got.Body))

	// Nothing is left to send, nothing has been queued to go back the way
	// it came, and mail that was sent on is marked so.
	assert.Empty(t, q2.queueFor(t, "Q1BBS"))
	assert.Empty(t, q1.queueFor(t, "Q2BBS"))
	assert.Equal(t, mail.Forwarded, q2.byTitle(t, personal.Title).Status)
	assert.Equal(t, mail.Forwarded, q1.byTitle(t, reply.Title).Status)

	// A second call has nothing to do: what was sent is not sent again.
	call(t, q2, q1)

	var all, listErr = q1.store.List(t.Context(), everything())
	require.NoError(t, listErr)
	assert.Len(t, all, 3)
}

func TestForwardingIsOnlyForPartners(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\n")
	var q3 = newBBS(t, "call: Q3BBS\npartners:\n  - call: Q1BBS\n    covers: [\"*\"]\n")

	q3.send(t, mail.Personal, "Q1TEST@Q1BBS", "Unwanted")

	call(t, q3, q1)

	assert.Equal(t, []int{1}, q3.queueFor(t, "Q1BBS"), "turned away, so still queued")
}
