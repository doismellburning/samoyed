// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeConn records what is sent to it.
type fakeConn struct {
	sent   strings.Builder
	closed bool
}

func (c *fakeConn) Send(data []byte) { c.sent.Write(data) }
func (c *fakeConn) Close()           { c.closed = true }

// take returns what has been sent since it was last asked, with the node's
// carriage returns as newlines.
func (c *fakeConn) take() string {
	var s = strings.ReplaceAll(c.sent.String(), "\r", "\n")
	c.sent.Reset()

	return s
}

// fakeBackend is a node with fixed tables, recording onward connections.
type fakeBackend struct {
	nodes     []netrom.Destination
	neighbour []netrom.Neighbour
	heard     []HeardStation

	down       *fakeConn
	downEvents Downlink
	downTo     string
	downVia    []string
	downUser   string
	connectErr error
}

func (b *fakeBackend) Ports() []Port {
	return []Port{{Number: 0, Description: "Radio, 1200 baud"}, {Number: 10, Description: "AXUDP, UDP port 10093"}}
}

func (b *fakeBackend) Nodes() []netrom.Destination    { return b.nodes }
func (b *fakeBackend) Neighbours() []netrom.Neighbour { return b.neighbour }

func (b *fakeBackend) Heard(port int) []HeardStation {
	var list []HeardStation

	for _, h := range b.heard {
		if port < 0 || h.Port == port {
			list = append(list, h)
		}
	}

	return list
}

func (b *fakeBackend) ConnectNode(dest string, user string, events Downlink) (Conn, error) { //nolint:ireturn // Backend's signature.
	if b.connectErr != nil {
		return nil, b.connectErr
	}

	b.down = new(fakeConn)
	b.downEvents = events
	b.downTo = dest
	b.downUser = user

	return b.down, nil
}

func (b *fakeBackend) ConnectAX25(port int, call string, via []string, user string, events Downlink) (Conn, error) { //nolint:ireturn // Backend's signature.
	var c, err = b.ConnectNode(call, user, events)
	b.downTo = call + " port " + strconv.Itoa(port)
	b.downVia = via

	return c, err
}

func newTestShell(t *testing.T, apps ...Application) (*Shell, *fakeConn, *fakeBackend) {
	t.Helper()

	var be = &fakeBackend{
		nodes: []netrom.Destination{
			{Call: "Q2TEST", Alias: "TWO", Routes: []netrom.Route{{Neighbour: netrom.NeighbourKey{Port: 10, Call: "Q2TEST"}, Quality: 200, Obsolescence: 6, Locked: false}}},
			{Call: "Q3TEST", Alias: "THREE", Routes: nil},
		},
		neighbour: []netrom.Neighbour{{Port: 10, Call: "Q2TEST", Alias: "TWO", Quality: 200, Locked: true}},
		heard:     []HeardStation{{Port: 0, Call: "Q5TEST", Last: time.Unix(1000, 0), Frames: 3}},

		down:       nil,
		downEvents: nil,
		downTo:     "",
		downVia:    nil,
		downUser:   "",
		connectErr: nil,
	}

	var up = new(fakeConn)

	var s = NewShell(Config{Call: "Q1TEST", Alias: "ONE", Info: "A test node.\nIn a test.", IdleTimeout: time.Minute, Applications: apps}, be, "Q4TEST", up, time.Unix(1000, 0))
	s.Start()

	return s, up, be
}

func input(s *Shell, text string) {
	s.Input([]byte(text), time.Unix(1060, 0).Add(-time.Second))
}

func TestShellGreetsAndHelps(t *testing.T) {
	var s, up, _ = newTestShell(t)
	assert.Equal(t, "ONE:Q1TEST} Welcome to ONE:Q1TEST, Q4TEST.  Type ? for a list of commands.\n", up.take())

	input(s, "?\r")
	assert.Contains(t, up.take(), "C <node>")

	input(s, "bogus\r")
	assert.Equal(t, "ONE:Q1TEST} Invalid command.  Type ? for a list of commands.\n", up.take())

	input(s, "\r\n  \r")
	assert.Empty(t, up.take(), "blank lines are ignored")
}

func TestShellLists(t *testing.T) {
	var s, up, _ = newTestShell(t)
	up.take()

	input(s, "n\r")
	assert.Equal(t, "ONE:Q1TEST} Nodes:\nTWO:Q2TEST         THREE:Q3TEST\n", up.take())

	input(s, "N two\r")
	assert.Equal(t, "ONE:Q1TEST} Routes to TWO:Q2TEST:\n  quality 200  obsolescence 6  port 10  via Q2TEST\n", up.take())

	input(s, "N FOUR\r")
	assert.Equal(t, "ONE:Q1TEST} Not found: FOUR\n", up.take())

	input(s, "R\r")
	assert.Equal(t, "ONE:Q1TEST} Neighbours:\n  port 10  TWO:Q2TEST          quality 200 (locked)\n", up.take())

	input(s, "MH 0\r")
	assert.Equal(t, "ONE:Q1TEST} Heard on port 0:\n  Q5TEST    port 0  0:00:00:59 ago  3 frames\n", up.take())

	input(s, "MH x\r")
	assert.Contains(t, up.take(), "Bad port number")

	input(s, "P\r")
	assert.Equal(t, "ONE:Q1TEST} Ports:\n   0 Radio, 1200 baud\n  10 AXUDP, UDP port 10093\n", up.take())

	input(s, "I\r")
	assert.Equal(t, "ONE:Q1TEST} A test node.\nIn a test.\n", up.take())
}

