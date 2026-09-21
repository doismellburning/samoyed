// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTqPeekUnderConcurrentAppend is a regression test for a data race.
// tq_peek used to read queue_head without holding tq_mutex, on the strength of
// a comment saying a critical region was not needed.  Every other reader and
// writer of that head pointer holds the mutex, and tq_peek runs on the
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

	tq_init(t.Context(), audioConfig)

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
			lm_data_request(CHANNEL, PRIO, pp)
		}
	})

	wg.Go(func() {
		for {
			select {
			case <-appendDone:
				return
			default:
			}

			var pp = tq_peek(CHANNEL, PRIO)
			if pp != nil && !queued[pp] {
				strayPeek.Store(true)

				return
			}
		}
	})

	wg.Wait()

	assert.False(t, strayPeek.Load(), "tq_peek returned a packet that was never queued")
	require.Equal(t, NUM_PKTS, tq_count(CHANNEL, PRIO, "", "", false),
		"every packet should still be queued: peeking must not consume")
}
