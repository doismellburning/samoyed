// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user's position ambiguity comes from its own B-field, which a later touch
// tone sequence need not repeat.  tt_user_heard keeps the stored value when
// the new message does not carry one - but the parse state started ambiguity
// at 0, a perfectly real value meaning "omit no digits", so the guard never
// fired and every ambiguity-less message silently reset it.
func TestUserHeardKeepsAmbiguityFromAnEarlierMessage(t *testing.T) {
	var my_audio_config audio_s

	my_audio_config.mycall[0] = "Q1TEST-15"

	var my_tt_config tt_config_s

	my_tt_config.retain_time = 20
	my_tt_config.num_xmits = 1
	my_tt_config.xmit_delay[0] = 3

	tt_user_init(&my_audio_config, &my_tt_config)

	require.Equal(t, 0, tt_user_heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		maybe.Just(2), "", "", "", ' ', "!T99!"))

	var i = tt_user_search("Q2TEST", 'J')
	require.GreaterOrEqual(t, i, 0, "the user should have been recorded")
	require.Equal(t, maybe.Just(2), tt_user[i].ambiguity)

	// A second sequence that says nothing about ambiguity, as the parse state
	// hands it over.
	require.Equal(t, 0, tt_user_heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		newTTParseState().ambiguity, "", "", "", ' ', "!T99!"))

	assert.Equal(t, maybe.Just(2), tt_user[i].ambiguity,
		"an ambiguity-less message should leave the earlier ambiguity alone")
}

// aprs_tt stores a frequency as "146.520MHz", the form Dire Wolf's atof read
// the number from the front of.  strconv.ParseFloat wants the whole string to
// be a number, so it failed, the error was ignored, and the object report
// carried no frequency at all.
func TestObjectReportCarriesFrequency(t *testing.T) {
	var my_audio_config audio_s

	my_audio_config.mycall[0] = "Q1TEST-15"

	var my_tt_config tt_config_s

	my_tt_config.retain_time = 20
	my_tt_config.num_xmits = 1
	my_tt_config.xmit_delay[0] = 3

	tt_user_init(&my_audio_config, &my_tt_config)

	require.Equal(t, 0, tt_user_heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		maybe.Just(0), "146.955MHz", "074", "", ' ', ""))

	var i = tt_user_search("Q2TEST", 'J')
	require.GreaterOrEqual(t, i, 0, "the user should have been recorded")

	assert.Contains(t, object_report_text(i, true), "146.955MHz T074 ")
}
