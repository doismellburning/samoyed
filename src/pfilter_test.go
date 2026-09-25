package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
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

// An "i" filter asks the heard-recently database about the addressee, and
// pfilter runs in places - config-file validation, and callers that are not
// the TNC - that can get there before the database exists.  An absent one
// answers as an empty one does rather than bringing the program down.
func Test_pfilter_igate_without_a_heard_database(t *testing.T) {
	var p_igate_config igate_config_s
	pfilter_init(&p_igate_config, 0)

	deviceIDData = NewDeviceIDData()

	var saved_mheardDB = mheardDB
	mheardDB = nil

	defer func() { mheardDB = saved_mheardDB }()

	var pp = AX25FromText("Q1TEST>APDW17::Q2TEST   :Hello", true)
	require.NotNil(t, pp)

	var result, err = pfilter(MAX_TOTAL_CHANS, 0, "i/60/0/51.5/-0.1/50", pp, true)

	require.NoError(t, err)
	assert.Equal(t, 0, result, "an absent database has heard nothing, as an empty one would")
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

// A position report, a message, a status and a weather report, so a test can
// pick a packet that a given filter has an opinion about.
const (
	pfilterTestPositionPacket = "Q1TEST>APDW17:!4237.14NS07120.83W#PHG7130Chelmsford MA"
	pfilterTestMessagePacket  = "Q1TEST>APDW17::Q2TEST   :Hello"
	pfilterTestStatusPacket   = "Q2TEST>APDW17,WIDE1-1*:>Status text"
	pfilterTestWeatherPacket  = "Q2TEST>APDW17:!4237.14N/07120.83W_180/005g010t077"
)

// pfilterMonitorLines runs a filter over a handful of packets, reporting each
// verdict as a pass/drop bool.
func pfilterMonitorLines(t *testing.T, from_chan int, to_chan int, filter string, is_aprs bool, monitorLines []string) []bool {
	t.Helper()

	var verdicts []bool

	for _, monitorLine := range monitorLines {
		var pass, err = PfilterMonitorLine(from_chan, to_chan, filter, is_aprs, monitorLine)

		require.NoError(t, err)

		verdicts = append(verdicts, pass)
	}

	return verdicts
}

func Test_PfilterMonitorLine(t *testing.T) {
	PfilterStandaloneInit(0)

	var testCases = []struct {
		name     string
		filter   string
		packets  []string
		expected []bool
	}{
		{
			name:     "budlist matches the source address",
			filter:   "b/Q1TEST",
			packets:  []string{pfilterTestPositionPacket, pfilterTestStatusPacket},
			expected: []bool{true, false},
		},
		{
			name:     "budlist wildcard matches a prefix",
			filter:   "b/Q*",
			packets:  []string{pfilterTestPositionPacket, pfilterTestStatusPacket},
			expected: []bool{true, true},
		},
		{
			name:     "packet type selects messages",
			filter:   "t/m",
			packets:  []string{pfilterTestMessagePacket, pfilterTestPositionPacket},
			expected: []bool{true, false},
		},
		{
			name:     "packet type selects weather",
			filter:   "t/w",
			packets:  []string{pfilterTestWeatherPacket, pfilterTestPositionPacket},
			expected: []bool{true, false},
		},
		{
			name:     "digipeater filter matches a used digipeater",
			filter:   "d/WIDE1-1",
			packets:  []string{pfilterTestStatusPacket, pfilterTestPositionPacket},
			expected: []bool{true, false},
		},
		{
			name:     "negation inverts a specification",
			filter:   "! b/Q1TEST",
			packets:  []string{pfilterTestPositionPacket, pfilterTestStatusPacket},
			expected: []bool{false, true},
		},
		{
			name:     "or passes either side",
			filter:   "b/Q1TEST | t/s",
			packets:  []string{pfilterTestPositionPacket, pfilterTestStatusPacket, pfilterTestWeatherPacket},
			expected: []bool{true, true, false},
		},
		{
			name:     "and needs both sides",
			filter:   "b/Q2TEST & t/s",
			packets:  []string{pfilterTestStatusPacket, pfilterTestWeatherPacket, pfilterTestMessagePacket},
			expected: []bool{true, false, false},
		},
		{
			name:     "range filter measures from a point",
			filter:   "r/42.62/-71.34/10",
			packets:  []string{pfilterTestPositionPacket},
			expected: []bool{true},
		},
		{
			name:     "range filter rejects a distant point",
			filter:   "r/51.5/-0.1/10",
			packets:  []string{pfilterTestPositionPacket},
			expected: []bool{false},
		},
		{
			name:     "an empty filter rejects everything",
			filter:   "",
			packets:  []string{pfilterTestPositionPacket, pfilterTestMessagePacket},
			expected: []bool{false, false},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, pfilterMonitorLines(t, 0, 0, testCase.filter, true, testCase.packets))
		})
	}
}

