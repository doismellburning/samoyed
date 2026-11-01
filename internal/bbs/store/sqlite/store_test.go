// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/routing"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testBBS = "Q1BBS"

// openTest opens a fresh store in the test's own directory, with a clock the
// test controls.
func openTest(t *testing.T) (*Store, string) {
	t.Helper()

	var file = filepath.Join(t.TempDir(), "bbs.db")

	var s, err = Open(t.Context(), file)
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	s.now = func() time.Time { return time.Date(2026, 10, 31, 12, 34, 56, 789, time.UTC) }

	return s, file
}

// query is a ListQuery with set's changes.
func query(set func(q *mail.ListQuery)) mail.ListQuery {
	var q = new(mail.ListQuery)

	if set != nil {
		set(q)
	}

	return *q
}

func message(from string, to string, typ mail.Type, title string) mail.NewMessage {
	var address, _ = mail.ParseAddress(to)

	var m = new(mail.NewMessage)

	m.Type = typ
	m.From = from
	m.To = address
	m.Title = title
	m.Body = []byte(title + " body.\r")

	return *m
}

func TestAcceptAndGet(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var stored, err = s.Accept(t.Context(), message("q2test", "Q1TEST@Q1BBS", mail.Personal, "Hello"), testBBS)
	require.NoError(t, err)

	assert.Equal(t, 1, stored.Number)
	assert.Equal(t, "1_Q1BBS", stored.MID, "a message originated here gets an ID from its number")
	assert.Equal(t, "Q2TEST", stored.From)
	assert.Equal(t, mail.New, stored.Status)
	assert.Equal(t, time.Date(2026, 10, 31, 12, 34, 56, 0, time.UTC), stored.Created)

	var got, getErr = s.Get(t.Context(), 1)
	require.NoError(t, getErr)
	assert.Equal(t, maybe.Just(stored), got)

	var second, secondErr = s.Accept(t.Context(), message("Q2TEST", "ALL", mail.Bulletin, "News"), testBBS)
	require.NoError(t, secondErr)
	assert.Equal(t, 2, second.Number)

	var missing, missingErr = s.Get(t.Context(), 99)
	require.NoError(t, missingErr)
	assert.True(t, missing.IsNothing())
}

func TestAcceptKeepsAGivenMID(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m = message("Q2TEST", "ALL@WW", mail.Bulletin, "News")
	m.MID = maybe.Just("42_Q3BBS")

	var stored, err = s.Accept(t.Context(), m, testBBS)
	require.NoError(t, err)
	assert.Equal(t, "42_Q3BBS", stored.MID)

	var seen, seenErr = s.SeenBID(t.Context(), "42_Q3BBS")
	require.NoError(t, seenErr)
	assert.True(t, seen)

	var unseen, unseenErr = s.SeenBID(t.Context(), "43_Q3BBS")
	require.NoError(t, unseenErr)
	assert.False(t, unseen)
}

func TestAcceptRefusesADuplicateAndChangesNothing(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m = message("Q2TEST", "ALL@WW", mail.Bulletin, "News")
	m.MID = maybe.Just("42_Q3BBS")

	var _, err = s.Accept(t.Context(), m, testBBS)
	require.NoError(t, err)

	var _, dupeErr = s.Accept(t.Context(), m, testBBS)
	require.ErrorIs(t, dupeErr, ErrDuplicateBID)

	var all, listErr = s.List(t.Context(), query(nil))
	require.NoError(t, listErr)
	assert.Len(t, all, 1, "the duplicate left nothing behind")

	// Nor did it use up a number: the transaction took it back.
	var next, nextErr = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Hi"), testBBS)
	require.NoError(t, nextErr)
	assert.Equal(t, 2, next.Number)
}

func TestAcceptRefusesAGeneratedIDSeenBefore(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	// A partner once sent a message whose ID is the one message 1 here
	// would generate.
	var m = message("Q2TEST", "ALL@WW", mail.Bulletin, "News")
	m.MID = maybe.Just("2_Q1BBS")

	var _, err = s.Accept(t.Context(), m, testBBS)
	require.NoError(t, err)

	var _, clashErr = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Hi"), testBBS)
	require.ErrorIs(t, clashErr, ErrDuplicateBID)
}

func TestAcceptRefusesAnInvalidMessage(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m = message("Q2TEST", "Q1TEST", mail.Personal, "Hi")
	m.Type = 'X'

	var _, err = s.Accept(t.Context(), m, testBBS)
	require.Error(t, err)
}

