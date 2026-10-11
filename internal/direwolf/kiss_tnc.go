// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import "github.com/doismellburning/samoyed/internal/ax25"

// kissTNCAdapter is the TNC a KISS client application drives through a
// KissHandler: the transmit queue its data frames go on, the channel table
// they are checked against, and the transmit timing its commands change.
type kissTNCAdapter struct {
	*XmitService

	audioConfig *RadioConfig
	queue       clientTransmitQueue // Where a client's data frames go, and what TXBUF reports on; nil to drop them.
}

// newKissTNC hands a KissHandler the channels of audioConfig to transmit on,
// through queue, with the timing in xmit.  With no queue, a client's frames
// are dropped and TXBUF reports an empty queue.
func newKissTNC(audioConfig *RadioConfig, xmit *XmitService, queue clientTransmitQueue) kissTNCAdapter {
	return kissTNCAdapter{XmitService: xmit, audioConfig: audioConfig, queue: queue}
}

// Transmit queues pp for channel, on the high priority queue if it is urgent.
func (t kissTNCAdapter) Transmit(channel int, urgent bool, pp *ax25.Packet) {
	if t.queue == nil {
		return
	}

	if urgent {
		t.queue.Append(channel, TQ_PRIO_0_HI, pp)
	} else {
		t.queue.Append(channel, TQ_PRIO_1_LO, pp)
	}
}

// QueuedBytes is how many bytes of frames are waiting to go out on channel.
func (t kissTNCAdapter) QueuedBytes(channel int) int {
	if t.queue == nil {
		return 0
	}

	return t.queue.Count(channel, -1, "", "", true)
}

// ChannelValid reports whether channel is one we have, of any sort.
//
// Dire Wolf also excused MEDIUM_IGATE here, which could only ever matter for
// an out-of-range channel - and indexed chan_medium with it to find out.  An
// in-range IGate channel is not MEDIUM_NONE.
func (t kissTNCAdapter) ChannelValid(channel int) bool {
	return channel >= 0 && channel < MAX_TOTAL_CHANS && t.audioConfig.chan_medium[channel] != MEDIUM_NONE
}
