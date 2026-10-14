// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"encoding/hex"
	"sync"

	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/sirupsen/logrus"
)

// maxPendingKissAcks is how many ACKMODE acknowledgements can wait for one
// client.  That many still unsent means it has stopped reading, and more
// would only pile up behind it, so later ones are dropped instead.
const maxPendingKissAcks = 64

// kissAck is an ACKMODE acknowledgement waiting to go to a client: the ID of
// a frame it sent for channel.
type kissAck struct {
	channel int
	id      []byte
}

// kissAckQueue is what is still to be sent to one client, and how many have
// been dropped since it last had nothing waiting.
type kissAckQueue struct {
	acks    []kissAck
	dropped int
}

// kissAcks sends KISS clients the ACKMODE acknowledgements for their frames.
//
// It is told a frame has gone from the transmit thread, which mustn't wait on
// a client that has stopped reading: nothing would go out on the air
// meanwhile.  So each client's acknowledgements go out from a goroutine of
// their own, in the order the frames went, and that goroutine lasts only
// while there are some to send.
type kissAcks struct {
	ctx context.Context //nolint:containedctx // Handed to each client's goroutine as it starts.

	// mu guards pending.  A client has an entry exactly while its goroutine
	// is running, holding what is still to be sent to it.  kissClient values
	// are compared as map keys, which every transport's is fit for: a
	// pointer, or kissNetClient, whose connection is one.
	mu      sync.Mutex
	pending map[kissClient]*kissAckQueue
}

// newKissAcks makes a kissAcks whose goroutines stop when ctx is cancelled.
func newKissAcks(ctx context.Context) *kissAcks {
	var a = new(kissAcks)
	a.ctx = ctx
	a.pending = make(map[kissClient]*kissAckQueue)

	return a
}

// send has id, the ID of an ACKMODE frame to client for channel, acknowledged
// as soon as client will take it, after any acknowledgements still waiting
// for it.  It never waits for client itself.
func (a *kissAcks) send(client kissClient, channel int, id []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var queue, running = a.pending[client]
	if !running {
		queue = new(kissAckQueue)
		a.pending[client] = queue

		go a.drain(a.ctx, client)
	}

	if len(queue.acks) >= maxPendingKissAcks {
		// Said once, not for every frame: a client that has stopped reading
		// can go on sending, and drain says how many went once it catches up.
		if queue.dropped == 0 {
			logrus.WithFields(logrus.Fields{
				"channel": channel,
				"id":      hex.EncodeToString(id),
			}).Warn("KISS client isn't reading its ACKMODE acknowledgements.  Dropping them until it catches up.")
		}

		queue.dropped++

		return
	}

	queue.acks = append(queue.acks, kissAck{channel: channel, id: id})
}

// drain sends client what is waiting for it, one at a time, until there is
// nothing left or ctx is cancelled.
func (a *kissAcks) drain(ctx context.Context, client kissClient) {
	for {
		a.mu.Lock()

		var queue = a.pending[client]
		if len(queue.acks) == 0 || ctx.Err() != nil {
			// Said before the entry goes, so whoever sees it gone sees this too.
			if queue.dropped > 0 {
				logrus.WithField("dropped", queue.dropped).Warn("KISS client has caught up with its ACKMODE acknowledgements, but some were dropped.")
			}

			delete(a.pending, client)
			a.mu.Unlock()

			return
		}

		var next = queue.acks[0]
		queue.acks = queue.acks[1:]

		a.mu.Unlock()

		client.reply(next.channel, kiss.CmdXKissData, next.id)
	}
}
