// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/dispatcher"
	"github.com/doismellburning/samoyed/internal/bbs/forward/script"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/scheduler"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/memt"
	"github.com/doismellburning/samoyed/internal/maybe"
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

	var stored, acceptErr = newMailbox(b.cfg, b.store, nil).Accept(t.Context(), *m, b.cfg.Call)
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

// farEnd is the calling BBS's end of an in-memory session.  Its writes are
// buffered, as a real link's are: a net.Pipe write waits for the far end to
// read all of it, so two BBSes each saying something at once - an FBB
// proposal sent with a SID, met by a refusal - would wait on each other for
// ever.
type farEnd struct {
	net.Conn

	peer   transport.Peer
	writes chan []byte
	done   chan struct{}

	mu   sync.Mutex
	sent bytes.Buffer
}

func newFarEnd(conn net.Conn, peer transport.Peer) *farEnd {
	var f = new(farEnd)

	f.Conn = conn
	f.peer = peer
	f.writes = make(chan []byte, 256)
	f.done = make(chan struct{})

	go func() {
		defer close(f.done)

		for b := range f.writes {
			_, _ = conn.Write(b) // A write after the far end has gone is lost, as on air
		}
	}()

	return f
}

func (f *farEnd) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.sent.Write(b)
	f.mu.Unlock()

	f.writes <- bytes.Clone(b)

	return len(b), nil
}

func (f *farEnd) Close() error {
	close(f.writes)

	var err = f.Conn.Close()

	<-f.done

	return err
}

func (f *farEnd) Peer() transport.Peer { return f.peer }

// said is everything the calling BBS sent.
func (f *farEnd) said() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.sent.String()
}

// callWith has caller connect to answerer over an in-memory link, run
// script, and forward with it as the calling side, until one of them hangs
// up; it returns what came of the call.
func callWith(t *testing.T, caller bbs, answerer bbs, steps ...script.Step) error {
	t.Helper()

	var _, err = callSaid(t, caller, answerer, steps...)

	return err
}

// callSaid is callWith, also returning everything the caller sent.
func callSaid(t *testing.T, caller bbs, answerer bbs, steps ...script.Step) (string, error) {
	t.Helper()

	var ctx, cancel = context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var session, station = memt.Pair(transport.Peer{Call: caller.cfg.Call, Kind: transport.Memory, Authenticated: false})

	var served = make(chan error, 1)

	go func() {
		served <- newDispatcher(answerer.cfg, newMailbox(answerer.cfg, answerer.store, nil), new(dispatcher.Busy)).Serve(ctx, session)
	}()

	var partner = config.Partner{
		Call:   answerer.cfg.Call,
		Covers: nil,
		Floods: nil,
		Connect: &config.Connect{
			Port: 0, To: "", Via: nil, Script: steps, Every: time.Hour, Timeout: time.Second,
		},
	}

	var callerSide = newFarEnd(station, transport.Peer{Call: answerer.cfg.Call, Kind: transport.Memory, Authenticated: false})
	var callerDispatcher = newDispatcher(caller.cfg, newMailbox(caller.cfg, caller.store, nil), nil)

	var err = forwardWith(ctx, callerSide, callerDispatcher, newMailbox(caller.cfg, caller.store, nil), caller.cfg, partner)

	require.NoError(t, callerSide.Close())
	require.NoError(t, <-served)

	return callerSide.said(), err
}

// call is callWith, for a call that should go well.
func call(t *testing.T, caller bbs, answerer bbs) {
	t.Helper()

	require.NoError(t, callWith(t, caller, answerer))
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

	require.ErrorIs(t, callWith(t, q3, q1), errHungUp, "turned away")

	assert.Equal(t, []int{1}, q3.queueFor(t, "Q1BBS"), "so still queued")
}

func TestCallingThroughAConnectScript(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\npartners:\n  - call: Q2BBS\n    covers: [\"*\"]\n")
	var q2 = newBBS(t, "call: Q2BBS\npartners:\n  - call: Q1BBS\n    covers: [\"*\"]\n")

	q2.send(t, mail.Personal, "Q1TEST@Q1BBS", "Through")

	// The script waits for the banner, as one would for a node's
	// "Connected to"; the exchange carries on from what follows.
	require.NoError(t, callWith(t, q2, q1, script.ExpectText("welcome to q1bbs")))

	assert.Equal(t, "Through", q1.byTitle(t, "Through").Title)
	assert.Empty(t, q2.queueFor(t, "Q1BBS"))
}

func TestACallWhoseScriptFails(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\npartners:\n  - call: Q2BBS\n    covers: [\"*\"]\n")
	var q2 = newBBS(t, "call: Q2BBS\npartners:\n  - call: Q1BBS\n    covers: [\"*\"]\n")

	q2.send(t, mail.Personal, "Q1TEST@Q1BBS", "Stuck")

	var err = callWith(t, q2, q1, script.ExpectText("Connected to Q1BBS"))

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), `step 1 (expect "Connected to Q1BBS")`)
	assert.Equal(t, []int{1}, q2.queueFor(t, "Q1BBS"), "still queued")
}

