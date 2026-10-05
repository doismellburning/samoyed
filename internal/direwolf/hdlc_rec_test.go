// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hdlcRecTestDelivery is one frame an HDLC receiver handed to its sink.
type hdlcRecTestDelivery struct {
	channel, subchannel, slice int
	frame                      []byte
	alevel                     ax25.ALevel
	retries                    BitFixLevel
	fecType                    fec_type_t
}

// A receiver needs nothing but its config, its line decoder, and the two
// functions it is handed: it takes the audio level from one and gives each
// frame it extracts to the other.
func TestHDLCReceiverHandsFramesToItsSink(t *testing.T) {
	var got []hdlcRecTestDelivery

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var line linecode.Decoder

	var rx = newHDLCReceiver(hdlcConfig{fixBits: RETRY_NONE, passall: false, ais: false, sanityTest: SANITY_AX25}, 1, 2, 3, false, &line,
		func(channel int, subchannel int) ax25.ALevel {
			assert.Equal(t, 1, channel)
			assert.Equal(t, 2, subchannel)

			return alevel
		},
		func(channel int, subchannel int, slice int, frame []byte, alevel ax25.ALevel, retries BitFixLevel, fecType fec_type_t) {
			got = append(got, hdlcRecTestDelivery{channel, subchannel, slice, append([]byte{}, frame...), alevel, retries, fecType})
		})

	var pllNudgeTotal int64

	var pllSymbolCount int

	var sender = NewHDLCSender(linecode.NewEncoder(func(level int) {
		var raw = level != 0
		rx.recBit(raw, line.Decode(raw, false), false, &pllNudgeTotal, &pllSymbolCount)
	}), 1)

	var frame = newHDLCSendTestPacket(t, 16).Pack()

	sender.SendFlags(4)
	sender.SendFrame(frame, false)
	sender.SendFlags(2)

	require.Len(t, got, 1)
	assert.Equal(t, hdlcRecTestDelivery{1, 2, 3, frame, alevel, RETRY_NONE, fec_type_none}, got[0])
}
