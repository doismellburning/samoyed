package direwolf

/*
Can't use cgo directly in test code, *can* use go code that uses cgo though, so here we are!
https://github.com/golang/go/issues/4030
*/

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var digipeaterTestMyCall string
var digipeaterTestAliasRegexp *regexp.Regexp
var digipeaterTestWideRegexp *regexp.Regexp
var digipeaterTestConfigATGP = "HOP"
var digipeaterTestFailed = 0
var preempt = PREEMPT_OFF

func digipeater_test(t *testing.T, in, out string) {
	t.Helper()

	dw_printf("\n")

	/*
	 * As an extra test, change text to internal format back to
	 * text again to make sure it comes out the same.
	 */
	var pp = ax25_from_text(in, true)
	assert.NotNil(t, pp)

	var rec = ax25_format_addrs(pp)
	var pinfo = ax25_get_info(pp)
	rec += string(pinfo)

	if in != rec {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Text/internal/text error-1 %s -> %s\n", in, rec)
	}

	/*
	 * Just for more fun, write as the frame format, read it back
	 * again, and make sure it is still the same.
	 */

	var frame = ax25_pack(pp)
	ax25_delete(pp)

	var alevel alevel_t
	alevel.rec = 50
	alevel.mark = 50
	alevel.space = 50

	pp = ax25_from_frame(frame, alevel)
	assert.NotNil(t, pp)
	rec = ax25_format_addrs(pp)
	pinfo = ax25_get_info(pp)
	rec += string(pinfo)

	if in != rec {
		text_color_set(DW_COLOR_ERROR)
		dw_printf(
			"internal/frame/internal/text error-2 %s -> %s\n",
			in,
			rec,
		)
	}

	/*
	 * On with the digipeater test.
	 */

	text_color_set(DW_COLOR_REC)
	dw_printf("Rec\t%s\n", rec)

	//TODO:										  	             Add filtering to test.
	//											             V
	var result = digipeat_match(0, pp, digipeaterTestMyCall, digipeaterTestMyCall, digipeaterTestAliasRegexp, digipeaterTestWideRegexp, 0, preempt, digipeaterTestConfigATGP, "")

	var xmit string
	if result != nil {
		dedupe_remember(result, 0)
		xmit = ax25_format_addrs(result)
		pinfo = ax25_get_info(result)
		xmit += string(pinfo)
		ax25_delete(result)
	}

	text_color_set(DW_COLOR_XMIT)
	dw_printf("Xmit\t%s\n", xmit)

	if !assert.Equal(t, out, xmit) { //nolint:testifylint
		digipeaterTestFailed++
	}

	dw_printf("\n")
}

func digipeater_test_main(t *testing.T) bool {
	t.Helper()

	digipeaterTestMyCall = "WB2OSZ-9"

	dedupe_init(4 * time.Second)

	/*
	 * Compile the patterns.
	 */
	digipeaterTestAliasRegexp = regexp.MustCompile("^WIDE[4-7]-[1-7]|CITYD$")
	digipeaterTestWideRegexp = regexp.MustCompile("^WIDE[1-7]-[1-7]$|^TRACE[1-7]-[1-7]$|^MA[1-7]-[1-7]$|^HOP[1-7]-[1-7]$")

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
	time.Sleep(5 * time.Second)

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

	preempt = PREEMPT_DROP

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:drop",
		"W1ABC>TEST11,WB2OSZ-9*,CITYE:drop")

	preempt = PREEMPT_MARK

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,CITYD,CITYE:mark1",
		"W1ABC>TEST11,CITYA,CITYB,CITYC,WB2OSZ-9*,CITYE:mark1")

	digipeater_test(t, "W1ABC>TEST11,CITYA*,CITYB,CITYC,WB2OSZ-9,CITYE:mark2",
		"W1ABC>TEST11,CITYA,CITYB,CITYC,WB2OSZ-9*,CITYE:mark2")

	preempt = PREEMPT_TRACE

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

	preempt = PREEMPT_OFF // Shouldn't make a difference here.

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

	digipeaterTestMyCall = "CLNGMN-1"
	digipeater_test(t, "W1ABC>TEST60,HOP7-7,HOP7-7:",
		"W1ABC>TEST60,CLNGMN-1*,HOP7-6,HOP7-7:")
	digipeater_test(t, "W1ABC>TEST61,ROAN-3*,HOP7-6,HOP7-7:",
		"W1ABC>TEST61,CLNGMN-1*,HOP7-5,HOP7-7:")

	digipeaterTestMyCall = "GDHILL-8"
	digipeater_test(t, "W1ABC>TEST62,MDMTNS-7*,HOP7-1,HOP7-7:",
		"W1ABC>TEST62,GDHILL-8,HOP7*,HOP7-7:")
	digipeater_test(t, "W1ABC>TEST63,CAMLBK-9*,HOP7-1,HOP7-7:",
		"W1ABC>TEST63,GDHILL-8,HOP7*,HOP7-7:")

	digipeaterTestMyCall = "MDMTNS-7"
	digipeater_test(t, "W1ABC>TEST64,GDHILL-8*,HOP7*,HOP7-7:",
		"W1ABC>TEST64,MDMTNS-7*,HOP7-6:")

	digipeaterTestMyCall = "CAMLBK-9"
	digipeater_test(t, "W1ABC>TEST65,GDHILL-8,HOP7*,HOP7-7:",
		"W1ABC>TEST65,CAMLBK-9*,HOP7-6:")

	digipeaterTestMyCall = "KATHDN-15"
	digipeater_test(t, "W1ABC>TEST66,MTWASH-14*,HOP7-1:",
		"W1ABC>TEST66,KATHDN-15,HOP7*:")

	digipeaterTestMyCall = "SPRNGR-1"
	digipeater_test(t, "W1ABC>TEST67,CLNGMN-1*,HOP7-1:",
		"W1ABC>TEST67,SPRNGR-1,HOP7*:")

	if digipeaterTestFailed == 0 {
		dw_printf("SUCCESS -- All digipeater tests passed.\n")
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - %d digipeater tests failed.\n", digipeaterTestFailed)
		t.Fail()
	}

	return (digipeaterTestFailed != 0)
} /* end main */
