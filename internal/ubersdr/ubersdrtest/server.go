// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package ubersdrtest is a fake UberSDR server for tests, speaking enough of
// the protocol for a client to register a session and receive audio.
package ubersdrtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/doismellburning/samoyed/internal/ubersdr/internal/v4enc"
)

// Server is a fake UberSDR instance.  Each audio WebSocket a client opens
// arrives on Accept, for the test to send audio down.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	sessions map[string]bool
	refuse   string

	conns chan *Conn
}

// Conn is one client's audio WebSocket.
type Conn struct {
	// Query is the WebSocket request's query string, so a test can check
	// what the client asked to be tuned to.
	Query url.Values

	// Registered reports whether the session ID in Query had been POSTed to
	// /connection first, as a real server insists.
	Registered bool

	ws   *websocket.Conn
	enc  *v4enc.Encoder
	done chan struct{}
}

// NewServer starts a fake server, which is closed when the test ends.
func NewServer(t *testing.T) *Server {
	t.Helper()

	var s = &Server{ //nolint:exhaustruct_v5 // The embedded server is started below
		sessions: map[string]bool{},
		conns:    make(chan *Conn, 16),
	}

	var mux = http.NewServeMux()
	mux.HandleFunc("POST /connection", s.handleConnection)
	mux.HandleFunc("GET /ws", s.handleWebSocket)

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)

	return s
}

// Refuse makes /connection refuse every session from now on, giving reason.
// An empty reason allows them again.
func (s *Server) Refuse(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refuse = reason
}

// Accept waits for the next audio WebSocket a client opens.
func (s *Server) Accept(t *testing.T) *Conn {
	t.Helper()

	select {
	case c := <-s.conns:
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("no UberSDR client connected")

		return nil
	}
}

func (s *Server) handleConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserSessionID string `json:"user_session_id"` //nolint:tagliatelle // The server's name for it
	}

	var err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	s.mu.Lock()
	s.sessions[req.UserSessionID] = true
	var refuse = s.refuse
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	var resp = struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}{refuse == "", refuse}

	encErr := json.NewEncoder(w).Encode(resp) // := for errchkjson, which does not follow var
	if encErr != nil {
		return // Nothing to do about a client that has gone
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	var ws, err = websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	s.mu.Lock()
	var registered = s.sessions[r.URL.Query().Get("user_session_id")]
	s.mu.Unlock()

	var c = &Conn{
		Query:      r.URL.Query(),
		Registered: registered,
		ws:         ws,
		enc:        v4enc.New(),
		done:       make(chan struct{}),
	}

	s.conns <- c

	// Read, and discard, whatever the client sends - its pings - until it
	// or the test closes the socket.  The handler has to stay running for
	// that long, or the server closes the connection under the test.
	for {
		var _, _, readErr = ws.Read(r.Context())
		if readErr != nil {
			break
		}
	}

	close(c.done)
}

// SendAudio sends one packet of mono 16-bit audio at sampleRate, encoded as
// the real server would.
func (c *Conn) SendAudio(t *testing.T, samples []int16, sampleRate int) {
	t.Helper()

	var pkt, err = c.enc.Encode(samples, sampleRate, 1, 0)
	if err != nil {
		t.Fatalf("encoding audio: %v", err)
	}

	c.SendBinary(t, pkt)
}

// SendBinary sends data as a binary message.
func (c *Conn) SendBinary(t *testing.T, data []byte) {
	t.Helper()

	var err = c.ws.Write(t.Context(), websocket.MessageBinary, data)
	if err != nil {
		t.Fatalf("sending to the UberSDR client: %v", err)
	}
}

// SendText sends data as a text message.
func (c *Conn) SendText(t *testing.T, data string) {
	t.Helper()

	var err = c.ws.Write(t.Context(), websocket.MessageText, []byte(data))
	if err != nil {
		t.Fatalf("sending to the UberSDR client: %v", err)
	}
}

// Close closes the socket from the server's end.
func (c *Conn) Close() {
	_ = c.ws.Close(websocket.StatusGoingAway, "test closing")
}

// WaitClosed waits for the client to close the socket.
func (c *Conn) WaitClosed(t *testing.T) {
	t.Helper()

	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatal("UberSDR client did not close its connection")
	}
}
