package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIL2PSendFrameCRCDefaultMatchesEnabled verifies that sendIL2PFrame uses
// the same CRC defaulting logic as il2p_crc_enabled: when save_audio_config_p
// is nil, CRC should be enabled (il2p_crc_enabled returns true), so the
// transmitted frame must include IL2P_CRC_ENCODED_SIZE extra bytes.
func TestIL2PSendFrameCRCDefaultMatchesEnabled(t *testing.T) {
	var origConfig = save_audio_config_p
	t.Cleanup(func() { save_audio_config_p = origConfig })
	save_audio_config_p = nil

	require.True(t, il2p_crc_enabled(0), "il2p_crc_enabled should default to true with nil config")

	// Only the bit count matters here, so throw the bits away rather than
	// looking for an audio device that isn't there.
	var savedCapture = toneGenCapture
	t.Cleanup(func() { toneGenCapture = savedCapture })

	toneGenCapture = func(_ int, _ int) {}

	il2p_init(0)

	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	// Compute expected bits: preamble(1B) + sync(3B) + encoded-with-CRC.
	var _, lenWithCRC = il2p_encode_frame(pp, IL2P_VERSION_COMPAT, 0, true)
	var expectedBits = (1 + IL2P_SYNC_WORD_SIZE + lenWithCRC) * 8

	var actual = NewHDLCSender(0, nil).sendIL2PFrame(pp, IL2P_VERSION_COMPAT, 0, 0)
	assert.Equal(t, expectedBits, actual, "sendIL2PFrame should append CRC when il2p_crc_enabled returns true")
}
