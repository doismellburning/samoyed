package direwolf

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
	return fcs_calc(data)
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

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_enabled
 *
 * Purpose:	Check if IL2P trailing CRC is enabled for a channel.
 *
 * Inputs:	channel	- Radio channel number.
 *
 * Returns:	true if CRC is enabled, false otherwise.
 *
 *--------------------------------------------------------------*/

func il2p_crc_enabled(channel int) bool {
	if save_audio_config_p == nil {
		return true // Default to enabled if no config available.
	}
	return save_audio_config_p.achan[channel].il2p_crc
}
