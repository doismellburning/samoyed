// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTqPeekUnderConcurrentAppend is a regression test for a data race.
// TransmitQueue.Peek used to read the queue's head without holding the queue's mutex, on the strength of
// a comment saying a critical region was not needed.  Every other reader and
// writer of that head pointer holds the mutex, and TransmitQueue.Peek runs on the
// transmit thread while producers append from the KISS, AGW, beacon and
// digipeater goroutines, so the unguarded read raced with every enqueue.
//
// The race detector is the oracle here: a racing pointer read does not tear on
// any platform we support, so there is no wrong value for a final-state
// assertion to catch.  What this test contributes is putting the two accesses
// in flight at the same time, so `make race` can see the conflict at all -
// which it previously never did, because nothing in the suite peeked while
// another goroutine was appending.
func TestTqPeekUnderConcurrentAppend(t *testing.T) {
	const (
		CHANNEL  = 0
		PRIO     = TQ_PRIO_1_LO
		NUM_PKTS = 512
	)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)

	// Built up front: ax25_new increments an unsynchronised global sequence
	// counter, which is a separate matter from the queue and would otherwise
	// be what -race reported instead of the race under test.
	//
	// queued is written only here, before either goroutine starts, so the
	// peeker may read it freely.
	var packets = make([]*packet_t, 0, NUM_PKTS)

	var queued = make(map[*packet_t]bool, NUM_PKTS)

	for range NUM_PKTS {
		var pp = newTestPacket(t)

		packets = append(packets, pp)
		queued[pp] = true
	}

	var appendDone = make(chan struct{})

	var strayPeek atomic.Bool

	var wg sync.WaitGroup

	wg.Go(func() {
		defer close(appendDone)

		for _, pp := range packets {
			transmitQueue.LMDataRequest(CHANNEL, PRIO, pp)
		}
	})

	wg.Go(func() {
		for {
			select {
			case <-appendDone:
				return
			default:
			}

			var pp = transmitQueue.Peek(CHANNEL, PRIO)
			if pp != nil && !queued[pp] {
				strayPeek.Store(true)

				return
			}
		}
	})

	wg.Wait()

	assert.False(t, strayPeek.Load(), "tq_peek returned a packet that was never queued")
	require.Equal(t, NUM_PKTS, transmitQueue.Count(CHANNEL, PRIO, "", "", false),
		"every packet should still be queued: peeking must not consume")
}

// TestTqWaitWhileEmptyDoesNotMissAnAppend is a regression test for a lost
// wake-up.  TransmitQueue.WaitWhileEmpty decided whether to wait under the queue's mutex and
// then waited on a different mutex, while the enqueue paths signalled only if
// a flag said the transmit thread was already waiting.  A packet queued
// between that decision and the wait itself found the flag still false, so no
// signal was sent and the transmit thread settled down to sleep on a queue
// that was no longer empty.  Nothing woke it until the next enqueue on that
// channel happened to signal - on a quiet channel, an arbitrarily delayed
// transmission.
//
// A wake-up now latches rather than being dropped when nobody is yet
// listening, so a correct implementation passes every round and there is no
// timing assumption left to flake on.  Each round runs a waiter and its
// producer as a separate pair of goroutines on every channel at once, so the
// two are genuinely in flight together.
//
// What this reliably catches is the unsynchronised read of the waiting flag,
// and only under -race: measured against the old implementation it failed 8
// runs out of 8 with the detector on, and 0 out of 8 with it off.  The lost
// wake-up itself is a window a few instructions wide and is not hit often
// enough to test for directly - the same change fixes both, so the detector
// standing guard over the flag is what keeps them both from coming back.
// That makes this a make race test in practice rather than a make test one.
func TestTqWaitWhileEmptyDoesNotMissAnAppend(t *testing.T) {
	const (
		PRIO      = TQ_PRIO_1_LO
		CHANNELS  = MAX_RADIO_CHANS
		ROUNDS    = 128
		WAKE_WAIT = 5 * time.Second
	)

	var audioConfig = new(audio_s)
	for c := range CHANNELS {
		audioConfig.chan_medium[c] = MEDIUM_RADIO
	}

	transmitQueue.Init(audioConfig)

	// Built up front: ax25_new increments an unsynchronised global sequence
	// counter, which is a separate matter from the queue and would otherwise
	// be what -race reported.
	var packets = make([][]*packet_t, CHANNELS)
	for c := range CHANNELS {
		packets[c] = make([]*packet_t, 0, ROUNDS)

		for range ROUNDS {
			packets[c] = append(packets[c], newTestPacket(t))
		}
	}

	for round := range ROUNDS {
		for c := range CHANNELS {
			require.Equal(t, 0, transmitQueue.Count(c, -1, "", "", false),
				"round %d channel %d should start with an empty queue", round, c)
		}

		// Release every goroutine at once, so the waiters are deciding whether
		// to wait while the producers are queueing.
		var start = make(chan struct{})

		var woke = make([]chan struct{}, CHANNELS)

		var wg sync.WaitGroup

		for c := range CHANNELS {
			woke[c] = make(chan struct{})

			go func() {
				defer close(woke[c])

				<-start

				transmitQueue.WaitWhileEmpty(t.Context(), c)
			}()

			wg.Go(func() {
				<-start

				transmitQueue.LMDataRequest(c, PRIO, packets[c][round])
			})
		}

		close(start)
		wg.Wait()

		for c := range CHANNELS {
			select {
			case <-woke[c]:
			case <-time.After(WAKE_WAIT):
				t.Fatalf("round %d channel %d: transmit thread still asleep %v after a packet was queued",
					round, c, WAKE_WAIT)
			}

			require.NotNil(t, transmitQueue.Remove(c, PRIO),
				"round %d channel %d: the queued packet should still be there", round, c)
		}
	}
}

// TestTqWaitWhileEmptyReturnsWhenCancelled covers the other way out of the
// wait.  A transmit thread parked on an empty queue has to come back when its
// context is cancelled, or shutdown blocks on a packet that is never coming.
// This used to need a separate broadcast registered on the context, because
// nothing else could reach a thread sitting in a condition variable wait.
func TestTqWaitWhileEmptyReturnsWhenCancelled(t *testing.T) {
	const CHANNEL = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)

	var ctx, cancel = context.WithCancel(t.Context())

	var returned = make(chan struct{})

	go func() {
		defer close(returned)

		transmitQueue.WaitWhileEmpty(ctx, CHANNEL)
	}()

	cancel()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("tq_wait_while_empty did not return when its context was cancelled")
	}

	assert.Equal(t, 0, transmitQueue.Count(CHANNEL, -1, "", "", false),
		"cancellation should not have invented a packet")
}

// TestTqAppendOutOfRangeChannel is a regression test for TransmitQueue.Append indexing
// chan_medium, to see whether the channel belongs to the IGate or a network
// TNC, before it had checked the channel was in range at all - so the request
// the bounds check exists to reject panicked before reaching it.
func TestTqAppendOutOfRangeChannel(t *testing.T) {
	var audioConfig = new(audio_s)
	audioConfig.chan_medium[0] = MEDIUM_RADIO

	transmitQueue.Init(audioConfig)

	for _, channel := range []int{-1, MAX_TOTAL_CHANS} {
		assert.NotPanics(t, func() {
			transmitQueue.Append(channel, TQ_PRIO_1_LO, newTestPacket(t))
		}, "channel %d", channel)
	}

	assert.Equal(t, 0, transmitQueue.Count(0, -1, "", "", false),
		"an out-of-range request should not have landed on a real channel")
}
