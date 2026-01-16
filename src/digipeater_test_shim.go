package direwolf

/*
Can't use cgo directly in test code, *can* use go code that uses cgo though, so here we are!
https://github.com/golang/go/issues/4030
*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <assert.h>
// #include <stdio.h>
// #include <ctype.h>
// #include "regex.h"
// #include <unistd.h>
// #include "ax25_pad.h"
// #include "digipeater.h"
// #include "tq.h"
// packet_t digipeat_match (int from_chan, packet_t pp, char *mycall_rec, char *mycall_xmit, regex_t *uidigi, regex_t *uitrace, int to_chan, enum preempt_e preempt, char *atgp, char *type_filter);
// char *mycall;
// regex_t alias_re;
// regex_t wide_re;
// int failed;
// enum preempt_e preempt = PREEMPT_OFF;
// char *config_atgp = "HOP";
import "C"

import (
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func digipeater_test(t *testing.T, _in, out string) {
	t.Helper()

	var in = C.CString(_in)

	dw_printf("\n")

	/*
	 * As an extra test, change text to internal format back to
	 * text again to make sure it comes out the same.
	 */
	var pp = ax25_from_text(in, 1)
	assert.NotNil(t, pp)

	var rec [256]C.char
	var pinfo *C.uchar
	ax25_format_addrs(pp, &rec[0])
	ax25_get_info(pp, &pinfo)
	C.strcat(&rec[0], (*C.char)(unsafe.Pointer(pinfo)))

	if C.strcmp(in, &rec[0]) != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Text/internal/text error-1 %s -> %s\n", C.GoString(in), C.GoString(&rec[0]))
	}

	/*
	 * Just for more fun, write as the frame format, read it back
	 * again, and make sure it is still the same.
	 */

	var frame [C.AX25_MAX_PACKET_LEN]C.uchar
	var frame_len = ax25_pack(pp, &frame[0])
	ax25_delete(pp)

	var alevel C.alevel_t
	alevel.rec = 50
	alevel.mark = 50
	alevel.space = 50

	pp = ax25_from_frame(&frame[0], frame_len, alevel)
	assert.NotNil(t, pp)
	ax25_format_addrs(pp, &rec[0])
	ax25_get_info(pp, &pinfo)
	C.strcat(&rec[0], (*C.char)(unsafe.Pointer(pinfo)))

	if C.strcmp(in, &rec[0]) != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf(
			"internal/frame/internal/text error-2 %s -> %s\n",
			C.GoString(in),
			C.GoString(&rec[0]),
		)
	}

	/*
	 * On with the digipeater test.
	 */

	text_color_set(DW_COLOR_REC)
	dw_printf("Rec\t%s\n", C.GoString(&rec[0]))

	//TODO:										  	             Add filtering to test.
	//											             V
	var result = digipeat_match(0, pp, C.mycall, C.mycall, &C.alias_re, &C.wide_re, 0, C.preempt, C.config_atgp, nil)

	var xmit [256]C.char
	if result != nil {
		dedupe_remember(result, 0)
		ax25_format_addrs(result, &xmit[0])
		ax25_get_info(result, &pinfo)
		C.strcat(&xmit[0], (*C.char)(unsafe.Pointer(pinfo)))
		ax25_delete(result)
	} else {
		C.strcpy(&xmit[0], C.CString(""))
	}

	text_color_set(DW_COLOR_XMIT)
	dw_printf("Xmit\t%s\n", C.GoString(&xmit[0]))

	if !assert.Equal(t, out, C.GoString(&xmit[0])) { //nolint:testifylint
		C.failed++
	}

	dw_printf("\n")
}

