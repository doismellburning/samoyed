// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/stretchr/testify/require"
)

// fakeServer is just enough of an AGW server for the client's tests.  It
// answers the startup requests itself - version, ports, registration - and the
// outstanding frames question if told to, and hands everything else the
// client sends to the test on got.
type fakeServer struct {
	t  *testing.T
	ln net.Listener

	portsReply string
	registerOK byte

	answerY     atomic.Bool
	outstanding atomic.Uint32

	got        chan *agwpe.Message // Everything but the startup requests
	logins     chan *agwpe.Message
	registered chan int // Each port registered, on any connection
	accepted   chan struct{}

	mu     sync.Mutex
	conn   net.Conn
	closed bool // The test has ended, so no connection is to be served

	// done is closed when the test ends, and serving waits for serving to
	// stop: a reply it goes on to log after the test has finished would race
	// with the test finishing.
	done    chan struct{}
	serving sync.WaitGroup
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()

	var lc net.ListenConfig

	var ln, err = lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var s = new(fakeServer)

	s.t = t
	s.ln = ln
	s.portsReply = "1;Port1 fake soundcard mono;"
	s.registerOK = 1
	s.answerY.Store(true)
	s.got = make(chan *agwpe.Message, 1024)
	s.logins = make(chan *agwpe.Message, 16)
	s.registered = make(chan int, 64)
	s.accepted = make(chan struct{}, 16)
	s.done = make(chan struct{})

	t.Cleanup(func() {
		close(s.done)
		_ = ln.Close()

		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		s.dropConnection()
		s.serving.Wait()
	})

	return s
}

func (s *fakeServer) addr() string { return s.ln.Addr().String() }

// start serves connections, one at a time, until the test ends.
func (s *fakeServer) start() {
	s.serving.Go(func() {
		for {
			var conn, err = s.ln.Accept()
			if err != nil {
				return
			}

			// One accepted as the test ends would otherwise be left open,
			// with nothing to close it and serve reading from it forever.
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				_ = conn.Close()

				return
			}

			s.conn = conn
			s.mu.Unlock()

			deliver(s, s.accepted, struct{}{})

			s.serve(conn)
		}
	})
}

// deliver hands v to the test on ch, unless the test has ended and is no
// longer taking anything from it.
func deliver[T any](s *fakeServer, ch chan<- T, v T) {
	select {
	case ch <- v:
	case <-s.done:
	}
}

func (s *fakeServer) serve(conn net.Conn) {
	for {
		var msg, err = readFrame(conn, DefaultMaxData)
		if err != nil {
			return
		}

		switch msg.Header.DataKind {
		case kindLogin:
			deliver(s, s.logins, msg)
		case kindVersion:
			var data = binary.LittleEndian.AppendUint32(nil, 2005)
			s.reply(conn, 0, kindVersion, "", "", binary.LittleEndian.AppendUint32(data, 127))
		case kindPorts:
			s.reply(conn, 0, kindPorts, "", "", []byte(s.portsReply))
		case kindRegister:
			s.reply(conn, int(msg.Header.Portx), kindRegister, msg.Header.CallFrom.String(), "", []byte{s.registerOK})
			deliver(s, s.registered, int(msg.Header.Portx))
		case kindOutstanding:
			deliver(s, s.got, msg)

			if s.answerY.Load() {
				var count = binary.LittleEndian.AppendUint32(nil, s.outstanding.Load())
				s.reply(conn, int(msg.Header.Portx), kindOutstanding, msg.Header.CallFrom.String(), msg.Header.CallTo.String(), count)
			}
		default:
			deliver(s, s.got, msg)
		}
	}
}

func (s *fakeServer) reply(conn net.Conn, port int, kind byte, from string, to string, data []byte) {
	var msg, err = newFrame(port, kind, from, to, data)
	if err == nil {
		err = writeFrame(conn, msg)
	}

	if err != nil && !errors.Is(err, net.ErrClosed) {
		s.t.Logf("fake server reply: %v", err)
	}
}

// send sends a frame to the client on the current connection.
func (s *fakeServer) send(port int, kind byte, from string, to string, data []byte) {
	s.mu.Lock()
	var conn = s.conn
	s.mu.Unlock()

	require.NotNil(s.t, conn, "no client connected")
	s.reply(conn, port, kind, from, to, data)
}

// dropConnection closes the current connection, as a server going away would.
func (s *fakeServer) dropConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// next returns the next frame the client sent that isn't an outstanding
// frames question, failing the test if none comes soon.
func (s *fakeServer) next() *agwpe.Message {
	s.t.Helper()

	for {
		var msg = s.nextAny()
		if msg.Header.DataKind != kindOutstanding {
			return msg
		}
	}
}

// nextAny returns the next frame the client sent, failing the test if none
// comes soon.
func (s *fakeServer) nextAny() *agwpe.Message {
	s.t.Helper()

	select {
	case msg := <-s.got:
		return msg
	case <-time.After(5 * time.Second):
		s.t.Fatal("timed out waiting for a frame from the client")

		return nil
	}
}

// testConfig is a client configuration for s with short timeouts.
func testConfig(s *fakeServer) Config {
	var cfg = new(Config)

	cfg.Addr = s.addr()
	cfg.Call = "Q1TEST"
	cfg.Ports = []int{0}
	cfg.YTimeout = time.Second
	cfg.YPoll = 10 * time.Millisecond
	cfg.FallbackDelay = 10 * time.Millisecond
	cfg.ConnectTimeout = 5 * time.Second
	cfg.CloseTimeout = time.Second
	cfg.StartupTimeout = 2 * time.Second
	cfg.Backoff = 10 * time.Millisecond
	cfg.MaxBackoff = 50 * time.Millisecond

	return *cfg
}

// runClient starts a client with cfg running until the test ends, and waits
// for it to be ready.
func runClient(t *testing.T, cfg Config) (*Client, context.CancelFunc) {
	t.Helper()

	var c = New(cfg)
	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		defer close(done)

		_ = c.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitReady(t, c)

	return c, cancel
}

func waitReady(t *testing.T, c *Client) {
	t.Helper()

	select {
	case <-c.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("client did not become ready")
	}
}
