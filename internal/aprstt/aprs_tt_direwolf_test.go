// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

//nolint:gochecknoglobals
package aprstt

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

/*
 * Regression test for the parsing.
 * It does not maintain any history so abbreviation will not invoke previous full call.
 */

/* Some examples are derived from http://www.aprs.org/aprstt/aprstt-coding24.txt */

type ttTestCase struct {
	/* Tone sequence in. */
	toneseq string

	/* Expected results... */
	callsign  string
	ssid      string
	symbol    string
	freq      string
	comment   string
	latitude  string
	longitude string
	dao       string
}

var ttTestCases = []ttTestCase{

	/* Callsigns & abbreviations, traditional */

	{"A9A2B42A7A7C71#", "WB4APR", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"}, /* WB4APR/7 */
	{"A27773#", "277", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"},            /* abbreviated form */

	/* Intentionally wrong - Has 6 for checksum when it should be 3. */
	{"A27776#", "", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"}, /* Expect error message. */

	/* Example in spec is wrong.  checksum should be 5 in this case. */
	{"A2A7A7C71#", "", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"},   /* Spelled suffix, overlay, checksum */
	{"A2A7A7C75#", "APR", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"}, /* Spelled suffix, overlay, checksum */
	{"A27773#", "277", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"},    /* Suffix digits, overlay, checksum */

	{"A9A2B26C7D9D71#", "WB2OSZ", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"}, /* WB2OSZ/7 numeric overlay */
	{"A67979#", "679", "12", "7A", "", "", "Nothing", "Nothing", "!T  !"},            /* abbreviated form */

	{"A9A2B26C7D9D5A9#", "WB2OSZ", "12", "JA", "", "", "Nothing", "Nothing", "!T  !"}, /* WB2OSZ/J letter overlay */
	{"A6795A7#", "679", "12", "JA", "", "", "Nothing", "Nothing", "!T  !"},            /* abbreviated form */

	{"A277#", "277", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"}, /* Tactical call "277" no overlay and no checksum */

	/* QIKcom-2 style 10 digit call & 5 digit suffix */

	{"AC9242771558#", "WB4APR", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"},
	{"AC27722#", "APR", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"},

	/* Locations */

	{"B01*A67979#", "679", "12", "7A", "", "", "12.2500", "56.2500", "!T1 !"},
	{"B988*A67979#", "679", "12", "7A", "", "", "12.5000", "56.5000", "!T88!"},

	{"B51000125*A67979#", "679", "12", "7A", "", "", "52.7907", "0.8309", "!TB5!"}, /* expect about 52.79  +0.83 */

	{"B5206070*A67979#", "679", "12", "7A", "", "", "37.9137", "-81.1366", "!TB5!"}, /* Try to get from Hilltop Tower to Archery & Target Range. */
	/* Latitude comes out ok, 37.9137 -> 55.82 min. */
	/* Longitude -81.1254 -> 8.20 min */
	{"B21234*A67979#", "679", "12", "7A", "", "", "12.3400", "56.1200", "!TB2!"},

	{"B533686*A67979#", "679", "12", "7A", "", "", "37.9222", "81.1143", "!TB5!"},

	// TODO: should test other coordinate systems.

	/* Comments */

	{"C1", "", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"},
	{"C2", "", "12", "\\A", "", "", "Nothing", "Nothing", "!T  !"},
	{"C146520", "", "12", "\\A", "146.520MHz", "", "Nothing", "Nothing", "!T  !"},
	{"C7788444222550227776669660333666990122223333",
		"", "12", "\\A", "", "QUICK BROWN FOX 123", "Nothing", "Nothing", "!T  !"},
	/* Macros */

	{"88345", "BIKE 345", "0", "/b", "", "", "12.5000", "56.5000", "!T88!"},

	/* 10 digit representation for callsign & satellite grid. WB4APR near 39.5, -77   */

	{"AC9242771558*BA1819", "WB4APR", "12", "\\A", "", "", "39.5000", "-77.0000", "!TBA!"},
	{"18199242771558", "WB4APR", "12", "\\A", "", "", "39.5000", "-77.0000", "!TBA!"},
}

var aprs_tt_test_config = []*Loc{
	{Type: LocPoint, Pattern: "B01", Point: PointLoc{Lat: 12.25, Lon: 56.25}},  //nolint:exhaustruct_v5
	{Type: LocPoint, Pattern: "B988", Point: PointLoc{Lat: 12.50, Lon: 56.50}}, //nolint:exhaustruct_v5

	{Type: LocVector, Pattern: "B5bbbdddd", Vector: VectorLoc{Lat: 53., Lon: -1., Scale: 1000.}}, //nolint:exhaustruct_v5 // km units

	// Hilltop Tower http://www.aprs.org/aprs-jamboree-2013.html
	{Type: LocVector, Pattern: "B5bbbddd", Vector: VectorLoc{Lat: 37 + 55.37/60., Lon: -(81 + 7.86/60.), Scale: 16.09344}}, //nolint:exhaustruct_v5 // .01 mile units

	{Type: LocGrid, Pattern: "B2xxyy", Grid: GridLoc{Lat0: 12.00, Lon0: 56.00, //nolint:exhaustruct_v5
		Lat9: 12.99, Lon9: 56.99}},
	{Type: LocGrid, Pattern: "Byyyxxx", Grid: GridLoc{Lat0: 37 + 50./60.0, Lon0: 81, //nolint:exhaustruct_v5
		Lat9: 37 + 59.99/60.0, Lon9: 81 + 9.99/60.0}},

	{Type: LocMHead, Pattern: "BAxxxxxx", MHead: MHeadLoc{Prefix: "326129"}}, //nolint:exhaustruct_v5

	{Type: LocSatSq, Pattern: "BAxxxx"}, //nolint:exhaustruct_v5

	{Type: LocMacro, Pattern: "xxyyy", Macro: MacroLoc{Definition: "B9xx*AB166*AA2B4C5B3B0Ayyy"}},   //nolint:exhaustruct_v5
	{Type: LocMacro, Pattern: "xxxxzzzzzzzzzz", Macro: MacroLoc{Definition: "BAxxxx*ACzzzzzzzzzz"}}, //nolint:exhaustruct_v5
}

// fourDecimalPlaces renders a position for comparison against the expected
// results above, which spell an absent one "Nothing".
func fourDecimalPlaces(m maybe.Maybe[float64]) string {
	return maybe.Fold("Nothing", func(value float64) string {
		return fmt.Sprintf("%.4f", value)
	}, m)
}

func check_result(t *testing.T, gateway *Gateway, testCase ttTestCase) {
	t.Helper()

	var state = gateway.lastParseState

	assert.Equal(t, testCase.callsign, state.callsign, testCase.toneseq)

	assert.Equal(t, testCase.ssid, strconv.Itoa(state.ssid), testCase.toneseq)

	var stemp = fmt.Sprintf("%c%c", state.symtabOrOverlay, state.symbolCode)
	assert.Equal(t, testCase.symbol, stemp, testCase.toneseq)

	assert.Equal(t, testCase.freq, state.freq, testCase.toneseq)

	assert.Equal(t, testCase.comment, state.comment, testCase.toneseq)

	assert.Equal(t, testCase.latitude, fourDecimalPlaces(state.latitude), testCase.toneseq)

	assert.Equal(t, testCase.longitude, fourDecimalPlaces(state.longitude), testCase.toneseq)

	assert.Equal(t, testCase.dao, string(state.dao[:]), testCase.toneseq)
}

func Test_APRS_TT(t *testing.T) {
	t.Parallel()

	var cfg Config
	cfg.Locs = aprs_tt_test_config
	var gateway = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gateway.runningTests = true

	for testNum, testCase := range ttTestCases {
		t.Logf("Test case %d: %s", testNum, testCase.toneseq)

		gateway.Sequence(t.Context(), 0, testCase.toneseq)
		check_result(t, gateway, testCase)
	}
}
