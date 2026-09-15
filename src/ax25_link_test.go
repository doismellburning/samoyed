// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// newNegotiationTestLink is a link whose parameters are all distinct from the
// defaults negotiation would otherwise reach for, so a test can tell "kept what
// we had" apart from "filled in a default".
func newNegotiationTestLink() *ax25_dlsm_t {
	var S = new(ax25_dlsm_t)

	S.modulo = modulo_8
	S.srej_enable = srej_none
	S.n1_paclen = 128
	S.k_maxframe = 2
	S.n2_retry = 5
	S.t1v = 7 * time.Second

	return S
}

// An XID command that specifies nothing should get back a complete set of
// defaults, per the 2006 revision of the spec.
func TestNegotiationResponseFillsInDefaults(t *testing.T) {
	setupTestEnv(t)

	// Distinct from the 3000 mSec default so the two branches can be told apart.
	g_misc_config_p.frack = 5

	var S = newNegotiationTestLink()

	var param, _, status = xid_parse(nil)
	assert.Equal(t, 1, status)

	negotiation_response(S, param)

	assert.Equal(t, modulo_8, param.modulo)
	assert.Equal(t, srej_none, param.srej)
	assert.Equal(t, maybe.Just(AX25_N1_PACLEN_DEFAULT), param.i_field_length_rx)
	assert.Equal(t, maybe.Just(AX25_K_MAXFRAME_BASIC_DEFAULT), param.window_size_rx)
	assert.Equal(t, maybe.Just(3000), param.ack_timer)
	assert.Equal(t, maybe.Just(AX25_N2_RETRY_DEFAULT), param.retries)

	// And what we agreed is what the link now runs with.
	assert.Equal(t, AX25_N1_PACLEN_DEFAULT, S.n1_paclen)
	assert.Equal(t, AX25_K_MAXFRAME_BASIC_DEFAULT, S.k_maxframe)
	assert.Equal(t, AX25_N2_RETRY_DEFAULT, S.n2_retry)
	assert.Equal(t, 3*time.Second, S.t1v)
}

// What the other station asks for is reduced to what we can manage, and the ack
// timer and retries go the other way - we take the larger of the two.
func TestNegotiationResponseBoundsWhatTheOtherStationAsksFor(t *testing.T) {
	var tests = []struct {
		name       string
		modulo     ax25_modulo_t
		wantWindow int
	}{
		{"modulo 8", modulo_8, AX25_K_MAXFRAME_BASIC_MAX},
		{"modulo 128", modulo_128, AX25_K_MAXFRAME_EXTENDED_MAX},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTestEnv(t)

			g_misc_config_p.frack = 5

			var S = newNegotiationTestLink()

			var param = new(xid_param_s)
			param.srej = srej_multi
			param.modulo = test.modulo
			param.i_field_length_rx = maybe.Just(8000) // More than we can hold.
			param.window_size_rx = maybe.Just(100)     // Wider than we allow.
			param.ack_timer = maybe.Just(1000)         // Quicker than our FRACK.
			param.retries = maybe.Just(2)              // Fewer than our N2.

			negotiation_response(S, param)

			assert.Equal(t, maybe.Just(AX25_N1_PACLEN_MAX), param.i_field_length_rx)
			assert.Equal(t, maybe.Just(test.wantWindow), param.window_size_rx)
			assert.Equal(t, maybe.Just(5000), param.ack_timer)
			assert.Equal(t, maybe.Just(5), param.retries)

			assert.Equal(t, AX25_N1_PACLEN_MAX, S.n1_paclen)
			assert.Equal(t, test.wantWindow, S.k_maxframe)
			assert.Equal(t, 5*time.Second, S.t1v)
			assert.Equal(t, 5, S.n2_retry)
			assert.Equal(t, test.modulo, S.modulo)
			assert.Equal(t, srej_multi, S.srej_enable)
		})
	}
}

// "If this field is not present, the current values are retained."  A response
// that leaves a parameter out must not reset the running configuration to a
// default, or to zero.
func TestCompleteNegotiationKeepsWhatTheResponseOmits(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()

	// Everything absent, as xid_parse gives for an empty info field.
	var param, _, status = xid_parse(nil)
	assert.Equal(t, 1, status)

	complete_negotiation(S, param)

	assert.Equal(t, modulo_8, S.modulo)
	assert.Equal(t, srej_none, S.srej_enable)
	assert.Equal(t, 128, S.n1_paclen)
	assert.Equal(t, 2, S.k_maxframe)
	assert.Equal(t, 5, S.n2_retry)
	assert.Equal(t, 7*time.Second, S.t1v)
}

// A response that specifies one parameter applies that one and leaves the rest,
// including converting the acknowledge timer from mSec to a Duration.
func TestCompleteNegotiationAppliesOnlyWhatTheResponseSpecifies(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()

	var param, _, status = xid_parse(nil)
	assert.Equal(t, 1, status)

	param.ack_timer = maybe.Just(4500)

	complete_negotiation(S, param)

	assert.Equal(t, 4500*time.Millisecond, S.t1v)

	assert.Equal(t, 128, S.n1_paclen)
	assert.Equal(t, 2, S.k_maxframe)
	assert.Equal(t, 5, S.n2_retry)
}
