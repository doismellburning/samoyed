// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

// Applications on the node's AX.25 links, without the shell in between: a
// station connecting straight to an application's own callsign, such as a
// BBS's, and links an application opens itself, such as a BBS forwarding to
// another.

import (
	"github.com/doismellburning/samoyed/internal/ax25"
)

// Service opens an application's session for a station, user, that connected
// to it directly, sending to conn, which closing disconnects.  It is called
// without the node's lock held.
type Service func(user string, conn Conn) AppSession

// AddService has stations that connect to call, on any of the node's ports,
// handed to open rather than the shell.  It must be called before Start, with
// a call that is neither the node's address nor its alias.
func (n *Node) AddService(call string, open Service) {
	n.services[call] = open
}

// appLink is a link an application opened, and what it is told of it.
type appLink struct {
	events Downlink
	up     bool
}

// ConnectFrom opens an AX.25 link on channel, from the address from to to by
// way of the digipeaters via, for an application, telling events how it goes,
// without the node's lock held.  What is sent to the result before the link
// is up is lost, so wait for events.Connected.
func (n *Node) ConnectFrom(channel int, from string, to string, via []string, events Downlink) Conn { //nolint:ireturn // As Attach, the far end is any Conn.
	var key = linkKey{channel: channel, ownCall: from, remoteCall: to}

	n.mu.Lock()
	n.appLinks[key] = &appLink{events: events, up: false}
	n.mu.Unlock()

	var a = addrs(key)

	var numAddr = ax25.Repeater1 + min(len(via), ax25.MaxAddrs-ax25.Repeater1)
	for i := ax25.Repeater1; i < numAddr; i++ {
		a[i] = via[i-ax25.Repeater1]
	}

	n.link.ConnectRequest(a, numAddr, channel, n.client, ax25.PIDNoLayer3)

	return linkConn{n: n, key: key}
}

// linkConn is the node's side of an AX.25 link, for an application.
type linkConn struct {
	n   *Node
	key linkKey
}

func (c linkConn) Send(data []byte) { c.n.send(c.key, data) }
func (c linkConn) Close()           { c.n.disconnect(c.key) }

// applicationLinkUp is told key is up, and says whether it is an
// application's: one it opened, or a station connecting to a service.
func (n *Node) applicationLinkUp(key linkKey, incoming bool) bool {
	n.mu.Lock()

	if l, ok := n.appLinks[key]; ok {
		// The data link says so again when the far end resets the link.
		var first = !l.up
		l.up = true
		n.mu.Unlock()

		if first {
			l.events.Connected()
		}

		return true
	}

	var open, isService = n.services[key.ownCall]
	if !isService || !incoming {
		n.mu.Unlock()

		return false
	}

	if _, reset := n.serviceSessions[key]; reset {
		n.mu.Unlock()

		return true
	}

	var run = &appRun{name: key.ownCall, session: nil, closed: false}
	n.serviceSessions[key] = run
	n.mu.Unlock()

	var session = open(key.remoteCall, linkConn{n: n, key: key})

	n.mu.Lock()
	run.session = session
	var closed = run.closed
	n.mu.Unlock()

	if closed {
		session.Close()
	}

	return true
}

// applicationLinkDown is told key has gone down, and says whether it was an
// application's.
func (n *Node) applicationLinkDown(key linkKey, timeout bool) bool {
	n.mu.Lock()

	if l, ok := n.appLinks[key]; ok {
		delete(n.appLinks, key)
		n.mu.Unlock()

		var failure string
		if !l.up {
			failure = failureFor(timeout)
		}

		l.events.Closed(failure)

		return true
	}

	var run, ok = n.serviceSessions[key]
	if !ok {
		n.mu.Unlock()

		return false
	}

	delete(n.serviceSessions, key)
	run.closed = true
	var session = run.session
	n.mu.Unlock()

	// Without a session yet, the one Open returns is closed instead.
	if session != nil {
		session.Close()
	}

	return true
}

// applicationData is handed what arrived on key, and says whether it was for
// an application.
func (n *Node) applicationData(key linkKey, data []byte) bool {
	n.mu.Lock()

	if l, ok := n.appLinks[key]; ok {
		n.mu.Unlock()
		l.events.Received(data)

		return true
	}

	var run, ok = n.serviceSessions[key]

	var session AppSession
	if ok {
		session = run.session
	}

	n.mu.Unlock()

	if session != nil {
		session.Input(data)
	}

	return ok
}
