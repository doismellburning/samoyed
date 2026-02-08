package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
import "C"

import (
	"unsafe"
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

func il2p_encode_frame(pp *packet_t, max_fec C.int, iout *C.uchar) C.int {

	// Can a type 1 header be used?

	var hdr [IL2P_HEADER_SIZE + IL2P_HEADER_PARITY]C.uchar
	var out_len C.int

	var e = il2p_type_1_header(pp, max_fec, &hdr[0])

	if e >= 0 {
		var _scrambled = il2p_scramble_block(C.GoBytes(unsafe.Pointer(&hdr[0]), IL2P_HEADER_SIZE))

		C.memcpy(unsafe.Pointer(iout), unsafe.Pointer(&_scrambled[0]), C.size_t(len(_scrambled)))

		var parity = il2p_encode_rs(_scrambled, IL2P_HEADER_PARITY)
		C.memcpy(unsafe.Add(unsafe.Pointer(iout), IL2P_HEADER_SIZE), C.CBytes(parity), C.size_t(len(parity)))

		out_len = IL2P_HEADER_SIZE + IL2P_HEADER_PARITY

		if e == 0 {
			// Success. No info part.
			return (out_len)
		}

		// Payload is AX.25 info part.
		var pinfo = ax25_get_info(pp)

		var k = il2p_encode_payload((*C.uchar)(C.CBytes(pinfo)), C.int(len(pinfo)), max_fec, (*C.uchar)(unsafe.Add(unsafe.Pointer(iout), out_len)))
		if k > 0 {
			out_len += k
			// Success. Info part was <= 1023 bytes.
			return (out_len)
		}

		// Something went wrong with the payload encoding.
		return (-1)
	} else if e == -1 {

		// Could not use type 1 header for some reason.
		// e.g. More than 2 addresses, extended (mod 128) sequence numbers, etc.

		e = il2p_type_0_header(pp, max_fec, &hdr[0])
		if e > 0 {
			var _scrambled = il2p_scramble_block(C.GoBytes(unsafe.Pointer(&hdr[0]), IL2P_HEADER_SIZE))

			C.memcpy(unsafe.Pointer(iout), unsafe.Pointer(&_scrambled[0]), C.size_t(len(_scrambled)))

			var parity = il2p_encode_rs(_scrambled, IL2P_HEADER_PARITY)
			C.memcpy(unsafe.Add(unsafe.Pointer(iout), IL2P_HEADER_SIZE), C.CBytes(parity), C.size_t(len(parity)))

			out_len = IL2P_HEADER_SIZE + IL2P_HEADER_PARITY

			// Payload is entire AX.25 frame.

			var frame_data = ax25_get_frame_data(pp)
			var frame_len = ax25_get_frame_len(pp)
			var k = il2p_encode_payload((*C.uchar)(C.CBytes(frame_data)), C.int(frame_len), max_fec, (*C.uchar)(unsafe.Add(unsafe.Pointer(iout), out_len)))
			if k > 0 {
				out_len += k
				// Success. Entire AX.25 frame <= 1023 bytes.
				return (out_len)
			}
			// Something went wrong with the payload encoding.
			return (-1)
		} else if e == 0 { //nolint:gocritic
			// Impossible condition.  Type 0 header must have payload.
			return (-1)
		} else {
			// AX.25 frame is too large.
			return (-1)
		}
	}

	// AX.25 Information part is too large.
	return (-1)
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

func il2p_decode_frame(irec *C.uchar) *packet_t {
	var uhdr [IL2P_HEADER_SIZE]C.uchar // After FEC and descrambling.
	var e = il2p_clarify_header(irec, &uhdr[0])

	// TODO?: for symmetry we might want to clarify the payload before combining.

	return (il2p_decode_header_payload(&uhdr[0], (*C.uchar)(unsafe.Add(unsafe.Pointer(irec), IL2P_HEADER_SIZE+IL2P_HEADER_PARITY)), &e))
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

func il2p_decode_header_payload(uhdr *C.uchar, epayload *C.uchar, symbols_corrected *C.int) *packet_t {
	var hdr_type, max_fec C.int
	var payload_len = il2p_get_header_attributes(uhdr, &hdr_type, &max_fec)

	if hdr_type == 1 {

		// Header type 1.  Any payload is the AX.25 Information part.

		var pp = il2p_decode_header_type_1(uhdr, *symbols_corrected)
		if pp == nil {
			// Failed for some reason.
			return (nil)
		}

		if payload_len > 0 {
			// This is the AX.25 Information part.

			var extracted [IL2P_MAX_PAYLOAD_SIZE]C.uchar
			var e = il2p_decode_payload(epayload, payload_len, max_fec, &extracted[0], symbols_corrected)

			// It would be possible to have a good header but too many errors in the payload.

			if e <= 0 {
				ax25_delete(pp)
				return nil
			}

			if e != payload_len {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("IL2P Internal Error: il2p_decode_header_payload(): hdr_type=%d, max_fec=%d, payload_len=%d, e=%d.\n", hdr_type, max_fec, payload_len, e)
			}

			ax25_set_info(pp, C.GoBytes(unsafe.Pointer(&extracted[0]), payload_len))
		}
		return (pp)
	} else {

		// Header type 0.  The payload is the entire AX.25 frame.

		var extracted [IL2P_MAX_PAYLOAD_SIZE]C.uchar
		var e = il2p_decode_payload(epayload, payload_len, max_fec, &extracted[0], symbols_corrected)

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

		var pp = ax25_from_frame(C.GoBytes(unsafe.Pointer(&extracted[0]), payload_len), alevel)
		return (pp)
	}

} // end il2p_decode_header_payload

// end il2p_codec.c
