// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// incoming has the fake server announce a connection from remote to the
// client's Q1TEST, and returns the link the client accepts for it.
func incoming(t *testing.T, s *fakeServer, c *Client, remote string) *Link {
	t.Helper()

	s.send(0, kindConnect, remote, "Q1TEST", []byte("*** CONNECTED To Station "+remote+"\r\x00"))

	var ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var link, err = c.Accept(ctx)
	require.NoError(t, err)

	return link
}

func readAll(t *testing.T, link *Link, n int) []byte {
	t.Helper()

	var ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var got []byte
	var buf = make([]byte, 64)

	for len(got) < n {
		var m, err = link.ReadContext(ctx, buf)
		require.NoError(t, err)

		got = append(got, buf[:m]...)
	}

	return got
}

func TestStartupRegistersEachPort(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.portsReply = "2;Port1 first soundcard left;Port2 first soundcard right;"
	s.start()

	var cfg = testConfig(s)
	cfg.Ports = []int{0, 1}

	var c, _ = runClient(t, cfg)

	assert.ElementsMatch(t, []int{0, 1}, []int{<-s.registered, <-s.registered})
	assert.Equal(t, []PortInfo{{Number: 0, Description: "first soundcard left"}, {Number: 1, Description: "first soundcard right"}}, c.Ports())
	assert.Empty(t, s.logins, "no login configured, so none sent")

	s.send(1, kindConnect, "Q2TEST", "Q1TEST", nil)

	var link, err = c.Accept(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, link.Port())
}

func TestStartupRegistersOnEveryPortWithNoneConfigured(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.portsReply = "2;Port1 first soundcard mono;Port11 Network TNC;"
	s.start()

	var cfg = testConfig(s)
	cfg.Ports = nil

	runClient(t, cfg)

	assert.ElementsMatch(t, []int{0, 10}, []int{<-s.registered, <-s.registered})
}

func TestStartupLogsIn(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var cfg = testConfig(s)
	cfg.Login = maybe.Just(Credentials{User: "Q1TEST", Password: "correct horse"})

	runClient(t, cfg)

	var login = <-s.logins
	assert.Equal(t, "Q1TEST", string(bytes.TrimRight(login.Data[:loginFieldLen], "\x00")))
	assert.Equal(t, "correct horse", string(bytes.TrimRight(login.Data[loginFieldLen:], "\x00")))
}

func TestStartupFails(t *testing.T) {
	t.Parallel()

	t.Run("port the server doesn't have", func(t *testing.T) {
		t.Parallel()

		var s = newFakeServer(t)
		s.start()

		var cfg = testConfig(s)
		cfg.Ports = []int{3}

		var wasUp, err = New(cfg).session(t.Context())
		require.ErrorContains(t, err, "no port 3")
		assert.False(t, wasUp)
	})

	t.Run("registration refused", func(t *testing.T) {
		t.Parallel()

		var s = newFakeServer(t)
		s.registerOK = 0
		s.start()

		var wasUp, err = New(testConfig(s)).session(t.Context())
		require.ErrorContains(t, err, "refused to register Q1TEST")
		assert.False(t, wasUp)
	})

	t.Run("nothing listening", func(t *testing.T) {
		t.Parallel()

		var s = newFakeServer(t)
		var cfg = testConfig(s)
		require.NoError(t, s.ln.Close())

		var wasUp, err = New(cfg).session(t.Context())
		require.Error(t, err)
		assert.False(t, wasUp)
	})
}

func TestAcceptIncomingLink(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))
	var link = incoming(t, s, c, "Q2TEST")

	assert.Equal(t, "Q2TEST", link.Remote())
	assert.Equal(t, "Q1TEST", link.Local())
	assert.Equal(t, 0, link.Port())

	s.send(0, kindData, "Q2TEST", "Q1TEST", []byte("hello "))
	s.send(0, kindData, "Q2TEST", "Q1TEST", []byte("there"))
	assert.Equal(t, "hello there", string(readAll(t, link, 11)))

	var n, err = link.Write([]byte("hi"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	var data = s.next()
	assert.Equal(t, byte(kindData), data.Header.DataKind)
	assert.Equal(t, "Q1TEST", data.Header.CallFrom.String())
	assert.Equal(t, "Q2TEST", data.Header.CallTo.String())
	assert.Equal(t, byte(pidNoLayer3), data.Header.PID)
	assert.Equal(t, []byte("hi"), data.Data)

	// What arrived before the far end left is still there to read.
	s.send(0, kindData, "Q2TEST", "Q1TEST", []byte("bye"))
	s.send(0, kindDisconnect, "Q2TEST", "Q1TEST", []byte("*** DISCONNECTED From Station Q2TEST\r\x00"))
	assert.Equal(t, "bye", string(readAll(t, link, 3)))

	var _, readErr = link.Read(make([]byte, 10))
	require.ErrorIs(t, readErr, io.EOF)

	var _, writeErr = link.Write([]byte("too late"))
	require.ErrorIs(t, writeErr, ErrLinkClosed)
}

func TestConnect(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))

	var result = make(chan connectResult, 1)

	go func() {
		var link, err = c.Connect(t.Context(), 0, "q1test", "Q2TEST-0", nil)
		result <- connectResult{link: link, err: err}
	}()

	var req = s.next()
	assert.Equal(t, byte(kindConnect), req.Header.DataKind)
	assert.Equal(t, "Q1TEST", req.Header.CallFrom.String())
	assert.Equal(t, "Q2TEST", req.Header.CallTo.String())

	s.send(0, kindConnect, "Q2TEST", "Q1TEST", []byte("*** CONNECTED With Station Q2TEST\r\x00"))
	s.send(0, kindData, "Q2TEST", "Q1TEST", []byte("welcome"))

	var got = <-result
	require.NoError(t, got.err)
	assert.Equal(t, "Q2TEST", got.link.Remote())
	assert.Equal(t, "welcome", string(readAll(t, got.link, 7)))

	var _, err = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
	require.ErrorIs(t, err, ErrLinkExists)

	// An outgoing link is not one to accept.
	var ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	var _, acceptErr = c.Accept(ctx)
	require.ErrorIs(t, acceptErr, context.DeadlineExceeded)
}

