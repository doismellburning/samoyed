package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <unistd.h>
// #include "textcolor.h"
// #include "xid.h"
import "C"

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

/* From Figure 4.6. Typical XID frame, from AX.25 protocol spec, v. 2.2 */
/* This is the info part after a control byte of 0xAF. */

var xid_example []C.uchar = []C.uchar{

	/* FI */ 0x82, /* Format indicator */
	/* GI */ 0x80, /* Group Identifier - parameter negotiation */
	/* GL */ 0x00, /* Group length - all of the PI/PL/PV fields */
	/* GL */ 0x17, /* (2 bytes) */
	/* PI */ 0x02, /* Parameter Indicator - classes of procedures */
	/* PL */ 0x02, /* Parameter Length */

	// Erratum: Example in the protocol spec looks wrong.
	///* PV */	0x00,	/* Parameter Variable - Half Duplex, Async, Balanced Mode */
	///* PV */	0x20,	/*  */
	// I think it should be like this instead.
	/* PV */ 0x21, /* Parameter Variable - Half Duplex, Async, Balanced Mode */
	/* PV */ 0x00, /* Reserved */

	/* PI */ 0x03, /* Parameter Indicator - optional functions */
	/* PL */ 0x03, /* Parameter Length */
	/* PV */ 0x86, /* Parameter Variable - SREJ/REJ, extended addr */
	/* PV */ 0xA8, /* 16-bit FCS, TEST cmd/resp, Modulo 128 */
	/* PV */ 0x02, /* synchronous transmit */
	/* PI */ 0x06, /* Parameter Indicator - Rx I field length (bits) */
	/* PL */ 0x02, /* Parameter Length */

	// Erratum: The text does not say anything about the byte order for multibyte
	// numeric values.  In the example, we have two cases where 16 bit numbers are
	// sent with the more significant byte first.

	/* PV */ 0x04, /* Parameter Variable - 1024 bits (128 octets) */
	/* PV */ 0x00, /* */
	/* PI */ 0x08, /* Parameter Indicator - Rx window size */
	/* PL */ 0x01, /* Parameter length */
	/* PV */ 0x02, /* Parameter Variable - 2 frames */
	/* PI */ 0x09, /* Parameter Indicator - Timer T1 */
	/* PL */ 0x02, /* Parameter Length */
	/* PV */ 0x10, /* Parameter Variable - 4096 MSec */
	/* PV */ 0x00, /* */
	/* PI */ 0x0A, /* Parameter Indicator - Retries (N1) */
	/* PL */ 0x01, /* Parameter Length */
	/* PV */ 0x03, /* Parameter Variable - 3 retries */
}

