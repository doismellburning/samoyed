package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIL2PDecodeFrameShortInputReturnsNil(t *testing.T) {
	il2p_init(0)
	// Input shorter than IL2P_HEADER_SIZE+IL2P_HEADER_PARITY must not panic.
	var pp = il2p_decode_frame([]byte{0x01, 0x02, 0x03})
	assert.Nil(t, pp)
}

func TestIL2PDecodeFrameHeaderFECFailureReturnsNil(t *testing.T) {
	il2p_init(0)
	// Two symbol errors exceed the correction capacity of the 2-parity header (e < 0).
	// il2p_decode_frame must return nil rather than proceeding with a corrupt header.
	var twoErrors = make([]byte, IL2P_HEADER_SIZE+IL2P_HEADER_PARITY)
	twoErrors[0] = 0x01
	twoErrors[1] = 0x01
	var pp = il2p_decode_frame(twoErrors)
	assert.Nil(t, pp)
}

func TestIL2PDecodeFrameTruncatedPayloadReturnsNil(t *testing.T) {
	il2p_init(0)

	// Build a real frame and encode it, then truncate the payload portion.
	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello world")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)
	t.Cleanup(func() { AX25Delete(pp) })

	var encoded, elen = il2p_encode_frame(pp, 0)
	require.Positive(t, elen)

	// Keep only the header bytes plus 1 byte of payload — far less than encoded_payload_size.
	var truncated = encoded[:IL2P_HEADER_SIZE+IL2P_HEADER_PARITY+1]
	var pp2 = il2p_decode_frame(truncated)
	assert.Nil(t, pp2)
}

func TestIL2PDecodeFrameJunkTrailingBytesReturnsNil(t *testing.T) {
	il2p_init(0)

	// A frame with 1–3 trailing bytes beyond encoded_payload_size is malformed:
	// not enough to be a CRC, not exactly the right payload length. Must return nil.
	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"
	var pinfo = []byte("hello world")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)
	t.Cleanup(func() { AX25Delete(pp) })

	var encoded, elen = il2p_encode_frame(pp, 0)
	require.Positive(t, elen)

	for junk := 1; junk < IL2P_CRC_ENCODED_SIZE; junk++ {
		var padded = append(encoded, make([]byte, junk)...)
		var pp2 = il2p_decode_frame(padded)
		assert.Nil(t, pp2, "expected nil for %d trailing junk byte(s)", junk)
	}
}
