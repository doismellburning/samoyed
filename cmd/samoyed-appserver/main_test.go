// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLink stands in for an AX.25 link: a test types into it as the station
// would, and reads back what the appserver sent.
type fakeLink struct {
	remote string
	port   int

	in  chan []byte // From the station; closed when it disconnects
	out chan string // Each write from the appserver

	// drain, if set, is what Drain does; otherwise it returns at once.
	drain func(ctx context.Context) error

	mu      sync.Mutex
	pending []byte
	drains  int
	closed  bool
}

func newFakeLink(remote string) *fakeLink {
	var l = new(fakeLink)

	l.remote = remote
	l.in = make(chan []byte, 16)
	l.out = make(chan string, 2048)

	return l
}

func (l *fakeLink) ReadContext(ctx context.Context, p []byte) (int, error) {
	l.mu.Lock()

	if len(l.pending) > 0 {
		var n = copy(p, l.pending)
		l.pending = l.pending[n:]
		l.mu.Unlock()

		return n, nil
	}

	l.mu.Unlock()

	select {
	case data, ok := <-l.in:
		if !ok {
			return 0, io.EOF
		}

		l.mu.Lock()
		defer l.mu.Unlock()

		var n = copy(p, data)
		l.pending = append(l.pending, data[n:]...)

		return n, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (l *fakeLink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return 0, agwclient.ErrLinkClosed
	}

	l.out <- string(p)

	return len(p), nil
}

func (l *fakeLink) Drain(ctx context.Context) error {
	l.mu.Lock()
	l.drains++
	var drain = l.drain
	l.mu.Unlock()

	if drain != nil {
		return drain(ctx)
	}

	return nil
}

func (l *fakeLink) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.closed = true

	return nil
}

func (l *fakeLink) Remote() string { return l.remote }
func (l *fakeLink) Port() int      { return l.port }

func (l *fakeLink) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.closed
}

func (l *fakeLink) drainCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.drains
}

// say sends text from the station.
func (l *fakeLink) say(text string) {
	l.in <- []byte(text)
}

