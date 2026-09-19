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

	assert.Equal(t, TT_ERROR_INVALID_MHEAD, gw.parseLocation(&state, "BA7474"),
		"an out of range locator should be reported, not ignored")
	assert.True(t, state.latitude.IsNothing(), "position should be left alone")
	assert.True(t, state.longitude.IsNothing(), "position should be left alone")
}
