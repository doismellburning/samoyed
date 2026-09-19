// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A DTMF sequence can decode to a well-formed pair of letters that is still not
// a legal Maidenhead locator: the first pair has to be A through R, but "74"
// keys 'S'.  TTMheadToText is happy with it, so the rejection only comes from
// ll_from_grid_square, and that used to be dropped on the floor - the position
// silently stayed at wherever it already was and the caller was told the
// sequence had parsed.
func TestParseLocationRejectsOutOfRangeMaidenhead(t *testing.T) {
	var cfg tt_config_s
	cfg.ttlocs = []*ttloc_s{
		{ttlocType: TTLOC_MHEAD, pattern: "BAxxxx", mhead: mheadTTLoc{prefix: ""}}, //nolint:exhaustruct_v5
	}

	var gw = NewTTGateway(&cfg, 0)
	gw.runningTests = true

	// Confirm the premise: the locator converts cleanly but is out of range.
	var mh, errs = TTMheadToText("7474", true)
	require.Equal(t, 0, errs, "DTMF should convert without complaint")
	require.Equal(t, "SS", mh)

	var _, _, err = ll_from_grid_square(mh)
	require.Error(t, err, "SS is not a valid Maidenhead locator")

	var state ttParseState

	var parseErr = gw.parseLocation(&state, "BA7474")
	require.Error(t, parseErr, "an out of range locator should be reported, not ignored")
	assert.Equal(t, TT_ERROR_INVALID_MHEAD, ttErrorCode(parseErr))
	assert.True(t, state.latitude.IsNothing(), "position should be left alone")
	assert.True(t, state.longitude.IsNothing(), "position should be left alone")
}

// A bad checksum used to be a number and a line on stdout; the number is now
// carried by an error that also says what was wrong with what.
func TestParseCallsignReportsBadChecksum(t *testing.T) {
	var cfg tt_config_s
	var gw = NewTTGateway(&cfg, 0)
	var state = newTTParseState()

	// "A27773" is the documented example, so 6 is the wrong checksum here.
	var parseErr = gw.parseCallsign(&state, "A27776")

	require.Error(t, parseErr)
	assert.Equal(t, TT_ERROR_BAD_CHECKSUM, ttErrorCode(parseErr))
	assert.Contains(t, parseErr.Error(), "expected 3 but received 6")
}

// Not everything worth saying is worth rejecting the sequence for.  A pattern
// with too few bearing digits is the configuration's fault, not the caller's,
// so the position is still worked out and the complaint comes back alongside.
func TestParseLocationWarnsAboutShortBearing(t *testing.T) {
	var cfg tt_config_s
	cfg.ttlocs = []*ttloc_s{
		{ttlocType: TTLOC_VECTOR, pattern: "B5bbdddd", vector: vectorTTLoc{lat: 53., lon: -1., scale: 1000.}}, //nolint:exhaustruct_v5
	}

	var gw = NewTTGateway(&cfg, 0)
	var state = newTTParseState()

	var parseErr = gw.parseLocation(&state, "B5120100")

	require.NoError(t, parseErr, "a short bearing should not reject the sequence")
	require.Len(t, state.warnings, 1)
	assert.Contains(t, state.warnings[0].Error(), `bearing "12" should be 3 digits`)
	assert.True(t, state.latitude.IsJust(), "position should still have been worked out")
}
