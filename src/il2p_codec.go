package direwolf

import (
	"bytes"
)

/*-------------------------------------------------------------
 *
 * File:	il2p_codec.c
 *
 * Purpose:	Convert IL2P encoded format from and to direwolf internal packet format.
 *
 *--------------------------------------------------------------*/

/*-------------------------------------------------------------
 *
 * Name:	il2p_encode_frame
 *
 * Purpose:	Convert AX.25 frame to IL2P encoding.
 *
 * Inputs:
 *
 *		pp	- Packet object pointer.
 *
 *		max_fec	- 1 to send maximum FEC size rather than automatic.
 *
 * Outputs:	iout	- Encoded result, excluding the 3 byte sync word.
 *			  Caller should provide  IL2P_MAX_PACKET_SIZE  bytes.
 *
 * Returns:	Number of bytes for transmission.
 *		-1 is returned for failure.
 *
 * Description:	Encode into IL2P format.
 *
 * Errors:	If something goes wrong, return -1.
 *
 *		Most likely reason is that the frame is too large.
 *		IL2P has a max payload size of 1023 bytes.
 *		For a type 1 header, this is the maximum AX.25 Information part size.
 *		For a type 0 header, this is the entire AX.25 frame.
 *
 *--------------------------------------------------------------*/

func il2p_encode_frame(pp *packet_t, max_fec int, crc ...bool) ([]byte, int) {

	var appendCRC = len(crc) > 0 && crc[0]

	// Can a type 1 header be used?

	var hdr, e = il2p_type_1_header(pp, max_fec)

	if e >= 0 {
		var outbuf = new(bytes.Buffer)

		var scrambled = il2p_scramble_block(hdr)
		outbuf.Write(scrambled)

		var parity = il2p_encode_rs(scrambled, IL2P_HEADER_PARITY)
		outbuf.Write(parity)

		if e == 0 {
			// Success. No info part.
			if appendCRC {
				// Use the decoded-header frame data for CRC, so that the receiver (which
				// reconstructs the packet from the IL2P header) computes the same CRC.
				// IL2P type-1 encoding normalises certain AX.25 fields (e.g. C/R bits),
				// so the decoded frame may differ from the original.
				var normalized = il2p_decode_header_type_1(hdr, 0)
				if normalized != nil {
					var crcBytes = il2p_crc_encode(il2p_crc_calc(ax25_get_frame_data(normalized)))
					outbuf.Write(crcBytes[:])
					ax25_delete(normalized)
				}
			}
			return outbuf.Bytes(), outbuf.Len()
		}

		// Payload is AX.25 info part.
		var pinfo = ax25_get_info(pp)

		var encodedPayload, k = il2p_encode_payload(pinfo, max_fec)
		if k > 0 {
			outbuf.Write(encodedPayload)

			if appendCRC {
				// Use the decoded-header frame data for CRC (see comment above).
				var normalized = il2p_decode_header_type_1(hdr, 0)
				if normalized != nil {
					ax25_set_info(normalized, pinfo)
					var crcBytes = il2p_crc_encode(il2p_crc_calc(ax25_get_frame_data(normalized)))
					outbuf.Write(crcBytes[:])
					ax25_delete(normalized)
				}
			}

			// Success. Info part was <= 1023 bytes.
			return outbuf.Bytes(), outbuf.Len()
		}

		// Something went wrong with the payload encoding.
		return nil, -1
	} else if e == -1 {

		// Could not use type 1 header for some reason.
		// e.g. More than 2 addresses, extended (mod 128) sequence numbers, etc.

		hdr, e = il2p_type_0_header(pp, max_fec)
		if e > 0 {
			var outbuf = new(bytes.Buffer)

			var scrambled = il2p_scramble_block(hdr)
			outbuf.Write(scrambled)

			var parity = il2p_encode_rs(scrambled, IL2P_HEADER_PARITY)
			outbuf.Write(parity)

			// Payload is entire AX.25 frame.

			var frame_data = ax25_get_frame_data(pp)
			var encodedPayload, k = il2p_encode_payload(frame_data, max_fec)
			if k > 0 {
				outbuf.Write(encodedPayload)

				if appendCRC {
					var crcBytes = il2p_crc_encode(il2p_crc_calc(frame_data))
					outbuf.Write(crcBytes[:])
				}

				// Success. Entire AX.25 frame <= 1023 bytes.
				return outbuf.Bytes(), outbuf.Len()
			}
			// Something went wrong with the payload encoding.
			return nil, -1
		} else if e == 0 { //nolint:gocritic
			// Impossible condition.  Type 0 header must have payload.
			return nil, -1
		} else {
			// AX.25 frame is too large.
			return nil, -1
		}
	}

	// AX.25 Information part is too large.
	return nil, -1
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_decode_frame
 *
 * Purpose:	Convert IL2P encoding to AX.25 frame.
 *		This is only used during testing, with a whole encoded frame.
 *		During reception, the header would have FEC and descrambling
 *		applied first so we would know how much to collect for the payload.
 *
 * Inputs:	irec	- Received IL2P frame excluding the 3 byte sync word.
 *
 * Future Out:	Number of symbols corrected.
 *
 * Returns:	Packet pointer or nil for error.
 *
 *--------------------------------------------------------------*/

func il2p_decode_frame(irec []byte) *packet_t {
	var uhdr, e = il2p_clarify_header(irec[:IL2P_HEADER_SIZE+IL2P_HEADER_PARITY])

	// TODO?: for symmetry we might want to clarify the payload before combining.

	var payload = irec[IL2P_HEADER_SIZE+IL2P_HEADER_PARITY:]

	// Determine if trailing CRC is present by computing the encoded payload size
	// and checking if there are extra bytes beyond it.
	var _, max_fec, payload_len = il2p_get_header_attributes(uhdr)
	var _, encoded_payload_size = il2p_payload_compute(payload_len, max_fec)

	var crc_bytes []byte
	if len(payload) >= encoded_payload_size+IL2P_CRC_ENCODED_SIZE {
		crc_bytes = payload[encoded_payload_size : encoded_payload_size+IL2P_CRC_ENCODED_SIZE]
		payload = payload[:encoded_payload_size]
	}

	var pp = il2p_decode_header_payload(uhdr, payload, &e)

	// Validate CRC if present.
	if pp != nil && crc_bytes != nil {
		var frame_data = ax25_get_frame_data(pp)
		if !il2p_crc_check(frame_data, crc_bytes) {
			if il2p_get_debug() >= 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("IL2P trailing CRC mismatch.\n")
			}
			ax25_delete(pp)
			return nil
		}
	}

	return pp
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_decode_header_payload
 *
 * Purpose:	Convert IL2P encoding to AX.25 frame
 *
 * Inputs:	uhdr 		- Received header after FEC and descrambling.
 *		epayload	- Encoded payload.
 *
 * In/Out:	symbols_corrected - Symbols (bytes) corrected in the header.
 *				  Should be 0 or 1 because it has 2 parity symbols.
 *				  Here we add number of corrections for the payload.
 *
 * Returns:	Packet pointer or nil for error.
 *
 *--------------------------------------------------------------*/

func il2p_decode_header_payload(uhdr []byte, epayload []byte, symbols_corrected *int) *packet_t {
	var hdr_type, max_fec, payload_len = il2p_get_header_attributes(uhdr)

	if hdr_type == 1 {

		// Header type 1.  Any payload is the AX.25 Information part.

		var pp = il2p_decode_header_type_1(uhdr, *symbols_corrected)
		if pp == nil {
			// Failed for some reason.
			return (nil)
		}

		if payload_len > 0 {
			// This is the AX.25 Information part.

			var extracted, e = il2p_decode_payload(epayload, payload_len, max_fec, symbols_corrected)

			// It would be possible to have a good header but too many errors in the payload.

			if e <= 0 {
				ax25_delete(pp)
				return nil
			}

			if e != payload_len {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("IL2P Internal Error: il2p_decode_header_payload(): hdr_type=%d, max_fec=%d, payload_len=%d, e=%d.\n", hdr_type, max_fec, payload_len, e)
			}

			ax25_set_info(pp, extracted)
		}
		return (pp)
	} else {

		// Header type 0.  The payload is the entire AX.25 frame.

		var extracted, e = il2p_decode_payload(epayload, payload_len, max_fec, symbols_corrected)

		if e <= 0 { // Payload was not received correctly.
			return (nil)
		}

		if e != payload_len {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("IL2P Internal Error: il2p_decode_header_payload(): hdr_type=%d, e=%d, payload_len=%d\n", hdr_type, e, payload_len)
			return (nil)
		}

		var alevel alevel_t
		//alevel = demod_get_audio_level (chan, subchan); 	// What TODO? We don't know channel here.
		// I think alevel gets filled in somewhere later making
		// this redundant.

		var pp = ax25_from_frame(extracted, alevel)
		return (pp)
	}

} // end il2p_decode_header_payload

/*-------------------------------------------------------------
 *
 * Name:	il2p_reconstruct_packet
 *
 * Purpose:	Build an AX.25 packet from a clarified header and an
 *		already-decoded (descrambled) payload slice.
 *
 * Inputs:	uhdr		Header after FEC and descrambling.
 *		payload		Decoded payload bytes.
 *		symbols_corrected - Header correction count (passed through).
 *
 * Returns:	Packet pointer or nil for error.
 *
 *--------------------------------------------------------------*/

func il2p_reconstruct_packet(uhdr []byte, payload []byte, symbols_corrected *int) *packet_t {
	var hdr_type, _, _ = il2p_get_header_attributes(uhdr)
	if hdr_type == 1 {
		var pp = il2p_decode_header_type_1(uhdr, *symbols_corrected)
		if pp != nil && len(payload) > 0 {
			ax25_set_info(pp, payload)
		}
		return pp
	}
	var alevel alevel_t
	return ax25_from_frame(payload, alevel)
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_crc_retry_decode
 *
 * Purpose:	After a CRC failure, retry RS decoding with single-erasure
 *		hints at every byte position of every payload block.
 *		Returns the first candidate that passes the trailing CRC.
 *
 * Inputs:	uhdr		Header after FEC and descrambling.
 *		epayload	Encoded payload (raw, with parity bytes).
 *		scrc		Received Hamming-encoded CRC (4 bytes).
 *
 * Returns:	Packet pointer on success, nil if no erasure hint produces
 *		a CRC-passing frame.
 *
 *--------------------------------------------------------------*/

func il2p_crc_retry_decode(uhdr []byte, epayload []byte, scrc []byte) *packet_t {
	var _, max_fec, payload_len = il2p_get_header_attributes(uhdr)
	var ipp, _ = il2p_payload_compute(payload_len, max_fec)
	if ipp == nil || ipp.payload_block_count == 0 {
		return nil
	}

	type blockInfo struct {
		data       []byte // descrambled decoded data
		blockSize  int    // data bytes only
		recvOffset int    // byte offset of this block within epayload
		totalBytes int    // data + parity bytes
	}

	var blocks = make([]blockInfo, ipp.payload_block_count)
	var pin = epayload
	var offset = 0

	for b := 0; b < ipp.large_block_count; b++ {
		var total = ipp.large_block_size + ipp.parity_symbols_per_block
		var corrected, _ = il2p_decode_rs(pin[:total], ipp.parity_symbols_per_block)
		blocks[b] = blockInfo{il2p_descramble_block(corrected), ipp.large_block_size, offset, total}
		pin = pin[total:]
		offset += total
	}
	for b := 0; b < ipp.small_block_count; b++ {
		var total = ipp.small_block_size + ipp.parity_symbols_per_block
		var corrected, _ = il2p_decode_rs(pin[:total], ipp.parity_symbols_per_block)
		blocks[ipp.large_block_count+b] = blockInfo{il2p_descramble_block(corrected), ipp.small_block_size, offset, total}
		pin = pin[total:]
		offset += total
	}

	for bi, blk := range blocks {
		var recBlock = epayload[blk.recvOffset : blk.recvOffset+blk.totalBytes]
		for pos := 0; pos < blk.totalBytes; pos++ {
			var corrected, e = il2p_decode_rs_with_erasure(recBlock, ipp.parity_symbols_per_block, pos)
			if e < 0 {
				continue
			}
			var descrambled = il2p_descramble_block(corrected)

			// Assemble full payload substituting this block's erasure-decoded data.
			var payload []byte
			for ci, cb := range blocks {
				if ci == bi {
					payload = append(payload, descrambled...)
				} else {
					payload = append(payload, cb.data...)
				}
			}

			var sc = 0
			var pp = il2p_reconstruct_packet(uhdr, payload, &sc)
			if pp == nil {
				continue
			}
			if il2p_crc_check(ax25_get_frame_data(pp), scrc) {
				return pp
			}
			ax25_delete(pp)
		}
	}
	return nil
}

// end il2p_codec.c
