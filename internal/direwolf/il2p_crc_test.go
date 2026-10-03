package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
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
	for nibble := range byte(16) {
		var codeword = il2p_hamming_encode[nibble]
		var decoded = il2p_hamming_decode[codeword&0x7f]
		assert.Equal(t, nibble, decoded, "Hamming round-trip failed for nibble %d", nibble)
	}
}

func TestIL2PCRCHammingSingleBitCorrection(t *testing.T) {
	// Verify single-bit error correction in the Hamming decode table.
	for nibble := range byte(16) {
		var codeword = il2p_hamming_encode[nibble]
		// Flip each bit and verify correction.
		for bit := range 7 {
			var corrupted = codeword ^ (1 << bit)
			var decoded = il2p_hamming_decode[corrupted&0x7f]
			assert.Equal(t, nibble, decoded,
				"Hamming correction failed for nibble %d, bit %d flipped", nibble, bit)
		}
	}
}

func TestIL2PCRCCalcSpec(t *testing.T) {
	il2p_init(0)

	// Example 1: S-frame AX.25 data → CRC should decode to match encoded bytes.
	var ex1AX25 = []byte{0x96, 0x82, 0x64, 0x88, 0x8A, 0xAE, 0xE4, 0x96, 0x96, 0x68, 0x90, 0x8A, 0x94, 0x6F, 0x81}
	var crc1 = il2p_crc_calc(ex1AX25)
	var encoded1 = il2p_crc_encode(crc1)
	assert.Equal(t, [4]byte{0x7F, 0x00, 0x1D, 0x2B}, encoded1)

	// Example 2: UI frame AX.25 data.
	var ex2AX25 = []byte{0x86, 0xA2, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0x96, 0x68, 0x90, 0x8A, 0x94, 0xFF, 0x03, 0xF0}
	var crc2 = il2p_crc_calc(ex2AX25)
	var encoded2 = il2p_crc_encode(crc2)
	assert.Equal(t, [4]byte{0x47, 0x6C, 0x54, 0x54}, encoded2)
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

	// Example 1: Verify CRC check passes.
	var ex1AX25 = []byte{0x96, 0x82, 0x64, 0x88, 0x8A, 0xAE, 0xE4, 0x96, 0x96, 0x68, 0x90, 0x8A, 0x94, 0x6F, 0x81}
	var ex1CRCBytes = []byte{0x7F, 0x00, 0x1D, 0x2B}
	assert.True(t, il2p_crc_check(ex1AX25, ex1CRCBytes))

	// Wrong CRC bytes should fail.
	var badCRCBytes = []byte{0x7F, 0x00, 0x1D, 0x00}
	assert.False(t, il2p_crc_check(ex1AX25, badCRCBytes))
}

func TestIL2PCRCEncodeDecodeFrame(t *testing.T) {
	il2p_init(0)

	// Encode a frame with CRC, then decode it and verify round-trip.
	var addrs [ax25.MaxAddrs]string
	addrs[0] = "W2UB"
	addrs[1] = "WB2OSZ-12"
	var pinfo = []byte("Hello CRC test")
	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, pinfo)
	require.NotNil(t, pp)

	for max_fec := range 2 {
		var encoded, enc_len = il2p_encode_frame(pp, IL2P_VERSION_0_4, max_fec, true)
		assert.Positive(t, enc_len)

		// Encoded should be 4 bytes longer than without CRC.
		var encodedNoCRC, enc_len_no_crc = il2p_encode_frame(pp, IL2P_VERSION_0_4, max_fec)
		assert.Equal(t, enc_len_no_crc+IL2P_CRC_ENCODED_SIZE, enc_len)
		_ = encodedNoCRC

		// Decode should succeed with CRC.
		var pp2 = il2p_decode_frame(encoded, IL2P_VERSION_0_4)
		require.NotNil(t, pp2, "Failed to decode frame with CRC, max_fec=%d", max_fec)

		assert.Equal(t, pp.FrameData(), pp2.FrameData())
	}
}

