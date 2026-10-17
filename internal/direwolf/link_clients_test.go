// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// loggingLinkClients remembers what it is told, one line per call.
type loggingLinkClients struct {
	calls []string
}

func (c *loggingLinkClients) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	c.calls = append(c.calls, fmt.Sprintf("established %d %d %s %s %t", channel, client, remoteCall, ownCall, incoming))
}

func (c *loggingLinkClients) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	c.calls = append(c.calls, fmt.Sprintf("terminated %d %d %s %s %t", channel, client, remoteCall, ownCall, timeout))
}

func (c *loggingLinkClients) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	c.calls = append(c.calls, fmt.Sprintf("data %d %d %s %s %d %q", channel, client, remoteCall, ownCall, pid, data))
}

func (c *loggingLinkClients) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	c.calls = append(c.calls, fmt.Sprintf("outstanding %d %d %s %s %d", channel, client, ownCall, remoteCall, count))
}

// tellAll tells clients one of everything about client's link.
func tellAll(clients linkClients, client int) {
	clients.LinkEstablished(1, client, "Q2TEST", "Q1TEST", true)
	clients.RecConnData(1, client, "Q2TEST", "Q1TEST", 0xf0, []byte("hi"))
	clients.OutstandingFramesReply(1, client, "Q1TEST", "Q2TEST", 3)
	clients.LinkTerminated(1, client, "Q2TEST", "Q1TEST", true)
}

func TestLinkClientRouter(t *testing.T) {
	t.Parallel()

	var agw = new(loggingLinkClients)
	var node = new(loggingLinkClients)
	var router = newLinkClientRouter(agw, node)

	tellAll(router, 0)
	tellAll(router, MAX_NET_CLIENTS-1)
	tellAll(router, nodeClient)

	var want = func(client int) []string {
		return []string{
			fmt.Sprintf("established 1 %d Q2TEST Q1TEST true", client),
			fmt.Sprintf("data 1 %d Q2TEST Q1TEST 240 \"hi\"", client),
			fmt.Sprintf("outstanding 1 %d Q1TEST Q2TEST 3", client),
			fmt.Sprintf("terminated 1 %d Q2TEST Q1TEST true", client),
		}
	}

	assert.Equal(t, append(want(0), want(MAX_NET_CLIENTS-1)...), agw.calls)
	assert.Equal(t, want(nodeClient), node.calls)
}

func TestLinkClientRouterWithoutAGW(t *testing.T) {
	t.Parallel()

	var node = new(loggingLinkClients)
	var router = newLinkClientRouter(nil, node)

	tellAll(router, 0)
	tellAll(router, nodeClient)

	assert.Len(t, node.calls, 4)
}

func TestLinkClientRouterWithoutNode(t *testing.T) {
	t.Parallel()

	var agw = new(loggingLinkClients)
	var router = newLinkClientRouter(agw, nil)

	tellAll(router, 0)
	tellAll(router, nodeClient)

	assert.Len(t, agw.calls, 4)
}
