// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTNC stands in for the socket to the TNC: agwlib writes frames into it and
// a test reads them back out to see what the appserver said.
type fakeTNC struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (f *fakeTNC) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.buf.Write(p)
}

func (f *fakeTNC) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.buf.Read(p)
}

func (f *fakeTNC) Close() error                       { return nil }
func (f *fakeTNC) LocalAddr() net.Addr                { return nil }
func (f *fakeTNC) RemoteAddr() net.Addr               { return nil }
func (f *fakeTNC) SetDeadline(_ time.Time) error      { return nil }
func (f *fakeTNC) SetReadDeadline(_ time.Time) error  { return nil }
func (f *fakeTNC) SetWriteDeadline(_ time.Time) error { return nil }

// tncFrame is one AGWPE frame the appserver sent towards the TNC.
type tncFrame struct {
	kind     byte
	channel  byte
	callFrom Callsign
	callTo   Callsign
	data     string
}

// frames drains and decodes everything written to the TNC so far.
func (f *fakeTNC) frames(t *testing.T) []tncFrame {
	t.Helper()

	var frames []tncFrame

	for {
		var h = new(AGWPEHeader)

		var readErr = binary.Read(f, binary.LittleEndian, h)
		if errors.Is(readErr, io.EOF) {
			return frames
		}

		if readErr != nil {
			t.Fatalf("Could not decode frame header: %s", readErr)
		}

		var data = make([]byte, h.DataLen)
		if h.DataLen > 0 {
			var _, dataErr = io.ReadFull(f, data)
			if dataErr != nil {
				t.Fatalf("Could not read %d bytes of frame data: %s", h.DataLen, dataErr)
			}
		}

		frames = append(frames, tncFrame{
			kind:     h.DataKind,
			channel:  h.Portx,
			callFrom: h.CallFrom,
			callTo:   h.CallTo,
			data:     string(data),
		})
	}
}

// sentText joins the text of the connected-data frames, for asserting on what
// the user would have seen.
func sentText(frames []tncFrame) string {
	var parts []string

	for _, f := range frames {
		if f.kind == 'D' {
			parts = append(parts, f.data)
		}
	}

	return strings.Join(parts, "")
}

// hasDisconnect reports whether the appserver asked the TNC to take the link
// down ('d', "Disconnect").
func hasDisconnect(frames []tncFrame) bool {
	for _, f := range frames {
		if f.kind == 'd' {
			return true
		}
	}

	return false
}

func testCallsign(call string) Callsign {
	var c Callsign

	copy(c[:], call)

	return c
}

var (
	testMyCall    = testCallsign("Q1TEST")
	testTheirCall = testCallsign("Q2TEST")
)

// newTestServer points the appserver globals at a fake TNC and an empty session
// table, restoring them when the test finishes.
func newTestServer(t *testing.T) *fakeTNC {
	t.Helper()

	var tnc = new(fakeTNC)

	var oldSock, oldSrv, oldMycall = s_tnc_sock, srv, mycall

	s_tnc_sock = tnc
	srv = newAppServer()
	mycall = testMyCall

	t.Cleanup(func() {
		s_tnc_sock = oldSock
		srv = oldSrv
		mycall = oldMycall
	})

	return tnc
}

// connect runs an incoming connection through the callbacks, as the listener
// goroutine would, and returns the resulting session.
func connect(t *testing.T, tnc *fakeTNC) *session {
	t.Helper()

	on_C_connection_received(0, testTheirCall, testMyCall, true, []byte("*** CONNECTED To Station Q2TEST\r"))

	tnc.frames(t) // Discard the greeting.

	var s = srv.findSession(0, testTheirCall)
	if s == nil {
		t.Fatal("Connecting did not create a session")
	}

	return s
}

// send delivers a command from the other station, as the listener goroutine
// would, and returns what the appserver sent back.
func send(t *testing.T, tnc *fakeTNC, command string) []tncFrame {
	t.Helper()

	agw_cb_D_connected_data(0, testTheirCall, testMyCall, []byte(command))

	return tnc.frames(t)
}

