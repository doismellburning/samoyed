// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SendFrame appends IL2P's trailing CRC when the sender's own channel
// settings ask for it.
func TestIL2PSendFrameFollowsTheChannelsCRCSetting(t *testing.T) {
	il2p_init(0)

	var pp = newHDLCSendTestPacket(t, 16)

	var _, lenWithoutCRC = il2p_encode_frame(pp, IL2P_VERSION_COMPAT, 0, false)
	require.Positive(t, lenWithoutCRC)

	for _, crc := range []bool{false, true} {
		var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)
		audioConfig.achan[hdlcSendTestChannel].il2p_version = IL2P_VERSION_COMPAT
		audioConfig.achan[hdlcSendTestChannel].il2p_crc = crc

		var bits = captureBits(t, audioConfig, func(s *Layer2Sender) {
			s.SendFrame(pp, false)
		})

		var expected = 1 + IL2P_SYNC_WORD_SIZE + lenWithoutCRC
		if crc {
			expected += IL2P_CRC_ENCODED_SIZE
		}

		assert.Len(t, packMSBFirst(t, bits), expected, "il2p_crc = %v", crc)
	}
}

// IL2P goes out without NRZI, on the same line HDLC uses.  It must leave the
// NRZI level alone, or an HDLC frame sent after it would start from the wrong
// level and arrive with its first bit inverted.
func TestIL2PLeavesTheNRZILevelForTheNextHDLCFrame(t *testing.T) {
	il2p_init(0)

	// A stuffed zero leaves the line at 1 after this frame, where starting
	// the next frame afresh from 0 would show.
	var first = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T'}
	var second = []byte{'Q', '2', 'T', 'E', 'S', 'T'}

	var pp = newHDLCSendTestPacket(t, 16)

	var withoutIL2P = captureBits(t, nil, func(s *Layer2Sender) {
		s.hdlc.SendFrame(first, false)
		s.hdlc.SendFrame(second, false)
	})

	for _, polarity := range []int{0, 1} {
		var firstLen, il2pLen int

		var withIL2P = captureBits(t, nil, func(s *Layer2Sender) {
			firstLen = s.hdlc.SendFrame(first, false)
			il2pLen = s.il2p.SendFrame(pp, IL2P_VERSION_COMPAT, 0, false, polarity)
			s.hdlc.SendFrame(second, false)
		})

		require.Positive(t, il2pLen)
		require.Equal(t, 1, withIL2P[firstLen-1], "the first frame should leave the line at 1")

		var after = append(append([]int{}, withIL2P[:firstLen]...), withIL2P[firstLen+il2pLen:]...)

		assert.Equal(t, withoutIL2P, after, "polarity %d: the HDLC frames should be as if the IL2P frame had not been sent", polarity)
	}
}
