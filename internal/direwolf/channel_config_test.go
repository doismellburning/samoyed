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
