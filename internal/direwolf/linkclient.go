// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import "sync"

// linkClient is what the connected-mode data link reports to: a link coming
// up or going down, data arriving on it, and the answer to an outstanding
// frames query.  The AGW server is the usual one, on behalf of the network
// clients attached to it; a subsystem inside samoyed that holds AX.25
// connections of its own registers itself as an internal client instead.
type linkClient interface {
	LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool)
	LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool)
	RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte)
	OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int)
}

// Client numbers below MAX_NET_CLIENTS belong to the AGW server's network
// clients.  Those from MAX_NET_CLIENTS up are for internal clients, so the
// two can never collide.
const (
	netromClient = MAX_NET_CLIENTS + iota // The NET/ROM node's neighbour links.
)

// internalLinkClients maps an internal client number to what the data link
// reports to for it.  It is set during startup and read from the data link's
// goroutine, hence the lock.
var internalLinkClients = new(linkClientRegistry) //nolint:gochecknoglobals

type linkClientRegistry struct {
	mu      sync.Mutex
	clients map[int]linkClient
}

// setInternalLinkClient makes c the recipient of the data link's reports for
// client, or removes the entry when c is nil.
func setInternalLinkClient(client int, c linkClient) {
	internalLinkClients.mu.Lock()
	defer internalLinkClients.mu.Unlock()

	if c == nil {
		delete(internalLinkClients.clients, client)

		return
	}

	if internalLinkClients.clients == nil {
		internalLinkClients.clients = make(map[int]linkClient)
	}

	internalLinkClients.clients[client] = c
}

// linkClientFor returns what the data link should report to for client: the
// internal client registered under that number, or otherwise the AGW server.
// An internal client number with nothing registered gets a client that
// discards everything, rather than reaching the AGW server with a number it
// has no slot for.
func linkClientFor(client int) linkClient { //nolint:ireturn // choosing among implementations is the point
	if client < MAX_NET_CLIENTS {
		return agwServer
	}

	internalLinkClients.mu.Lock()
	defer internalLinkClients.mu.Unlock()

	if c, ok := internalLinkClients.clients[client]; ok {
		return c
	}

	return discardLinkClient{}
}

// discardLinkClient drops every report.
type discardLinkClient struct{}

func (discardLinkClient) LinkEstablished(int, int, string, string, bool)       {}
func (discardLinkClient) LinkTerminated(int, int, string, string, bool)        {}
func (discardLinkClient) RecConnData(int, int, string, string, int, []byte)    {}
func (discardLinkClient) OutstandingFramesReply(int, int, string, string, int) {}
