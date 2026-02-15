package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIL2PCRCHammingEncodeTable(t *testing.T) {
	// Verify some known values from the spec's Hamming (7,4) encode table.
	assert.Equal(t, byte(0x00), il2p_hamming_encode[0x0])
	assert.Equal(t, byte(0x71), il2p_hamming_encode[0x1])
	assert.Equal(t, byte(0x47), il2p_hamming_encode[0x7])
	assert.Equal(t, byte(0x7f), il2p_hamming_encode[0xF])
}

func TestIL2PCRCHammingDecodeTable(t *testing.T) {
	// Verify that decoding each valid Hamming codeword recovers the original nibble.
	for nibble := byte(0); nibble < 16; nibble++ {
		var codeword = il2p_hamming_encode[nibble]
		var decoded = il2p_hamming_decode[codeword&0x7f]
		assert.Equal(t, nibble, decoded, "Hamming round-trip failed for nibble %d", nibble)
	}
}

func TestIL2PCRCHammingSingleBitCorrection(t *testing.T) {
	// Verify single-bit error correction in the Hamming decode table.
	for nibble := byte(0); nibble < 16; nibble++ {
		var codeword = il2p_hamming_encode[nibble]
		// Flip each bit and verify correction.
		for bit := 0; bit < 7; bit++ {
			var corrupted = codeword ^ (1 << bit)
			var decoded = il2p_hamming_decode[corrupted&0x7f]
			assert.Equal(t, nibble, decoded,
				"Hamming correction failed for nibble %d, bit %d flipped", nibble, bit)
		}
	}
}

func TestIL2PCRCCalcSpec(t *testing.T) {
	il2p_init(0)

	var crc1 = il2p_crc_calc(il2pSpecSFrameAX25)
	assert.Equal(t, [4]byte{0x7F, 0x00, 0x1D, 0x2B}, il2p_crc_encode(crc1))

	var crc2 = il2p_crc_calc(il2pSpecUIFrameAX25)
	assert.Equal(t, [4]byte{0x47, 0x6C, 0x54, 0x54}, il2p_crc_encode(crc2))
}

func TestIL2PCRCEncodeDecode(t *testing.T) {
	// Round-trip: encode a CRC then decode it.
	var testCRCs = []uint16{0x0000, 0xFFFF, 0x1234, 0xABCD, 0xF0DB}
	for _, crc := range testCRCs {
		var encoded = il2p_crc_encode(crc)
		var decoded = il2p_crc_decode(encoded[:])
		assert.Equal(t, crc, decoded, "CRC round-trip failed for 0x%04X", crc)
	}
}

func TestIL2PCRCCheck(t *testing.T) {
	il2p_init(0)

	var crcBytes = il2pSpecSFrameIL2P[len(il2pSpecSFrameIL2P)-IL2P_CRC_ENCODED_SIZE:]
	assert.True(t, il2p_crc_check(il2pSpecSFrameAX25, crcBytes))

	var badCRCBytes = []byte{0x7F, 0x00, 0x1D, 0x00}
	assert.False(t, il2p_crc_check(il2pSpecSFrameAX25, badCRCBytes))
}

func TestIL2PCRCEncodeDecodeFrame(t *testing.T) {
	il2p_init(0)

	// Encode a frame with CRC, then decode it and verify round-trip.
	var addrs [AX25_MAX_ADDRS]string
	addrs[0] = "W2UB"
	addrs[1] = "WB2OSZ-12"
	var pinfo = []byte("Hello CRC test")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	for max_fec := 0; max_fec <= 1; max_fec++ {
		var encoded, enc_len = il2p_encode_frame(pp, max_fec, true)
		assert.Positive(t, enc_len)

		// Encoded should be 4 bytes longer than without CRC.
		var encodedNoCRC, enc_len_no_crc = il2p_encode_frame(pp, max_fec)
		assert.Equal(t, enc_len_no_crc+IL2P_CRC_ENCODED_SIZE, enc_len)
		_ = encodedNoCRC

		// Decode should succeed with CRC.
		var pp2 = il2p_decode_frame(encoded)
		require.NotNil(t, pp2, "Failed to decode frame with CRC, max_fec=%d", max_fec)

		assert.Equal(t, ax25_get_frame_data(pp), ax25_get_frame_data(pp2))
		ax25_delete(pp2)
	}
	ax25_delete(pp)
}

func TestIL2PCRCSpecExamplesEndToEnd(t *testing.T) {
	il2p_init(0)

	var testData = []struct {
		name     string
		il2pData []byte
		ax25Data []byte
	}{
		{"S-frame", il2pSpecSFrameIL2P, il2pSpecSFrameAX25},
		{"UI-frame", il2pSpecUIFrameIL2P, il2pSpecUIFrameAX25},
	}

	for _, td := range testData {
		t.Run(td.name, func(t *testing.T) {
			var pp = il2p_decode_frame(td.il2pData)
			require.NotNil(t, pp)

			assert.Equal(t, td.ax25Data, ax25_get_frame_data(pp))

			// Verify CRC matches.
			var crcBytes = td.il2pData[len(td.il2pData)-IL2P_CRC_ENCODED_SIZE:]
			assert.True(t, il2p_crc_check(ax25_get_frame_data(pp), crcBytes))

			ax25_delete(pp)
		})
	}
}
