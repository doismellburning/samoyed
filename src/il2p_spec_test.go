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

// The three example packets from the v0.6 spec.  The I-frame is the one that
// shows the difference from v0.4: its 9 byte payload carries 16 parity symbols,
// where v0.4 would have used 2, even though the header bit that v0.4 reads as
// "max FEC" is clear.
//
//nolint:gochecknoglobals // Shared by the decode and encode tests below.
var il2pSpecExamples = []struct {
	name          string
	inputData     string
	expectedAddrs string
	ax25Data      string
}{
	{
		name:          "S-frame",
		inputData:     "26 57 4D 57 F1 D2 A8 F0 6A F2 7B AD 23 BD C0 7F 00 1D 2B",
		expectedAddrs: "KK4HEJ-7>KA2DEW-2:",
		ax25Data:      "96 82 64 88 8A AE E4 96 96 68 90 8A 94 6F 81",
	},
	{
		name:          "U-frame",
		inputData:     "6A EA 9C C2 01 11 FC 14 1F DA 6E F2 53 91 BD 47 6C 54 54",
		expectedAddrs: "KK4HEJ-15>CQ:",
		ax25Data:      "86 A2 40 40 40 40 60 96 96 68 90 8A 94 FF 03 F0",
	},
	{
		name:          "I-frame",
		inputData:     "26 13 6D 02 8C FE FB E8 AA 94 2D 6A 34 43 35 3C 69 9F 0C 75 5A 38 A1 7F A5 DA D8 F6 EA 57 37 3D B1 2A B0 DE 44 A8 20 D0 1D 5A 2B 38",
		expectedAddrs: "KK4HEJ-2>KA2DEW-2:",
		ax25Data:      "96 82 64 88 8A AE E4 96 96 68 90 8A 94 65 B8 CF 30 31 32 33 34 35 36 37 38",
	},
}

func TestIL2PSpec(t *testing.T) {
	il2p_init(0)

	for _, testDatum := range il2pSpecExamples {
		t.Run(testDatum.name, func(t *testing.T) {
			var b = il2pDataStringToBytes(testDatum.inputData)
			var pp = il2p_decode_frame(b, IL2P_VERSION_0_6)

			// Did we actually decode a frame?
			require.NotNil(t, pp)

			// Does it have the data we expect?
			assert.Equal(t, testDatum.expectedAddrs, AX25FormatAddrs(pp))

			// Does it match the AX.25 data in the spec?
			assert.Equal(t, il2pDataStringToBytes(testDatum.ax25Data), AX25Pack(pp))

			// Verify the trailing CRC bytes are valid for the decoded frame.
			var frameData = ax25_get_frame_data(pp)
			var crcBytes = b[len(b)-IL2P_CRC_ENCODED_SIZE:]
			assert.True(t, il2p_crc_check(frameData, crcBytes),
				"Trailing CRC mismatch for %s", testDatum.expectedAddrs)

			// The default version receives v0.6 too.
			assert.Equal(t, AX25Pack(pp), AX25Pack(il2p_decode_frame(b, IL2P_VERSION_COMPAT)))
		})
	}
}

// A station speaking v0.4 reads the payload blocks of a v0.6 frame as the
// wrong size.  It should come up empty handed rather than mistake the result
// for a frame it has decoded correctly.
func TestIL2PSpecExamplesRejectedAsV04(t *testing.T) {
	il2p_init(0)

	// Only the I-frame example has a payload, so only it can differ.
	var b = il2pDataStringToBytes(il2pSpecExamples[2].inputData)

	assert.Nil(t, il2p_decode_frame(b, IL2P_VERSION_0_4))
}

func TestIL2PSpecEncode(t *testing.T) {
	il2p_init(0)

	for _, testDatum := range il2pSpecExamples {
		t.Run(testDatum.name, func(t *testing.T) {
			var alevel ALevel
			var pp = AX25FromFrame(il2pDataStringToBytes(testDatum.ax25Data), alevel)
			require.NotNil(t, pp)

			var encoded, elen = il2p_encode_frame(pp, IL2P_VERSION_0_6, 0, true)
			require.Positive(t, elen)

			assert.Equal(t, il2pDataStringToBytes(testDatum.inputData), encoded)
		})
	}
}
