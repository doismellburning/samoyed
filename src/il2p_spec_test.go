package direwolf

// Test examples found in the IL2P spec
// https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Convenience function for turning example packets from the spec PDF into Go byte arrays to work with
// Example input: "26 57 4D 57 F1 D2 A8 F0 6A F2 7B AD 23 BD C0 7F 00 1D 2B"
func il2pDataStringToBytes(s string) []byte {
	var data, err = hex.DecodeString(strings.ReplaceAll(s, " ", ""))

	if err != nil {
		panic(err)
	}

	// From the spec PDF: "All IL2P data samples below include Trailing CRC and lack Sync Word"

	return data
}

// Spec example 1: AX.25 S-Frame (KK4HEJ-7 → KA2DEW-2)
var il2pSpecSFrameAX25 = il2pDataStringToBytes("96 82 64 88 8A AE E4 96 96 68 90 8A 94 6F 81")
var il2pSpecSFrameIL2P = il2pDataStringToBytes("26 57 4D 57 F1 D2 A8 F0 6A F2 7B AD 23 BD C0 7F 00 1D 2B")

// Spec example 2: AX.25 UI-Frame (KK4HEJ-15 → CQ)
var il2pSpecUIFrameAX25 = il2pDataStringToBytes("86 A2 40 40 40 40 60 96 96 68 90 8A 94 FF 03 F0")
var il2pSpecUIFrameIL2P = il2pDataStringToBytes("6A EA 9C C2 01 11 FC 14 1F DA 6E F2 53 91 BD 47 6C 54 54")

func TestIL2PSpec(t *testing.T) {
	il2p_init(0)

	var testData = []struct {
		il2pData      []byte
		expectedAddrs string
		ax25Data      []byte
	}{
		{
			il2pData:      il2pSpecSFrameIL2P,
			expectedAddrs: "KK4HEJ-7>KA2DEW-2:",
			ax25Data:      il2pSpecSFrameAX25,
		},
		{
			il2pData:      il2pSpecUIFrameIL2P,
			expectedAddrs: "KK4HEJ-15>CQ:",
			ax25Data:      il2pSpecUIFrameAX25,
		},
	}

	for _, testDatum := range testData {
		var pp = il2p_decode_frame(testDatum.il2pData)

		// Did we actually decode a frame?
		require.NotNil(t, pp)

		// Does it have the data we expect?
		assert.Equal(t, testDatum.expectedAddrs, ax25_format_addrs(pp))

		// Does it match the AX.25 data in the spec?
		assert.Equal(t, testDatum.ax25Data, ax25_pack(pp))

		// Verify the trailing CRC bytes are valid for the decoded frame.
		var frameData = ax25_get_frame_data(pp)
		var crcBytes = testDatum.il2pData[len(testDatum.il2pData)-IL2P_CRC_ENCODED_SIZE:]
		assert.True(t, il2p_crc_check(frameData, crcBytes),
			"Trailing CRC mismatch for %s", testDatum.expectedAddrs)
	}
}
