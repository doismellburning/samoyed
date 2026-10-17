// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/touchtone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A DTMF sequence can decode to a well-formed pair of letters that is still not
// a legal Maidenhead locator: the first pair has to be A through R, but "74"
// keys 'S'.  MheadToText is happy with it, so the rejection only comes from
// latlong.FromGridSquare, and that used to be dropped on the floor - the position
// silently stayed at wherever it already was and the caller was told the
// sequence had parsed.
func TestParseLocationRejectsOutOfRangeMaidenhead(t *testing.T) {
	var cfg tt_config_s
	cfg.ttlocs = []*ttloc_s{
		{ttlocType: TTLOC_MHEAD, pattern: "BAxxxx", mhead: mheadTTLoc{prefix: ""}}, //nolint:exhaustruct_v5
	}

	var gw = NewTTGateway(new(RadioConfig), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	// Confirm the premise: the locator converts cleanly but is out of range.
	var mh, errs = touchtone.MheadToText("7474", true)
	require.Equal(t, 0, errs, "DTMF should convert without complaint")
	require.Equal(t, "SS", mh)

	var _, _, err = latlong.FromGridSquare(mh)
	require.Error(t, err, "SS is not a valid Maidenhead locator")

	var state ttParseState

	assert.Equal(t, TT_ERROR_INVALID_MHEAD, gw.parseLocation(&state, "BA7474"),
		"an out of range locator should be reported, not ignored")
	assert.True(t, state.latitude.IsNothing(), "position should be left alone")
	assert.True(t, state.longitude.IsNothing(), "position should be left alone")
}

// A callsign in two key format can end in a two key overlay whose keys don't
// translate to anything - "BA" is a letter key where a digit belongs, then a
// stray letter.  TwoKeyToText's error count was ignored there, and its empty
// result indexed, so a tone sequence anyone could send panicked the gateway.
func TestParseCallsignTwoKeyOverlayThatDoesNotTranslate(t *testing.T) {
	var cfg tt_config_s

	var gw = NewTTGateway(new(RadioConfig), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, 0, gw.parseCallsign(&state, "A1234BA1"))
	})
	assert.Equal(t, rune(0), state.symtabOrOverlay, "an overlay that doesn't translate should be left empty")
}

// A field that is nothing but an A is a callsign with nothing in it.  Its
// second character was read to tell which kind of A field it was, and a tone
// sequence anyone could send panicked the gateway.
func TestParseFieldsLoneA(t *testing.T) {
	var cfg tt_config_s

	var gw = NewTTGateway(new(RadioConfig), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, TT_ERROR_INVALID_CALL, gw.parseFields(&state, "A#"))
	})
}