func TestList(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	for _, m := range []mail.NewMessage{
		message("Q2TEST", "Q1TEST", mail.Personal, "one"),
		message("Q3TEST", "ALL@WW", mail.Bulletin, "two"),
		message("Q2TEST", "Q3TEST", mail.Personal, "three"),
		message("Q1TEST", "Q2TEST", mail.Traffic, "four"),
	} {
		var _, err = s.Accept(t.Context(), m, testBBS)
		require.NoError(t, err)
	}

	require.NoError(t, s.SetStatus(t.Context(), 3, mail.Killed))

	var titles = func(q mail.ListQuery) []string {
		t.Helper()

		var got, err = s.List(t.Context(), q)
		require.NoError(t, err)

		var out []string

		for _, summary := range got {
			out = append(out, summary.Title)
		}

		return out
	}

	assert.Equal(t, []string{"four", "two", "one"}, titles(query(nil)), "newest first, without the killed one")
	assert.Equal(t, []string{"two"}, titles(query(func(q *mail.ListQuery) { q.Types = []mail.Type{mail.Bulletin} })))
	assert.Equal(t, []string{"four", "one"}, titles(query(func(q *mail.ListQuery) { q.Types = []mail.Type{mail.Personal, mail.Traffic} })))
	assert.Equal(t, []string{"one"}, titles(query(func(q *mail.ListQuery) { q.From = maybe.Just("q2test") })))
	assert.Equal(t, []string{"one"}, titles(query(func(q *mail.ListQuery) { q.To = maybe.Just("Q1TEST") })))
	assert.Equal(t, []string{"two"}, titles(query(func(q *mail.ListQuery) { q.Lowest = maybe.Just(2); q.Highest = maybe.Just(3) })))
	assert.Equal(t, []string{"three"}, titles(query(func(q *mail.ListQuery) { q.Statuses = []mail.Status{mail.Killed} })))

	var summaries, err = s.List(t.Context(), query(func(q *mail.ListQuery) { q.To = maybe.Just("ALL") }))
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, maybe.Just("WW"), summaries[0].To.At)
	assert.Equal(t, len("two body.\r"), summaries[0].Size)
}

func TestSetStatus(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var _, err = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Hi"), testBBS)
	require.NoError(t, err)

	require.NoError(t, s.SetStatus(t.Context(), 1, mail.Read))

	var got, getErr = s.Get(t.Context(), 1)
	require.NoError(t, getErr)
	assert.Equal(t, mail.Read, maybe.FromJust(got).Status)

	require.ErrorIs(t, s.SetStatus(t.Context(), 2, mail.Read), ErrNoSuchMessage)
	require.Error(t, s.SetStatus(t.Context(), 1, 'X'))
}

func TestConcurrentAcceptsGetTheirOwnNumbers(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	const n = 50

	var numbers = make(chan int, n)
	var wg sync.WaitGroup

	for i := range n {
		wg.Go(func() {
			var stored, err = s.Accept(context.Background(), message("Q2TEST", "Q1TEST", mail.Personal, strconv.Itoa(i)), testBBS)
			assert.NoError(t, err)

			numbers <- stored.Number
		})
	}

	wg.Wait()
	close(numbers)

	var seen = make(map[int]bool)

	for number := range numbers {
		assert.False(t, seen[number], "number %d given twice", number)
		seen[number] = true
	}

	assert.Len(t, seen, n)
}

func TestReopening(t *testing.T) {
	t.Parallel()

	var s, file = openTest(t)

	var _, err = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Kept"), testBBS)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	var again, openErr = Open(t.Context(), file)
	require.NoError(t, openErr)

	t.Cleanup(func() { _ = again.Close() })

	var got, getErr = again.Get(t.Context(), 1)
	require.NoError(t, getErr)
	assert.Equal(t, "Kept", maybe.FromJust(got).Title)

	var version int
	require.NoError(t, again.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version))
	assert.Equal(t, 3, version, "migrations aren't applied twice")
}

func TestOpenRefusesANewerSchema(t *testing.T) {
	t.Parallel()

	var s, file = openTest(t)

	var _, err = s.db.ExecContext(t.Context(), "PRAGMA user_version = 99")
	require.NoError(t, err)
	require.NoError(t, s.Close())

	var _, openErr = Open(t.Context(), file)
	require.ErrorContains(t, openErr, "newer than this BBS knows")
}

func TestOpenUsesWAL(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var mode string
	require.NoError(t, s.db.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)
}

func TestOpenFailsOnABadPath(t *testing.T) {
	t.Parallel()

	var _, err = Open(t.Context(), filepath.Join(t.TempDir(), "no", "such", "dir", "bbs.db"))
	require.Error(t, err)
}

