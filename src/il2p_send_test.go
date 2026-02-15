package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIL2PSendFrameCRCDefaultMatchesEnabled verifies that il2p_send_frame uses
// the same CRC defaulting logic as il2p_crc_enabled: when save_audio_config_p
// is nil, CRC should be enabled (il2p_crc_enabled returns true), so the
// transmitted frame must include IL2P_CRC_ENCODED_SIZE extra bytes.
func TestIL2PSendFrameCRCDefaultMatchesEnabled(t *testing.T) {
	var origConfig = save_audio_config_p
	t.Cleanup(func() { save_audio_config_p = origConfig })
	save_audio_config_p = nil

	require.True(t, il2p_crc_enabled(0), "il2p_crc_enabled should default to true with nil config")

	IL2P_TEST = true
	t.Cleanup(func() { IL2P_TEST = false })
	il2p_init(0)

	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)
	t.Cleanup(func() { ax25_delete(pp) })

	// Compute expected bits: preamble(1B) + sync(3B) + encoded-with-CRC.
	var _, lenWithCRC = il2p_encode_frame(pp, 0, true)
	var expectedBits = (1 + IL2P_SYNC_WORD_SIZE + lenWithCRC) * 8

	var actual = il2p_send_frame(0, pp, 0, 0)
	assert.Equal(t, expectedBits, actual, "il2p_send_frame should append CRC when il2p_crc_enabled returns true")
}