func xid_test_main(t *testing.T) {
	t.Helper()

	// Assorted constants are #define-d in the C, so ends up as Go types when used here, but we want a specific C type
	var G_UNKNOWN = C.int(C.G_UNKNOWN)
	var modulo_128 = uint32(C.modulo_128)
	var modulo_8 = uint32(C.modulo_8)
	var modulo_unknown = uint32(C.modulo_unknown)
	var srej_single = uint32(C.srej_single)
	var srej_multi = uint32(C.srej_multi)
	var srej_not_specified = uint32(C.srej_not_specified)
	var srej_none = uint32(C.srej_none)

	/*
		struct xid_param_s param;
		struct xid_param_s param2;
		int n;
		unsigned char info[40];	// Currently max of 27 but things can change.
		char desc[150];		// I've seen 109.
	*/

	var desc [150]C.char
	var param C.struct_xid_param_s
	var param2 C.struct_xid_param_s
	var info [40]C.uchar

	/* parse example. */

	var n = C.xid_parse(&xid_example[0], C.int(len(xid_example)), &param, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, C.int(1), n)
	assert.Equal(t, C.int(0), param.full_duplex)
	assert.Equal(t, srej_single, param.srej)
	assert.Equal(t, modulo_128, param.modulo)
	assert.Equal(t, C.int(128), param.i_field_length_rx)
	assert.Equal(t, C.int(2), param.window_size_rx)
	assert.Equal(t, C.int(4096), param.ack_timer)
	assert.Equal(t, C.int(3), param.retries)

	/* encode and verify it comes out the same. */

	n = C.xid_encode(&param, &info[0], C.cr_cmd)
	assert.Equal(t, C.int(len(xid_example)), n)

	n = C.memcmp(unsafe.Pointer(&info[0]), unsafe.Pointer(&xid_example[0]), 27)
	assert.Equal(t, C.int(0), n, "n: %d, info: %v, xid_example[0]: %v", n, C.GoBytes(unsafe.Pointer(&info[0]), 27), C.GoBytes(unsafe.Pointer(&xid_example[0]), 27))

	/* try a couple different values, no srej. */

	param.full_duplex = 1
	param.srej = srej_none
	param.modulo = modulo_8
	param.i_field_length_rx = 2048
	param.window_size_rx = 3
	param.ack_timer = 1234
	param.retries = 12

	n = C.xid_encode(&param, &info[0], C.cr_cmd)
	C.xid_parse(&info[0], n, &param2, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, C.int(1), param2.full_duplex)
	assert.Equal(t, srej_none, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, C.int(2048), param2.i_field_length_rx)
	assert.Equal(t, C.int(3), param2.window_size_rx)
	assert.Equal(t, C.int(1234), param2.ack_timer)
	assert.Equal(t, C.int(12), param2.retries)

	/* Other values, single srej. */

	param.full_duplex = 0
	param.srej = srej_single
	param.modulo = modulo_8
	param.i_field_length_rx = 61
	param.window_size_rx = 4
	param.ack_timer = 5555
	param.retries = 9

	n = C.xid_encode(&param, &info[0], C.cr_cmd)
	C.xid_parse(&info[0], n, &param2, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, C.int(0), param2.full_duplex)
	assert.Equal(t, srej_single, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, C.int(61), param2.i_field_length_rx)
	assert.Equal(t, C.int(4), param2.window_size_rx)
	assert.Equal(t, C.int(5555), param2.ack_timer)
	assert.Equal(t, C.int(9), param2.retries)

	/* Other values, multi srej. */

	param.full_duplex = 0
	param.srej = srej_multi
	param.modulo = modulo_128
	param.i_field_length_rx = 61
	param.window_size_rx = 4
	param.ack_timer = 5555
	param.retries = 9

	n = C.xid_encode(&param, &info[0], C.cr_cmd)
	C.xid_parse(&info[0], n, &param2, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, C.int(0), param2.full_duplex)
	assert.Equal(t, srej_multi, param2.srej)
	assert.Equal(t, modulo_128, param2.modulo)
	assert.Equal(t, C.int(61), param2.i_field_length_rx)
	assert.Equal(t, C.int(4), param2.window_size_rx)
	assert.Equal(t, C.int(5555), param2.ack_timer)
	assert.Equal(t, C.int(9), param2.retries)

	/* Specify some and not others. */

	param.full_duplex = 0
	param.srej = srej_single
	param.modulo = modulo_8
	param.i_field_length_rx = G_UNKNOWN
	param.window_size_rx = G_UNKNOWN
	param.ack_timer = 999
	param.retries = G_UNKNOWN

	n = C.xid_encode(&param, &info[0], C.cr_cmd)
	C.xid_parse(&info[0], n, &param2, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, C.int(0), param2.full_duplex)
	assert.Equal(t, srej_single, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, G_UNKNOWN, param2.i_field_length_rx)
	assert.Equal(t, G_UNKNOWN, param2.window_size_rx)
	assert.Equal(t, C.int(999), param2.ack_timer)
	assert.Equal(t, G_UNKNOWN, param2.retries)

	/* Default values for empty info field. */

	n = 0
	C.xid_parse(&info[0], n, &param2, &desc[0], C.int(len(desc)))

	C.text_color_set(C.DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, C.GoString(&desc[0]))
	C.sleep(1)

	C.text_color_set(C.DW_COLOR_ERROR)

	assert.Equal(t, G_UNKNOWN, param2.full_duplex)
	assert.Equal(t, srej_not_specified, param2.srej)
	assert.Equal(t, modulo_unknown, param2.modulo)
	assert.Equal(t, G_UNKNOWN, param2.i_field_length_rx)
	assert.Equal(t, G_UNKNOWN, param2.window_size_rx)
	assert.Equal(t, G_UNKNOWN, param2.ack_timer)
	assert.Equal(t, G_UNKNOWN, param2.retries)

	C.text_color_set(C.DW_COLOR_REC)
	dw_printf("XID test:  Success.\n")
}
