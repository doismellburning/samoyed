package direwolf

// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <assert.h>
import "C"

import (
	"unsafe"
)

/*--------------------------------------------------------------------------------
 *
 * Purpose:	Functions dealing with the payload.
 *
 *--------------------------------------------------------------------------------*/

type il2p_payload_properties_t struct {
	payload_byte_count       C.int // Total size, 0 thru 1023
	payload_block_count      C.int
	small_block_size         C.int
	large_block_size         C.int
	large_block_count        C.int
	small_block_count        C.int
	parity_symbols_per_block C.int // 2, 4, 6, 8, 16
}

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_payload_compute
 *
 * Purpose:	Compute number and sizes of data blocks based on total size.
 *
 * Inputs:	payload_size	0 to 1023.  (IL2P_MAX_PAYLOAD_SIZE)
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * Outputs:	*p		Payload block sizes and counts.
 *				Number of parity symbols per block.
 *
 * Returns:	Number of bytes in the encoded format.
 *		Could be 0 for no payload blocks.
 *		-1 for error (i.e. invalid unencoded size: <0 or >1023)
 *
 *--------------------------------------------------------------------------------*/

func il2p_payload_compute(payload_size C.int, max_fec C.int) (*il2p_payload_properties_t, C.int) {

	var p = new(il2p_payload_properties_t)

	if payload_size < 0 || payload_size > IL2P_MAX_PAYLOAD_SIZE {
		return p, -1
	}
	if payload_size == 0 {
		return p, 0
	}

	if max_fec != 0 {
		p.payload_byte_count = payload_size
		p.payload_block_count = (p.payload_byte_count + 238) / 239
		p.small_block_size = p.payload_byte_count / p.payload_block_count
		p.large_block_size = p.small_block_size + 1
		p.large_block_count = p.payload_byte_count - (p.payload_block_count * p.small_block_size)
		p.small_block_count = p.payload_block_count - p.large_block_count
		p.parity_symbols_per_block = 16
	} else {
		p.payload_byte_count = payload_size
		p.payload_block_count = (p.payload_byte_count + 246) / 247
		p.small_block_size = p.payload_byte_count / p.payload_block_count
		p.large_block_size = p.small_block_size + 1
		p.large_block_count = p.payload_byte_count - (p.payload_block_count * p.small_block_size)
		p.small_block_count = p.payload_block_count - p.large_block_count
		//p.parity_symbols_per_block = (p.small_block_size / 32) + 2;  // Looks like error in documentation

		// It would work if the number of parity symbols was based on large block size.

		if p.small_block_size <= 61 {
			p.parity_symbols_per_block = 2
		} else if p.small_block_size <= 123 {
			p.parity_symbols_per_block = 4
		} else if p.small_block_size <= 185 {
			p.parity_symbols_per_block = 6
		} else if p.small_block_size <= 247 {
			p.parity_symbols_per_block = 8
		} else {
			// Should not happen.  But just in case...
			text_color_set(DW_COLOR_ERROR)
			dw_printf("IL2P parity symbol per payload block error.  small_block_size = %d\n", p.small_block_size)
			return p, -1
		}
	}

	// Return the total size for the encoded format.

	return p, (p.small_block_count*(p.small_block_size+p.parity_symbols_per_block) +
		p.large_block_count*(p.large_block_size+p.parity_symbols_per_block))

} // end il2p_payload_compute

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_encode_payload
 *
 * Purpose:	Split payload into multiple blocks such that each set
 *		of data and parity symbols fit into a 255 byte RS block.
 *
 * Inputs:	payload	Slice of bytes.
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * Returns:	Encoded payload for transmission.
 *				Up to IL2P_MAX_ENCODED_SIZE bytes.
 *
 * Returns:	-1 for error (i.e. invalid size)
 *		0 for no blocks.  (i.e. size zero)
 *		Number of bytes generated.  Maximum IL2P_MAX_ENCODED_SIZE.
 *
 * Note:	I interpreted the protocol spec as saying the LFSR state is retained
 *		between data blocks.  During interoperability testing, I found that
 *		was not the case.  It is reset for each data block.
 *
 *--------------------------------------------------------------------------------*/

func il2p_encode_payload(payload []byte, max_fec int) ([]byte, int) {

	var payload_size = len(payload)

	if payload_size > IL2P_MAX_PAYLOAD_SIZE {
		return nil, -1
	}
	if payload_size == 0 {
		return nil, 0
	}

	// Determine number of blocks and sizes.

	var ipp, e = il2p_payload_compute(C.int(payload_size), C.int(max_fec))
	if e <= 0 {
		return nil, int(e)
	}

	var pin = payload
	var pout []byte
	var encoded_length C.int = 0

	// First the large blocks.

	for b := C.int(0); b < ipp.large_block_count; b++ {
		var scram = il2p_scramble_block(pin[:ipp.large_block_size])
		pout = append(pout, scram...)

		pin = pin[ipp.large_block_size:]

		encoded_length += ipp.large_block_size

		var parity = il2p_encode_rs(scram, int(ipp.parity_symbols_per_block))
		pout = append(pout, parity...)

		encoded_length += ipp.parity_symbols_per_block
	}

	// Then the small blocks.

	for b := C.int(0); b < ipp.small_block_count; b++ {
		var scram = il2p_scramble_block(pin[:ipp.small_block_size])
		pout = append(pout, scram...)

		pin = pin[ipp.small_block_size:]
		encoded_length += ipp.small_block_size

		var parity = il2p_encode_rs(scram, int(ipp.parity_symbols_per_block))
		pout = append(pout, parity...)

		encoded_length += ipp.parity_symbols_per_block
	}

	return pout, int(encoded_length)

} // end il2p_encode_payload

