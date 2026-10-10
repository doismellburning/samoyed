// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import "sync"

// firstAppClient is the first client number handed to an application running
// in this process - the NET/ROM node, say.  The AGW server's client numbers
// are its socket slots, 0 to MAX_NET_CLIENTS-1, so these are well clear.
const firstAppClient = 100

// linkRouter is the link layer's linkClients when more than the AGW server
// asks for connected-mode links: it passes each event to whichever of the AGW
// server or an in-process application the link's client number belongs to.
//
// The link layer calls it from its own goroutine, and so calls the
// applications from there too; they must not block.  Attaching and detaching
// happen from elsewhere, hence the lock.
type linkRouter struct {
	mu   sync.Mutex
	agw  linkClients
	apps map[int]linkClients
	next int
}

// newLinkRouter returns a linkRouter passing the AGW server's links to agw,
// which may be nil for none.
func newLinkRouter(agw linkClients) *linkRouter {
	if agw == nil {
		agw = noLinkClients{}
	}

	var r = new(linkRouter)
	r.agw = agw
	r.apps = make(map[int]linkClients)
	r.next = firstAppClient

	return r
}

// attach adds an in-process application, returning the client number its
// link requests are to carry.
func (r *linkRouter) attach(app linkClients) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var client = r.next
	r.next++
	r.apps[client] = app

	return client
}

// detach removes the application with client number client.  Anything the
// link layer still has to say about its links goes nowhere.
func (r *linkRouter) detach(client int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.apps, client)
}

// route returns whoever is to hear about client's links.
func (r *linkRouter) route(client int) linkClients { //nolint:ireturn // Which implementation is the whole point.
	if client < firstAppClient {
		return r.agw
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var app, ok = r.apps[client]
	if !ok {
		return noLinkClients{}
	}

	return app
}

func (r *linkRouter) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	r.route(client).LinkEstablished(channel, client, remoteCall, ownCall, incoming)
}

func (r *linkRouter) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	r.route(client).LinkTerminated(channel, client, remoteCall, ownCall, timeout)
}

func (r *linkRouter) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	r.route(client).RecConnData(channel, client, remoteCall, ownCall, pid, data)
}

func (r *linkRouter) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	r.route(client).OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
}
