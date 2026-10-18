// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package aprstt

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/touchtone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ttMyCalls stands in for each channel's callsign, from channel 0 up; any
// further channel has none.
func ttMyCalls(calls ...string) func(channel int) string {
	return func(channel int) string {
		if channel < 0 || channel >= len(calls) {
			return ""
		}

		return calls[channel]
	}
}

// A DTMF sequence can decode to a well-formed pair of letters that is still not
// a legal Maidenhead locator: the first pair has to be A through R, but "74"
// keys 'S'.  MheadToText is happy with it, so the rejection only comes from
// latlong.FromGridSquare, and that used to be dropped on the floor - the position
// silently stayed at wherever it already was and the caller was told the
// sequence had parsed.
func TestParseLocationRejectsOutOfRangeMaidenhead(t *testing.T) {
	t.Parallel()

	var cfg Config
	cfg.Locs = []*Loc{
		{Type: LocMHead, Pattern: "BAxxxx", MHead: MHeadLoc{Prefix: ""}}, //nolint:exhaustruct_v5
	}

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	// Confirm the premise: the locator converts cleanly but is out of range.
	var mh, errs = touchtone.MheadToText("7474", true)
	require.Equal(t, 0, errs, "DTMF should convert without complaint")
	require.Equal(t, "SS", mh)

	var _, _, err = latlong.FromGridSquare(mh)
	require.Error(t, err, "SS is not a valid Maidenhead locator")

	var state ttParseState

	assert.Equal(t, ErrorInvalidMHead, gw.parseLocation(&state, "BA7474"),
		"an out of range locator should be reported, not ignored")
	assert.True(t, state.latitude.IsNothing(), "position should be left alone")
	assert.True(t, state.longitude.IsNothing(), "position should be left alone")
}

// A callsign in two key format can end in a two key overlay whose keys don't
// translate to anything - "BA" is a letter key where a digit belongs, then a
// stray letter.  TwoKeyToText's error count was ignored there, and its empty
// result indexed, so a tone sequence anyone could send panicked the gateway.
// There is no overlay to give it, so the field is rejected.
func TestParseCallsignTwoKeyOverlayThatDoesNotTranslate(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, ErrorInvalidSymbol, gw.parseCallsign(&state, "A1234BA1"))
	})
}

// Keys that translate only in part - "5Z" is a digit, then a letter that isn't
// a column key - were taken for the character that did, as Dire Wolf took
// them, though a symbol field's overlay was rejected for the same keys.
// Reject both, as for the callsign itself.
func TestParseCallsignTwoKeyOverlayThatPartlyTranslates(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	assert.Equal(t, ErrorInvalidSymbol, gw.parseCallsign(&state, "A12345Z5"))
	assert.Equal(t, byte(APRSTT_DEFAULT_SYMTAB), state.symtabOrOverlay)
}

// A three digit abbreviation can end in a two key overlay too.  One whose keys
// translate to nothing - "1A" has no letter - became a NUL overlay, which
// went out as the symbol table of the object report.  Reject it, and one that
// translates only in part, as for a callsign in two key format.
func TestParseAbbreviationTwoKeyOverlay(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	assert.Equal(t, ErrorInvalidSymbol, gw.parseCallsign(&state, "A1231A7"), "nothing translates")

	state = newTTParseState()

	assert.Equal(t, ErrorInvalidSymbol, gw.parseCallsign(&state, "A1235Z1"), "part translates")
}

// A field that is nothing but an A is a callsign with nothing in it.  Its
// second character was read to tell which kind of A field it was, and a tone
// sequence anyone could send panicked the gateway.
func TestParseFieldsLoneA(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, ErrorInvalidCall, gw.parseFields(&state, "A#"))
	})
}

// A field that is nothing but a C is an empty comment.  Its second character
// was read to look for the enhanced comment format, and a tone sequence anyone
// could send panicked the gateway.
func TestParseCommentLoneC(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, 0, gw.parseComment(&state, "C"))
	})
	assert.Empty(t, state.comment)
}

// A symbol field is normally AB, the table and two digits, but its length is
// only checked to be at least four.  The second digit was read regardless, so
// a field with only one panicked the gateway on a tone sequence anyone could
// send.  Dire Wolf read the string's terminating NUL there and so took the one
// digit as the number.
func TestParseSymbolOneDigit(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, 0, gw.parseSymbol(&state, "AB15"))
	})
	assert.Equal(t, byte('/'), state.symtabOrOverlay)
	assert.Equal(t, byte(32+5), state.symbolCode)
}

// A callsign in two key format whose keys don't all translate - "1A" has no
// letter - used to be taken for what did translate, and its object report sent
// as though from a real user.  Reject it.
func TestParseCallsignTwoKeyThatPartlyTranslates(t *testing.T) {
	t.Parallel()

	var cfg Config

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	assert.Equal(t, ErrorInvalidCall, gw.parseCallsign(&state, "A9A1A2A7A7C75"))
	assert.Empty(t, state.callsign)
}

// A location pattern can be nothing but the B, and a field that is the same
// matches it.  Its second character was read for the DAO, and a tone sequence
// anyone could send panicked the gateway.  Dire Wolf read the string's
// terminating NUL there; a space keeps the DAO printable.
func TestParseLocationLoneB(t *testing.T) {
	t.Parallel()

	var cfg Config
	cfg.Locs = []*Loc{
		{Type: LocPoint, Pattern: "B", Point: PointLoc{Lat: 12.25, Lon: 56.25}}, //nolint:exhaustruct_v5
	}

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	require.NotPanics(t, func() {
		assert.Equal(t, 0, gw.parseLocation(&state, "B"))
	})
	assert.Equal(t, "!TB !", string(state.dao[:]))
}

// A macro whose definition expands to itself recursed until the stack ran out,
// which no recover catches.  The macros are the operator's, not what's heard
// on frequency, but one wrong line in the config shouldn't take the gateway
// down with the first tone sequence to match it.
func TestExpandMacroThatExpandsToItself(t *testing.T) {
	t.Parallel()

	var cfg Config
	cfg.Locs = []*Loc{
		{Type: LocMacro, Pattern: "1", Macro: MacroLoc{Definition: "1"}}, //nolint:exhaustruct_v5
	}

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, nil, nil, 0)
	gw.runningTests = true

	var state = newTTParseState()

	assert.Equal(t, ErrorInternal, gw.parseFields(&state, "1#"))
}

// A TTCMD script's output is the audible response, made one line as Dire Wolf
// made it: its newline used to go into the response frame as it was.
func TestSequenceResponseFromScriptIsOneLine(t *testing.T) {
	t.Parallel()

	var cfg Config
	cfg.ObjXmitChan = -1
	cfg.Cmd = "echo Q1TEST"
	cfg.Response[ErrorOK].Method = "SPEECH"

	var responses []string

	var transmit = func(_ int, _ int, pp *ax25.Packet) {
		responses = append(responses, pp.FormatAddrs()+string(pp.Info()))
	}

	var gw = NewGateway(ttMyCalls(), &cfg, nil, nil, nil, nil, transmit, nil, 0)

	gw.Sequence(t.Context(), 0, "A9A2B42A7A7C71#") // WB4APR/7

	assert.Equal(t, []string{"APRSTT>SPEECH:Q1TEST "}, responses)
}
