// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package shell

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTime is when every message in the tests was sent.
func testTime() time.Time { return time.Date(2026, 10, 31, 12, 34, 0, 0, time.UTC) }

// fakeMailbox is a message store in memory, with the store's rules: numbers
// from 1, IDs made from them, duplicates refused, killed messages left out of
// listings unless asked for, newest first.
type fakeMailbox struct {
	messages []mail.Message
	seen     map[string]bool
	fail     error
}

func newFakeMailbox() *fakeMailbox {
	var b = new(fakeMailbox)

	b.seen = make(map[string]bool)

	return b
}

func (b *fakeMailbox) Accept(_ context.Context, m mail.NewMessage, bbsCall string) (mail.Message, error) {
	if b.fail != nil {
		return mail.Message{}, b.fail
	}

	var validErr = m.Validate()
	if validErr != nil {
		return mail.Message{}, validErr
	}

	var number = len(b.messages) + 1
	var mid = maybe.FromMaybe(mail.GenerateMID(number, bbsCall), m.MID)

	if b.seen[mid] {
		return mail.Message{}, mail.ErrDuplicateBID
	}

	b.seen[mid] = true

	var stored = new(mail.Message)

	stored.Number = number
	stored.MID = mid
	stored.Type = m.Type
	stored.Status = mail.New
	stored.From = strings.ToUpper(m.From)
	stored.To = m.To
	stored.Title = m.Title
	stored.Size = len(m.Body)
	stored.Created = testTime()
	stored.Body = m.Body

	b.messages = append(b.messages, *stored)

	return *stored, nil
}

func (b *fakeMailbox) Get(_ context.Context, n int) (maybe.Maybe[mail.Message], error) {
	if b.fail != nil {
		return maybe.Nothing[mail.Message](), b.fail
	}

	if n < 1 || n > len(b.messages) {
		return maybe.Nothing[mail.Message](), nil
	}

	return maybe.Just(b.messages[n-1]), nil
}

func (b *fakeMailbox) List(_ context.Context, q mail.ListQuery) ([]mail.Summary, error) {
	if b.fail != nil {
		return nil, b.fail
	}

	var out []mail.Summary

	for _, v := range slices.Backward(b.messages) {
		var s = v.Summary

		var keep = (len(q.Types) == 0 || slices.Contains(q.Types, s.Type)) &&
			(len(q.Statuses) > 0 && slices.Contains(q.Statuses, s.Status) || len(q.Statuses) == 0 && s.Status != mail.Killed) &&
			maybe.Fold(true, func(from string) bool { return strings.EqualFold(from, s.From) }, q.From) &&
			maybe.Fold(true, func(to string) bool { return strings.EqualFold(to, s.To.To) }, q.To) &&
			maybe.Fold(true, func(lowest int) bool { return s.Number >= lowest }, q.Lowest) &&
			maybe.Fold(true, func(highest int) bool { return s.Number <= highest }, q.Highest)

		if keep {
			out = append(out, s)
		}
	}

	return out, nil
}

func (b *fakeMailbox) SetStatus(_ context.Context, n int, status mail.Status) error {
	if b.fail != nil {
		return b.fail
	}

	b.messages[n-1].Status = status

	return nil
}

// render turns actions into the lines a transcript expects: output as it is,
// a prompt as its text, and a disconnection as "*** disconnected".
func render(actions []Action) []string {
	var lines []string

	for _, action := range actions {
		switch a := action.(type) {
		case Output:
			lines = append(lines, a.Lines...)
		case Prompt:
			lines = append(lines, a.Text)
		case Disconnect:
			lines = append(lines, "*** disconnected")
		}
	}

	return lines
}

func newSession(box mailbox, call string, sysop bool) *Machine {
	var peer = transport.Peer{Call: call, Kind: transport.Memory, Authenticated: sysop}

	return New(peer, box, Config{Call: "Q1BBS-3", Prompt: "de Q1BBS>", HRoute: maybe.Just("#77.GBR.EURO"), Now: testTime})
}

// runTranscript plays a transcript against one mailbox.  "> " lines are typed
// by the user of the current session (a bare ">" types an empty line), "< "
// lines are what is expected back (a bare "<" an empty line),
// and "@session CALL [sysop]" starts a new session as CALL.  Blank lines and
// lines starting # are ignored.  Typing ^Z puts a Ctrl-Z in the line.
func runTranscript(t *testing.T, file string) {
	t.Helper()

	var f, openErr = os.Open(file) //nolint:gosec // G304: the test's own transcript
	require.NoError(t, openErr)

	t.Cleanup(func() { _ = f.Close() })

	var box = newFakeMailbox()
	var session *Machine
	var want, got []string

	var scanner = bufio.NewScanner(f)

	for scanner.Scan() {
		var line = scanner.Text()

		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "@session "):
			var fields = strings.Fields(line)
			session = newSession(box, fields[1], len(fields) > 2 && fields[2] == "sysop")
			got = append(got, render(session.Start(t.Context()))...)
		case line == ">":
			require.NotNil(t, session, "input before any @session")

			got = append(got, render(session.Line(t.Context(), ""))...)
		case strings.HasPrefix(line, "> "):
			require.NotNil(t, session, "input before any @session")

			var typed = strings.ReplaceAll(strings.TrimPrefix(line, "> "), "^Z", "\x1a")
			got = append(got, render(session.Line(t.Context(), typed))...)
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

func TestStoreFailuresAreSaidSo(t *testing.T) {
	t.Parallel()

	var box = newFakeMailbox()
	box.fail = errors.New("disk on fire")

	var session = newSession(box, "Q2TEST", false)

	for line, want := range map[string]string{
		"L":   "Sorry, the messages can't be listed just now.",
		"R 1": "Sorry, message 1 can't be read just now.",
		"K 1": "Sorry, message 1 can't be killed just now.",
	} {
		assert.Contains(t, render(session.Line(t.Context(), line)), want, line)
	}

	session.Line(t.Context(), "S Q1TEST")
	session.Line(t.Context(), "Hello")

	var got = render(session.Line(t.Context(), "/EX"))
	assert.Contains(t, got, "Sorry, the message couldn't be saved: disk on fire")
}

func FuzzMachine(f *testing.F) {
	f.Add("S Q1TEST\nHello\nbody\n/EX\nL\nR 1\nK 1\nB")
	f.Add("SB ALL@WW $1_X\n\n^Z\nLB 1-1\nL< Q2TEST\nL> Q1TEST")
	f.Add("L 0\nR x\nK\nS\nS @\n?")

	f.Fuzz(func(t *testing.T, script string) {
		var box = newFakeMailbox()
		var session = newSession(box, "Q2TEST", false)

		// A line reader never hands over a line with its ending in it.
		for line := range strings.SplitSeq(strings.ReplaceAll(script, "\r", "\n"), "\n") {
			for _, action := range session.Line(context.Background(), line) {
				if out, ok := action.(Output); ok {
					for _, l := range out.Lines {
						if strings.ContainsAny(l, "\r\n") {
							t.Fatalf("output line %q has a line ending in it", l)
						}
					}
				}
			}
		}
	})
}
