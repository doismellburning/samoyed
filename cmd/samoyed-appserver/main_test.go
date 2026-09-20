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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
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
