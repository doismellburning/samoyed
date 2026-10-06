// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A receiver hands each packet it decodes to the sink it was made with,
// rather than straight to the rest of the receive path, along with the audio
// level from the function it was made with.
func TestIL2PReceiverHandsPacketsToItsSink(t *testing.T) {
	il2p_init(0)

	type delivery struct {
		channel, subchannel, slice int
		pp                         *ax25.Packet
		alevel                     ax25.ALevel
		fecType                    phy.FECType
	}

	var got []delivery

	var alevel = ax25.ALevel{Rec: 42, Mark: 41, Space: 43}

	var audioLevel = func(channel int, subchannel int) ax25.ALevel {
		assert.Equal(t, 1, channel)
		assert.Equal(t, 2, subchannel)

		return alevel
	}

	var rx = newIL2PReceiver(1, 2, 3, IL2P_VERSION_COMPAT, false, audioLevel, func(channel int, subchannel int, slice int, pp *ax25.Packet, alevel ax25.ALevel, _ phy.BitFixLevel, fecType phy.FECType) {
		got = append(got, delivery{channel, subchannel, slice, pp, alevel, fecType})
	})

	var sender = NewIL2PSender(linecode.NewEncoder(rx.recBit), 1)

	var pp = newHDLCSendTestPacket(t, 16)
	require.Positive(t, sender.SendFrame(pp, IL2P_VERSION_COMPAT, 1, false, 0))

	rx.recBit(0) // One more bit to see the last byte through.

	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].channel)
	assert.Equal(t, 2, got[0].subchannel)
	assert.Equal(t, 3, got[0].slice)
	assert.Equal(t, alevel, got[0].alevel)
	assert.Equal(t, phy.FECIL2P, got[0].fecType)
	assert.Equal(t, pp.FrameData(), got[0].pp.FrameData())
}
