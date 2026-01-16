package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <ctype.h>
// #include <assert.h>
// #include <stdarg.h>
import "C"

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func test_text2tt(t *testing.T, _text string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	var text = C.CString(_text)

	dw_printf("\nConvert from text \"%s\" to tone sequence.\n", _text)

	var _buttons [100]C.char
	var buttons = &_buttons[0]

	tt_text_to_multipress(text, 0, buttons)
	assert.Equal(t, _expect_mp, C.GoString(buttons), "Unexpected multi-press value for text %s", _text) //nolint:testifylint

	tt_text_to_two_key(text, 0, buttons)
	assert.Equal(t, _expect_2k, C.GoString(buttons), "Unexpected two-key value for text %s", _text) //nolint:testifylint

	tt_text_to_call10(text, 0, buttons)
	assert.Equal(t, _expect_c10, C.GoString(buttons), "Unexpected call10 value for text %s", _text) //nolint:testifylint

	tt_text_to_mhead(text, 0, buttons, 100)
	assert.Equal(t, _expect_loc, C.GoString(buttons), "Unexpected Maidenhead value for text %s", _text) //nolint:testifylint

	tt_text_to_satsq(text, 0, buttons, 100)
	assert.Equal(t, _expect_sat, C.GoString(buttons), "Unexpected SatSq value for text %s", _text) //nolint:testifylint
}

func test_tt2text(t *testing.T, _buttons string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	var buttons = C.CString(_buttons)

	dw_printf("\nConvert tone sequence \"%s\" to text.\n", _buttons)

	var _text [100]C.char
	var text = &_text[0]

	tt_multipress_to_text(buttons, 0, text)
	assert.Equal(t, _expect_mp, C.GoString(text), "Unexpected multi-press value for buttons %s", _buttons) //nolint:testifylint

	tt_two_key_to_text(buttons, 0, text)
	assert.Equal(t, _expect_2k, C.GoString(text), "Unexpected two-key value for buttons %s", _buttons) //nolint:testifylint

	tt_call10_to_text(buttons, 0, text)
	assert.Equal(t, _expect_c10, C.GoString(text), "Unexpected call10 value for buttons %s", _buttons) //nolint:testifylint

	tt_mhead_to_text(buttons, 0, text, 100)
	assert.Equal(t, _expect_loc, C.GoString(text), "Unexpected Maidenhead value for buttons %s", _buttons) //nolint:testifylint

	tt_satsq_to_text(buttons, 0, text)
	assert.Equal(t, _expect_sat, C.GoString(text), "Unexpected SatSq value for buttons %s", _buttons) //nolint:testifylint
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
