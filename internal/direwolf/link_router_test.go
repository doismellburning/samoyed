// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// eventLinkClients notes every event it is told of.
type eventLinkClients struct {
	name   string
	events []string
}

func (e *eventLinkClients) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	e.events = append(e.events, fmt.Sprintf("%s up %d/%d %s>%s %t", e.name, channel, client, ownCall, remoteCall, incoming))
}

func (e *eventLinkClients) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	e.events = append(e.events, fmt.Sprintf("%s down %d/%d %s>%s %t", e.name, channel, client, ownCall, remoteCall, timeout))
}

func (e *eventLinkClients) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	e.events = append(e.events, fmt.Sprintf("%s data %d/%d %s>%s %#x %q", e.name, channel, client, ownCall, remoteCall, pid, data))
}

func (e *eventLinkClients) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	e.events = append(e.events, fmt.Sprintf("%s outstanding %d/%d %s>%s %d", e.name, channel, client, ownCall, remoteCall, count))
}

func TestLinkRouterRoutesByClient(t *testing.T) {
	var agw = &eventLinkClients{name: "agw", events: nil}
	var app = &eventLinkClients{name: "app", events: nil}

	var r = newLinkRouter(agw)
	var client = r.attach(app)
	assert.GreaterOrEqual(t, client, firstAppClient)

	r.LinkEstablished(0, 1, "Q2TEST", "Q1TEST", true)
	r.LinkEstablished(0, client, "Q3TEST", "Q1TEST", false)
	r.RecConnData(1, client, "Q3TEST", "Q1TEST", 0xcf, []byte("x"))
	r.OutstandingFramesReply(0, 2, "Q1TEST", "Q2TEST", 3)
	r.LinkTerminated(0, client, "Q3TEST", "Q1TEST", true)

	assert.Equal(t, []string{
		"agw up 0/1 Q1TEST>Q2TEST true",
		"agw outstanding 0/2 Q1TEST>Q2TEST 3",
	}, agw.events)
	assert.Equal(t, []string{
		fmt.Sprintf("app up 0/%d Q1TEST>Q3TEST false", client),
		fmt.Sprintf("app data 1/%d Q1TEST>Q3TEST 0xcf \"x\"", client),
		fmt.Sprintf("app down 0/%d Q1TEST>Q3TEST true", client),
	}, app.events)
}

func TestLinkRouterDropsDetached(t *testing.T) {
	var app = &eventLinkClients{name: "app", events: nil}

	var r = newLinkRouter(nil)
	var client = r.attach(app)
	r.detach(client)

	r.LinkEstablished(0, client, "Q3TEST", "Q1TEST", false)
	r.LinkEstablished(0, 0, "Q3TEST", "Q1TEST", false) // No AGW server: nowhere.
	r.LinkEstablished(0, client+1, "Q3TEST", "Q1TEST", false)

	assert.Empty(t, app.events)
}

func TestLinkRouterNumbersAppsApart(t *testing.T) {
	var r = newLinkRouter(nil)

	var a = r.attach(noLinkClients{})
	var b = r.attach(noLinkClients{})

	assert.NotEqual(t, a, b)
	assert.GreaterOrEqual(t, a, MAX_NET_CLIENTS)
}
