// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// wait_for_clear_channel locks the audio output device on success, and it is
// xmit_next's job to release it again.  An empty queue means there is nothing
// to send after all, which is one of the paths where that release used to be
// skipped, leaving the device locked forever and every later transmission on
// it blocked.
func TestXmitNextReleasesAudioOutDevWhenQueueIsEmpty(t *testing.T) {
	var channel = 0

	var xs = new(XmitService)
	xs.fulldup[channel] = true // Skip the channel-busy check and random wait.

	// The transmit queues are package globals shared with every other test
	// here, some of which leave entries behind, so empty this channel's rather
	// than assume they already are.
	for _, prio := range []int{TQ_PRIO_0_HI, TQ_PRIO_1_LO} {
		for tq_remove(channel, prio) != nil {
		}
	}

	xs.xmit_next(channel)

	if !xs.audioOutDevMutex[ACHAN2ADEV(channel)].TryLock() {
		t.Fatal("Audio output device is still locked after xmit_next found nothing to send")
	}

	xs.audioOutDevMutex[ACHAN2ADEV(channel)].Unlock()
}

// A channel whose audio device has no output must not transmit at all: keying
// PTT to play samples that go nowhere would put an unmodulated carrier on the
// air, and mute the receiver for the duration of a half-duplex transmission.
// Frames queued for such a channel are thrown away instead.
func TestDiscardUntransmittableEmptiesTheQueue(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	tq_init(audioConfig)

	var xs = new(XmitService)

	tq_append(channel, TQ_PRIO_1_LO, newTestPacket(t))
	tq_append(channel, TQ_PRIO_0_HI, newTestPacket(t))

	xs.discard_untransmittable(channel)

	assert.Nil(t, tq_peek(channel, TQ_PRIO_0_HI))
	assert.Nil(t, tq_peek(channel, TQ_PRIO_1_LO))

	// The explanation is printed once, however many frames are discarded.
	assert.True(t, xs.saidCannotTransmit[channel])
}

// The null frame lm_seize_request queues is a request for a transmission
// opportunity rather than something to send, and send_one_frame answers it
// with a seize confirm.  Discarding it silently would leave a connected mode
// session waiting for a confirmation that never comes, so the discard path
// answers it too.
func TestDiscardUntransmittableAnswersSeizeRequest(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	tq_init(audioConfig)
	dlq_init()

	var xs = new(XmitService)

	tq_append(channel, TQ_PRIO_1_LO, ax25_new()) // What lm_seize_request queues.
	tq_append(channel, TQ_PRIO_1_LO, newTestPacket(t))

	xs.discard_untransmittable(channel)

	assert.Nil(t, tq_peek(channel, TQ_PRIO_1_LO))

	var confirmed = false

	for item := dlq_remove(); item != nil; item = dlq_remove() {
		if item._type == DLQ_SEIZE_CONFIRM && item._chan == channel {
			confirmed = true
		}
	}

	assert.True(t, confirmed, "Expected a seize confirm for the discarded null frame")
}

// The scheduler itself, not just the discard helper, has to keep a channel
// with no transmit device away from xmit_next: that is what stops PTT being
// keyed for audio that goes nowhere.  Removing the guard in xmit_until_empty
// makes this test transmit instead of discard, and saidCannotTransmit stays
// false.
func TestXmitUntilEmptyDiscardsWithNoTransmitDevice(t *testing.T) {
	var channel = 0

	var audioConfig = new(audio_s)
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	tq_init(audioConfig)

	var xs = new(XmitService)
	xs.audioOutAvailable[ACHAN2ADEV(channel)] = false

	tq_append(channel, TQ_PRIO_1_LO, newTestPacket(t))
	tq_append(channel, TQ_PRIO_0_HI, newTestPacket(t))

	xs.xmit_until_empty(channel)

	assert.Nil(t, tq_peek(channel, TQ_PRIO_0_HI))
	assert.Nil(t, tq_peek(channel, TQ_PRIO_1_LO))
	assert.True(t, xs.saidCannotTransmit[channel], "Expected the frames to go down the discard path, not the transmit path")

	// The audio output device is never seized on the way, so nothing is left
	// holding its lock.
	if !xs.audioOutDevMutex[ACHAN2ADEV(channel)].TryLock() {
		t.Fatal("Audio output device was locked by a channel that cannot transmit")
	}

	xs.audioOutDevMutex[ACHAN2ADEV(channel)].Unlock()
}
