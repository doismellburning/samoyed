// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

//nolint:gochecknoglobals
package il2p

import (
	"slices"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/fcs"
)

/*-------------------------------------------------------------
 *
 * Purpose:	IL2P Trailing CRC-16-CCITT protected by (7,4) Hamming encoding.
 *
 * 		The CRC provides a final validity check after RS FEC decoding,
 *		catching rare cases where RS decoding silently produces
 *		incorrect data under extreme error conditions.
 *
 * Reference:	IL2P specification v0.6
 *
 *--------------------------------------------------------------*/

// Hamming (7,4) encode table from the IL2P spec.
// Maps 4-bit data nibble to 7-bit Hamming codeword.
var il2p_hamming_encode = [16]byte{
	0x00, 0x71, 0x62, 0x13, 0x54, 0x25, 0x36, 0x47,
	0x38, 0x49, 0x5a, 0x2b, 0x6c, 0x1d, 0x0e, 0x7f,
}

// Hamming (7,4) decode table from the IL2P spec.
// Maps 7-bit received codeword to 4-bit data nibble.
// Provides single-bit error correction.
var il2p_hamming_decode = [128]byte{
	0x00, 0x00, 0x00, 0x03, 0x00, 0x05, 0x0e, 0x07,
	0x00, 0x09, 0x0e, 0x0b, 0x0e, 0x0d, 0x0e, 0x0e,
	0x00, 0x03, 0x03, 0x03, 0x04, 0x0d, 0x06, 0x03,
	0x08, 0x0d, 0x0a, 0x03, 0x0d, 0x0d, 0x0e, 0x0d,
	0x00, 0x05, 0x02, 0x0b, 0x05, 0x05, 0x06, 0x05,
	0x08, 0x0b, 0x0b, 0x0b, 0x0c, 0x05, 0x0e, 0x0b,
	0x08, 0x01, 0x06, 0x03, 0x06, 0x05, 0x06, 0x06,
	0x08, 0x08, 0x08, 0x0b, 0x08, 0x0d, 0x06, 0x0f,
	0x00, 0x09, 0x02, 0x07, 0x04, 0x07, 0x07, 0x07,
	0x09, 0x09, 0x0a, 0x09, 0x0c, 0x09, 0x0e, 0x07,
	0x04, 0x01, 0x0a, 0x03, 0x04, 0x04, 0x04, 0x07,
	0x0a, 0x09, 0x0a, 0x0a, 0x04, 0x0d, 0x0a, 0x0f,
	0x02, 0x01, 0x02, 0x02, 0x0c, 0x05, 0x02, 0x07,
	0x0c, 0x09, 0x02, 0x0b, 0x0c, 0x0c, 0x0c, 0x0f,
	0x01, 0x01, 0x02, 0x01, 0x04, 0x01, 0x06, 0x0f,
	0x08, 0x01, 0x0a, 0x0f, 0x0c, 0x0f, 0x0f, 0x0f,
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_calc
 *
 * Purpose:	Compute CRC-16-CCITT over AX.25 frame data.
 *
 * Inputs:	data	- AX.25 frame bytes (without AX.25 FCS).
 *
 * Returns:	16-bit CRC value.
 *
 *--------------------------------------------------------------*/

func il2p_crc_calc(data []byte) uint16 {
	return fcs.Calc(data)
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_encode
 *
 * Purpose:	Hamming-encode a 16-bit CRC into 4 bytes.
 *
 * Inputs:	crc	- 16-bit CRC value.
 *
 * Returns:	4 bytes, each containing a Hamming (7,4) encoded nibble.
 *		High nibble of CRC is encoded first.
 *
 *--------------------------------------------------------------*/

func il2p_crc_encode(crc uint16) [IL2P_CRC_ENCODED_SIZE]byte {
	var encoded [IL2P_CRC_ENCODED_SIZE]byte
	encoded[0] = il2p_hamming_encode[(crc>>12)&0x0f]
	encoded[1] = il2p_hamming_encode[(crc>>8)&0x0f]
	encoded[2] = il2p_hamming_encode[(crc>>4)&0x0f]
	encoded[3] = il2p_hamming_encode[crc&0x0f]

	return encoded
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_decode
 *
 * Purpose:	Decode 4 Hamming-encoded bytes back to a 16-bit CRC.
 *
 * Inputs:	encoded	- 4 bytes of Hamming (7,4) encoded CRC.
 *
 * Returns:	16-bit CRC value.
 *
 *--------------------------------------------------------------*/

func il2p_crc_decode(encoded []byte) uint16 {
	var n0 = uint16(il2p_hamming_decode[encoded[0]&0x7f])
	var n1 = uint16(il2p_hamming_decode[encoded[1]&0x7f])
	var n2 = uint16(il2p_hamming_decode[encoded[2]&0x7f])
	var n3 = uint16(il2p_hamming_decode[encoded[3]&0x7f])

	return (n0 << 12) | (n1 << 8) | (n2 << 4) | n3
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_check
 *
 * Purpose:	Validate received Hamming-encoded CRC against AX.25 frame data.
 *
 * Inputs:	frame_data   - Decoded AX.25 frame bytes.
 *		encoded_crc  - 4 bytes of received Hamming-encoded CRC.
 *
 * Returns:	true if CRC matches, false otherwise.
 *
 *--------------------------------------------------------------*/

func il2p_crc_check(frame_data []byte, encoded_crc []byte) bool {
	var expected = il2p_crc_calc(frame_data)
	var received = il2p_crc_decode(encoded_crc)

	return expected == received
}

// il2pCRCPacket checks a received trailing CRC against pp, which was rebuilt
// from an IL2P header of type hdrType, and returns the packet the CRC vouches
// for, or nil if it vouches for none.
//
// The CRC covers the AX.25 frame as it was before IL2P translation.  A type 1
// header carries a single C bit, so a sender that uses one for a frame with
// AX.25 v1 C bits (both set or both clear) - as QtSoundModem does - sends a CRC
// that the rebuilt frame cannot match.  Rather than drop such a frame, try the
// CRC against each combination of the two C bits, and hand on the frame as it
// was sent.  That is three more chances for a frame Reed-Solomon decoding got
// wrong to match by accident, so about 4 in 65536 rather than 1.
func il2pCRCPacket(pp *ax25.Packet, hdrType int, encodedCRC []byte) *ax25.Packet {
	var frame = pp.FrameData()
	if il2p_crc_check(frame, encodedCRC) {
		return pp
	}

	// A type 1 header holds exactly a destination and a source.
	const destC, srcC = ax25.Destination*7 + 6, ax25.Source*7 + 6

	if hdrType != 1 || len(frame) <= srcC {
		return nil
	}

	var candidate = slices.Clone(frame)

	for _, bits := range [][2]byte{{ax25.SSIDHMask, ax25.SSIDHMask}, {0, 0}, {ax25.SSIDHMask, 0}, {0, ax25.SSIDHMask}} {
		candidate[destC] = frame[destC]&^ax25.SSIDHMask | bits[0]
		candidate[srcC] = frame[srcC]&^ax25.SSIDHMask | bits[1]

		if il2p_crc_check(candidate, encodedCRC) {
			var alevel ax25.ALevel

			return ax25.FromFrame(candidate, alevel)
		}
	}

	return nil
}
