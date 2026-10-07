// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user's position ambiguity comes from its own B-field, which a later touch
// tone sequence need not repeat.  heard keeps the stored value when
// the new message does not carry one - but the parse state started ambiguity
// at 0, a perfectly real value meaning "omit no digits", so the guard never
// fired and every ambiguity-less message silently reset it.
func TestUserHeardKeepsAmbiguityFromAnEarlierMessage(t *testing.T) {
	var my_audio_config RadioConfig

	my_audio_config.mycall[0] = "Q1TEST-15"

	var my_tt_config tt_config_s

	my_tt_config.retain_time = 20
	my_tt_config.num_xmits = 1
	my_tt_config.xmit_delay[0] = 3

	var users = newTTUsers(&my_audio_config, &my_tt_config)

	require.Equal(t, 0, users.heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		maybe.Just(2), "", "", "", ' ', "!T99!"))

	var i = users.search("Q2TEST", 'J')
	require.GreaterOrEqual(t, i, 0, "the user should have been recorded")
	require.Equal(t, maybe.Just(2), users.user[i].ambiguity)

	// A second sequence that says nothing about ambiguity, as the parse state
	// hands it over.
	require.Equal(t, 0, users.heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		newTTParseState().ambiguity, "", "", "", ' ', "!T99!"))

	assert.Equal(t, maybe.Just(2), users.user[i].ambiguity,
		"an ambiguity-less message should leave the earlier ambiguity alone")
}

// aprs_tt stores a frequency as "146.520MHz", the form Dire Wolf's atof read
// the number from the front of.  strconv.ParseFloat wants the whole string to
// be a number, so it failed, the error was ignored, and the object report
// carried no frequency at all.
func TestObjectReportCarriesFrequency(t *testing.T) {
	var my_audio_config RadioConfig

	my_audio_config.mycall[0] = "Q1TEST-15"

	var my_tt_config tt_config_s

	my_tt_config.retain_time = 20
	my_tt_config.num_xmits = 1
	my_tt_config.xmit_delay[0] = 3

	var users = newTTUsers(&my_audio_config, &my_tt_config)

	require.Equal(t, 0, users.heard("Q2TEST", 12, 'J', 'A', "", maybe.Just(37.25), maybe.Just(-71.75),
		maybe.Just(0), "146.955MHz", "074", "", ' ', ""))

	var i = users.search("Q2TEST", 'J')
	require.GreaterOrEqual(t, i, 0, "the user should have been recorded")

	assert.Contains(t, users.objectReportText(i, true), "146.955MHz T074 ")
}

// TextToTwoKey passes any Unicode digit through, but only ASCII ones belong in
// the suffix - another used to be narrowed to a byte and taken for a letter.
func TestDigitSuffixIgnoresNonASCIIDigits(t *testing.T) {
	assert.Equal(t, digit_suffix("Q1TEST"), digit_suffix("Q1TE\u0663ST")) // ARABIC-INDIC DIGIT THREE
}

// The receive processing goroutine records users as their tone sequences
// arrive, while the TTOBJ receive channel's audio goroutine polls the same
// table, through Button's idle ticks, to send the object reports it has
// scheduled.  Nothing ordered the two, so they raced on the table.
func TestUserTableIsSafeFromBothGoroutines(t *testing.T) {
	var my_audio_config RadioConfig

	my_audio_config.mycall[0] = "Q1TEST-15"

	var my_tt_config tt_config_s

	my_tt_config.retain_time = 20
	my_tt_config.num_xmits = 1
	my_tt_config.obj_xmit_chan = -1 // Keep the reports off the transmit queue.

	var gw = NewTTGateway(&my_audio_config, &my_tt_config, nil, nil, 0)

	var done = make(chan struct{})

	go func() {
		defer close(done)

		for range 100 * 39 {
			gw.Button(0, '.')
		}
	}()

	for i := range 100 {
		require.Equal(t, 0, gw.users.heard(fmt.Sprintf("Q%dTEST", i%10), 12, 'J', 'A', "",
			maybe.Just(37.25), maybe.Just(-71.75), maybe.Just(0), "", "", "", ' ', ""))
	}

	<-done
}

// An object report going out over the radio is remembered by the digipeater
// the gateway was given, so that hearing it again doesn't get it digipeated.
func TestTransmittedObjectReportIsRememberedByTheDigipeater(t *testing.T) {
	var audioConfig = makeBeaconModemConfig()

	setupBeaconTransmitQueue(t, audioConfig)

	var ttConfig tt_config_s

	ttConfig.obj_xmit_chan = 0

	var digiConfig = new(digi_config_s)
	digiConfig.dedupe_time = 30

	var digi = NewDigipeater(audioConfig, digiConfig, nil)
	var gw = NewTTGateway(audioConfig, &ttConfig, nil, digi, 0)

	const report = "Q1TEST>APDW17:;Q2TEST   *111111z4237.14N/07120.83W="

	gw.users.sendObjectReport(report, false)

	var pp = ax25.FromText(report, true)
	require.NotNil(t, pp)
	assert.True(t, digi.dedupe.Check(pp, 0), "the digipeater should have remembered the report we sent")
}