func TestConnectVia(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))

	go func() {
		_, _ = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", []string{"Q3TEST", "Q4TEST-2"})
	}()

	var req = s.next()
	assert.Equal(t, byte(kindConnectVia), req.Header.DataKind)
	require.Len(t, req.Data, 1+2*callsignLen)
	assert.Equal(t, byte(2), req.Data[0])
}

func TestConnectRefused(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))

	var result = make(chan error, 1)

	go func() {
		var _, err = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
		result <- err
	}()

	s.next()
	s.send(0, kindDisconnect, "Q2TEST", "Q1TEST", []byte("*** DISCONNECTED RETRYOUT With Q2TEST\r\x00"))

	require.ErrorIs(t, <-result, ErrConnectFailed)
}

func TestConnectGivenUp(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var cfg = testConfig(s)
	cfg.ConnectTimeout = 50 * time.Millisecond

	var c, _ = runClient(t, cfg)

	var _, err = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
	require.ErrorIs(t, err, ErrConnectFailed)

	assert.Equal(t, byte(kindConnect), s.next().Header.DataKind)

	var abort = s.next()
	assert.Equal(t, byte(kindDisconnect), abort.Header.DataKind, "asks the server to stop trying")

	// The connection comes up after all, too late.  The client hangs up
	// rather than offer it to Accept as though it were incoming.
	s.send(0, kindConnect, "Q2TEST", "Q1TEST", []byte("*** CONNECTED With Station Q2TEST\r\x00"))

	var hangUp = s.next()
	assert.Equal(t, byte(kindDisconnect), hangUp.Header.DataKind)
	assert.Equal(t, "Q2TEST", hangUp.Header.CallTo.String())

	var ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	var _, acceptErr = c.Accept(ctx)
	require.ErrorIs(t, acceptErr, context.DeadlineExceeded)
}

func TestConnectAgainAfterGivingUp(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var cfg = testConfig(s)
	cfg.ConnectTimeout = 50 * time.Millisecond

	var c, _ = runClient(t, cfg)

	var _, err = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
	require.ErrorIs(t, err, ErrConnectFailed)

	// The server never answers the attempt given up on, but that mustn't
	// stand in the way of another.
	_, err = c.Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
	require.ErrorIs(t, err, ErrConnectFailed)
	require.NotErrorIs(t, err, ErrLinkExists)
}

func TestConnectWithoutServer(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)

	var _, err = New(testConfig(s)).Connect(t.Context(), 0, "Q1TEST", "Q2TEST", nil)
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestWriteSplitsAtPacLen(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var cfg = testConfig(s)
	cfg.PacLen = 10

	var c, _ = runClient(t, cfg)
	var link = incoming(t, s, c, "Q2TEST")

	var payload = []byte("0123456789abcdefghijKLMNO")

	var n, err = link.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)

	var got []byte

	for _, want := range []int{10, 10, 5} {
		var msg = s.next()
		assert.Len(t, msg.Data, want)

		got = append(got, msg.Data...)
	}

	assert.Equal(t, payload, got)
}

func TestWriteWaitsForOutstandingFrames(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.outstanding.Store(4)
	s.start()

	var cfg = testConfig(s)
	cfg.PacLen = 1
	cfg.HighWater = 4

	var c, _ = runClient(t, cfg)
	var link = incoming(t, s, c, "Q2TEST")

	var done = make(chan error, 1)

	go func() {
		var _, err = link.Write([]byte("ab"))
		done <- err
	}()

	assert.Equal(t, "a", string(s.next().Data))

	// It keeps asking, and doesn't send the second frame.
	for range 3 {
		var msg = s.nextAny()
		assert.Equal(t, byte(kindOutstanding), msg.Header.DataKind)
		assert.Equal(t, "Q1TEST", msg.Header.CallFrom.String())
		assert.Equal(t, "Q2TEST", msg.Header.CallTo.String())
	}

	select {
	case err := <-done:
		t.Fatalf("write finished while the window was full: %v", err)
	default:
	}

	s.outstanding.Store(3)

	assert.Equal(t, "b", string(s.next().Data))
	require.NoError(t, <-done)
}

