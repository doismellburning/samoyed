// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kissTestRadioConfig is the channel table the KISS tests hand to
// the KissHandler, to check a transmit
// request against: channels 0 and 1 are radios, and nothing else is set up.
func kissTestRadioConfig() *RadioConfig {
	var audioConfig = new(RadioConfig)
	audioConfig.chan_medium[0] = MEDIUM_RADIO
	audioConfig.chan_medium[1] = MEDIUM_RADIO

	return audioConfig
}

// quietKissClient is a client application that drops whatever it is sent, for
// the tests that care only what the TNC makes of what it sends.
type quietKissClient struct{}

func (quietKissClient) Reply(int, int, []byte) {}

func (quietKissClient) Prompt([]byte) {}

func (quietKissClient) RadioChannel(frameChannel int) int { return frameChannel }

// handleKissMsg hands h msg, a KISS frame without its framing, as though from
// had sent it.
func handleKissMsg(h *KissHandler, msg []byte, from kissClient) {
	var kc kiss.Collector

	for _, b := range kiss.Encapsulate(msg) {
		h.RecByte(&kc, b, 0, from)
	}
}

// setupKissHandlerTNC hands back the TNC a KissHandler drives, over the
// channel table kissTestRadioConfig lays out, with transmit settings and a
// transmit queue of its own.
func setupKissHandlerTNC(t *testing.T) kissHandlerTNCAdapter {
	t.Helper()

	var audioConfig = kissTestRadioConfig()

	return newKissHandlerTNC(audioConfig, new(XmitService), newTestTransmitQueue(audioConfig))
}

// setupKissHandler hands back a KissHandler over the TNC setupKissHandlerTNC
// lays out, for the transports' tests to give their clients' bytes to.  ctx
// stops the handler's goroutines.
func setupKissHandler(ctx context.Context, t *testing.T) *KissHandler {
	t.Helper()

	return NewKissHandler(ctx, setupKissHandlerTNC(t), nil)
}

// An original goes on the low priority queue behind anything already being
// repeated, and an urgent frame jumps it.
func Test_kissHandlerTNC_transmit(t *testing.T) {
	t.Parallel()

	var tnc = setupKissHandlerTNC(t)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	tnc.Transmit(0, false, pp)

	assert.Equal(t, 1, tnc.queue.Count(0, TQ_PRIO_1_LO, "", "", false))
	assert.Equal(t, 0, tnc.queue.Count(0, TQ_PRIO_0_HI, "", "", false))

	var repeated = ax25.FromText("Q1TEST>Q2TEST,Q3TEST*:hello", true)
	require.NotNil(t, repeated)

	tnc.Transmit(1, true, repeated)

	assert.Equal(t, 1, tnc.queue.Count(1, TQ_PRIO_0_HI, "", "", false))
}

// What is waiting to go out is counted in bytes, for the TXBUF query.
func Test_kissHandlerTNC_queued_bytes(t *testing.T) {
	t.Parallel()

	var tnc = setupKissHandlerTNC(t)

	assert.Equal(t, 0, tnc.QueuedBytes(0))

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	tnc.Transmit(0, false, pp)

	assert.Equal(t, tnc.queue.Count(0, -1, "", "", true), tnc.QueuedBytes(0))
	assert.Positive(t, tnc.QueuedBytes(0), "a queued frame should count")
}

// With no transmit queue, a client's frames go nowhere, and TXBUF says
// nothing is waiting.
func Test_kissHandlerTNC_no_queue(t *testing.T) {
	t.Parallel()

	var tnc = newKissHandlerTNC(kissTestRadioConfig(), new(XmitService), nil)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello", true)
	require.NotNil(t, pp)

	assert.NotPanics(t, func() { tnc.Transmit(0, false, pp) })
	assert.Equal(t, 0, tnc.QueuedBytes(0))
}

// A channel is one a client can transmit on if it is in the channel table and
// set up as something.
func Test_kissHandlerTNC_channel_valid(t *testing.T) {
	t.Parallel()

	var tnc = setupKissHandlerTNC(t)

	assert.True(t, tnc.ChannelValid(0))
	assert.True(t, tnc.ChannelValid(1))
	assert.False(t, tnc.ChannelValid(2), "a channel that is not set up")
	assert.False(t, tnc.ChannelValid(-1))
	assert.False(t, tnc.ChannelValid(MAX_TOTAL_CHANS))
}

// The timing a client sets is the transmit timing.
func Test_kissHandlerTNC_timing(t *testing.T) {
	t.Parallel()

	var tnc = setupKissHandlerTNC(t)

	tnc.SetTxdelay(0, 30)
	tnc.SetFulldup(1, true)

	assert.Equal(t, 30, tnc.timing[0].txdelay)
	assert.True(t, tnc.timing[1].fulldup)
}
