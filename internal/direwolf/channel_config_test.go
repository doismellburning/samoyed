// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Each channel is receive only exactly when canTransmit says that channel -
// not its device, nor any other channel - has nowhere to send audio.
func TestPttChannelsReceiveOnly(t *testing.T) {
	t.Parallel()

	var c = new(RadioConfig)
	c.chan_medium[0] = MEDIUM_RADIO
	c.chan_medium[1] = MEDIUM_RADIO
	c.chan_medium[2] = MEDIUM_RADIO

	var asked []int

	var channels = c.pttChannels(func(channel int) bool {
		asked = append(asked, channel)

		return channel == 1
	})

	assert.True(t, channels[0].ReceiveOnly)
	assert.False(t, channels[1].ReceiveOnly)
	assert.True(t, channels[2].ReceiveOnly)

	for ch := range MAX_RADIO_CHANS {
		assert.Contains(t, asked, ch, "every channel is asked about")
	}
}

// myCall is a channel's callsign, and none for a channel there can't be.
func TestMyCall(t *testing.T) {
	t.Parallel()

	var c = new(RadioConfig)
	c.mycall[1] = "Q1TEST-15"

	assert.Equal(t, "Q1TEST-15", c.myCall(1))
	assert.Empty(t, c.myCall(0))
	assert.Empty(t, c.myCall(-1))
	assert.Empty(t, c.myCall(MAX_TOTAL_CHANS))
}