// TestByeDoesNotBlockTheListener covers the "bye" that used to sleep for ten
// seconds inside the callback, freezing every other session on the TNC with it.
func TestByeDoesNotBlockTheListener(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	var start = time.Now()

	var frames = send(t, tnc, "bye")

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("bye blocked the listener goroutine for %s; it must not block at all", elapsed)
	}

	if !strings.Contains(sentText(frames), "come on back") {
		t.Errorf("bye did not send a farewell, got %q", sentText(frames))
	}

	if hasDisconnect(frames) {
		t.Error("bye disconnected before the farewell could be acknowledged")
	}
}

// TestByeDisconnectsOnceTheFarewellIsAcknowledged covers the other half: the
// main loop must still pull the link down, once the queue has drained.
func TestByeDisconnectsOnceTheFarewellIsAcknowledged(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	send(t, tnc, "bye")

	// Still waiting for the farewell to be acknowledged.
	agw_cb_Y_outstanding_frames_for_station(0, testMyCall, testTheirCall, 2)
	s.poll()

	if hasDisconnect(tnc.frames(t)) {
		t.Error("Disconnected while frames were still queued for the station")
	}

	// Queue drained.
	agw_cb_Y_outstanding_frames_for_station(0, testMyCall, testTheirCall, 0)
	s.poll()

	if !hasDisconnect(tnc.frames(t)) {
		t.Error("Did not disconnect once the farewell had been acknowledged")
	}

	// And only the once.
	s.poll()

	if hasDisconnect(tnc.frames(t)) {
		t.Error("Disconnected a second time")
	}
}

// TestByeWaitsForTheTNCToAnswer covers a drain check that read a queue length
// the TNC had not reported yet.  Nothing asks the TNC about a station until it
// has something to wait for, so the first check after "bye" saw the zero the
// field started at, mistook it for an empty queue, and cut the farewell off.
func TestByeWaitsForTheTNCToAnswer(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	send(t, tnc, "bye")

	// The 'Y' reply has not arrived, so nothing is known about the queue yet.
	s.poll()

	if hasDisconnect(tnc.frames(t)) {
		t.Error("Disconnected before the TNC had said anything about the queue")
	}
}

// TestByeDisconnectsWhenTheStationStopsAcknowledging checks we do not wait for
// a drain that is never going to come.
func TestByeDisconnectsWhenTheStationStopsAcknowledging(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	send(t, tnc, "bye")

	agw_cb_Y_outstanding_frames_for_station(0, testMyCall, testTheirCall, 3)

	s.mu.Lock()
	s.byeDeadline = maybe.Just(time.Now().Add(-time.Second))
	s.mu.Unlock()

	s.poll()

	if !hasDisconnect(tnc.frames(t)) {
		t.Error("Did not disconnect after the drain timeout expired")
	}
}

// TestHelpListsTheCommands covers the "?" that the greeting points at, which
// used to answer "Help not yet available."
func TestHelpListsTheCommands(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	for _, command := range []string{"?", "help", "HELP"} {
		var got = sentText(send(t, tnc, command))

		if strings.Contains(got, "not yet available") {
			t.Errorf("%q still has no help: %q", command, got)
		}

		for _, c := range userCommands() {
			if !strings.Contains(got, c.usage) {
				t.Errorf("%q did not list %q, got %q", command, c.usage, got)
			}
		}
	}
}

// TestHelpDescribesOneCommand covers "HELP <command>".
func TestHelpDescribesOneCommand(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	for _, c := range userCommands() {
		var got = sentText(send(t, tnc, "help "+c.name))

		for _, line := range c.detail {
			if !strings.Contains(got, line) {
				t.Errorf("HELP %s did not say %q, got %q", c.name, line, got)
			}
		}
	}
}

// TestHelpRejectsAnUnknownCommand checks we say so rather than silently
// printing nothing.
func TestHelpRejectsAnUnknownCommand(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	var got = sentText(send(t, tnc, "help wombat"))

	if !strings.Contains(got, "No such command: wombat") {
		t.Errorf("HELP for an unknown command said %q", got)
	}
}