func TestWriteWithoutOutstandingReplies(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.answerY.Store(false)
	s.start()

	var cfg = testConfig(s)
	cfg.PacLen = 1
	cfg.YTimeout = 50 * time.Millisecond

	var c, _ = runClient(t, cfg)
	var link = incoming(t, s, c, "Q2TEST")

	var n, err = link.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	// One question goes unanswered, and after that it stops asking.
	var kinds []byte

	for range 4 {
		kinds = append(kinds, s.nextAny().Header.DataKind)
	}

	assert.Equal(t, []byte{kindData, kindOutstanding, kindData, kindData}, kinds)
	require.NoError(t, link.Drain(t.Context()))
}

func TestDrain(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.outstanding.Store(2)
	s.start()

	var c, _ = runClient(t, testConfig(s))
	var link = incoming(t, s, c, "Q2TEST")

	var done = make(chan error, 1)

	go func() { done <- link.Drain(t.Context()) }()

	s.nextAny()

	select {
	case err := <-done:
		t.Fatalf("drained with frames outstanding: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	s.outstanding.Store(0)
	require.NoError(t, <-done)
}

func TestClose(t *testing.T) {
	t.Parallel()

	t.Run("confirmed", func(t *testing.T) {
		t.Parallel()

		var s = newFakeServer(t)
		s.start()

		var c, _ = runClient(t, testConfig(s))
		var link = incoming(t, s, c, "Q2TEST")

		go func() {
			var req = s.next()
			s.send(0, kindDisconnect, req.Header.CallTo.String(), req.Header.CallFrom.String(), nil)
		}()

		require.NoError(t, link.Close())
		require.NoError(t, link.Close(), "closing twice is harmless")

		var _, err = link.Read(make([]byte, 1))
		require.ErrorIs(t, err, ErrLinkClosed)

		// With the link gone, the same pair can connect again.
		incoming(t, s, c, "Q2TEST")
	})

	t.Run("unconfirmed", func(t *testing.T) {
		t.Parallel()

		var s = newFakeServer(t)
		s.start()

		var cfg = testConfig(s)
		cfg.CloseTimeout = 50 * time.Millisecond

		var c, _ = runClient(t, cfg)
		var link = incoming(t, s, c, "Q2TEST")

		require.ErrorIs(t, link.Close(), ErrLinkClosed)
		assert.Equal(t, byte(kindDisconnect), s.next().Header.DataKind)
	})
}

func TestConnectionLoss(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))
	<-s.accepted
	<-s.registered

	var link = incoming(t, s, c, "Q2TEST")

	s.dropConnection()

	var _, readErr = link.Read(make([]byte, 1))
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)

	var _, writeErr = link.Write([]byte("x"))
	require.ErrorIs(t, writeErr, io.ErrUnexpectedEOF)

	// It reconnects and registers again.
	select {
	case <-s.accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("client did not reconnect")
	}

	assert.Equal(t, 0, <-s.registered)
	waitReady(t, c)

	assert.Equal(t, "Q3TEST", incoming(t, s, c, "Q3TEST").Remote())
}

func TestCancellation(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.outstanding.Store(100)
	s.start()

	var c, cancelRun = runClient(t, testConfig(s))
	var link = incoming(t, s, c, "Q2TEST")

	var readCtx, cancelRead = context.WithCancel(t.Context())

	var readDone = make(chan error, 1)

	go func() {
		var _, err = link.ReadContext(readCtx, make([]byte, 1))
		readDone <- err
	}()

	cancelRead()
	require.ErrorIs(t, <-readDone, context.Canceled)

	// A write held up by a full window gives up when the client stops.
	var writeDone = make(chan error, 1)

	go func() {
		var _, err = link.Write([]byte("x"))
		writeDone <- err
	}()

	s.next()
	cancelRun()

	require.ErrorIs(t, <-writeDone, io.ErrUnexpectedEOF)

	var _, readErr = link.Read(make([]byte, 1))
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
}

func TestIgnoresFramesForUnknownLinks(t *testing.T) {
	t.Parallel()

	var s = newFakeServer(t)
	s.start()

	var c, _ = runClient(t, testConfig(s))

	s.send(0, kindData, "Q9TEST", "Q1TEST", []byte("stray"))
	s.send(0, kindDisconnect, "Q9TEST", "Q1TEST", nil)
	s.send(0, kindOutstanding, "Q1TEST", "Q9TEST", binary.LittleEndian.AppendUint32(nil, 1))
	s.send(0, kindOutstanding, "Q1TEST", "Q9TEST", []byte{1})
	s.send(0, 'K', "", "", []byte("raw"))

	// Still working afterwards.
	incoming(t, s, c, "Q2TEST")
}
