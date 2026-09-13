// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestPacket builds the frame these tests queue.  The exact contents do not
// matter to the queue; what matters is that it is a real packet, as opposed to
// the null wake-up frame lm_seize_request queues.
func newTestPacket(t *testing.T) *packet_t {
	t.Helper()

	const (
		DEST   = "Q1TEST"
		SOURCE = "Q2TEST"
	)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = DEST
	addrs[AX25_SOURCE] = SOURCE

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_TEST, 0, 0, []byte("hello"))
	require.NotNil(t, pp)

	return pp
}

// TestTxQueueDepthAgreesWithQueueUnderLock is a regression test for the
// transmit queue depth gauge.  The depth used to be re-read via tq_count
// *after* tq_mutex was released, taking the lock a second time - so between an
// appender releasing the lock and re-taking it to count, any other holder of
// the lock could see a queue of n packets and a gauge still reporting n-1.
// Counting and publishing now happen in the same critical section, so anyone
// holding tq_mutex sees the two agree.
//
// The test hammers the lock while packets are being appended, which is what
// puts an observer inside that window; a final-state assertion alone does not
// find it, because by then every delayed publish has landed.
func TestTxQueueDepthAgreesWithQueueUnderLock(t *testing.T) {
	const (
		CHANNEL   = 0
		PRIO      = TQ_PRIO_1_LO
		NUM_SEND  = 8
		PER_SENDR = 64
		NUM_PKTS  = NUM_SEND * PER_SENDR
	)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	tq_init(audioConfig)

	var labels = map[string]string{
		"channel":  strconv.Itoa(CHANNEL),
		"priority": strconv.Itoa(PRIO),
	}

	// Build the packets up front: ax25_new increments an unsynchronised global
	// sequence counter, which is a separate matter from the queue and would
	// otherwise be the only thing this test found under -race.
	var packets = make([]*packet_t, 0, NUM_PKTS)

	for range NUM_PKTS {
		var pp = newTestPacket(t)

		packets = append(packets, pp)
	}

	var done atomic.Bool

	var observerDone = make(chan struct{})

	var sawCount, sawGauge atomic.Int64

	var disagreed atomic.Bool

	go func() {
		defer close(observerDone)

		for !done.Load() {
			tq_mutex.Lock()
			var count = tq_count_locked(CHANNEL, PRIO, "", "", false)
			var gauge, gatherErr = gatherMetricValue("samoyed_tx_queue_depth", labels)
			tq_mutex.Unlock()

			if gatherErr != nil {
				return
			}

			if float64(count) != gauge {
				sawCount.Store(int64(count))
				sawGauge.Store(int64(gauge))
				disagreed.Store(true)

				return
			}
		}
	}()

	var wg sync.WaitGroup

	for sender := range NUM_SEND {
		wg.Go(func() {
			for i := range PER_SENDR {
				lm_data_request(CHANNEL, PRIO, packets[sender*PER_SENDR+i])
			}
		})
	}

	wg.Wait()
	done.Store(true)
	<-observerDone

	assert.False(t, disagreed.Load(),
		"queue held %d packets but the gauge reported %d", sawCount.Load(), sawGauge.Load())

	var actual = tq_count(CHANNEL, PRIO, "", "", false)
	require.Equal(t, NUM_PKTS, actual, "all packets should be queued")

	assert.InDelta(t, float64(actual), metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"the gauge should describe the queue that actually exists")
}