// expect reads what the appserver sends until something contains want, and
// returns all of it.
func (l *fakeLink) expect(t *testing.T, want string) string {
	t.Helper()

	var got strings.Builder

	for {
		select {
		case text := <-l.out:
			got.WriteString(text)

			if strings.Contains(text, want) {
				return got.String()
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %q; got %q", want, got.String())
		}
	}
}

// startSession serves a station on l, as serve would, and waits for its
// greeting.  The returned channel is closed when the session ends.
func startSession(t *testing.T, srv *appServer, l *fakeLink) <-chan struct{} {
	t.Helper()

	var done = make(chan struct{})

	go func() {
		defer close(done)
		srv.handle(t.Context(), l)
	}()

	l.expect(t, "Welcome!")

	t.Cleanup(func() {
		select {
		case <-done:
		default:
			close(l.in)
			<-done
		}
	})

	return done
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
}

func TestHelpListsTheCommands(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	for _, command := range []string{"?", "help", "HELP"} {
		l.say(command + "\r")

		var got = l.expect(t, "Type HELP <command> for details.")

		for _, c := range userCommands() {
			assert.Contains(t, got, c.usage, "%q should list %q", command, c.usage)
		}
	}
}

func TestHelpDescribesOneCommand(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	for _, c := range userCommands() {
		l.say("help " + c.name + "\r")

		var got = l.expect(t, c.detail[len(c.detail)-1])

		for _, line := range c.detail {
			assert.Contains(t, got, line, "HELP %s", c.name)
		}
	}
}

func TestHelpRejectsAnUnknownCommand(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	l.say("help wombat\r")
	l.expect(t, "No such command: wombat")
}

// TestEveryCommandIsDocumented guards against a command being added to the
// table without anything for "?" and HELP to say about it.
func TestEveryCommandIsDocumented(t *testing.T) {
	t.Parallel()

	for _, c := range userCommands() {
		if c.usage == "" || c.summary == "" || len(c.detail) == 0 {
			t.Errorf("Command %q is not documented: %+v", c.name, c)
		}

		if c.handler == nil {
			t.Errorf("Command %q has no handler", c.name)
		}
	}
}

func TestEmptyLineGetsAHint(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	l.say("\r")
	l.expect(t, "Type ? for list of commands")
}

func TestInvalidCommand(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	l.say("wombat\r")
	l.expect(t, "Invalid command.")
}

// TestWhoShowsLoginTimes covers the Since column, which used to be the literal
// string "[time later]" even though the login time was right there.
func TestWhoShowsLoginTimes(t *testing.T) {
	t.Parallel()

	var srv = newAppServer()

	var other = newFakeLink("Q3TEST")
	other.port = 1
	startSession(t, srv, other)

	var l = newFakeLink("Q2TEST")
	startSession(t, srv, l)

	l.say("who\r")

	var got = l.expect(t, "Q3TEST")

	assert.Contains(t, got, "Q2TEST")
	assert.NotContains(t, got, "[time later]")

	for _, s := range srv.sortedSessions() {
		assert.Contains(t, got, s.loginTime.UTC().Format(loginTimeFormat))
	}
}

// TestCommandsIgnoreTheLineEnding covers a terminal that ends the line the way
// terminals do: the carriage return used to be parsed as part of the command,
// or of its last argument, so "who\r" was an invalid command.
func TestCommandsIgnoreTheLineEnding(t *testing.T) {
	t.Parallel()

	for name, said := range map[string][]string{
		"CR":                   {"who\r"},
		"CRLF":                 {"who\r\n"},
		"LF":                   {"who\n"},
		"LF in a later frame":  {"who\r", "\n"},
		"line over two frames": {"wh", "o\r"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var l = newFakeLink("Q2TEST")
			startSession(t, newAppServer(), l)

			for _, text := range said {
				l.say(text)
			}

			l.say("help wombat\r")

			var got = l.expect(t, "No such command: wombat")

			assert.Contains(t, got, "Session")
			assert.NotContains(t, got, "Invalid command")
			assert.NotContains(t, got, "Type ? for list", "a CRLF is one line ending, not two")
		})
	}
}

func TestTimingTest(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	l.say("test 3 64\r")

	l.expect(t, "Sending 3 frame(s) of 64 bytes")

	for n := 1; n <= 3; n++ {
		var frame = l.expect(t, "\r")
		assert.Len(t, frame, 64)
		assert.True(t, strings.HasPrefix(frame, strconv.Itoa(1000000 + n)[1:]+" "), "frame %d is %q", n, frame)
	}

	var got = l.expect(t, "bytes/sec")
	assert.Equal(t, 1, l.drainCount(), "waits for the frames to be acknowledged before the summary")

	// The summary used to print the elapsed time with %d on a
	// time.Duration: "192 bytes in 21015042 seconds", the nanosecond count.
	var summary = regexp.MustCompile(`192 bytes in ([0-9.]+) seconds, ([0-9]+) bytes/sec`).FindStringSubmatch(got)
	require.NotNil(t, summary, "no summary in %q", got)

	var seconds, parseErr = strconv.ParseFloat(summary[1], 64)
	require.NoError(t, parseErr)
	assert.Less(t, seconds, 60.0)

	var rate, rateErr = strconv.ParseFloat(summary[2], 64)
	require.NoError(t, rateErr)
	assert.Positive(t, rate)
}

func TestTimingTestClampsTheLength(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	startSession(t, newAppServer(), l)

	l.say("test 1 5\r")
	l.expect(t, "1 frame(s) of 16 bytes")
	assert.Len(t, l.expect(t, "\r"), 16)
}

func TestTimingTestRejectsBadArguments(t *testing.T) {
	t.Parallel()

	for command, want := range map[string]string{
		"test wombat":                          "not a frame count",
		"test 0":                               "not a frame count",
		"test " + strconv.Itoa(maxTestCount+1): "not a frame count",
		"test 3 wombat":                        "not a frame length",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			var l = newFakeLink("Q2TEST")
			startSession(t, newAppServer(), l)

			l.say(command + "\r")
			l.expect(t, want)

			// Nothing was sent: the next thing out answers the next command.
			l.say("help wombat\r")
			assert.NotContains(t, l.expect(t, "No such command"), "Sending")
			assert.Zero(t, l.drainCount())
		})
	}
}