func forwardTo(partners ...string) routing.Decision {
	return routing.Decision{Local: false, Partners: partners, Held: maybe.Nothing[string]()}
}

func TestAcceptRoutedQueuesForEachPartner(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var personal, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS.GBR.EURO", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)
	assert.Equal(t, mail.New, personal.Status)

	var bulletin = message("Q2TEST", "ALL@GBR", mail.Bulletin, "News")
	bulletin.MID = maybe.Just("7_Q2BBS")

	var news, newsErr = s.AcceptRouted(t.Context(), bulletin, testBBS, routing.Decision{Local: true, Partners: []string{"Q3BBS", "Q4BBS"}, Held: maybe.Nothing[string]()})
	require.NoError(t, newsErr)

	var _, localErr = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Here"), testBBS)
	require.NoError(t, localErr)

	var forQ3, q3Err = s.QueueFor(t.Context(), "q3bbs")
	require.NoError(t, q3Err)
	assert.Equal(t, []int{personal.Number, news.Number}, forQ3, "oldest first")

	var forQ4, q4Err = s.QueueFor(t.Context(), "Q4BBS")
	require.NoError(t, q4Err)
	assert.Equal(t, []int{news.Number}, forQ4)

	var forNobody, nobodyErr = s.QueueFor(t.Context(), "Q9BBS")
	require.NoError(t, nobodyErr)
	assert.Empty(t, forNobody)
}

func TestAcceptRoutedHolds(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var held, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Lost"), testBBS,
		routing.Decision{Local: false, Partners: nil, Held: maybe.Just("no partner covers Q6BBS")})
	require.NoError(t, err)
	assert.Equal(t, mail.Held, held.Status)

	var got, getErr = s.Get(t.Context(), held.Number)
	require.NoError(t, getErr)
	assert.Equal(t, mail.Held, maybe.FromJust(got).Status)
}

func TestForwardingFinishesPersonalMail(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)

	require.NoError(t, s.MarkForwarded(t.Context(), m.Number, "Q3BBS"))

	var got, _ = s.Get(t.Context(), m.Number)
	assert.Equal(t, mail.Forwarded, maybe.FromJust(got).Status, "sent on, with nobody left to send it to")

	var queue, _ = s.QueueFor(t.Context(), "Q3BBS")
	assert.Empty(t, queue)

	require.ErrorIs(t, s.MarkForwarded(t.Context(), m.Number, "Q3BBS"), ErrNoSuchMessage, "only once")
}

func TestForwardingKeepsABulletinsStatus(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "ALL@GBR", mail.Bulletin, "News"), testBBS,
		routing.Decision{Local: true, Partners: []string{"Q3BBS", "Q4BBS"}, Held: maybe.Nothing[string]()})
	require.NoError(t, err)

	require.NoError(t, s.MarkForwarded(t.Context(), m.Number, "Q3BBS"))
	require.NoError(t, s.MarkRefused(t.Context(), m.Number, "Q4BBS"))

	var got, _ = s.Get(t.Context(), m.Number)
	assert.Equal(t, mail.New, maybe.FromJust(got).Status, "a bulletin is kept here to be read")
}

func TestARefusalStillFinishesPersonalMail(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)

	require.NoError(t, s.MarkRefused(t.Context(), m.Number, "Q3BBS"))

	var got, _ = s.Get(t.Context(), m.Number)
	assert.Equal(t, mail.Forwarded, maybe.FromJust(got).Status, "a partner that already has it is as good as one that took it")
}

func TestKilledMessagesLeaveTheQueue(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)
	require.NoError(t, s.SetStatus(t.Context(), m.Number, mail.Killed))

	var queue, _ = s.QueueFor(t.Context(), "Q3BBS")
	assert.Empty(t, queue)

	require.NoError(t, s.MarkForwarded(t.Context(), m.Number, "Q3BBS"))

	var got, _ = s.Get(t.Context(), m.Number)
	assert.Equal(t, mail.Killed, maybe.FromJust(got).Status, "still killed")
}

func TestMigratingAVersion1Store(t *testing.T) {
	t.Parallel()

	var s, file = openTest(t)

	var _, err = s.Accept(t.Context(), message("Q2TEST", "Q1TEST", mail.Personal, "Old"), testBBS)
	require.NoError(t, err)

	// Back to how version 1 left it.
	var _, dropErr = s.db.ExecContext(t.Context(), "DROP TABLE forward_queue; PRAGMA user_version = 1")
	require.NoError(t, dropErr)
	require.NoError(t, s.Close())

	var again, openErr = Open(t.Context(), file)
	require.NoError(t, openErr)

	t.Cleanup(func() { _ = again.Close() })

	var queue, queueErr = again.QueueFor(t.Context(), "Q3BBS")
	require.NoError(t, queueErr, "the queue is there after migrating")
	assert.Empty(t, queue)

	var got, _ = again.Get(t.Context(), 1)
	assert.Equal(t, "Old", maybe.FromJust(got).Title, "and the old messages are kept")
}

