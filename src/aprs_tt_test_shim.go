//nolint:gochecknoglobals
package direwolf

import (
	"fmt"
	"strconv"
	"testing"

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

	{"A9A2B42A7A7C71#", "WB4APR", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* WB4APR/7 */
	{"A27773#", "277", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"},            /* abbreviated form */

	/* Intentionally wrong - Has 6 for checksum when it should be 3. */
	{"A27776#", "", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* Expect error message. */

	/* Example in spec is wrong.  checksum should be 5 in this case. */
	{"A2A7A7C71#", "", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"},   /* Spelled suffix, overlay, checksum */
	{"A2A7A7C75#", "APR", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* Spelled suffix, overlay, checksum */
	{"A27773#", "277", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"},    /* Suffix digits, overlay, checksum */

	{"A9A2B26C7D9D71#", "WB2OSZ", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* WB2OSZ/7 numeric overlay */
	{"A67979#", "679", "12", "7A", "", "", "-999999.0000", "-999999.0000", "!T  !"},            /* abbreviated form */

	{"A9A2B26C7D9D5A9#", "WB2OSZ", "12", "JA", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* WB2OSZ/J letter overlay */
	{"A6795A7#", "679", "12", "JA", "", "", "-999999.0000", "-999999.0000", "!T  !"},            /* abbreviated form */

	{"A277#", "277", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"}, /* Tactical call "277" no overlay and no checksum */

	/* QIKcom-2 style 10 digit call & 5 digit suffix */

	{"AC9242771558#", "WB4APR", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"},
	{"AC27722#", "APR", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"},

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

	{"C1", "", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"},
	{"C2", "", "12", "\\A", "", "", "-999999.0000", "-999999.0000", "!T  !"},
	{"C146520", "", "12", "\\A", "146.520MHz", "", "-999999.0000", "-999999.0000", "!T  !"},
	{"C7788444222550227776669660333666990122223333",
		"", "12", "\\A", "", "QUICK BROWN FOX 123", "-999999.0000", "-999999.0000", "!T  !"},
	/* Macros */

	{"88345", "BIKE 345", "0", "/b", "", "", "12.5000", "56.5000", "!T88!"},

	/* 10 digit representation for callsign & satellite grid. WB4APR near 39.5, -77   */

	{"AC9242771558*BA1819", "WB4APR", "12", "\\A", "", "", "39.5000", "-77.0000", "!TBA!"},
	{"18199242771558", "WB4APR", "12", "\\A", "", "", "39.5000", "-77.0000", "!TBA!"},
}

var gateway *TTGateway

func check_result(t *testing.T, testCase ttTestCase) {
	t.Helper()

	var state = gateway.lastParseState

	assert.Equal(t, testCase.callsign, state.callsign, testCase.toneseq)

	assert.Equal(t, testCase.ssid, strconv.Itoa(state.ssid), testCase.toneseq)

	var stemp = fmt.Sprintf("%c%c", state.symtabOrOverlay, state.symbolCode)
	assert.Equal(t, testCase.symbol, stemp, testCase.toneseq)

	assert.Equal(t, testCase.freq, state.freq, testCase.toneseq)

	assert.Equal(t, testCase.comment, state.comment, testCase.toneseq)

	var latTmp = fmt.Sprintf("%.4f", state.latitude)
	assert.Equal(t, testCase.latitude, latTmp, testCase.toneseq)

	var lonTmp = fmt.Sprintf("%.4f", state.longitude)
	assert.Equal(t, testCase.longitude, lonTmp, testCase.toneseq)

	assert.Equal(t, testCase.dao, string(state.dao[:]), testCase.toneseq)
}

func aprs_tt_test_main(t *testing.T) {
	t.Helper()

	gateway = NewTTGateway(nil, 0)
	gateway.runningTests = true

	for testNum, testCase := range ttTestCases {
		dw_printf("\nTest case %d: %s\n", testNum, testCase.toneseq)

		gateway.Sequence(0, testCase.toneseq)
		check_result(t, testCase)
	}
}