func TestByeWaitsForTheFarewellThenHangsUp(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	var drained = make(chan struct{})

	l.drain = func(ctx context.Context) error {
		select {
		case <-drained:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	var srv = newAppServer()
	var done = startSession(t, srv, l)

	l.say("bye\r")
	l.expect(t, "come on back")

	assert.Eventually(t, func() bool { return l.drainCount() == 1 }, 5*time.Second, time.Millisecond)
	assert.False(t, l.isClosed(), "hung up before the farewell was acknowledged")

	close(drained)
	waitDone(t, done)

	assert.True(t, l.isClosed())
	assert.Empty(t, srv.sortedSessions())
}

// TestByeHangsUpWhenTheStationStopsAcknowledging checks we do not wait for a
// drain that is never going to come.
func TestByeHangsUpWhenTheStationStopsAcknowledging(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")

	l.drain = func(ctx context.Context) error {
		<-ctx.Done()

		return ctx.Err()
	}

	var srv = newAppServer()
	srv.byeDrainTimeout = 50 * time.Millisecond

	var done = startSession(t, srv, l)

	l.say("bye\r")
	waitDone(t, done)

	assert.True(t, l.isClosed())
}

// TestOneStationWaitingHoldsUpNoOther covers what the shared callback
// goroutine used to get wrong: anything one station waited for - a "bye"
// sleeping until its farewell went out - froze every other session too.
func TestOneStationWaitingHoldsUpNoOther(t *testing.T) {
	t.Parallel()

	var srv = newAppServer()

	var slow = newFakeLink("Q3TEST")
	var release = make(chan struct{})

	slow.drain = func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	t.Cleanup(func() { close(release) })

	startSession(t, srv, slow)

	slow.say("test 1 16\r")
	slow.expect(t, "Sending 1 frame(s)")

	var l = newFakeLink("Q2TEST")
	startSession(t, srv, l)

	l.say("who\r")
	l.expect(t, "Q3TEST")
}

func TestSessionEndsWhenTheStationLeaves(t *testing.T) {
	t.Parallel()

	var srv = newAppServer()
	var l = newFakeLink("Q2TEST")
	var done = startSession(t, srv, l)

	close(l.in)
	waitDone(t, done)

	assert.Empty(t, srv.sortedSessions())
}

func TestOverlongLineEndsTheSession(t *testing.T) {
	t.Parallel()

	var l = newFakeLink("Q2TEST")
	var done = startSession(t, newAppServer(), l)

	l.say(strings.Repeat("x", maxLineLen+1))
	waitDone(t, done)
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func TestMainRefusesBadArguments(t *testing.T) {
	t.Parallel()

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"no callsign":   {nil, "Exactly one argument required (MYCALL)"},
		"two callsigns": {[]string{"Q1TEST", "Q2TEST"}, "Exactly one argument required (MYCALL)"},
		"long callsign": {[]string{"Q1TESTLONG"}, "Callsign Q1TESTLONG too long"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 1, result.Status)
			assert.Contains(t, result.Output(), tc.want)
		})
	}
}

func TestMainHelp(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "--help")

	assert.Equal(t, 0, result.Status)
	assert.Contains(t, result.Output(), "Simple application server for connected mode AX.25")
}

// A TNC that isn't there yet is waited for, rather than given up on.
//
//nolint:paralleltest // Serial: relies on a port staying unused, which a parallel test could take.
func TestMainWaitsForTheTNC(t *testing.T) {
	var gone = testutils.UnusedPort(t)

	var p = testutils.StartMain(t, "-h", "127.0.0.1", "-p", gone, "Q1TEST")

	p.WaitFor(t, "Lost AGW server connection")
	p.WaitFor(t, "Lost AGW server connection") // And tries again.

	p.Signal(t, syscall.SIGTERM)
	assert.Equal(t, 0, p.Wait())
}