// TestEveryCommandIsDocumented guards against a command being added to the
// table without anything for "?" and HELP to say about it.
func TestEveryCommandIsDocumented(t *testing.T) {
	for _, c := range userCommands() {
		if c.usage == "" || c.summary == "" || len(c.detail) == 0 {
			t.Errorf("Command %q is not documented: %+v", c.name, c)
		}

		if c.handler == nil {
			t.Errorf("Command %q has no handler", c.name)
		}
	}
}

// TestWhoShowsLoginTimes covers the Since column, which used to be the literal
// string "[time later]" even though the login time was right there.
func TestWhoShowsLoginTimes(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	var got = sentText(send(t, tnc, "who"))

	if strings.Contains(got, "[time later]") {
		t.Errorf("who still has a placeholder where the login time goes: %q", got)
	}

	var want = s.loginTime.UTC().Format(loginTimeFormat)
	if !strings.Contains(got, want) {
		t.Errorf("who did not show the login time %q, got %q", want, got)
	}

	if !strings.Contains(got, testTheirCall.String()) {
		t.Errorf("who did not list the connected station, got %q", got)
	}
}

// TestCommandsIgnoreTheLineEnding covers a terminal that ends the line the way
// terminals do: the carriage return used to be parsed as part of the command,
// or of its last argument, so "who\r" was an invalid command.
func TestCommandsIgnoreTheLineEnding(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	for _, ending := range []string{"\r", "\r\n", "\n", ""} {
		var got = sentText(send(t, tnc, "who"+ending))

		if strings.Contains(got, "Invalid command") {
			t.Errorf("who%q was rejected: %q", ending, got)
		}

		if !strings.Contains(got, "Session") {
			t.Errorf("who%q did not list the sessions: %q", ending, got)
		}
	}
}

