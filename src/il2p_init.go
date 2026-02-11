package direwolf

// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <stdio.h>
import "C"

import (
	"unsafe"
)

// Interesting related stuff:
// https://www.kernel.org/doc/html/v4.15/core-api/librs.html
// https://berthub.eu/articles/posts/reed-solomon-for-programmers/

const MAX_NROOTS = 16

const NTAB = 5

type TabType struct {
	symsize C.uint // Symbol size, bits (1-8).  Always 8 for this application.
	genpoly C.uint // Field generator polynomial coefficients.
	fcs     C.uint // First root of RS code generator polynomial, index form. FX.25 uses 1 but IL2P uses 0.
	prim    C.uint // Primitive element to generate polynomial roots.
	nroots  C.uint // RS code generator polynomial degree (number of roots). Same as number of check bytes added.
	rs      *rs_t  // Pointer to RS codec control block.  Filled in at init time.
}

var Tab = [NTAB]TabType{
	{8, 0x11d, 0, 1, 2, nil},  // 2 parity
	{8, 0x11d, 0, 1, 4, nil},  // 4 parity
	{8, 0x11d, 0, 1, 6, nil},  // 6 parity
	{8, 0x11d, 0, 1, 8, nil},  // 8 parity
	{8, 0x11d, 0, 1, 16, nil}, // 16 parity
}

var g_il2p_debug = 0

/*-------------------------------------------------------------
 *
 * Name:	il2p_init
 *
 * Purpose:	This must be called at application start up time.
 *		It sets up tables for the Reed-Solomon functions.
 *
 * Inputs:	debug	- Enable debug output.
 *
 *--------------------------------------------------------------*/

func il2p_init(il2p_debug int) {
	g_il2p_debug = il2p_debug

	for i := 0; i < NTAB; i++ {
		Assert(Tab[i].nroots <= MAX_NROOTS)
		Tab[i].rs = init_rs_char(Tab[i].symsize, Tab[i].genpoly, Tab[i].fcs, Tab[i].prim, Tab[i].nroots)
		if Tab[i].rs == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("IL2P internal error: init_rs_char failed!\n")
			exit(1)
		}
	}

} // end il2p_init

func il2p_get_debug() int {
	return g_il2p_debug
}

func il2p_set_debug(debug int) {
	g_il2p_debug = debug
}

// Find RS codec control block for specified number of parity symbols.

func il2p_find_rs(nparity int) *rs_t {
	for n := 0; n < NTAB; n++ {
		if Tab[n].nroots == C.uint(nparity) {
			return Tab[n].rs
		}
	}
	text_color_set(DW_COLOR_ERROR)
	dw_printf("IL2P INTERNAL ERROR: il2p_find_rs: control block not found for nparity = %d.\n", nparity)
	return Tab[0].rs
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_encode_rs
 *
 * Purpose:	Add parity symbols to a block of data.
 *
 * Inputs:	tx_data		Header or other data to transmit.
 *		data_size	Number of data bytes in above.
 *		num_parity	Number of parity symbols to add.
 *				Maximum of IL2P_MAX_PARITY_SYMBOLS.
 *
 * Outputs:	parity_out	Specified number of parity symbols
 *
 * Restriction:	data_size + num_parity <= 255 which is the RS block size.
 *		The caller must ensure this.
 *
 *--------------------------------------------------------------*/

func il2p_encode_rs(tx_data []byte, num_parity int) []byte {

	var data_size = len(tx_data)

	Assert(data_size >= 1)

	Assert(num_parity == 2 || num_parity == 4 || num_parity == 6 || num_parity == 8 || num_parity == 16)
	Assert(data_size+num_parity <= 255)

	var rs_block [FX25_BLOCK_SIZE]C.uchar
	C.memcpy(unsafe.Pointer(&rs_block[len(rs_block)-data_size-num_parity]), C.CBytes(tx_data), C.size_t(data_size))

	var parity_out = make([]C.uchar, num_parity)
	encode_rs_char(il2p_find_rs(num_parity), &rs_block[0], &parity_out[0])

	return C.GoBytes(unsafe.Pointer(&parity_out[0]), C.int(num_parity))
}

/*-------------------------------------------------------------
 *
 * Name:	il2p_decode_rs
 *
 * Purpose:	Check and attempt to fix block with FEC.
 *
 * Inputs:	rec_block	Received block composed of data and parity.
 *				Total size is sum of following two parameters.
 *		rec_block	data_size + num_parity bytes.
 *		num_parity	Number of parity symbols (bytes) in above.
 *
 * Returns:	out		Original with possible corrections applied.
 *				data_size bytes.
 *
 * Returns:	-1 for unrecoverable.
 *		>= 0 for success.  Number of symbols corrected.
 *
 *--------------------------------------------------------------*/

func il2p_decode_rs(rec_block []byte, num_parity int) ([]byte, int) {

	var data_size = len(rec_block) - num_parity

	//  Use zero padding in front if data size is too small.

	var n = data_size + num_parity // total size in.

	var rs_block [FX25_BLOCK_SIZE]C.uchar

	C.memcpy(unsafe.Pointer(&rs_block[len(rs_block)-n]), C.CBytes(rec_block), C.size_t(n))

	if il2p_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("==============================  il2p_decode_rs  ==============================\n")
		dw_printf("%d filler zeros, %d data, %d parity\n", len(rs_block)-n, data_size, num_parity)
		fx_hex_dump(C.GoBytes(unsafe.Pointer(&rs_block[0]), C.int(len(rs_block))))
	}

	var derrlocs [FX25_MAX_CHECK]C.int // Half would probably be OK.

	var derrors = decode_rs_char(il2p_find_rs(num_parity), &rs_block[0], &derrlocs[0], 0)
	var out = C.GoBytes(unsafe.Pointer(&rs_block[len(rs_block)-n]), C.int(data_size))

	if il2p_get_debug() >= 3 {
		if derrors == 0 {
			dw_printf("No errors reported for RS block.\n")
		} else if derrors > 0 {
			dw_printf("%d errors fixed in positions:\n", derrors)
			for j := C.int(0); j < derrors; j++ {
				dw_printf("        %3d  (0x%02x)\n", derrlocs[j], derrlocs[j])
			}
			fx_hex_dump(C.GoBytes(unsafe.Pointer(&rs_block[0]), C.int(len(rs_block))))
		}
	}

	// It is possible to have a situation where too many errors are
	// present but the algorithm could get a good code block by "fixing"
	// one of the padding bytes that should be 0.

	for i := C.int(0); i < derrors; i++ {
		if derrlocs[i] < C.int(len(rs_block)-n) {
			if il2p_get_debug() >= 3 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("RS DECODE ERROR!  Padding position %d should be 0 but it was set to %02x.\n", derrlocs[i], rs_block[derrlocs[i]])
			}
			derrors = -1
			break
		}
	}

	if il2p_get_debug() >= 3 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("==============================  il2p_decode_rs  returns %d  ==============================\n", derrors)
	}
	return out, int(derrors)
}
