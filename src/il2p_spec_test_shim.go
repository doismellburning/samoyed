package direwolf

// Test examples found in the IL2P spec
// https://tarpn.net/t/il2p/il2p-specification_draft_v0-6.pdf

import "C"

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

func CGo_TestIL2PSpec(t *testing.T) {
	il2p_init(0)

	var testData = []struct {
		inputData     string
		expectedAddrs string
		ax25Data      string
	}{
		{
			inputData:     "26 57 4D 57 F1 D2 A8 F0 6A F2 7B AD 23 BD C0 7F 00 1D 2B",
			expectedAddrs: "KK4HEJ-7>KA2DEW-2:",
			ax25Data:      "96 82 64 88 8A AE E4 96 96 68 90 8A 94 6F 81",
		},
		{
			inputData:     "6A EA 9C C2 01 11 FC 14 1F DA 6E F2 53 91 BD 47 6C 54 54",
			expectedAddrs: "KK4HEJ-15>CQ:",
			ax25Data:      "86 A2 40 40 40 40 60 96 96 68 90 8A 94 FF 03 F0",
		},
		{
			inputData:     "26 13 6D 02 8C FE FB E8 AA 94 2D 6A 34 43 35 3C 69 9F 0C 75 5A 38 A1 7F A5 DA D8 F6 EA 57 37 3D B1 2A B0 DE 44 A8 20 D0 1D 5A 2B 38",
			expectedAddrs: "FIXME",
			ax25Data:      "",
		},
	}

	for _, testDatum := range testData {
		var b = il2pDataStringToBytes(testDatum.inputData)
		var pp = il2p_decode_frame((*C.uchar)(C.CBytes(b)))

		// Did we actually decode a frame?
		require.NotNil(t, pp)

		// Does it have the data we expect?
		assert.Equal(t, testDatum.expectedAddrs, ax25_format_addrs(pp))

		// Does it match the AX.25 data in the spec?
		assert.Equal(t, il2pDataStringToBytes(testDatum.ax25Data), ax25_pack(pp))
	}
}

func TestIL2PSpecDebug(t *testing.T) {
	il2p_init(0)

	var pp = ax25_i_frame(
		[AX25_MAX_ADDRS]string{"KA2DEW-2", "KK4HEJ-2"},
		2,
		1,
		8,
		5,
		4,
		1,
		0xCF,
		[]byte("012345678"),
	)

	require.NotNil(t, pp)

	// Building the packet by hand and comparing with the spec AX.25 bytes is fine:
	assert.Equal(t, il2pDataStringToBytes("96 82 64 88 8A AE E4 96 96 68 90 8A 94 65 B8 CF 30 31 32 33 34 35 36 37 38"), ax25_pack(pp))

	var b, errors = il2p_encode_frame(pp, 0)
	require.Equal(t, 0, errors)

	// Comparing with the spec IL2P bytes though, I get:
	// []byte{0x26, 0x13, 0x6d, 0x2, 0x8c, 0xfe, 0xfb, 0xe8, 0xaa, 0x94, 0x2d, 0x6a, 0x34, 0x43, 0x35, 0x3c, 0x69, 0x9f, 0x0c, 0x75, 0x5a, 0x38, 0xa1, 0x7f, 0xf3, 0xfc}
	// Looks like header etc. matches but body does not?
	assert.Equal(t, il2pDataStringToBytes("26 13 6D 02 8C FE FB E8 AA 94 2D 6A 34 43 35 3C 69 9F 0C 75 5A 38 A1 7F A5 DA D8 F6 EA 57 37 3D B1 2A B0 DE 44 A8 20 D0 1D 5A 2B 38"), b)

	// Decoding spec AX.25 bytes works fine, unsurprisingly
	var pp2 = ax25_from_frame(il2pDataStringToBytes("96 82 64 88 8A AE E4 96 96 68 90 8A 94 65 B8 CF 30 31 32 33 34 35 36 37 38"), alevel_t{})
	require.NotNil(t, pp2)
}
