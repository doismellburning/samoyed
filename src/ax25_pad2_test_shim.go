package direwolf

// #define AX25_PAD_C		/* this will affect behavior of ax25_pad.h */
// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>
// #include "textcolor.h"
// #include "ax25_pad.h"
// #include "ax25_pad2.h"
import "C"

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

/*------------------------------------------------------------------------------
 *
 * Purpose:	Quick unit test for ax25_pad2.c
 *
 * Description:	Generate a variety of frames.
 *		Each function calls ax25_frame_type to verify results.
 *
 *------------------------------------------------------------------------------*/

func ax25_pad2_test_main(t *testing.T) {
	t.Helper()

	var pid C.int = 0xf0
	var pinfo *C.uchar
	var info_len C.int

	var addrs [C.AX25_MAX_ADDRS][C.AX25_MAX_ADDR_LEN]C.char
	C.strcpy(&addrs[0][0], C.CString("W2UB"))
	C.strcpy(&addrs[1][0], C.CString("WB2OSZ-15"))
	var num_addr C.int = 2

	/* U frame */

	for ftype := ax25_frame_type_t(C.frame_type_U_SABME); ftype <= C.frame_type_U_TEST; ftype++ {
		for pf := C.int(0); pf <= 1; pf++ {
			var cmin C.cmdres_t = 0
			var cmax C.cmdres_t = 0

			switch ftype {
			// 0 = response, 1 = command
			case C.frame_type_U_SABME:
				cmin = 1
				cmax = 1
			case C.frame_type_U_SABM:
				cmin = 1
				cmax = 1
			case C.frame_type_U_DISC:
				cmin = 1
				cmax = 1
			case C.frame_type_U_DM:
				cmin = 0
				cmax = 0
			case C.frame_type_U_UA:
				cmin = 0
				cmax = 0
			case C.frame_type_U_FRMR:
				cmin = 0
				cmax = 0
			case C.frame_type_U_UI:
				cmin = 0
				cmax = 1
			case C.frame_type_U_XID:
				cmin = 0
				cmax = 1
			case C.frame_type_U_TEST:
				cmin = 0
				cmax = 1
			default:
			}

			for cr := cmin; cr <= cmax; cr++ {
				C.text_color_set(C.DW_COLOR_INFO)
				dw_printf("\nConstruct U frame, cr=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_u_frame(addrs, num_addr, cr, ftype, pf, pid, pinfo, info_len)
				check_ax25_u_frame(t, pp, cr, ftype, pf)
				ax25_hex_dump(pp)
				ax25_delete(pp)
			}
		}
	}

	dw_printf("\n----------\n\n")

	/* S frame */

	C.strcpy(&addrs[2][0], C.CString("DIGI1-1"))
	num_addr = 3

	for ftype := ax25_frame_type_t(C.frame_type_S_RR); ftype <= C.frame_type_S_SREJ; ftype++ {
		for pf := C.int(0); pf <= 1; pf++ {
			var modulo C.int = 8
			var nr = modulo/2 + 1

			for cr := C.cmdres_t(0); cr <= 1; cr++ {
				C.text_color_set(C.DW_COLOR_INFO)
				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil, 0)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				ax25_hex_dump(pp)
				ax25_delete(pp)
			}

			modulo = 128
			nr = modulo/2 + 1

			for cr := C.cmdres_t(0); cr <= 1; cr++ {
				C.text_color_set(C.DW_COLOR_INFO)
				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil, 0)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				ax25_hex_dump(pp)
				ax25_delete(pp)
			}
		}
	}

	/* SREJ is only S frame which can have information part. */

	var srej_info = []C.uchar{1 << 1, 2 << 1, 3 << 1, 4 << 1}

	var ftype ax25_frame_type_t = C.frame_type_S_SREJ
	for pf := C.int(0); pf <= 1; pf++ {
		var modulo C.int = 128
		var nr C.int = 127
		var cr C.cmdres_t = C.cr_res

		C.text_color_set(C.DW_COLOR_INFO)
		dw_printf("\nConstruct Multi-SREJ S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

		var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, &srej_info[0], C.int(len(srej_info)))
		check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

		ax25_hex_dump(pp)
		ax25_delete(pp)
	}

	dw_printf("\n----------\n\n")

	/* I frame */

	pinfo = (*C.uchar)(unsafe.Pointer(C.strdup(C.CString("The rain in Spain stays mainly on the plain."))))
	info_len = C.int(C.strlen((*C.char)(unsafe.Pointer(pinfo))))

	for pf := C.int(0); pf <= 1; pf++ {
		var modulo C.int = 8
		var nr = 0x55 & (modulo - 1)
		var ns = 0xaa & (modulo - 1)

		for cr := C.cmdres_t(0); cr <= 1; cr++ {
			C.text_color_set(C.DW_COLOR_INFO)
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, pinfo, info_len)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, pinfo, info_len)

			ax25_hex_dump(pp)
			ax25_delete(pp)
		}

		modulo = 128
		nr = 0x55 & (modulo - 1)
		ns = 0xaa & (modulo - 1)

		for cr := C.cmdres_t(0); cr <= 1; cr++ {
			C.text_color_set(C.DW_COLOR_INFO)
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, pinfo, info_len)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, pinfo, info_len)

			ax25_hex_dump(pp)
			ax25_delete(pp)
		}
	}

	C.text_color_set(C.DW_COLOR_REC)
	dw_printf("\n----------\n\n")
	dw_printf("\nSUCCESS!\n")
} /* end main */

