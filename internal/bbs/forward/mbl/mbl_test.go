// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mbl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTime is when every message in the tests was taken.
func testTime() time.Time { return time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC) }

var errStoreBroken = errors.New("store broken")

// fakeStore is the message store in memory: messages queued for the partner,
// what the partner has sent, and the IDs seen.
type fakeStore struct {
	messages []mail.Message
	state    map[int]string
	accepted []mail.NewMessage
	from     []string
	seen     map[string]bool
	fail     error
}

func newFakeStore() *fakeStore {
	var s = new(fakeStore)

	s.state = make(map[int]string)
	s.seen = make(map[string]bool)

	return s
}

// queue adds a message queued for the partner, offered as proposal.
func (s *fakeStore) queue(t *testing.T, proposal string) {
	t.Helper()

	var offered, err = ParseProposal(proposal)
	require.NoError(t, err)

	var n = len(s.messages) + 1

	var msg = new(mail.Message)

	msg.Number = n
	msg.MID = maybe.FromMaybe(mail.GenerateMID(n, "Q1BBS"), offered.MID)
	msg.Type = offered.Type
	msg.Status = mail.New
	msg.From = offered.From
	msg.To = offered.To
	msg.Title = "Title " + strconv.Itoa(n)
	msg.Body = []byte("R:261031/1200Z @:Q1BBS.#77.GBR.EURO\rBody " + strconv.Itoa(n) + ".\r")
	msg.Created = testTime()

	s.messages = append(s.messages, *msg)
	s.state[n] = "pending"
	s.seen[msg.MID] = true
}

func (s *fakeStore) SeenBID(_ context.Context, bid string) (bool, error) {
	if s.fail != nil {
		return false, s.fail
	}

	return s.seen[bid], nil
}

func (s *fakeStore) AcceptForwarded(_ context.Context, m mail.NewMessage, from string) (mail.Message, error) {
	if s.fail != nil {
		return mail.Message{}, s.fail
	}

	if bid, ok := m.MID.Get(); ok {
		if s.seen[bid] {
			return mail.Message{}, mail.ErrDuplicateBID
		}

		s.seen[bid] = true
	}

	s.accepted = append(s.accepted, m)
	s.from = append(s.from, from)

	var stored = new(mail.Message)

	stored.Number = len(s.accepted)
	stored.Body = m.Body

	return *stored, nil
}

func (s *fakeStore) QueueFor(_ context.Context, _ string) ([]int, error) {
	if s.fail != nil {
		return nil, s.fail
	}

	var queue []int

	for n := 1; n <= len(s.messages); n++ {
		if s.state[n] == "pending" {
			queue = append(queue, n)
		}
	}

	return queue, nil
}

func (s *fakeStore) Get(_ context.Context, n int) (maybe.Maybe[mail.Message], error) {
	if s.fail != nil {
		return maybe.Nothing[mail.Message](), s.fail
	}

	if n < 1 || n > len(s.messages) {
		return maybe.Nothing[mail.Message](), nil
	}

	return maybe.Just(s.messages[n-1]), nil
}

func (s *fakeStore) MarkForwarded(_ context.Context, n int, _ string) error {
	return s.mark(n, "forwarded")
}

func (s *fakeStore) MarkRefused(_ context.Context, n int, _ string) error {
	return s.mark(n, "refused")
}

func (s *fakeStore) mark(n int, state string) error {
	if s.fail != nil {
		return s.fail
	}

	if s.state[n] != "pending" {
		return fmt.Errorf("message %d is %s, not pending", n, s.state[n])
	}

	s.state[n] = state

	return nil
}

// tally is what has happened to the queue and the partner's messages, as a
// transcript's @check line gives it.
func (s *fakeStore) tally() string {
	var counts = map[string]int{}

	for _, state := range s.state {
		counts[state]++
	}

	return fmt.Sprintf("accepted=%d forwarded=%d refused=%d pending=%d",
		len(s.accepted), counts["forwarded"], counts["refused"], counts["pending"])
}

func testConfig() Config {
	return Config{Call: "Q1BBS-1", HRoute: maybe.Just("#77.GBR.EURO"), Prompt: "de Q1BBS>", SID: sid.Ours(0, 0), Now: testTime}
}

// render turns actions into the lines a transcript expects: output as it is,
// a prompt as its text, and a disconnection as "*** disconnected".
func render(actions []action.Action) []string {
	var lines []string

	for _, next := range actions {
		switch a := next.(type) {
		case action.Output:
			lines = append(lines, a.Lines...)
		case action.Prompt:
			lines = append(lines, a.Text)
		case action.Disconnect:
			lines = append(lines, "*** disconnected")
		}
	}

	return lines
}

