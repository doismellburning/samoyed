package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A UI frame with an empty information field, as sent by linbpq ID broadcasts
// (issue #504), used to trip an assertion in the type filter.
func Test_pfilter_empty_info(t *testing.T) {
	var p_igate_config igate_config_s
	pfilter_init(&p_igate_config, 0)

	deviceIDData = NewDeviceIDData()

	var pp = AX25FromText("Q1TEST>ID:", true)
	require.NotNil(t, pp)

	var result, err = pfilter(0, 0, "t/p", pp, true)

	require.NoError(t, err)
	assert.Equal(t, 0, result, "a frame with no information field matches no packet type")
}

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

	t.Run("a type letter with nothing after it returns an error", func(t *testing.T) {
		// Each of these reaches a different arm of parse_filter_spec's chain,
		// and every one of them used to index past the end of the token.
		for _, filter := range []string{"b", "d", "v", "u", "o", "g", "t", "r", "s", "i"} {
			assert.Error(t, pfilter_validate(0, 0, filter, true), "filter %q", filter)
		}
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

// Test_pfilter_igate_message_filter_conditions covers both of the "i" filter's
// conditions against a populated heard list.  Regression test for #660, where
// condition 1 was inverted, so a message was dropped precisely when its
// addressee had been heard nearby recently.
func Test_pfilter_igate_message_filter_conditions(t *testing.T) {
	var p_igate_config igate_config_s
	p_igate_config.max_digi_hops = 2
	pfilter_init(&p_igate_config, 0)

	// Q2TEST is about 4 km from 42.6 -71.3.
	const q2testPosition = "Q2TEST>APDW17:!4237.14NS07120.83W#"

	// hearRF makes the heard list believe the given monitor line just arrived
	// over the radio.
	var hearRF = func(t *testing.T, monitor string) {
		t.Helper()

		var pp = AX25FromText(monitor, true)
		require.NotNil(t, pp)

		var alevel ALevel

		mheardDB.SaveRF(0, decode_aprs(pp, true, ""), pp, alevel, BitFixNone)
	}

	var testCases = []struct {
		name     string
		filter   string
		heard    []string
		message  string
		expected int
	}{
		{
			// Condition 1 met, condition 2 met: the addressee is nearby and we
			// cannot hear the sender, so we are worth the trouble of relaying.
			name:     "addressee heard and sender not heard passes",
			filter:   "i/30",
			heard:    []string{q2testPosition},
			message:  "Q3TEST>APDW17::Q2TEST   :Happy Birthday{001",
			expected: 1,
		},
		{
			// Condition 1 not met: we have no reason to think the addressee
			// can hear us.
			name:     "addressee not heard drops",
			filter:   "i/30",
			heard:    []string{"Q1TEST>APDW17:!4237.14NS07120.83W#"},
			message:  "Q3TEST>APDW17::Q2TEST   :Happy Birthday{001",
			expected: 0,
		},
		{
			// Condition 2 not met: the sender is on RF too, so the addressee
			// stands a chance of hearing it without us.
			name:     "sender heard over RF drops",
			filter:   "i/30",
			heard:    []string{q2testPosition, "Q3TEST>APDW17:!4237.14NS07120.83W#"},
			message:  "Q3TEST>APDW17::Q2TEST   :Happy Birthday{001",
			expected: 0,
		},
		{
			name:     "addressee heard within distance passes",
			filter:   "i/30/2/42.6/-71.3/10",
			heard:    []string{q2testPosition},
			message:  "Q3TEST>APDW17::Q2TEST   :Happy Birthday{001",
			expected: 1,
		},
		{
			name:     "addressee heard too far away drops",
			filter:   "i/30/2/42.6/-71.3/1",
			heard:    []string{q2testPosition},
			message:  "Q3TEST>APDW17::Q2TEST   :Happy Birthday{001",
			expected: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var saved_mheardDB = mheardDB
			mheardDB = NewMHeardDB(0)

			defer func() { mheardDB = saved_mheardDB }()

			for _, monitor := range tc.heard {
				hearRF(t, monitor)
			}

			var message = AX25FromText(tc.message, true)
			require.NotNil(t, message)

			var result, err = pfilter(0, 0, tc.filter, message, true)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
