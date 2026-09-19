package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_pfilter_validate(t *testing.T) {
	var p_igate_config igate_config_s
	p_igate_config.max_digi_hops = 2
	pfilter_init(&p_igate_config, 0)

	t.Run("valid APRS filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate(0, 0, "t/p & b/Q1TEST", true))
	})

	t.Run("valid connected-mode filter returns no error", func(t *testing.T) {
		assert.NoError(t, pfilter_validate(0, 0, "b/Q1TEST", false))
	})

	t.Run("bad wildcard placement returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "b/Q1TEST*Q2TEST", true))
	})

	t.Run("unrecognized filter type returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "x/", true))
	})

	t.Run("filter type not allowed in connected mode returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "t/p", false))
	})

	t.Run("unbalanced parentheses returns an error", func(t *testing.T) {
		assert.Error(t, pfilter_validate(0, 0, "t/w & ( t/w | t/w ", true))
	})

	t.Run("error message reflects the real from/to channels", func(t *testing.T) {
		var err = pfilter_validate(1, 2, "x/", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "filter[1,2]")
	})
}

// Test_pfilter_igate_message_filter_is_evaluated checks that the syntax-only
// shortcut taken by pfilter_validate (and by the ported Dire Wolf filter
// tests) does not leak into a real packet's evaluation: an "i" filter must
// still consult the heard list, rather than passing everything.
func Test_pfilter_igate_message_filter_is_evaluated(t *testing.T) {
	var p_igate_config igate_config_s
	p_igate_config.max_digi_hops = 2
	pfilter_init(&p_igate_config, 0)

	var saved_mheardDB = mheardDB
	mheardDB = NewMHeardDB(0)

	defer func() { mheardDB = saved_mheardDB }()

	// Q1TEST has just been heard directly over the radio, and nothing at all
	// has been heard from the addressee Q2TEST, so the filter has every reason
	// to drop this message rather than pass it.
	var heard = AX25FromText("Q1TEST>APDW17:!4237.14NS07120.83W#", true)
	require.NotNil(t, heard)

	var alevel ALevel

	mheardDB.SaveRF(0, decode_aprs(heard, true, ""), heard, alevel, BitFixNone)

	var message = AX25FromText("Q1TEST>APDW17::Q2TEST   :Happy Birthday{001", true)
	require.NotNil(t, message)

	var result, err = pfilter(0, 0, "i/30", message, true)
	require.NoError(t, err)
	assert.Equal(t, 0, result, "an i/ filter should consult the heard list, not pass everything")

	// The same expression against the same packet is only parsed, not
	// evaluated, in syntax-only mode.
	var syntaxResult, syntaxErr = pfilter_eval(0, 0, "i/30", message, true, true)
	require.NoError(t, syntaxErr)
	assert.Equal(t, 1, syntaxResult)
}