func digipeater_test_main(t *testing.T) bool {
	t.Helper()

	C.mycall = C.CString("WB2OSZ-9")

	dedupe_init(4 * time.Second)

	/*
	 * Compile the patterns.
	 */
	var e C.int
	var message *C.char
	e = C.regcomp(&C.alias_re, C.CString("^WIDE[4-7]-[1-7]|CITYD$"), C.REG_EXTENDED|C.REG_NOSUB)
	if e != 0 {
		C.regerror(e, &C.alias_re, message, 256)
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\n%s\n\n", C.GoString(message))
		C.exit(1)
	}

	e = C.regcomp(
		&C.wide_re,
		C.CString("^WIDE[1-7]-[1-7]$|^TRACE[1-7]-[1-7]$|^MA[1-7]-[1-7]$|^HOP[1-7]-[1-7]$"),
		C.REG_EXTENDED|C.REG_NOSUB,
	)
	if e != 0 {
		C.regerror(e, &C.wide_re, message, 256)
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\n%s\n\n", C.GoString(message))
		C.exit(1)
	}

	/*
	 * Let's start with the most basic cases.
	 */

	digipeater_test(t, "W1ABC>TEST01,TRACE3-3:",
		"W1ABC>TEST01,WB2OSZ-9*,TRACE3-2:")

	digipeater_test(t, "W1ABC>TEST02,WIDE3-3:",
		"W1ABC>TEST02,WB2OSZ-9*,WIDE3-2:")

	digipeater_test(t, "W1ABC>TEST03,WIDE3-2:",
		"W1ABC>TEST03,WB2OSZ-9*,WIDE3-1:")

	digipeater_test(t, "W1ABC>TEST04,WIDE3-1:",
		"W1ABC>TEST04,WB2OSZ-9*:")

	/*
	 * Look at edge case of maximum number of digipeaters.
	 */
	digipeater_test(t, "W1ABC>TEST11,R1,R2,R3,R4,R5,R6*,WIDE3-3:",
		"W1ABC>TEST11,R1,R2,R3,R4,R5,R6,WB2OSZ-9*,WIDE3-2:")

	digipeater_test(t, "W1ABC>TEST12,R1,R2,R3,R4,R5,R6,R7*,WIDE3-3:",
		"W1ABC>TEST12,R1,R2,R3,R4,R5,R6,R7*,WIDE3-2:")

	digipeater_test(t, "W1ABC>TEST13,R1,R2,R3,R4,R5,R6,R7*,WIDE3-1:",
		"W1ABC>TEST13,R1,R2,R3,R4,R5,R6,R7,WB2OSZ-9*:")

	/*
	 * "Trap" large values of "N" by repeating only once.
	 */
	digipeater_test(t, "W1ABC>TEST21,WIDE4-4:",
		"W1ABC>TEST21,WB2OSZ-9*:")

	digipeater_test(t, "W1ABC>TEST22,WIDE7-7:",
		"W1ABC>TEST22,WB2OSZ-9*:")

	/*
	 * Only values in range of 1 thru 7 are valid.
	 */
	digipeater_test(t, "W1ABC>TEST31,WIDE0-4:",
		"")

	digipeater_test(t, "W1ABC>TEST32,WIDE8-4:",
		"")

	digipeater_test(t, "W1ABC>TEST33,WIDE2:",
		"")

	/*
	 * and a few cases actually heard.
	 */

	digipeater_test(t, "WA1ENO>FN42ND,W1MV-1*,WIDE3-2:",
		"WA1ENO>FN42ND,W1MV-1,WB2OSZ-9*,WIDE3-1:")

	digipeater_test(t, "W1ON-3>BEACON:",
		"")

	digipeater_test(t, "W1CMD-9>TQ3Y8P,N1RCW-2,W1CLA-1,N8VIM,WIDE2*:",
		"")

	digipeater_test(t, "W1CLA-1>APX192,W1GLO-1,WIDE2*:",
		"")

	digipeater_test(t, "AC1U-9>T2TX4S,AC1U,WIDE1,N8VIM*,WIDE2-1:",
		"AC1U-9>T2TX4S,AC1U,WIDE1,N8VIM,WB2OSZ-9*:")

	/*
	 * Someone is still using the old style and will probably be disappointed.
	 */

	digipeater_test(t, "K1CPD-1>T2SR5R,RELAY*,WIDE,WIDE,SGATE,WIDE:",
		"")

	/*
	 * Change destination SSID to normal digipeater if none specified.  (Obsolete, removed.)
	 */

	digipeater_test(t, "W1ABC>TEST-3:",
		"")

	digipeater_test(t, "W1DEF>TEST-3,WIDE2-2:",
		"W1DEF>TEST-3,WB2OSZ-9*,WIDE2-1:")

	/*
	 * Drop duplicates within specified time interval.
	 * Only the first 1 of 3 should be retransmitted.
	 * The 4th case might be controversial.
	 */

	digipeater_test(t, "W1XYZ>TESTD,R1*,WIDE3-2:info1",
		"W1XYZ>TESTD,R1,WB2OSZ-9*,WIDE3-1:info1")

	digipeater_test(t, "W1XYZ>TESTD,R2*,WIDE3-2:info1",
		"")

	digipeater_test(t, "W1XYZ>TESTD,R3*,WIDE3-2:info1",
		"")

	digipeater_test(t, "W1XYZ>TESTD,R1*,WB2OSZ-9:has explicit routing",
		"W1XYZ>TESTD,R1,WB2OSZ-9*:has explicit routing")

	/*
	 * Allow same thing after adequate time.
	 */
	C.sleep(5)

	digipeater_test(t, "W1XYZ>TEST,R3*,WIDE3-2:info1",
		"W1XYZ>TEST,R3,WB2OSZ-9*,WIDE3-1:info1")

	/*
	 * Although source and destination match, the info field is different.
	 */

	digipeater_test(t, "W1XYZ>TEST,R1*,WIDE3-2:info4",
		"W1XYZ>TEST,R1,WB2OSZ-9*,WIDE3-1:info4")

	digipeater_test(t, "W1XYZ>TEST,R1*,WIDE3-2:info5",
		"W1XYZ>TEST,R1,WB2OSZ-9*,WIDE3-1:info5")

	digipeater_test(t, "W1XYZ>TEST,R1*,WIDE3-2:info6",
		"W1XYZ>TEST,R1,WB2OSZ-9*,WIDE3-1:info6")

	/*
	 * New in version 0.8.
	 * "Preemptive" digipeating looks ahead beyond the first unused digipeater.
	 */

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:off",
		"")

	C.preempt = C.PREEMPT_DROP

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:drop",
		"W1ABC>TEST11,WB2OSZ-9*,CITYE:drop")

	C.preempt = C.PREEMPT_MARK

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:mark1",
		"W1ABC>TEST11,CITYA,CITYB,CITYC,WB2OSZ-9*,CITYE:mark1")

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,WB2OSZ-9,CITYE:mark2",
		"W1ABC>TEST11,CITYA,CITYB,CITYC,WB2OSZ-9*,CITYE:mark2")

	C.preempt = C.PREEMPT_TRACE

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:trace1",
		"W1ABC>TEST11,CITYA,WB2OSZ-9*,CITYE:trace1")

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD:trace2",
		"W1ABC>TEST11,CITYA,WB2OSZ-9*:trace2")

	digipeater_test(t, "W1ABC>TEST11,CITYB,CITYC,CITYD:trace3",
		"W1ABC>TEST11,WB2OSZ-9*:trace3")

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYW,CITYX,CITYY,CITYZ:nomatch",
		"")

	/*
	 * Did I miss any cases?
	 * Yes.  Don't retransmit my own.  1.4H
	 */

	digipeater_test(t, "WB2OSZ-7>TEST14,WIDE1-1,WIDE1-1:stuff",
		"WB2OSZ-7>TEST14,WB2OSZ-9*,WIDE1-1:stuff")

	digipeater_test(t, "WB2OSZ-9>TEST14,WIDE1-1,WIDE1-1:from myself",
		"")

	digipeater_test(t, "WB2OSZ-9>TEST14,WIDE1-1*,WB2OSZ-9:from myself but explicit routing",
		"WB2OSZ-9>TEST14,WIDE1-1,WB2OSZ-9*:from myself but explicit routing")

	digipeater_test(t, "WB2OSZ-15>TEST14,WIDE1-1,WIDE1-1:stuff",
		"WB2OSZ-15>TEST14,WB2OSZ-9*,WIDE1-1:stuff")

	// New in 1.7 - ATGP Hack

	C.preempt = C.PREEMPT_OFF // Shouldn't make a difference here.

	digipeater_test(t, "W1ABC>TEST51,HOP7-7,HOP7-7:stuff1",
		"W1ABC>TEST51,WB2OSZ-9*,HOP7-6,HOP7-7:stuff1")

	digipeater_test(t, "W1ABC>TEST52,ABCD*,HOP7-1,HOP7-7:stuff2",
		"W1ABC>TEST52,WB2OSZ-9,HOP7*,HOP7-7:stuff2") // Used up address remains.

	digipeater_test(t, "W1ABC>TEST53,HOP7*,HOP7-7:stuff3",
		"W1ABC>TEST53,WB2OSZ-9*,HOP7-6:stuff3") // But it gets removed here.

	digipeater_test(t, "W1ABC>TEST54,HOP7*,HOP7-1:stuff4",
		"W1ABC>TEST54,WB2OSZ-9,HOP7*:stuff4") // Remains again here.

	digipeater_test(t, "W1ABC>TEST55,HOP7,HOP7*:stuff5",
		"")

	// Examples given for desired result.

	C.mycall = C.CString("CLNGMN-1")
	digipeater_test(t, "W1ABC>TEST60,HOP7-7,HOP7-7:",
		"W1ABC>TEST60,CLNGMN-1*,HOP7-6,HOP7-7:")
	digipeater_test(t, "W1ABC>TEST61,ROAN-3*,HOP7-6,HOP7-7:",
		"W1ABC>TEST61,CLNGMN-1*,HOP7-5,HOP7-7:")

	C.mycall = C.CString("GDHILL-8")
	digipeater_test(t, "W1ABC>TEST62,MDMTNS-7*,HOP7-1,HOP7-7:",
		"W1ABC>TEST62,GDHILL-8,HOP7*,HOP7-7:")
	digipeater_test(t, "W1ABC>TEST63,CAMLBK-9*,HOP7-1,HOP7-7:",
		"W1ABC>TEST63,GDHILL-8,HOP7*,HOP7-7:")

	C.mycall = C.CString("MDMTNS-7")
	digipeater_test(t, "W1ABC>TEST64,GDHILL-8*,HOP7*,HOP7-7:",
		"W1ABC>TEST64,MDMTNS-7*,HOP7-6:")

	C.mycall = C.CString("CAMLBK-9")
	digipeater_test(t, "W1ABC>TEST65,GDHILL-8,HOP7*,HOP7-7:",
		"W1ABC>TEST65,CAMLBK-9*,HOP7-6:")

	C.mycall = C.CString("KATHDN-15")
	digipeater_test(t, "W1ABC>TEST66,MTWASH-14*,HOP7-1:",
		"W1ABC>TEST66,KATHDN-15,HOP7*:")

	C.mycall = C.CString("SPRNGR-1")
	digipeater_test(t, "W1ABC>TEST67,CLNGMN-1*,HOP7-1:",
		"W1ABC>TEST67,SPRNGR-1,HOP7*:")

	if C.failed == 0 {
		dw_printf("SUCCESS -- All digipeater tests passed.\n")
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - %d digipeater tests failed.\n", C.failed)
		t.Fail()
	}

	return (C.failed != 0)
} /* end main */
