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