// TestTestAcknowledgesTheRequest covers a "test" that used to start sending
// without telling the user anything had happened.
func TestTestAcknowledgesTheRequest(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	var got = sentText(send(t, tnc, "test 3 64"))

	if !strings.Contains(got, "3 frame(s) of 64 bytes") {
		t.Errorf("test did not say what it was about to send, got %q", got)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ttCount != 3 || s.ttLength != 64 {
		t.Errorf("test set count %d length %d, wanted 3 and 64", s.ttCount, s.ttLength)
	}
}

// TestTestRejectsAnUnreadableCount checks that a typo is reported rather than
// quietly setting the count to zero and doing nothing.
func TestTestRejectsAnUnreadableCount(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	var got = sentText(send(t, tnc, "test wombat"))

	if !strings.Contains(got, "not a frame count") {
		t.Errorf("test with a bad count said %q", got)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ttCount != 0 {
		t.Errorf("A rejected test still started, count %d", s.ttCount)
	}
}

// TestTestRejectsTooManyFrames checks the cap on how much air one station can
// ask for: any positive int used to be accepted, and the main loop would have
// kept feeding the channel for as long as the station stayed connected.
func TestTestRejectsTooManyFrames(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	var got = sentText(send(t, tnc, "test "+strconv.Itoa(maxTestCount+1)))

	if !strings.Contains(got, "not a frame count") {
		t.Errorf("test with an excessive count said %q", got)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ttCount != 0 {
		t.Errorf("A rejected test still started, count %d", s.ttCount)
	}
}

// TestTimingTestSummaryIsInSeconds covers a summary that used to print the
// elapsed time with %d on a time.Duration - so "3 bytes in 21015042 seconds",
// the nanosecond count, followed by rates derived from a one-nanosecond run.
func TestTimingTestSummaryIsInSeconds(t *testing.T) {
	var tnc = newTestServer(t)

	var s = connect(t, tnc)

	send(t, tnc, "test 1 64")

	// First tick queues the frame, second sees the queue drained and reports.
	s.poll()
	agw_cb_Y_outstanding_frames_for_station(0, testMyCall, testTheirCall, 0)
	s.poll()

	var got = sentText(tnc.frames(t))

	var summary = regexp.MustCompile(`64 bytes in ([0-9.]+) seconds, ([0-9]+) bytes/sec`).FindStringSubmatch(got)
	if summary == nil {
		t.Fatalf("No timing test summary in %q", got)
	}

	var seconds, parseErr = strconv.ParseFloat(summary[1], 64)
	if parseErr != nil {
		t.Fatalf("Elapsed time %q does not parse: %s", summary[1], parseErr)
	}

	if seconds > 60 {
		t.Errorf("Summary reported %s seconds for a test that took a moment; those look like nanoseconds", summary[1])
	}

	var rate, rateErr = strconv.ParseFloat(summary[2], 64)
	if rateErr != nil {
		t.Fatalf("Rate %q does not parse: %s", summary[2], rateErr)
	}

	if rate <= 0 {
		t.Errorf("Summary reported a rate of %s bytes/sec", summary[2])
	}
}

// The main loop's poll only asks the TNC about sessions with something
// waiting on the answer - here, a goodbye.
func TestPollAsksAboutSessionsWithWorkInHand(t *testing.T) {
	var tnc = newTestServer(t)

	connect(t, tnc)

	srv.poll()
	assert.Empty(t, tnc.frames(t), "nothing to ask about yet")

	send(t, tnc, "bye")

	srv.poll()

	var asked bool

	for _, f := range tnc.frames(t) {
		if f.kind == 'Y' && f.callTo == testTheirCall {
			asked = true
		}
	}

	assert.True(t, asked, "should ask how much is still to go to the station")
}

// runMainEnv, when set, has the test binary run main with the arguments it
// holds instead of the tests, since main exits or never returns.
const runMainEnv = "SAMOYED_APPSERVER_RUN_MAIN"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(runMainEnv); ok {
		os.Args = append([]string{"appserver"}, strings.Fields(args)...)

		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func mainCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	var cmd = exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec
	cmd.Env = append(os.Environ(), runMainEnv+"="+strings.Join(args, " "))

	return cmd
}

func TestMainRefusesBadArguments(t *testing.T) {
	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	var _, gone, splitErr = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, splitErr)

	// Nothing is listening once this is closed.
	require.NoError(t, ln.Close())

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"no callsign":   {nil, "Exactly one argument required (MYCALL)"},
		"two callsigns": {[]string{"Q1TEST", "Q2TEST"}, "Exactly one argument required (MYCALL)"},
		"long callsign": {[]string{"Q1TESTTOOLONG"}, "Callsign Q1TESTTOOLONG too long"},
		"no TNC":        {[]string{"-h", "127.0.0.1", "-p", gone, "Q1TEST"}, "Could not attach to network TNC"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var out, err = mainCommand(t, tc.args...).CombinedOutput()

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())
			assert.Contains(t, string(out), tc.want)
		})
	}
}

func TestMainHelp(t *testing.T) {
	var out, err = mainCommand(t, "--help").CombinedOutput()
	require.NoError(t, err)

	assert.Contains(t, string(out), "Simple application server for connected mode AX.25")
}

// On attaching to the TNC, main asks what ports it has, and registers its
// callsign on each.
func TestMainRegistersOnEachPort(t *testing.T) {
	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	defer ln.Close()

	var _, port, splitErr = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, splitErr)

	var cmd = mainCommand(t, "-h", "127.0.0.1", "-p", port, "q1test")

	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var tnc, acceptErr = ln.Accept()
	require.NoError(t, acceptErr)

	defer tnc.Close()

	require.NoError(t, tnc.SetDeadline(time.Now().Add(10*time.Second)))

	var h = new(AGWPEHeader)

	require.NoError(t, binary.Read(tnc, binary.LittleEndian, h))
	assert.Equal(t, byte('G'), h.DataKind)

	var ports = "2;Port1 first;Port2 second;"

	var reply = new(AGWPEHeader)
	reply.DataKind = 'G'
	reply.DataLen = uint32(len(ports))

	require.NoError(t, binary.Write(tnc, binary.LittleEndian, reply))

	var _, writeErr = tnc.Write([]byte(ports))
	require.NoError(t, writeErr)

	require.NoError(t, binary.Read(tnc, binary.LittleEndian, h))
	assert.Equal(t, byte('X'), h.DataKind)
	assert.Equal(t, byte(0), h.Portx)
	assert.Equal(t, testMyCall, h.CallFrom, "the callsign should be upper cased")
}