func TestQueueingMailKicksThePartner(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\npartners:\n  - call: Q2BBS\n    covers: [GBR.EURO]\n    floods: [GBR]\n  - call: Q3BBS\n    floods: [GBR]\n")

	var kicked []string
	var mailbox = newMailbox(q1.cfg, q1.store, func(partner string) { kicked = append(kicked, partner) })

	var accept = func(typ mail.Type, to string) {
		var address, err = mail.ParseAddress(to)
		require.NoError(t, err)

		var m = new(mail.NewMessage)

		m.Type = typ
		m.From = "Q9TEST"
		m.To = address
		m.Title = to
		m.Body = []byte("Hi.\r")

		var _, acceptErr = mailbox.Accept(t.Context(), *m, q1.cfg.Call)
		require.NoError(t, acceptErr)
	}

	accept(mail.Personal, "Q1TEST@Q6BBS.GBR.EURO")
	assert.Equal(t, []string{"Q2BBS"}, kicked)

	accept(mail.Bulletin, "ALL@GBR")
	assert.Equal(t, []string{"Q2BBS", "Q2BBS", "Q3BBS"}, kicked)

	accept(mail.Personal, "Q1TEST@Q6BBS.USA.NOAM")
	assert.Len(t, kicked, 3, "held mail kicks nobody")

	accept(mail.Personal, "Q1TEST")
	assert.Len(t, kicked, 3, "nor does mail that stays here")
}

func TestAFailedConnectIsAnError(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\nagw:\n  addr: localhost:8000\npartners:\n  - call: Q2BBS\n    connect:\n      to: Q2NOD\n")

	var c = newCaller(q1.cfg, agwclient.New(maybe.FromJust(q1.cfg.AGWClient())), newDispatcher(q1.cfg, newMailbox(q1.cfg, q1.store, nil), nil), newMailbox(q1.cfg, q1.store, nil))

	var asked []string

	c.connect = func(_ context.Context, _ int, from string, to string, _ []string) (*agwclient.Link, error) {
		asked = append(asked, from+">"+to)

		return nil, agwclient.ErrConnectFailed
	}

	var err = c.call(t.Context(), callTargets(q1.cfg)[0])

	require.ErrorIs(t, err, agwclient.ErrConnectFailed)
	assert.Contains(t, err.Error(), "connecting to Q2NOD")
	assert.Equal(t, []string{"Q1BBS>Q2NOD"}, asked)

	require.Error(t, c.call(t.Context(), scheduler.Target{Partner: "Q9BBS", Every: time.Hour}), "not a partner this BBS calls")
}

func TestCallTargets(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\nagw:\n  addr: localhost:8000\npartners:\n  - call: Q2BBS-3\n    connect: {every: 15m}\n  - call: Q3BBS\n")

	assert.Equal(t, []scheduler.Target{{Partner: "Q2BBS", Every: 15 * time.Minute}}, callTargets(q1.cfg))
}

func TestForwardingManyMessagesInFBB(t *testing.T) {
	t.Parallel()

	var q1 = newBBS(t, "call: Q1BBS\nfbb: true\npartners:\n  - call: Q2BBS\n    covers: [\"*\"]\n    floods: [\"*\"]\n")
	var q2 = newBBS(t, "call: Q2BBS\nfbb: true\npartners:\n  - call: Q1BBS\n    covers: [\"*\"]\n    floods: [\"*\"]\n")

	for i := range 7 {
		q2.send(t, mail.Personal, "Q1TEST@Q1BBS", fmt.Sprintf("Message %d", i+1))
	}

	q1.send(t, mail.Bulletin, "ALL@GBR", "Back")

	var said, err = callSaid(t, q2, q1)

	require.NoError(t, err)
	assert.Contains(t, said, "[SAMBBS-0.0-FH$]")
	assert.Equal(t, 7, strings.Count(said, "FB P "), "in FBB's protocol")
	assert.Equal(t, 2, strings.Count(said, "F> "), "two proposals: five, then two")

	for i := range 7 {
		assert.Equal(t, []string{"Q1BBS", "Q2BBS"}, mail.Path(q1.byTitle(t, fmt.Sprintf("Message %d", i+1)).Body))
	}

	assert.Equal(t, []string{"Q2BBS", "Q1BBS"}, mail.Path(q2.byTitle(t, "Back").Body), "and the other way")
	assert.Empty(t, q2.queueFor(t, "Q1BBS"))
	assert.Empty(t, q1.queueFor(t, "Q2BBS"))
}

func TestFBBNeedsBothSides(t *testing.T) {
	t.Parallel()

	// Q1BBS would, but Q2BBS, calling it, is left as it comes: MBL/RLI.
	var q1 = newBBS(t, "call: Q1BBS\nfbb: true\npartners:\n  - call: Q2BBS\n    covers: [\"*\"]\n")
	var q2 = newBBS(t, "call: Q2BBS\npartners:\n  - call: Q1BBS\n    covers: [\"*\"]\n")

	q2.send(t, mail.Personal, "Q1TEST@Q1BBS", "Plainly")

	var said, err = callSaid(t, q2, q1)

	require.NoError(t, err)
	assert.Contains(t, said, "[SAMBBS-0.0-H$]", "no F claimed")
	assert.Contains(t, said, "SP Q1TEST @ Q1BBS < Q9TEST", "so in MBL/RLI style")
	assert.NotContains(t, said, "FB ")
	assert.Equal(t, "Plainly", q1.byTitle(t, "Plainly").Title)
}