func TestIL2PCRCSpecExamplesEndToEnd(t *testing.T) {
	il2p_init(0)

	// Verify that the spec example IL2P data (which includes trailing CRC)
	// decodes correctly.
	var testData = []struct {
		name      string
		inputData string
		ax25Data  []byte
	}{
		{
			name:      "Example 1 S-frame",
			inputData: "26 57 4D 57 F1 D2 A8 F0 6A F2 7B AD 23 BD C0 7F 00 1D 2B",
			ax25Data:  []byte{0x96, 0x82, 0x64, 0x88, 0x8A, 0xAE, 0xE4, 0x96, 0x96, 0x68, 0x90, 0x8A, 0x94, 0x6F, 0x81},
		},
		{
			name:      "Example 2 UI frame",
			inputData: "6A EA 9C C2 01 11 FC 14 1F DA 6E F2 53 91 BD 47 6C 54 54",
			ax25Data:  []byte{0x86, 0xA2, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0x96, 0x68, 0x90, 0x8A, 0x94, 0xFF, 0x03, 0xF0},
		},
	}

	for _, td := range testData {
		t.Run(td.name, func(t *testing.T) {
			var b = il2pDataStringToBytes(td.inputData)
			var pp = il2p_decode_frame(b, IL2P_VERSION_COMPAT)
			require.NotNil(t, pp)

			var frameData = pp.FrameData()
			assert.Equal(t, td.ax25Data, frameData)

			// Verify CRC matches.
			var crc = il2p_crc_calc(frameData)
			var encodedCRC = il2p_crc_encode(crc)
			// The last 4 bytes of inputData should be the CRC.
			assert.Equal(t, encodedCRC[:], b[len(b)-IL2P_CRC_ENCODED_SIZE:])
		})
	}
}

// A type 1 header cannot represent every AX.25 frame exactly - AX.25 v1 C bits,
// with both set, come back as a command - and a trailing CRC computed over the
// original would then fail against what the receiver rebuilds.  FromText makes
// such a frame whenever there is no digipeater path.
func TestIL2PCRCLossyType1Header(t *testing.T) {
	il2p_init(0)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello world", true)
	require.NotNil(t, pp)

	var _, e = il2p_type_1_header(pp, 1)
	require.GreaterOrEqual(t, e, 0, "frame should fit a type 1 header")

	t.Run("codec", func(t *testing.T) {
		var encoded, n = il2p_encode_frame(pp, IL2P_VERSION_0_6, 1, true)
		require.Positive(t, n)

		var pp2 = il2p_decode_frame(encoded[:n], IL2P_VERSION_0_6)
		require.NotNil(t, pp2)
		assert.Equal(t, pp.FrameData(), pp2.FrameData())
	})

	t.Run("loopback", func(t *testing.T) {
		var recorder = il2pLoopback(t, IL2P_VERSION_0_6)

		require.Positive(t, NewHDLCSender(0, nil, nil, 0).sendIL2PFrame(pp, IL2P_VERSION_0_6, 1, true, 0))
		recorder.flush()

		var received = recorder.take()
		require.Len(t, received, 1)
		assert.Equal(t, []byte("hello world"), received[0].info)
	})
}

// The type 0 fallback is only for frames a type 1 header would change: one it
// represents exactly keeps the more compact header.
func TestIL2PCRCKeepsExactType1Header(t *testing.T) {
	il2p_init(0)

	var addrs [ax25.MaxAddrs]string
	addrs[0] = "Q1TEST"
	addrs[1] = "Q2TEST"

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, 0xF0, []byte("hello world"))
	require.NotNil(t, pp)

	var encoded, n = il2p_encode_frame(pp, IL2P_VERSION_0_6, 1, true)
	require.Positive(t, n)

	var uhdr, e = il2p_clarify_header(encoded[:IL2P_HEADER_SIZE+IL2P_HEADER_PARITY])
	require.GreaterOrEqual(t, e, 0)
	assert.Equal(t, 1, GET_HDR_TYPE(uhdr))
}

// Whether a receiver expects a trailing CRC is its own setting: nothing in the
// frame says whether one follows, so one that expects it hears only stations
// that send it, and one that doesn't hears everyone, without the check.
func TestIL2PCRCReceiveSetting(t *testing.T) {
	il2p_init(0)

	var pp = ax25.FromText("Q1TEST>Q2TEST:hello world", true)
	require.NotNil(t, pp)

	var testData = []struct {
		name     string
		txCRC    bool
		rxCRC    bool
		received bool
	}{
		{"CRC to CRC", true, true, true},
		{"CRC to no CRC", true, false, true},
		{"no CRC to no CRC", false, false, true},
		{"no CRC to CRC", false, true, false},
	}

	for _, testDatum := range testData {
		t.Run(testDatum.name, func(t *testing.T) {
			var recorder = il2pLoopback(t, IL2P_VERSION_0_6)
			recorder.rx = newIL2PReceiver(0, 0, 0, IL2P_VERSION_0_6, testDatum.rxCRC)

			require.Positive(t, NewHDLCSender(0, nil, nil, 0).sendIL2PFrame(pp, IL2P_VERSION_0_6, 1, testDatum.txCRC, 0))

			// Whatever follows the frame on the air, which a receiver expecting
			// a CRC takes for one.
			for range 64 {
				recorder.rx.recBit(0)
			}

			if testDatum.received {
				assert.Len(t, recorder.take(), 1)
			} else {
				assert.Empty(t, recorder.take())
			}
		})
	}
}
