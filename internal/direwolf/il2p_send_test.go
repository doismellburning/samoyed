package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/il2p"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SendFrame appends IL2P's trailing CRC when the sender's own channel
// settings ask for it.
func TestIL2PSendFrameFollowsTheChannelsCRCSetting(t *testing.T) {
	il2p.Init(0)

	var pp = newHDLCSendTestPacket(t, 16)

	var _, lenWithoutCRC = il2p.EncodeFrame(pp, il2p.VersionCompat, 0, false)
	require.Positive(t, lenWithoutCRC)

	for _, crc := range []bool{false, true} {
		var audioConfig = newHDLCSendTestConfig(LAYER2_IL2P)
		audioConfig.achan[hdlcSendTestChannel].il2p_version = il2p.VersionCompat
		audioConfig.achan[hdlcSendTestChannel].il2p_crc = crc

		var bits = captureBits(t, audioConfig, func(s *HDLCSender) {
			s.SendFrame(pp, false)
		})

		var expected = 1 + il2p.SyncWordSize + lenWithoutCRC
		if crc {
			expected += il2p.CRCEncodedSize
		}

		assert.Len(t, packMSBFirst(t, bits), expected, "il2p_crc = %v", crc)
	}
}
