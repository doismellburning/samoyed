package direwolf

// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <unistd.h>
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

/* From Figure 4.6. Typical XID frame, from AX.25 protocol spec, v. 2.2 */
/* This is the info part after a control byte of 0xAF. */

var xid_example = []byte{

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

	/*
		struct xid_param_s param;
		struct xid_param_s param2;
		int n;
		unsigned char info[40];	// Currently max of 27 but things can change.
		char desc[150];		// I've seen 109.
	*/

	var param2 *xid_param_s

	/* parse example. */

	var param, desc, n = xid_parse(xid_example)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, 1, n)
	assert.Equal(t, 0, param.full_duplex)
	assert.Equal(t, srej_single, param.srej)
	assert.Equal(t, modulo_128, param.modulo)
	assert.Equal(t, 128, param.i_field_length_rx)
	assert.Equal(t, 2, param.window_size_rx)
	assert.Equal(t, 4096, param.ack_timer)
	assert.Equal(t, 3, param.retries)

	/* encode and verify it comes out the same. */

	var info = xid_encode(param, cr_cmd)
	assert.Len(t, info, len(xid_example))

	assert.Equal(t, info, xid_example, "n: %d, info: %v, xid_example[0]: %v", n, info, xid_example)

	/* try a couple different values, no srej. */

	param.full_duplex = 1
	param.srej = srej_none
	param.modulo = modulo_8
	param.i_field_length_rx = 2048
	param.window_size_rx = 3
	param.ack_timer = 1234
	param.retries = 12

	info = xid_encode(param, cr_cmd)
	param2, desc, _ = xid_parse(info)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, 1, param2.full_duplex)
	assert.Equal(t, srej_none, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, 2048, param2.i_field_length_rx)
	assert.Equal(t, 3, param2.window_size_rx)
	assert.Equal(t, 1234, param2.ack_timer)
	assert.Equal(t, 12, param2.retries)

	/* Other values, single srej. */

	param.full_duplex = 0
	param.srej = srej_single
	param.modulo = modulo_8
	param.i_field_length_rx = 61
	param.window_size_rx = 4
	param.ack_timer = 5555
	param.retries = 9

	info = xid_encode(param, cr_cmd)
	param2, desc, _ = xid_parse(info)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, 0, param2.full_duplex)
	assert.Equal(t, srej_single, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, 61, param2.i_field_length_rx)
	assert.Equal(t, 4, param2.window_size_rx)
	assert.Equal(t, 5555, param2.ack_timer)
	assert.Equal(t, 9, param2.retries)

	/* Other values, multi srej. */

	param.full_duplex = 0
	param.srej = srej_multi
	param.modulo = modulo_128
	param.i_field_length_rx = 61
	param.window_size_rx = 4
	param.ack_timer = 5555
	param.retries = 9

	info = xid_encode(param, cr_cmd)
	param2, desc, _ = xid_parse(info)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, 0, param2.full_duplex)
	assert.Equal(t, srej_multi, param2.srej)
	assert.Equal(t, modulo_128, param2.modulo)
	assert.Equal(t, 61, param2.i_field_length_rx)
	assert.Equal(t, 4, param2.window_size_rx)
	assert.Equal(t, 5555, param2.ack_timer)
	assert.Equal(t, 9, param2.retries)

	/* Specify some and not others. */

	param.full_duplex = 0
	param.srej = srej_single
	param.modulo = modulo_8
	param.i_field_length_rx = G_UNKNOWN
	param.window_size_rx = G_UNKNOWN
	param.ack_timer = 999
	param.retries = G_UNKNOWN

	info = xid_encode(param, cr_cmd)
	param2, desc, _ = xid_parse(info)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, 0, param2.full_duplex)
	assert.Equal(t, srej_single, param2.srej)
	assert.Equal(t, modulo_8, param2.modulo)
	assert.Equal(t, G_UNKNOWN, param2.i_field_length_rx)
	assert.Equal(t, G_UNKNOWN, param2.window_size_rx)
	assert.Equal(t, 999, param2.ack_timer)
	assert.Equal(t, G_UNKNOWN, param2.retries)

	/* Default values for empty info field. */

	info = []byte{}
	param2, desc, _ = xid_parse(info)

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("%d: %s\n", 0, desc)
	C.sleep(1)

	text_color_set(DW_COLOR_ERROR)

	assert.Equal(t, G_UNKNOWN, param2.full_duplex)
	assert.Equal(t, srej_not_specified, param2.srej)
	assert.Equal(t, modulo_unknown, param2.modulo)
	assert.Equal(t, G_UNKNOWN, param2.i_field_length_rx)
	assert.Equal(t, G_UNKNOWN, param2.window_size_rx)
	assert.Equal(t, G_UNKNOWN, param2.ack_timer)
	assert.Equal(t, G_UNKNOWN, param2.retries)

	text_color_set(DW_COLOR_REC)
	dw_printf("XID test:  Success.\n")
}
