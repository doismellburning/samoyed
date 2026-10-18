// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoApp is an Application that echoes what it is sent, until "/q".
type echoApp struct {
	opened []string
	open   []*echoSession

	// exitOnOpen has the application leave as soon as it is opened.
	exitOnOpen bool

	// during, if set, is run as the application is being opened.
	during func()
}

func (a *echoApp) Name() string        { return "echo" }
func (a *echoApp) Description() string { return "Say it again" }

func (a *echoApp) Open(user string, out Sender, exit func()) AppSession { //nolint:ireturn // Application's signature.
	a.opened = append(a.opened, user)

	var s = &echoSession{out: out, exit: exit, input: nil, closed: false}
	a.open = append(a.open, s)

	out.Send([]byte("Echo here\r"))

	if a.during != nil {
		a.during()
	}

	if a.exitOnOpen {
		exit()
	}

	return s
}

type echoSession struct {
	out    Sender
	exit   func()
	input  []string
	closed bool
}

func (s *echoSession) Input(data []byte) {
	s.input = append(s.input, string(data))

	if strings.TrimSpace(string(data)) == "/q" {
		s.exit()

		return
	}

	s.out.Send(data)
}

func (s *echoSession) Close() { s.closed = true }

// withApp is connected, for a node with app.
func withApp(t *testing.T, app Application) (*Node, *fakeLink) {
	t.Helper()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, testClient)
	n.AddApplication(app)

	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	return n, link
}

func TestApplication(t *testing.T) {
	t.Parallel()

	var app = new(echoApp)
	var n, link = withApp(t, app)

	typeLine(n, "Echo")
	require.Equal(t, []string{userCall}, app.opened, "an application starts by its name, in any case")
	assert.Equal(t, "Echo here\r", link.output())
	assert.Equal(t, []SessionInfo{{User: userCall, Channel: testChannel, Local: nodeCall, Downlink: "", Connected: false, App: "ECHO"}}, n.Sessions())

	link.reset()
	typeLine(n, "INFO")
	assert.Equal(t, "INFO\r", link.output(), "what the user sends goes to the application, not the node")

	link.reset()
	typeLine(n, "/q")
	assert.Equal(t, "SAMNOD:Q1TEST-7} Back at the node\r", link.output())
	assert.False(t, app.open[0].closed, "an application the user left is not closed again")

	link.reset()
	typeLine(n, "PORTS")
	assert.Contains(t, link.output(), "Ports", "the user is back at the node")
}

func TestApplicationClosedWhenUserGoes(t *testing.T) {
	t.Parallel()

	var app = new(echoApp)
	var n, _ = withApp(t, app)

	typeLine(n, "ECHO")
	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)

	assert.True(t, app.open[0].closed)
	assert.Empty(t, n.Sessions())
}

func TestApplicationLeftWhileOpening(t *testing.T) {
	t.Parallel()

	var app = &echoApp{opened: nil, open: nil, exitOnOpen: true, during: nil}
	var n, link = withApp(t, app)

	typeLine(n, "ECHO")
	assert.Equal(t, "Echo here\rSAMNOD:Q1TEST-7} Back at the node\r", link.output())

	link.reset()
	typeLine(n, "/q")
	assert.Equal(t, invalidCommand, link.output(), "the user is back at the node")
}

func TestApplicationUserGoesWhileOpening(t *testing.T) {
	t.Parallel()

	var app = new(echoApp)
	var n, _ = withApp(t, app)

	app.during = func() { n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false) }

	typeLine(n, "ECHO")
	require.Len(t, app.open, 1)
	assert.True(t, app.open[0].closed, "a session whose user went while it opened is closed once it has")
}

func TestApplicationTakesTheRestOfTheFrame(t *testing.T) {
	t.Parallel()

	var app = new(echoApp)
	var n, link = withApp(t, app)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("ECHO\rINFO\rPOR"))
	assert.Equal(t, "Echo here\rINFO\rPOR", link.output(), "what follows the command goes to the application")
	require.Len(t, app.open, 1)
	assert.Equal(t, []string{"INFO\rPOR"}, app.open[0].input)
}

func TestApplicationInHelp(t *testing.T) {
	t.Parallel()

	var n, link = withApp(t, new(echoApp))

	typeLine(n, "?")
	assert.True(t, strings.HasSuffix(link.output(), "  ROUTES, R - List the routes known\r  ECHO - Say it again\r"), link.output())
}