func TestFailedAttemptsHoldMailForThePartner(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var personal, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)

	var bulletin, bullErr = s.AcceptRouted(t.Context(), message("Q2TEST", "ALL@GBR", mail.Bulletin, "News"), testBBS,
		routing.Decision{Local: true, Partners: []string{"Q3BBS", "Q4BBS"}, Held: maybe.Nothing[string]()})
	require.NoError(t, bullErr)

	for range 2 {
		var held, failErr = s.RecordFailedAttempt(t.Context(), "q3bbs", 3)
		require.NoError(t, failErr)
		assert.Empty(t, held, "not yet")
	}

	var queue, _ = s.QueueFor(t.Context(), "Q3BBS")
	assert.Equal(t, []int{personal.Number, bulletin.Number}, queue, "still waiting")

	var held, failErr = s.RecordFailedAttempt(t.Context(), "Q3BBS", 3)
	require.NoError(t, failErr)
	assert.Equal(t, []int{personal.Number, bulletin.Number}, held)

	queue, _ = s.QueueFor(t.Context(), "Q3BBS")
	assert.Empty(t, queue, "held mail waits no longer")

	var got, _ = s.Get(t.Context(), personal.Number)
	assert.Equal(t, mail.Held, maybe.FromJust(got).Status, "personal mail is held for the sysop")

	var news, _ = s.Get(t.Context(), bulletin.Number)
	assert.Equal(t, mail.New, maybe.FromJust(news).Status, "a bulletin is still here to be read")

	var forQ4, _ = s.QueueFor(t.Context(), "Q4BBS")
	assert.Equal(t, []int{bulletin.Number}, forQ4, "and still goes to the partners that can be reached")

	require.ErrorIs(t, s.MarkForwarded(t.Context(), personal.Number, "Q3BBS"), ErrNoSuchMessage, "held, not pending")
}

func TestAFailedAttemptOnlyCountsWhatIsWaiting(t *testing.T) {
	t.Parallel()

	var s, _ = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)
	require.NoError(t, s.MarkForwarded(t.Context(), m.Number, "Q3BBS"))

	var held, failErr = s.RecordFailedAttempt(t.Context(), "Q3BBS", 1)
	require.NoError(t, failErr)
	assert.Empty(t, held, "what has gone isn't held")

	var got, _ = s.Get(t.Context(), m.Number)
	assert.Equal(t, mail.Forwarded, maybe.FromJust(got).Status)
}

func TestMigratingAVersion2StoreKeepsTheQueue(t *testing.T) {
	t.Parallel()

	var s, file = openTest(t)

	var m, err = s.AcceptRouted(t.Context(), message("Q2TEST", "Q1TEST@Q6BBS", mail.Personal, "Away"), testBBS, forwardTo("Q3BBS"))
	require.NoError(t, err)

	// Back to how version 2 left it: no held state, so remake its table.
	var _, downErr = s.db.ExecContext(t.Context(), `
		CREATE TABLE q2 (msg INTEGER NOT NULL REFERENCES messages (number), partner TEXT NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('pending', 'forwarded', 'refused')),
			attempts INTEGER NOT NULL DEFAULT 0, last_try INTEGER, PRIMARY KEY (msg, partner));
		INSERT INTO q2 (msg, partner, state, attempts, last_try) SELECT msg, partner, state, attempts, last_try FROM forward_queue;
		DROP TABLE forward_queue;
		ALTER TABLE q2 RENAME TO forward_queue;
		PRAGMA user_version = 2`)
	require.NoError(t, downErr)
	require.NoError(t, s.Close())

	var again, openErr = Open(t.Context(), file)
	require.NoError(t, openErr)

	t.Cleanup(func() { _ = again.Close() })

	var queue, _ = again.QueueFor(t.Context(), "Q3BBS")
	assert.Equal(t, []int{m.Number}, queue, "the queue is kept")

	var held, failErr = again.RecordFailedAttempt(t.Context(), "Q3BBS", 1)
	require.NoError(t, failErr)
	assert.Equal(t, []int{m.Number}, held, "and can hold mail now")
}