func TestShellLineInPieces(t *testing.T) {
	var s, up, _ = newTestShell(t)
	up.take()

	input(s, "PO")
	assert.Empty(t, up.take())
	input(s, "RTS\rI\r")
	assert.Equal(t, "ONE:Q1TEST} Ports:\n   0 Radio, 1200 baud\n  10 AXUDP, UDP port 10093\nONE:Q1TEST} A test node.\nIn a test.\n", up.take())
}

func TestShellLongLine(t *testing.T) {
	var s, up, _ = newTestShell(t)
	up.take()

	input(s, strings.Repeat("x", 3*maxLine))
	assert.Equal(t, 3, strings.Count(up.take(), "Invalid command"))
}

func TestShellConnectsToNode(t *testing.T) {
	var s, up, be = newTestShell(t)
	up.take()

	input(s, "C two\rearly\r")
	assert.Equal(t, "TWO", be.downTo)
	assert.Equal(t, "Q4TEST", be.downUser)
	assert.Empty(t, up.take(), "nothing until the far end answers")

	be.downEvents.Connected()
	assert.Equal(t, "ONE:Q1TEST} Connected to TWO\n", up.take())
	assert.Equal(t, "early\r", be.down.sent.String(), "what was typed while connecting is passed on")

	input(s, "hello\r")
	assert.Equal(t, "early\rhello\r", be.down.sent.String())

	be.downEvents.Received([]byte("TWO:Q2TEST} hi\r"))
	assert.Equal(t, "TWO:Q2TEST} hi\n", up.take())

	be.downEvents.Closed(nil)
	assert.Equal(t, "ONE:Q1TEST} Reconnected to ONE:Q1TEST\n", up.take())

	input(s, "P\r")
	assert.Contains(t, up.take(), "Ports:", "back at the node's prompt")
}

func TestShellConnectsOnPort(t *testing.T) {
	var s, _, be = newTestShell(t)

	input(s, "c 0 q5test v q6test,q7test\r")
	assert.Equal(t, "Q5TEST port 0", be.downTo)
	assert.Equal(t, []string{"Q6TEST", "Q7TEST"}, be.downVia)
}

func TestShellConnectFails(t *testing.T) {
	var s, up, be = newTestShell(t)
	up.take()

	be.connectErr = errors.New("no route")
	input(s, "C NOWHERE\r")
	assert.Equal(t, "ONE:Q1TEST} Failure with NOWHERE: no route\n", up.take())

	be.connectErr = nil
	input(s, "C TWO\r")
	be.downEvents.Closed(errors.New("refused"))
	assert.Equal(t, "ONE:Q1TEST} Failure with TWO: refused\n", up.take())

	input(s, "C ONE\r")
	assert.Contains(t, up.take(), "already connected")

	input(s, "C\r")
	assert.Contains(t, up.take(), "Connect to which")
}

func TestShellUserLeavesWhileConnected(t *testing.T) {
	var s, _, be = newTestShell(t)

	input(s, "C TWO\r")
	be.downEvents.Connected()
	s.Closed()
	assert.True(t, be.down.closed, "the onward connection goes with the user")
}

func TestShellBye(t *testing.T) {
	var s, up, _ = newTestShell(t)

	input(s, "BYE\rI\r")
	assert.True(t, up.closed)
	assert.NotContains(t, up.take(), "test node", "nothing after BYE")
}

func TestShellIdle(t *testing.T) {
	var s, up, _ = newTestShell(t)
	up.take()

	s.Tick(time.Unix(1059, 0))
	assert.False(t, up.closed)

	s.Tick(time.Unix(1060, 0))
	assert.True(t, up.closed)
	assert.Contains(t, up.take(), "idle")
}

// echoApp is an Application that echoes what it is sent, until "/q".
type echoApp struct {
	open   int
	closed int
}

func (a *echoApp) Name() string        { return "ECHO" }
func (a *echoApp) Description() string { return "Echo what you type" }

func (a *echoApp) Open(user string, out Sender, exit func()) AppSession { //nolint:ireturn // Application's signature.
	a.open++
	out.Send([]byte("echo for " + user + "\r"))

	return &echoSession{app: a, out: out, exit: exit}
}

type echoSession struct {
	app  *echoApp
	out  Sender
	exit func()
}

func (e *echoSession) Input(data []byte) {
	if strings.TrimSpace(string(data)) == "/q" {
		e.exit()

		return
	}

	e.out.Send(data)
}

func (e *echoSession) Close() { e.app.closed++ }

func TestShellApplications(t *testing.T) {
	var app = new(echoApp)

	var s, up, _ = newTestShell(t, app)
	up.take()

	input(s, "?\r")
	assert.Contains(t, up.take(), "ECHO                      Echo what you type")

	input(s, "echo\r")
	assert.Equal(t, "echo for Q4TEST\n", up.take())

	input(s, "N\r")
	assert.Equal(t, "N\n", up.take(), "input goes to the application")

	input(s, "/q\r")
	assert.Equal(t, "ONE:Q1TEST} Returned to ONE:Q1TEST\n", up.take())

	input(s, "ECHO\r")
	s.Closed()
	assert.Equal(t, 1, app.closed)
	require.Equal(t, 2, app.open)
}

func FuzzShellInput(f *testing.F) {
	f.Add("C TWO\rhello\r")
	f.Add("N two\rR\rMH 0\rP\rI\r?\r")
	f.Add("c 0 q5test v q6test\r")
	f.Add("ECHO\rx\r/q\rBYE\r")

	f.Fuzz(func(t *testing.T, text string) {
		var s, _, be = newTestShell(t, new(echoApp))
		input(s, text)

		if be.downEvents != nil {
			be.downEvents.Connected()
			be.downEvents.Received([]byte(text))
			input(s, text)
			be.downEvents.Closed(nil)
		}

		input(s, text)
		s.Tick(time.Unix(2000, 0))
		s.Closed()
	})
}
