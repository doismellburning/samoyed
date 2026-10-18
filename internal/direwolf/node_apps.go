// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node's applications, such as its chat server, and the goroutine they
// run on.
//
// An application is not safe for concurrent use, and the shell calls it from
// whichever goroutine a user's data arrives on, so every call into one is
// queued for a single goroutine: NET/ROM's, when the node has it, so that an
// application can use NET/ROM as NET/ROM's own code does, or otherwise one of
// the applications' own.

import (
	"bytes"
	"context"
	"time"

	"github.com/doismellburning/samoyed/internal/chat"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
)

// nodeApps are the node's applications.
type nodeApps struct {
	// do queues a function to run on the applications' goroutine.
	do func(f func())

	// chat is the chat server, or nil for none.  Only the applications'
	// goroutine touches it.
	chat *chat.Server
}

// newNodeApps returns somewhere to run the node's applications: on nr's
// goroutine, or without NET/ROM, on one that runs until ctx is cancelled.
func newNodeApps(ctx context.Context, nr *netromNode) *nodeApps {
	var a = new(nodeApps)

	if nr != nil {
		a.do = nr.do

		return a
	}

	var work = newWorkQueue()
	a.do = work.push

	go runWork(ctx, work)

	return a
}

// runWork runs what is queued on work until ctx is cancelled.
func runWork(ctx context.Context, work *workQueue) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-work.ready:
			for _, f := range work.take() {
				f()
			}
		}
	}
}

// query runs f on the applications' goroutine, and waits for it as
// queryOn does.
func (a *nodeApps) query(ctx context.Context, f func()) error {
	return queryOn(ctx, a.do, f)
}

// add gives the shell app, whose every call is run on the applications'
// goroutine.
func (a *nodeApps) add(shell *node.Node, app node.Application) {
	shell.AddApplication(queuedApp{app: app, do: a.do})
}

// addChat gives the shell a chat server for cfg, publishing its users
// joining and leaving rooms on events, as the node nodeCall's.
func (a *nodeApps) addChat(shell *node.Node, cfg chat.Config, events *nodeevents.Bus, nodeCall string) error {
	var publish = func(kind nodeevents.Kind) func(user string, room string) {
		return func(user string, room string) {
			var ev = newEvent(kind, nodeCall, time.Now())
			ev.User = user
			ev.Room = room
			events.Publish(ev)
		}
	}

	cfg.OnJoin = publish(nodeevents.ChatJoin)
	cfg.OnLeave = publish(nodeevents.ChatLeave)

	var server, err = chat.New(cfg)
	if err != nil {
		return err
	}

	a.chat = server
	a.add(shell, server)

	return nil
}

// queuedApp is an application whose sessions are opened, and handed what
// users send, on the applications' goroutine.
type queuedApp struct {
	app node.Application
	do  func(f func())
}

func (q queuedApp) Name() string        { return q.app.Name() }
func (q queuedApp) Description() string { return q.app.Description() }

// Open queues the session's opening, returning at once with somewhere to
// queue what the user sends.  The queue keeps the order, so the session is
// open by the time any of it is handed over.
func (q queuedApp) Open(user string, out node.Sender, exit func()) node.AppSession { //nolint:ireturn // node.Application's signature.
	var s = &queuedAppSession{do: q.do, session: nil}

	q.do(func() { s.session = q.app.Open(user, out, exit) })

	return s
}

// queuedAppSession is a session of a queuedApp.
type queuedAppSession struct {
	do func(f func())

	// session is the application's own, touched only on the applications'
	// goroutine.
	session node.AppSession
}

func (s *queuedAppSession) Input(data []byte) {
	var copied = bytes.Clone(data)

	s.do(func() { s.session.Input(copied) })
}

func (s *queuedAppSession) Close() {
	s.do(func() { s.session.Close() })
}
