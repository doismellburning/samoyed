package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <ctype.h>
// #include <assert.h>
// #include <stdarg.h>
// #include "textcolor.h"
// #include "tt_text.h"
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func test_text2tt(t *testing.T, _text string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	var text = C.CString(_text)
	var expect_mp = C.CString(_expect_mp)
	var expect_2k = C.CString(_expect_2k)
	var expect_c10 = C.CString(_expect_c10)
	var expect_loc = C.CString(_expect_loc)
	var expect_sat = C.CString(_expect_sat)

	dw_printf("\nConvert from text \"%s\" to tone sequence.\n", _text)

	var _buttons [100]C.char
	var buttons = &_buttons[0]

	C.tt_text_to_multipress(text, 0, buttons)
	assert.Equal(t, C.int(0), C.strcmp(buttons, expect_mp), "Unexpected multi-press value") //nolint:testifylint

	C.tt_text_to_two_key(text, 0, buttons)
	assert.Equal(t, C.int(0), C.strcmp(buttons, expect_2k), "Unexpected two-key value") //nolint:testifylint

	C.tt_text_to_call10(text, 0, buttons)
	assert.Equal(t, C.int(0), C.strcmp(buttons, expect_c10), "Unexpected call10 value") //nolint:testifylint

	C.tt_text_to_mhead(text, 0, buttons, 100)
	assert.Equal(t, C.int(0), C.strcmp(buttons, expect_loc), "Unexpected Maidenhead value") //nolint:testifylint

	C.tt_text_to_satsq(text, 0, buttons, 100)
	assert.Equal(t, C.int(0), C.strcmp(buttons, expect_sat), "Unexpected SatSq value") //nolint:testifylint
}

func test_tt2text(t *testing.T, _buttons string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	var buttons = C.CString(_buttons)
	var expect_mp = C.CString(_expect_mp)
	var expect_2k = C.CString(_expect_2k)
	var expect_c10 = C.CString(_expect_c10)
	var expect_loc = C.CString(_expect_loc)
	var expect_sat = C.CString(_expect_sat)

	dw_printf("\nConvert tone sequence \"%s\" to text.\n", _buttons)

	var _text [100]C.char
	var text = &_text[0]

	C.tt_multipress_to_text(buttons, 0, text)
	assert.Equal(t, C.int(0), C.strcmp(text, expect_mp), "Unexpected multi-press value") //nolint:testifylint

	C.tt_two_key_to_text(buttons, 0, text)
	assert.Equal(t, C.int(0), C.strcmp(text, expect_2k), "Unexpected two-key value") //nolint:testifylint

	C.tt_call10_to_text(buttons, 0, text)
	assert.Equal(t, C.int(0), C.strcmp(text, expect_c10), "Unexpected call10 value") //nolint:testifylint

	C.tt_mhead_to_text(buttons, 0, text, 100)
	assert.Equal(t, C.int(0), C.strcmp(text, expect_loc), "Unexpected Maidenhead value") //nolint:testifylint

	C.tt_satsq_to_text(buttons, 0, text)
	assert.Equal(t, C.int(0), C.strcmp(text, expect_sat), "Unexpected SatSq value") //nolint:testifylint
}

func tt_text_test_main(t *testing.T) {
	t.Helper()

	dw_printf("Test conversions between normal text and DTMF representation.\n")
	dw_printf("Some error messages are normal.  Just look for number of errors at end.\n")

	/* original text   multipress                         two-key                 call10        mhead         satsq */

	test_text2tt(t, "abcdefg 0123", "2A22A2223A33A33340A00122223333", "2A2B2C3A3B3C4A0A0123", "", "", "")

	test_text2tt(t, "WB4APR", "922444427A777", "9A2B42A7A7C", "9242771558", "", "")

	test_text2tt(t, "EM29QE78", "3362222999997733777778888", "3B6A297B3B78", "", "326129723278", "")

	test_text2tt(t, "FM19", "3336199999", "3C6A19", "3619003333", "336119", "1819")

	/* tone_seq                          multipress       two-key                     call10        mhead         satsq */

	test_tt2text(t, "2A22A2223A33A33340A00122223333", "ABCDEFG 0123", "A2A222D3D3334 00122223333", "", "", "")

	test_tt2text(t, "9242771558", "WAGAQ1KT", "9242771558", "WB4APR", "", "")

	test_tt2text(t, "326129723278", "DAM1AWPADAPT", "326129723278", "", "EM29QE78", "")

	test_tt2text(t, "1819", "1T1W", "1819", "", "", "FM19")
}