func check_ax25_u_frame(t *testing.T, packet C.packet_t, cr C.cmdres_t, ftype ax25_frame_type_t, pf C.int) {
	t.Helper()

	var check_cr C.cmdres_t
	var check_desc [80]C.char
	var check_pf C.int
	var check_nr C.int
	var check_ns C.int

	var check_ftype = ax25_frame_type(packet, &check_cr, &check_desc[0], &check_pf, &check_nr, &check_ns)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d\n", check_ftype, C.GoString(&check_desc[0]), check_pf)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, C.int(-1), check_nr)
	assert.Equal(t, C.int(-1), check_ns)
}

func check_ax25_s_frame(t *testing.T, packet C.packet_t, cr C.cmdres_t, ftype ax25_frame_type_t, pf C.int, nr C.int) {
	t.Helper()

	var check_cr C.cmdres_t
	var check_desc [80]C.char
	var check_pf C.int
	var check_nr C.int
	var check_ns C.int

	// todo modulo must be input.
	var check_ftype = ax25_frame_type(packet, &check_cr, &check_desc[0], &check_pf, &check_nr, &check_ns)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d\n", check_ftype, C.GoString(&check_desc[0]), check_pf, check_nr)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, C.int(-1), check_ns)
}

func check_ax25_i_frame(t *testing.T, packet C.packet_t, cr C.cmdres_t, pf C.int, nr C.int, ns C.int, pinfo *C.uchar, info_len C.int) {
	t.Helper()

	var check_cr C.cmdres_t
	var check_desc [80]C.char
	var check_pf C.int
	var check_nr C.int
	var check_ns C.int

	var check_pinfo *C.uchar

	var check_ftype = ax25_frame_type(packet, &check_cr, &check_desc[0], &check_pf, &check_nr, &check_ns)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d, ns=%d\n", check_ftype, C.GoString(&check_desc[0]), check_pf, check_nr, check_ns)

	var check_info_len = ax25_get_info(packet, &check_pinfo)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ax25_frame_type_t(C.frame_type_I), check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, ns, check_ns)

	assert.Equal(t, info_len, check_info_len)
	assert.Equal(t, C.int(0), C.strcmp((*C.char)(unsafe.Pointer(check_pinfo)), (*C.char)(unsafe.Pointer(pinfo)))) //nolint:testifylint
}
