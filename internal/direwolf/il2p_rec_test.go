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

// A receiver hands each packet it decodes to the sink it was made with,
// rather than straight to the rest of the receive path.
func TestIL2PReceiverHandsPacketsToItsSink(t *testing.T) {
	il2p_init(0)

	type delivery struct {
		channel, subchannel, slice int
		pp                         *ax25.Packet
		fecType                    fec_type_t
	}

	var got []delivery

	var rx = newIL2PReceiver(1, 2, 3, IL2P_VERSION_COMPAT, false, func(channel int, subchannel int, slice int, pp *ax25.Packet, _ BitFixLevel, fecType fec_type_t) {
		got = append(got, delivery{channel, subchannel, slice, pp, fecType})
	})

	var sender = NewIL2PSender(linecode.NewEncoder(rx.recBit), 1)

	var pp = newHDLCSendTestPacket(t, 16)
	require.Positive(t, sender.SendFrame(pp, IL2P_VERSION_COMPAT, 1, false, 0))

	rx.recBit(0) // One more bit to see the last byte through.

	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].channel)
	assert.Equal(t, 2, got[0].subchannel)
	assert.Equal(t, 3, got[0].slice)
	assert.Equal(t, fec_type_il2p, got[0].fecType)
	assert.Equal(t, pp.FrameData(), got[0].pp.FrameData())
}
