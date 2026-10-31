// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwt

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tnc is just enough of an AGW server to hand a client one link: it answers
// the startup requests, and the outstanding frames question with outstanding.
type tnc struct {
	t           *testing.T
	conn        net.Conn
	outstanding atomic.Uint32
	got         chan *agwpe.Message // Everything else the client sends
}

func readMessage(conn net.Conn) (*agwpe.Message, error) {
	var msg = new(agwpe.Message)

	var err = binary.Read(conn, binary.LittleEndian, &msg.Header)
	if err != nil {
		return nil, err
	}

	msg.Data = make([]byte, msg.Header.DataLen)

	var _, dataErr = io.ReadFull(conn, msg.Data)

	return msg, dataErr
}

func (s *tnc) send(kind byte, from string, to string, data []byte) {
	var msg = new(agwpe.Message)

	msg.Header.DataKind = kind
	copy(msg.Header.CallFrom[:], from)
	copy(msg.Header.CallTo[:], to)
	msg.Header.DataLen = safecast.MustConvert[uint32](len(data))
	msg.Data = data

	_, _ = msg.Write(s.conn, binary.LittleEndian)
}

func (s *tnc) serve() {
	for {
		var msg, err = readMessage(s.conn)
		if err != nil {
			return
		}

		switch msg.Header.DataKind {
		case 'R':
			s.send('R', "", "", make([]byte, 8))
		case 'G':
			s.send('G', "", "", []byte("1;Port1 fake;"))
		case 'X':
			s.send('X', msg.Header.CallFrom.String(), "", []byte{1})
		case 'Y':
			s.send('Y', msg.Header.CallFrom.String(), msg.Header.CallTo.String(), binary.LittleEndian.AppendUint32(nil, s.outstanding.Load()))
		default:
			s.got <- msg
		}
	}
}

// next is the next frame the client sent, other than the questions tnc
// answers itself.
func (s *tnc) next() *agwpe.Message {
	s.t.Helper()

	select {
	case msg := <-s.got:
		return msg
	case <-time.After(5 * time.Second):
		s.t.Fatal("timed out waiting for a frame from the client")

		return nil
	}
}

// startClient runs an agwclient as Q1BBS against a tnc of its own, and
// returns both once the client is ready.
func startClient(t *testing.T) (*agwclient.Client, *tnc) {
	t.Helper()

	var lc net.ListenConfig

	var ln, err = lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var server = new(tnc)

	server.t = t
	server.got = make(chan *agwpe.Message, 256)

	var accepted = make(chan struct{})

	go func() {
		var conn, acceptErr = ln.Accept()
		if acceptErr != nil {
			return
		}

		server.conn = conn
		close(accepted)
		server.serve()
	}()

	var cfg = new(agwclient.Config)

	cfg.Addr = ln.Addr().String()
	cfg.Call = "Q1BBS"
	cfg.YTimeout = time.Second
	cfg.YPoll = 10 * time.Millisecond

	var client = agwclient.New(*cfg)
	var ctx, cancel = context.WithCancel(t.Context())
	var done = make(chan struct{})

	go func() {
		defer close(done)

		_ = client.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
		_ = ln.Close()
	})

	select {
	case <-client.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("client did not become ready")
	}

	<-accepted

	return client, server
}

// connect has a station connect to Q1BBS, and returns the session the
// listener hands out for it.
func connect(t *testing.T, client *agwclient.Client, server *tnc, drainTimeout time.Duration) *Session {
	t.Helper()

	var listener = NewListener(client.Accept, drainTimeout)

	server.send('C', "Q2TEST", "Q1BBS", []byte("*** CONNECTED To Station Q2TEST\r\x00"))

	var ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var s, err = listener.Accept(ctx)
	require.NoError(t, err)

	return s
}

func TestSession(t *testing.T) {
	t.Parallel()

	var client, server = startClient(t)
	var s = connect(t, client, server, time.Second)

	assert.Equal(t, transport.Peer{Call: "Q2TEST", Kind: transport.AGW, Authenticated: false}, s.Peer())

	// What the station sends is read a line at a time, and what the BBS
	// writes reaches it.
	server.send('D', "Q2TEST", "Q1BBS", []byte("L\r"))

	var line, readErr = lines.NewReader(s, lines.DefaultMaxLen).ReadLine(t.Context())
	require.NoError(t, readErr)
	assert.Equal(t, "L", line)

	require.NoError(t, lines.Write(s, "No messages."))

	var data = server.next()
	assert.Equal(t, byte('D'), data.Header.DataKind)
	assert.Equal(t, "No messages.\r", string(data.Data))
}

func TestCloseWaitsForWhatWasSent(t *testing.T) {
	t.Parallel()

	var client, server = startClient(t)
	var s = connect(t, client, server, 5*time.Second)

	server.outstanding.Store(1)

	var closed = make(chan error, 1)

	go func() { closed <- s.Close() }()

	select {
	case err := <-closed:
		t.Fatalf("hung up with a frame outstanding: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	server.outstanding.Store(0)

	var hangUp = server.next()
	assert.Equal(t, byte('d'), hangUp.Header.DataKind)
	assert.Equal(t, "Q2TEST", hangUp.Header.CallTo.String())

	server.send('d', "Q2TEST", "Q1BBS", nil)
	require.NoError(t, <-closed)
}

func TestCloseGivesUpWaiting(t *testing.T) {
	t.Parallel()

	var client, server = startClient(t)
	var s = connect(t, client, server, 50*time.Millisecond)

	server.outstanding.Store(1)

	go func() { _ = s.Close() }()

	assert.Equal(t, byte('d'), server.next().Header.DataKind, "hangs up anyway once the drain times out")
}

func TestListenerStops(t *testing.T) {
	t.Parallel()

	var client, _ = startClient(t)
	var listener = NewListener(client.Accept, time.Second)

	var ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	var _, err = listener.Accept(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
