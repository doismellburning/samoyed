// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"fmt"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const serviceCall = "Q1TEST-1"

// serviceFor is a Service opening sessions of app, as a user from the
// shell would get, but whose leaving disconnects them.
func serviceFor(app *echoApp) Service {
	return func(user string, conn Conn) AppSession {
		return app.Open(user, conn, conn.Close)
	}
}

func TestService(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, testClient)

	var app = new(echoApp)
	n.AddService(serviceCall, serviceFor(app))
	n.Start()

	assert.Contains(t, link.registered, fmt.Sprintf("%s@%d/%d", serviceCall, testChannel, testClient), "the service's callsign is registered on each port")

	n.LinkEstablished(testChannel, testClient, userCall, serviceCall, true)
	require.Equal(t, []string{userCall}, app.opened)
	assert.Equal(t, "Echo here\r", link.output(), "no greeting from the shell")
	assert.Equal(t, serviceCall, link.sent[0].from, "the service answers from its own callsign")

	n.LinkEstablished(testChannel, testClient, userCall, serviceCall, true)
	assert.Len(t, app.opened, 1, "a reset link keeps its session")

	assert.Equal(t, []SessionInfo{{User: userCall, Channel: testChannel, Local: serviceCall, Downlink: "", Connected: false, App: serviceCall}}, n.Sessions())

	link.reset()
	n.RecConnData(testChannel, testClient, userCall, serviceCall, ax25.PIDNoLayer3, []byte("INFO\r"))
	assert.Equal(t, "INFO\r", link.output(), "what the station sends goes to the service")

	n.RecConnData(testChannel, testClient, userCall, serviceCall, ax25.PIDNoLayer3, []byte("/q\r"))
	assert.Equal(t, []string{userCall}, link.disconnects, "leaving the service disconnects")

	n.LinkTerminated(testChannel, testClient, userCall, serviceCall, false)
	assert.True(t, app.open[0].closed)
	assert.Empty(t, n.Sessions())
}

func TestServiceStationGoesWhileOpening(t *testing.T) {
	t.Parallel()

	var n = New(testConfig(), new(fakeLink), noneHeard, testClient)

	var app = new(echoApp)
	app.during = func() { n.LinkTerminated(testChannel, testClient, userCall, serviceCall, false) }
	n.AddService(serviceCall, serviceFor(app))

	n.LinkEstablished(testChannel, testClient, userCall, serviceCall, true)
	require.Len(t, app.open, 1)
	assert.True(t, app.open[0].closed)
}

func TestServiceOnlyForIncomingLinks(t *testing.T) {
	t.Parallel()

	var n = New(testConfig(), new(fakeLink), noneHeard, testClient)

	var app = new(echoApp)
	n.AddService(serviceCall, serviceFor(app))

	n.LinkEstablished(testChannel, testClient, userCall, serviceCall, false)
	assert.Empty(t, app.opened)
}

// downlinkEvents records what a Downlink is told.
type downlinkEvents struct {
	said []string
}

func (d *downlinkEvents) Connected()           { d.said = append(d.said, "connected") }
func (d *downlinkEvents) Received(data []byte) { d.said = append(d.said, "received "+string(data)) }
func (d *downlinkEvents) Closed(failure string) {
	d.said = append(d.said, "closed "+failure)
}

func TestConnectFrom(t *testing.T) {
	t.Parallel()

	var link = new(fakeLink)
	var n = New(testConfig(), link, noneHeard, testClient)
	var events = new(downlinkEvents)

	var conn = n.ConnectFrom(testChannel, serviceCall, "Q3TEST", []string{"Q8TEST"}, events)
	require.Len(t, link.connects, 1)
	assert.Equal(t, sent{channel: testChannel, from: serviceCall, to: "Q3TEST", data: ""}, link.connects[0])
	assert.Equal(t, []string{"Q8TEST"}, link.via[0])

	n.LinkEstablished(testChannel, testClient, "Q3TEST", serviceCall, false)
	n.LinkEstablished(testChannel, testClient, "Q3TEST", serviceCall, true)
	n.RecConnData(testChannel, testClient, "Q3TEST", serviceCall, ax25.PIDNoLayer3, []byte("[FBB-7.00-$]\r"))

	conn.Send([]byte("hello\r"))
	assert.Equal(t, "hello\r", link.output())
	assert.Equal(t, serviceCall, link.sent[0].from)
	assert.Empty(t, n.Sessions(), "an application's link is nobody's session")

	conn.Close()
	assert.Equal(t, []string{"Q3TEST"}, link.disconnects)

	n.LinkTerminated(testChannel, testClient, "Q3TEST", serviceCall, false)
	assert.Equal(t, []string{"connected", "received [FBB-7.00-$]\r", "closed "}, events.said)

	n.LinkTerminated(testChannel, testClient, "Q3TEST", serviceCall, false)
	assert.Len(t, events.said, 3, "and is forgotten")
}

func TestConnectFromFails(t *testing.T) {
	t.Parallel()

	var n = New(testConfig(), new(fakeLink), noneHeard, testClient)
	var events = new(downlinkEvents)

	n.ConnectFrom(testChannel, serviceCall, "Q3TEST", nil, events)
	n.LinkTerminated(testChannel, testClient, "Q3TEST", serviceCall, true)
	assert.Equal(t, []string{"closed no answer"}, events.said)
}