// runTranscript plays a transcript against one store.  "> " lines are sent
// by the partner (a bare ">" sends an empty line, ^Z a Ctrl-Z), and "< " lines
// are what is expected back.  Directives:
//
//	@queued PROPOSAL   queues a message for the partner, as PROPOSAL offers it
//	@killed N          kills queued message N
//	@seen BID          has BID seen before
//	@broken            makes the store fail from here on
//	@answering SID     starts the session for a partner that has connected
//	                   and sent SID
//	@calling           starts the session for a partner this BBS called
//	@check TALLY       checks the store's tally (see fakeStore.tally)
//
// Blank lines and lines starting # are ignored.
func runTranscript(t *testing.T, file string) {
	t.Helper()

	var f, openErr = os.Open(file) //nolint:gosec // G304: the test's own transcript
	require.NoError(t, openErr)

	t.Cleanup(func() { _ = f.Close() })

	var st = newFakeStore()
	var m *Machine
	var want, got []string

	var scanner = bufio.NewScanner(f)

	for scanner.Scan() {
		var line = scanner.Text()
		var directive, arg, _ = strings.Cut(line, " ")

		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case directive == "@queued":
			st.queue(t, arg)
		case directive == "@killed":
			var n, err = strconv.Atoi(arg)
			require.NoError(t, err)

			st.messages[n-1].Status = mail.Killed
		case directive == "@seen":
			st.seen[arg] = true
		case directive == "@broken":
			st.fail = errStoreBroken
		case directive == "@answering":
			var theirs, err = sid.Parse(arg)
			require.NoError(t, err)

			m = NewAnswering("Q2BBS-1", theirs, st, testConfig())
			got = append(got, render(m.Start())...)
		case directive == "@calling":
			m = NewCalling("Q2BBS-1", st, testConfig())
			got = append(got, render(m.Start())...)
		case directive == "@check":
			assert.Equal(t, strings.Join(want, "\n"), strings.Join(got, "\n"), "before %s", line)
			assert.Equal(t, arg, st.tally(), line)
		case line == ">" || strings.HasPrefix(line, "> "):
			require.NotNil(t, m, "input before any session")

			var sent = strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(line, ">"), " "), "^Z", "\x1a")
			got = append(got, render(m.Line(t.Context(), sent))...)
		case strings.HasPrefix(line, "<"):
			want = append(want, strings.TrimPrefix(strings.TrimPrefix(line, "<"), " "))
		default:
			t.Fatalf("transcript line %q is not input, output or a directive", line)
		}
	}

	require.NoError(t, scanner.Err())
	assert.Equal(t, strings.Join(want, "\n"), strings.Join(got, "\n"))
}

func TestTranscripts(t *testing.T) {
	t.Parallel()

	var files, err = filepath.Glob("testdata/transcripts/*.txt")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			runTranscript(t, file)
		})
	}
}

func TestReceivedMessagesAreStampedAndAttributed(t *testing.T) {
	t.Parallel()

	var st = newFakeStore()
	var theirs, _ = sid.Parse("[FBB-7.0-ABFHM$]")
	var m = NewAnswering("q2bbs-1", theirs, st, testConfig())

	m.Start()

	for _, line := range []string{"SB ALL @ GBR < Q9TEST $77_Q3BBS", "News", "R:261031/0800Z @:Q3BBS.GBR.EURO", "Hello.", "/EX"} {
		m.Line(t.Context(), line)
	}

	require.Len(t, st.accepted, 1)

	var got = st.accepted[0]

	assert.Equal(t, mail.Bulletin, got.Type)
	assert.Equal(t, "ALL@GBR", got.To.String())
	assert.Equal(t, "Q9TEST", got.From)
	assert.Equal(t, maybe.Just("77_Q3BBS"), got.MID)
	assert.Equal(t, "News", got.Title)
	assert.Equal(t, "R:261101/0905Z @:Q1BBS.#77.GBR.EURO\rR:261031/0800Z @:Q3BBS.GBR.EURO\rHello.\r", string(got.Body))
	assert.Equal(t, []string{"Q2BBS"}, st.from, "routed as from the partner, without its SSID")
}

func TestLongTitlesAreCut(t *testing.T) {
	t.Parallel()

	var st = newFakeStore()
	var m = NewAnswering("Q2BBS", sid.Ours(0, 0), st, testConfig())

	m.Line(t.Context(), "SP Q1TEST < Q9TEST")
	m.Line(t.Context(), strings.Repeat("x", 100))
	m.Line(t.Context(), "/EX")

	require.Len(t, st.accepted, 1)
	assert.Len(t, st.accepted[0].Title, mail.MaxTitleLen)
}

func FuzzMachine(f *testing.F) {
	f.Add(true, "SP Q1TEST @ Q1BBS < Q9TEST $1_Q2BBS\nTitle\nBody\n/EX\nF>\nOK\nde Q2BBS>\n*** DONE")
	f.Add(false, "Welcome\n[FBB-7.0-ABFHM$]\nQ2BBS>\nQ2BBS>\nNO\nSB ALL < Q9TEST\nx\n\x1a\n*** DONE")
	f.Add(true, "S\nSP\nSP <\nSX Q1TEST < Q9TEST\nSP Q1TEST\n>\nF>\nF>")

	f.Fuzz(func(t *testing.T, answering bool, input string) {
		var st = newFakeStore()

		st.queue(t, "SP Q1TEST @ Q2BBS < Q9TEST $5_Q1BBS")
		st.queue(t, "SB ALL @ GBR < Q9TEST")

		var m *Machine
		if answering {
			m = NewAnswering("Q2BBS", sid.Ours(0, 0), st, testConfig())
		} else {
			m = NewCalling("Q2BBS", st, testConfig())
		}

		m.Start()

		for line := range strings.SplitSeq(input, "\n") {
			for _, a := range m.Line(t.Context(), line) {
				if _, ok := a.(action.Disconnect); ok {
					return
				}
			}

			for _, msg := range st.accepted {
				require.NoError(t, msg.Validate(), "only valid messages are stored")
			}
		}
	})
}
