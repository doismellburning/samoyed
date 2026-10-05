// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hdlc

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hdlcRecTestDelivery is one frame an HDLC receiver handed to its sink.
type hdlcRecTestDelivery struct {
	channel, subchannel, slice int
	frame                      []byte
	alevel                     ax25.ALevel
	retries                    phy.BitFixLevel
	fecType                    phy.FECType
}

// A receiver needs nothing but its config, its line decoder, and the two
// functions it is handed: it takes the audio level from one and gives each
// frame it extracts to the other.
func TestHDLCReceiverHandsFramesToItsSink(t *testing.T) {
	var got []hdlcRecTestDelivery

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var line linecode.Decoder

	var rx = NewReceiver(Config{FixBits: phy.BitFixNone, Passall: false, AIS: false, SanityTest: phy.SanityAX25}, 1, 2, 3, false, &line,
		func(channel int, subchannel int) ax25.ALevel {
			assert.Equal(t, 1, channel)
			assert.Equal(t, 2, subchannel)

			return alevel
		},
		func(channel int, subchannel int, slice int, frame []byte, alevel ax25.ALevel, retries phy.BitFixLevel, fecType phy.FECType) {
			got = append(got, hdlcRecTestDelivery{channel, subchannel, slice, append([]byte{}, frame...), alevel, retries, fecType})
		})

	var pllNudgeTotal int64

	var pllSymbolCount int

	var sender = NewSender(linecode.NewEncoder(func(level int) {
		var raw = level != 0
		rx.RecBit(raw, line.Decode(raw, false), false, &pllNudgeTotal, &pllSymbolCount)
	}), 1)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "Q2TEST"
	addrs[ax25.Source] = "Q1TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte("abcdefghijklmnop"))
	require.NotNil(t, pp)

	var frame = pp.Pack()

	sender.SendFlags(4)
	sender.SendFrame(frame, false)
	sender.SendFlags(2)

	require.Len(t, got, 1)
	assert.Equal(t, hdlcRecTestDelivery{1, 2, 3, frame, alevel, phy.BitFixNone, phy.FECNone}, got[0])
}
