// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
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
