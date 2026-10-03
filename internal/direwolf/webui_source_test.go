// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

func TestChannelDescription(t *testing.T) {
	var audio = new(AudioConfig)
	audio.chan_medium[0] = MEDIUM_RADIO
	audio.chan_medium[1] = MEDIUM_IGATE
	audio.chan_medium[2] = MEDIUM_NETTNC

	assert.Equal(t, maybe.Just("radio"), channelDescription(audio, 0))
	assert.Equal(t, maybe.Just("aprs-is"), channelDescription(audio, 1))
	assert.Equal(t, maybe.Just("network"), channelDescription(audio, 2))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, 3))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(audio, MAX_TOTAL_CHANS))
	assert.Equal(t, maybe.Nothing[string](), channelDescription(nil, 0))
}

func TestSubchanVia(t *testing.T) {
	assert.Equal(t, "radio", subchanVia(0))
	assert.Equal(t, "dtmf", subchanVia(-1))
	assert.Equal(t, "aprs-is", subchanVia(-2))
	assert.Equal(t, "network", subchanVia(-3))
}