/*--------------------------------------------------------------------------------
 *
 * Function:	il2p_decode_payload
 *
 * Purpose:	Extract original data from encoded payload.
 *
 * Inputs:	received	Array of bytes.  Size is unknown but in practice it
 *				must not exceed IL2P_MAX_ENCODED_SIZE.
 *		payload_size	0 to 1023.  (IL2P_MAX_PAYLOAD_SIZE)
 *				Expected result size based on header.
 *		max_fec		true for 16 parity symbols, false for automatic.
 *
 * Outputs:	payload_out	Recovered payload.
 *
 * In/Out:	symbols_corrected	Number of symbols corrected.
 *
 *
 * Returns:	Number of bytes extracted.  Should be same as payload_size going in.
 *		-3 for unexpected internal inconsistency.
 *		-2 for unable to recover from signal corruption.
 *		-1 for invalid size.
 *		0 for no blocks.  (i.e. size zero)
 *
 * Description:	Each block is scrambled separately but the LFSR state is carried
 *		from the first payload block to the next.
 *
 *--------------------------------------------------------------------------------*/

func il2p_decode_payload(received *C.uchar, payload_size C.int, max_fec C.int, payload_out *C.uchar, symbols_corrected *C.int) C.int {
	// Determine number of blocks and sizes.

	var ipp, e = il2p_payload_compute(payload_size, max_fec)
	if e <= 0 {
		return (e)
	}

	var pin = received
	var pout = payload_out
	var decoded_length C.int = 0
	var failed = false

	// First the large blocks.

	for b := C.int(0); b < ipp.large_block_count; b++ {
		var corrected_block, e = il2p_decode_rs(C.GoBytes(unsafe.Pointer(pin), ipp.large_block_size+ipp.parity_symbols_per_block), int(ipp.parity_symbols_per_block))

		// dw_printf ("%s:%d: large block decode_rs returned status = %d\n", __FILE__, __LINE__, e);

		if e < 0 {
			failed = true
		}
		*symbols_corrected += C.int(e)

		il2p_descramble_block((*C.uchar)(C.CBytes(corrected_block)), pout, ipp.large_block_size)

		if il2p_get_debug() >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Descrambled large payload block, %d bytes:\n", ipp.large_block_size)
			fx_hex_dump(C.GoBytes(unsafe.Pointer(pout), ipp.large_block_size))
		}

		pin = (*C.uchar)(unsafe.Add(unsafe.Pointer(pin), ipp.large_block_size+ipp.parity_symbols_per_block))
		pout = (*C.uchar)(unsafe.Add(unsafe.Pointer(pout), ipp.large_block_size))
		decoded_length += ipp.large_block_size
	}

	// Then the small blocks.

	for b := C.int(0); b < ipp.small_block_count; b++ {
		var corrected_block, e = il2p_decode_rs(C.GoBytes(unsafe.Pointer(pin), ipp.small_block_size+ipp.parity_symbols_per_block), int(ipp.parity_symbols_per_block))

		// dw_printf ("%s:%d: small block decode_rs returned status = %d\n", __FILE__, __LINE__, e);

		if e < 0 {
			failed = true
		}
		*symbols_corrected += C.int(e)

		il2p_descramble_block((*C.uchar)(C.CBytes(corrected_block)), pout, ipp.small_block_size)

		if il2p_get_debug() >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Descrambled small payload block, %d bytes:\n", ipp.small_block_size)
			fx_hex_dump(C.GoBytes(unsafe.Pointer(pout), ipp.small_block_size))
		}

		pin = (*C.uchar)(unsafe.Add(unsafe.Pointer(pin), ipp.small_block_size+ipp.parity_symbols_per_block))
		pout = (*C.uchar)(unsafe.Add(unsafe.Pointer(pout), ipp.small_block_size))
		decoded_length += ipp.small_block_size
	}

	if failed {
		//dw_printf ("%s:%d: failed = %0x\n", __FILE__, __LINE__, failed);
		return (-2)
	}

	if decoded_length != payload_size {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("IL2P Internal error: decoded_length = %d, payload_size = %d\n", decoded_length, payload_size)
		return (-3)
	}

	return (decoded_length)

} // end il2p_decode_payload
