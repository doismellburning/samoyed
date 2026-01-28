package direwolf

// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>
import "C"

import (
	"testing"

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

	var pid int = 0xf0
	var info []byte

	var addrs [AX25_MAX_ADDRS][AX25_MAX_ADDR_LEN]C.char
	C.strcpy(&addrs[0][0], C.CString("W2UB"))
	C.strcpy(&addrs[1][0], C.CString("WB2OSZ-15"))
	var num_addr C.int = 2

	/* U frame */

	for ftype := frame_type_U_SABME; ftype <= frame_type_U_TEST; ftype++ {
		for pf := 0; pf <= 1; pf++ {
			var cmin cmdres_t = 0
			var cmax cmdres_t = 0

			switch ftype {
			// 0 = response, 1 = command
			case frame_type_U_SABME:
				cmin = 1
				cmax = 1
			case frame_type_U_SABM:
				cmin = 1
				cmax = 1
			case frame_type_U_DISC:
				cmin = 1
				cmax = 1
			case frame_type_U_DM:
				cmin = 0
				cmax = 0
			case frame_type_U_UA:
				cmin = 0
				cmax = 0
			case frame_type_U_FRMR:
				cmin = 0
				cmax = 0
			case frame_type_U_UI:
				cmin = 0
				cmax = 1
			case frame_type_U_XID:
				cmin = 0
				cmax = 1
			case frame_type_U_TEST:
				cmin = 0
				cmax = 1
			default:
			}

			for cr := cmin; cr <= cmax; cr++ {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\nConstruct U frame, cr=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_u_frame(addrs, num_addr, cr, ftype, pf, pid, nil)
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

	for ftype := frame_type_S_RR; ftype <= frame_type_S_SREJ; ftype++ {
		for pf := 0; pf <= 1; pf++ {
			var modulo = modulo_8
			var nr = int(modulo/2 + 1)

			for cr := cmdres_t(0); cr <= 1; cr++ {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				ax25_hex_dump(pp)
				ax25_delete(pp)
			}

			modulo = modulo_128
			nr = int(modulo/2 + 1)

			for cr := cmdres_t(0); cr <= 1; cr++ {
				text_color_set(DW_COLOR_INFO)
				dw_printf("\nConstruct S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

				var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, nil)
				check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

				ax25_hex_dump(pp)
				ax25_delete(pp)
			}
		}
	}

	/* SREJ is only S frame which can have information part. */

	var srej_info = []byte{1 << 1, 2 << 1, 3 << 1, 4 << 1}

	var ftype ax25_frame_type_t = frame_type_S_SREJ
	for pf := 0; pf <= 1; pf++ {
		var modulo = modulo_128
		var nr = 127
		var cr cmdres_t = cr_res

		text_color_set(DW_COLOR_INFO)
		dw_printf("\nConstruct Multi-SREJ S frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

		var pp = ax25_s_frame(addrs, num_addr, cr, ftype, modulo, nr, pf, srej_info)
		check_ax25_s_frame(t, pp, cr, ftype, pf, nr)

		ax25_hex_dump(pp)
		ax25_delete(pp)
	}

	dw_printf("\n----------\n\n")

	/* I frame */

	info = []byte("The rain in Spain stays mainly on the plain.")

	for pf := 0; pf <= 1; pf++ {
		var modulo = modulo_8
		var nr = 0x55 & int(modulo-1)
		var ns = 0xaa & int(modulo-1)

		for cr := cmdres_t(0); cr <= 1; cr++ {
			text_color_set(DW_COLOR_INFO)
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, info)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, info)

			ax25_hex_dump(pp)
			ax25_delete(pp)
		}

		modulo = modulo_128
		nr = 0x55 & int(modulo-1)
		ns = 0xaa & int(modulo-1)

		for cr := cmdres_t(0); cr <= 1; cr++ {
			text_color_set(DW_COLOR_INFO)
			dw_printf("\nConstruct I frame, cmd=%d, ftype=%d, pid=0x%02x\n", cr, ftype, pid)

			var pp = ax25_i_frame(addrs, num_addr, cr, modulo, nr, ns, pf, pid, info)
			check_ax25_i_frame(t, pp, cr, pf, nr, ns, info)

			ax25_hex_dump(pp)
			ax25_delete(pp)
		}
	}

	text_color_set(DW_COLOR_REC)
	dw_printf("\n----------\n\n")
	dw_printf("\nSUCCESS!\n")
} /* end main */

func check_ax25_u_frame(t *testing.T, packet *packet_t, cr cmdres_t, ftype ax25_frame_type_t, pf int) {
	t.Helper()

	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = ax25_frame_type(packet)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d\n", check_ftype, check_desc, check_pf)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, -1, check_nr)
	assert.Equal(t, -1, check_ns)
}

func check_ax25_s_frame(t *testing.T, packet *packet_t, cr cmdres_t, ftype ax25_frame_type_t, pf int, nr int) {
	t.Helper()

	// todo modulo must be input.
	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = ax25_frame_type(packet)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d\n", check_ftype, check_desc, check_pf, check_nr)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, ftype, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, -1, check_ns)
}

func check_ax25_i_frame(t *testing.T, packet *packet_t, cr cmdres_t, pf int, nr int, ns int, info []byte) {
	t.Helper()

	var check_cr, check_desc, check_pf, check_nr, check_ns, check_ftype = ax25_frame_type(packet)

	dw_printf("check: ftype=%d, desc=\"%s\", pf=%d, nr=%d, ns=%d\n", check_ftype, check_desc, check_pf, check_nr, check_ns)

	var check_info = ax25_get_info(packet)

	assert.Equal(t, cr, check_cr)
	assert.Equal(t, frame_type_I, check_ftype)
	assert.Equal(t, pf, check_pf)
	assert.Equal(t, nr, check_nr)
	assert.Equal(t, ns, check_ns)

	assert.Equal(t, info, check_info)
}