// readTNC reads one frame the appserver sent to the TNC.
func readTNC(t *testing.T, conn net.Conn) *agwpe.Message {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))

	var msg = new(agwpe.Message)

	require.NoError(t, binary.Read(conn, binary.LittleEndian, &msg.Header))

	msg.Data = make([]byte, msg.Header.DataLen)

	var _, err = io.ReadFull(conn, msg.Data)
	require.NoError(t, err)

	return msg
}

// readTNCKind reads frames the appserver sent to the TNC until one of kind,
// answering any outstanding frames questions on the way with 0.
func readTNCKind(t *testing.T, conn net.Conn, kind byte) *agwpe.Message {
	t.Helper()

	for {
		var msg = readTNC(t, conn)

		if msg.Header.DataKind == kind {
			return msg
		}

		if msg.Header.DataKind == 'Y' {
			writeTNC(t, conn, msg.Header.Portx, 'Y', msg.Header.CallFrom.String(), msg.Header.CallTo.String(), binary.LittleEndian.AppendUint32(nil, 0))
		}
	}
}

// writeTNC sends the appserver a frame as the TNC.
func writeTNC(t *testing.T, conn net.Conn, port byte, kind byte, from string, to string, data []byte) {
	t.Helper()

	var msg = new(agwpe.Message)

	msg.Header.Portx = port
	msg.Header.DataKind = kind
	copy(msg.Header.CallFrom[:], from)
	copy(msg.Header.CallTo[:], to)
	msg.Header.DataLen = safecast.RequireConvert[uint32](t, len(data))
	msg.Data = data

	var _, err = msg.Write(conn, binary.LittleEndian)
	if !errors.Is(err, net.ErrClosed) {
		require.NoError(t, err)
	}
}

// On attaching to the TNC, main asks what ports it has, and registers its
// callsign on each; then a station connecting gets served.
func TestMainRegistersOnEachPortAndServes(t *testing.T) {
	t.Parallel()

	var ln, port = testutils.Listen(t)

	testutils.StartMain(t, "-h", "127.0.0.1", "-p", port, "q1test")

	var tnc = testutils.Accept(t, ln)

	assert.Equal(t, byte('R'), readTNC(t, tnc).Header.DataKind)
	writeTNC(t, tnc, 0, 'R', "", "", binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, 2005), 127))

	assert.Equal(t, byte('G'), readTNC(t, tnc).Header.DataKind)
	writeTNC(t, tnc, 0, 'G', "", "", []byte("2;Port1 first;Port2 second;"))

	for _, want := range []byte{0, 1} {
		var reg = readTNC(t, tnc)

		assert.Equal(t, byte('X'), reg.Header.DataKind)
		assert.Equal(t, want, reg.Header.Portx)
		assert.Equal(t, "Q1TEST", reg.Header.CallFrom.String(), "the callsign should be upper cased")

		writeTNC(t, tnc, reg.Header.Portx, 'X', "Q1TEST", "", []byte{1})
	}

	// A station connects on the second port, asks who is on, and leaves.
	writeTNC(t, tnc, 1, 'C', "Q2TEST", "Q1TEST", []byte("*** CONNECTED To Station Q2TEST\r\x00"))
	assert.Contains(t, string(readTNCKind(t, tnc, 'D').Data), "Welcome!")

	writeTNC(t, tnc, 1, 'D', "Q2TEST", "Q1TEST", []byte("who\r"))

	var who = readTNCKind(t, tnc, 'D')
	for !strings.Contains(string(who.Data), "Q2TEST") {
		who = readTNCKind(t, tnc, 'D')
	}

	assert.Equal(t, byte(1), who.Header.Portx)
	assert.Equal(t, "Q2TEST", who.Header.CallTo.String())

	writeTNC(t, tnc, 1, 'D', "Q2TEST", "Q1TEST", []byte("bye\r"))
	assert.Contains(t, string(readTNCKind(t, tnc, 'D').Data), "come on back")

	var hangUp = readTNCKind(t, tnc, 'd')
	assert.Equal(t, "Q2TEST", hangUp.Header.CallTo.String())
}