// TestTxQueueDepthTracksDrain covers the other half of the maintained queue
// length: tq_remove decrements it.  The depth is published from a counter kept
// alongside the list rather than by walking it - tq_remove pops the head in
// constant time, and counting the list there would make draining a long queue
// quadratic under the one mutex every queue operation contends for - so the
// counter is state that can drift from the list it describes if a mutation
// site forgets to maintain it.  Each assertion cross-checks the published depth
// against an actual traversal, which is what catches drift.
func TestTxQueueDepthTracksDrain(t *testing.T) {
	const (
		CHANNEL  = 0
		PRIO     = TQ_PRIO_1_LO
		NUM_PKTS = 8
	)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	tq_init(audioConfig)

	var labels = map[string]string{
		"channel":  strconv.Itoa(CHANNEL),
		"priority": strconv.Itoa(PRIO),
	}

	for range NUM_PKTS {
		var pp = newTestPacket(t)

		lm_data_request(CHANNEL, PRIO, pp)
	}

	assert.InDelta(t, float64(NUM_PKTS), metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"all packets queued")

	for remaining := NUM_PKTS - 1; remaining >= 0; remaining-- {
		require.NotNil(t, tq_remove(CHANNEL, PRIO))

		assert.InDelta(t, float64(tq_count(CHANNEL, PRIO, "", "", false)),
			metricValue(t, "samoyed_tx_queue_depth", labels), 0,
			"published depth should match the queue after removing down to %d", remaining)
		assert.InDelta(t, float64(remaining), metricValue(t, "samoyed_tx_queue_depth", labels), 0,
			"depth should fall by one per removal")
	}

	// Removing from an empty queue must not take the depth negative.
	assert.Nil(t, tq_remove(CHANNEL, PRIO))
	assert.InDelta(t, 0, metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"an empty queue stays at zero")
}

// TestTxQueueDepthIgnoresSeizeMarker is a regression test: lm_seize_request
// queues a null frame to wake the transmitter, and tq_count deliberately does
// not count it as a packet.  The published depth is maintained separately from
// the list, so it has to make the same distinction - otherwise ordinary
// connected-mode acknowledgement reports a packet waiting when there is
// nothing to send, and the gauge shows a backlog that does not exist.
func TestTxQueueDepthIgnoresSeizeMarker(t *testing.T) {
	const (
		CHANNEL = 0
		PRIO    = TQ_PRIO_1_LO
	)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	tq_init(audioConfig)

	var labels = map[string]string{
		"channel":  strconv.Itoa(CHANNEL),
		"priority": strconv.Itoa(PRIO),
	}

	lm_seize_request(CHANNEL)

	require.Equal(t, 0, tq_count(CHANNEL, PRIO, "", "", false),
		"the wake-up marker is not a packet")
	assert.InDelta(t, 0, metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"nor should it be published as one")

	// A real packet behind it is still counted, and only it.
	var pp = newTestPacket(t)

	lm_data_request(CHANNEL, PRIO, pp)

	require.Equal(t, 1, tq_count(CHANNEL, PRIO, "", "", false))
	assert.InDelta(t, 1, metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"the real packet is counted, the marker still is not")

	// Draining the marker must not take the depth below the real packet count.
	require.NotNil(t, tq_remove(CHANNEL, PRIO))
	assert.InDelta(t, float64(tq_count(CHANNEL, PRIO, "", "", false)),
		metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"depth still matches the queue after the marker is taken")
}

// TestTxQueueDepthResetOnInit covers tq_init: it clears the queues, so the
// published depth has to be cleared with them.  A gauge left at its old value
// would describe a queue that no longer exists until the next enqueue.
func TestTxQueueDepthResetOnInit(t *testing.T) {
	const (
		CHANNEL = 0
		PRIO    = TQ_PRIO_1_LO
	)

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[CHANNEL] = MEDIUM_RADIO

	tq_init(audioConfig)

	var labels = map[string]string{
		"channel":  strconv.Itoa(CHANNEL),
		"priority": strconv.Itoa(PRIO),
	}

	var pp = newTestPacket(t)

	lm_data_request(CHANNEL, PRIO, pp)
	require.InDelta(t, 1, metricValue(t, "samoyed_tx_queue_depth", labels), 0)

	tq_init(audioConfig)

	assert.InDelta(t, 0, metricValue(t, "samoyed_tx_queue_depth", labels), 0,
		"re-initialising the queues must clear the published depth too")
}