func Test_PfilterMonitorLine_connectedMode(t *testing.T) {
	PfilterStandaloneInit(0)

	var packets = []string{pfilterTestPositionPacket, pfilterTestStatusPacket}

	assert.Equal(t, []bool{true, false}, pfilterMonitorLines(t, 0, 0, "b/Q1TEST", false, packets))
}

func Test_PfilterMonitorLine_unparseablePacket(t *testing.T) {
	PfilterStandaloneInit(0)

	// AX25FromText has plenty to say about a line it cannot parse, and says it
	// on stdout, so let it.
	var pass, err = false, error(nil)

	testutils.CaptureOutput(t, func() { pass, err = PfilterMonitorLine(0, 0, "b/Q1TEST", true, "this is not a packet") })

	assert.False(t, pass)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse monitoring format input")
}

func Test_PfilterMonitorLine_igateFilterWithNothingHeard(t *testing.T) {
	// PfilterStandaloneInit gives the filter engine an empty "heard recently"
	// database, and an "i" filter gates a message only to an addressee that
	// has been heard, so there is nothing here it will pass.
	PfilterStandaloneInit(0)

	var packets = []string{pfilterTestMessagePacket, pfilterTestStatusPacket}

	var verdicts []bool

	var output = testutils.CaptureOutput(t, func() {
		verdicts = pfilterMonitorLines(t, MAX_TOTAL_CHANS, 0, "i/60/0/51.5/-0.1/50", true, packets)
	})

	assert.Equal(t, []bool{false, false}, verdicts)

	// The lookup explains itself as it goes, and saying so is how this test
	// knows it got that far rather than stopping at filt_i's syntax-only
	// shortcut, which passes a message without asking anything.
	assert.Contains(t, output, "we have not heard Q2TEST over the radio")
}

func Test_PfilterValidate(t *testing.T) {
	PfilterStandaloneInit(0)

	t.Run("a valid filter is accepted", func(t *testing.T) {
		assert.NoError(t, PfilterValidate(0, 0, "t/m & ! d/WIDE*", true))
	})

	t.Run("a bad expression is rejected", func(t *testing.T) {
		var err = PfilterValidate(0, 0, "t/m & ( t/w", true)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `Expected ")" here.`)
	})

	t.Run("an APRS-only filter type is rejected in connected mode", func(t *testing.T) {
		assert.Error(t, PfilterValidate(0, 0, "t/m", false))
	})

	t.Run("the channels appear in the error message", func(t *testing.T) {
		var err = PfilterValidate(MAX_TOTAL_CHANS, 2, "x/", true)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "filter[IG,2]")
	})

	t.Run("a channel out of range is rejected rather than asserted on", func(t *testing.T) {
		require.Error(t, PfilterValidate(-1, 0, "b/Q1TEST", true))
		require.Error(t, PfilterValidate(0, MAX_TOTAL_CHANS+1, "b/Q1TEST", true))

		var _, err = PfilterMonitorLine(MAX_TOTAL_CHANS+1, 0, "b/Q1TEST", true, pfilterTestPositionPacket)
		require.Error(t, err)
	})

	t.Run("validation says nothing about its own synthetic packet", func(t *testing.T) {
		PfilterStandaloneInit(PfilterMaxDebugLevel)

		defer PfilterStandaloneInit(0)

		var output = testutils.CaptureOutput(t, func() {
			require.NoError(t, PfilterValidate(0, 0, "b/Q1TEST", true))
		})

		assert.Empty(t, output)
	})
}

func Test_PfilterStandaloneInit_debugLevelExplainsTheDecision(t *testing.T) {
	PfilterStandaloneInit(2)

	defer PfilterStandaloneInit(0)

	testutils.AssertOutputContains(t, func() {
		var _, err = PfilterMonitorLine(0, 0, "b/Q1TEST", true, pfilterTestPositionPacket)
		require.NoError(t, err)
	}, "b/Q1TEST returns TRUE")
}
