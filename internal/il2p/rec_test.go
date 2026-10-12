// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A receiver hands each packet it decodes to the sink it was made with,
// rather than straight to the rest of the receive path, along with the audio
// level from the function it was made with.
func TestIL2PReceiverHandsPacketsToItsSink(t *testing.T) {
	t.Parallel()

	type delivery struct {
		from   dwcore.SlicerID
		pp     *ax25.Packet
		rxInfo dwcore.RxInfo
	}

	var got []delivery

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var audioLevel = func(channel int, subchannel int) ax25.ALevel {
		assert.Equal(t, 1, channel)
		assert.Equal(t, 2, subchannel)

		return alevel
	}

	var from = dwcore.SlicerID{Channel: 1, Subchannel: 2, Slice: 3}

	var rx = NewReceiver(from, VersionCompat, false, 0, audioLevel, func(from dwcore.SlicerID, pp *ax25.Packet, rxInfo dwcore.RxInfo) {
		got = append(got, delivery{from, pp, rxInfo})
	})

	var sender = NewSender(linecode.NewEncoder(rx.RecBit), 1, 0)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "Q2TEST"
	addrs[ax25.Source] = "Q1TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte("abcdefghijklmnop"))
	require.NotNil(t, pp)
	require.Positive(t, sender.SendFrame(pp, VersionCompat, 1, false, 0))

	rx.RecBit(0) // One more bit to see the last byte through.

	require.Len(t, got, 1)
	assert.Equal(t, from, got[0].from)
	assert.Equal(t, alevel, got[0].rxInfo.ALevel)
	assert.Equal(t, phy.FECIL2P, got[0].rxInfo.FEC)
	assert.Equal(t, pp.FrameData(), got[0].pp.FrameData())
}
