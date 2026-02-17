package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func test_text2tt(t *testing.T, text string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	dw_printf("\nConvert from text \"%s\" to tone sequence.\n", text)

	var buttons string

	buttons, _ = tt_text_to_multipress(text, false)
	assert.Equal(t, _expect_mp, buttons, "Unexpected multi-press value for text %s", text) //nolint:testifylint

	buttons, _ = tt_text_to_two_key(text, false)
	assert.Equal(t, _expect_2k, buttons, "Unexpected two-key value for text %s", text) //nolint:testifylint

	buttons, _ = tt_text_to_call10(text, false)
	assert.Equal(t, _expect_c10, buttons, "Unexpected call10 value for text %s", text) //nolint:testifylint

	buttons, _ = tt_text_to_mhead(text, false)
	assert.Equal(t, _expect_loc, buttons, "Unexpected Maidenhead value for text %s", text) //nolint:testifylint

	buttons, _ = tt_text_to_satsq(text, false)
	assert.Equal(t, _expect_sat, buttons, "Unexpected SatSq value for text %s", text) //nolint:testifylint
}

func test_tt2text(t *testing.T, buttons string, _expect_mp string, _expect_2k string, _expect_c10 string, _expect_loc string, _expect_sat string) {
	t.Helper()

	dw_printf("\nConvert tone sequence \"%s\" to text.\n", buttons)

	var text string

	text, _ = tt_multipress_to_text(buttons, false)
	assert.Equal(t, _expect_mp, text, "Unexpected multi-press value for buttons %s", buttons) //nolint:testifylint

	text, _ = tt_two_key_to_text(buttons, false)
	assert.Equal(t, _expect_2k, text, "Unexpected two-key value for buttons %s", buttons) //nolint:testifylint

	text, _ = tt_call10_to_text(buttons, false)
	assert.Equal(t, _expect_c10, text, "Unexpected call10 value for buttons %s", buttons) //nolint:testifylint

	text, _ = tt_mhead_to_text(buttons, false)
	assert.Equal(t, _expect_loc, text, "Unexpected Maidenhead value for buttons %s", buttons) //nolint:testifylint

	text, _ = tt_satsq_to_text(buttons, false)
	assert.Equal(t, _expect_sat, text, "Unexpected SatSq value for buttons %s", buttons) //nolint:testifylint
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
