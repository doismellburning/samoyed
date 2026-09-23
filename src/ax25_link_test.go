// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	ax25Link.miscConfig.frack = 5

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

			ax25Link.miscConfig.frack = 5

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

// Numbers from the other station that make no sense are brought into range on
// both ways into complete_negotiation: the XID command we answer, and the XID
// response to one we sent, which skips negotiation_response altogether.
// Each is logged as a warning, except where negotiation_response has already
// taken the lesser of what was asked and what we can do, which is just
// negotiation. Regression test for issue #688.
func TestNegotiationBoundsWhatMakesNoSense(t *testing.T) {
	var paths = []struct {
		name    string
		apply   func(S *ax25_dlsm_t, param *xid_param_s)
		command bool
	}{
		{"command", negotiation_response, true},
		{"response", complete_negotiation, false},
	}

	var tests = []struct {
		name       string
		modulo     ax25_modulo_t
		length     int
		window     int
		wantModulo ax25_modulo_t
		wantLength int
		wantWindow int
		parameter  string
		// negotiation_response brings it into range before it gets to
		// complete_negotiation, so a command draws no warning.
		negotiated bool
	}{
		{"I field too short", modulo_8, 0, 4, modulo_8, AX25_N1_PACLEN_MIN, 4, "i_field_length_rx", false},
		{"I field negative", modulo_8, -8, 4, modulo_8, AX25_N1_PACLEN_MIN, 4, "i_field_length_rx", false},
		{"I field too short for modulo 128", modulo_128, 2, 4, modulo_128, 3, 4, "i_field_length_rx", false},
		{"I field too long", modulo_8, 8000, 4, modulo_8, AX25_N1_PACLEN_MAX, 4, "i_field_length_rx", true},
		{"window too wide for modulo 8", modulo_8, 256, 127, modulo_8, 256, AX25_K_MAXFRAME_BASIC_MAX, "window_size_rx", true},
		{"window too wide for modulo 128", modulo_128, 256, 127, modulo_128, 256, AX25_K_MAXFRAME_EXTENDED_MAX, "window_size_rx", true},
		{"window closed", modulo_8, 256, 0, modulo_8, 256, AX25_K_MAXFRAME_BASIC_MIN, "window_size_rx", false},
		{"window closed modulo 128", modulo_128, 256, 0, modulo_128, 256, AX25_K_MAXFRAME_EXTENDED_MIN, "window_size_rx", false},
		{"modulo we don't implement", 16, 256, 4, modulo_8, 256, 4, "modulo", false},
	}

	for _, path := range paths {
		for _, tc := range tests {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				setupTestEnv(t)

				var hook = test.NewGlobal()

				t.Cleanup(hook.Reset)

				var S = newNegotiationTestLink()

				var param = new(xid_param_s)
				param.srej = srej_none
				param.modulo = tc.modulo
				param.i_field_length_rx = maybe.Just(tc.length)
				param.window_size_rx = maybe.Just(tc.window)

				path.apply(S, param)

				assert.Equal(t, tc.wantModulo, S.modulo)
				assert.Equal(t, tc.wantLength, S.n1_paclen)
				assert.Equal(t, tc.wantWindow, S.k_maxframe)

				var warnings []*logrus.Entry
				for _, entry := range hook.AllEntries() {
					if entry.Level == logrus.WarnLevel {
						warnings = append(warnings, entry)
					}
				}

				if path.command && tc.negotiated {
					assert.Empty(t, warnings)
				} else if assert.Len(t, warnings, 1) {
					assert.Equal(t, tc.parameter, warnings[0].Data["parameter"])
				}
			})
		}
	}
}

// Answering an XID command, what we send back is what we now run with, so the
// two ends agree - not the nonsense the other station asked for.
func TestNegotiationResponseSendsBackWhatItApplied(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()

	var param = new(xid_param_s)
	param.srej = srej_none
	param.modulo = 16
	param.i_field_length_rx = maybe.Just(0)
	param.window_size_rx = maybe.Just(0)

	negotiation_response(S, param)

	assert.Equal(t, S.modulo, param.modulo)
	assert.Equal(t, maybe.Just(S.n1_paclen), param.i_field_length_rx)
	assert.Equal(t, maybe.Just(S.k_maxframe), param.window_size_rx)
}

// A response that moves the link to modulo 8 but leaves the window size out
// must not keep a window wider than modulo 8 can hold.
func TestCompleteNegotiationNarrowsTheWindowToTheNewModulo(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()
	S.modulo = modulo_128
	S.k_maxframe = 32

	var param, _, status = xid_parse(nil)
	assert.Equal(t, 1, status)

	param.modulo = modulo_8

	complete_negotiation(S, param)

	assert.Equal(t, modulo_8, S.modulo)
	assert.Equal(t, AX25_K_MAXFRAME_BASIC_MAX, S.k_maxframe)
}

// A response that moves the link to modulo 128 but leaves the I field length
// out must not keep an N1 too small for V2.2 segmentation to send anything.
func TestCompleteNegotiationRaisesN1ToTheNewModulo(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()
	S.n1_paclen = 1

	var param, _, status = xid_parse(nil)
	assert.Equal(t, 1, status)

	param.modulo = modulo_128

	complete_negotiation(S, param)

	assert.Equal(t, modulo_128, S.modulo)
	assert.Equal(t, 3, S.n1_paclen)
}

// A SABM addressed to a callsign no client has registered goes unanswered - we
// are not the station it was sent to - but it used to go unremarked as well, so
// an operator who had set MYCALL and expected connected mode to work saw
// nothing at all while the far end timed out.  Regression test for issue #665:
// the frame is still ignored, and now says so.
func TestIgnoredConnectRequestIsLogged(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 1
	)

	setupTestEnv(t) // Leaves ax25Link.regCallsignList empty, so nothing is registered.

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var previousLevel = logrus.GetLevel()

	logrus.SetLevel(logrus.InfoLevel)

	t.Cleanup(func() { logrus.SetLevel(previousLevel) })

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_SOURCE] = THEIR_CALL
	addrs[AX25_DESTINATION] = MY_CALL

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 1, 0, nil)
	require.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	assert.Nil(t, ax25Link.listHead, "an unregistered callsign gets no link state machine")

	var entry = hook.LastEntry()

	require.NotNil(t, entry, "an ignored connect request must be logged")
	assert.Equal(t, logrus.InfoLevel, entry.Level)
	assert.Contains(t, entry.Message, "no client has registered this callsign")
	assert.Equal(t, CHANNEL, entry.Data["channel"])
	assert.Equal(t, THEIR_CALL, entry.Data["source"])
	assert.Equal(t, MY_CALL, entry.Data["destination"])
}

// runWithTimeout fails the test if fn has not returned in a few seconds,
// rather than letting something that never returns take the whole package's
// test timeout with it.
func runWithTimeout(t *testing.T, what string, fn func()) {
	t.Helper()

	var done = make(chan struct{})

	go func() {
		defer close(done)

		fn()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

// newDataRequest is a client application asking to send connected mode data.
func newDataRequest(myCall string, theirCall string, channel int, data []byte) *dlq_item_t {
	var E = new(dlq_item_t)

	E._type = DLQ_XMIT_DATA_REQUEST
	E._chan = channel
	E.addrs[OWNCALL] = myCall
	E.addrs[PEERCALL] = theirCall
	E.num_addr = 2
	E.txdata = dataLinkQueue.NewCData(0xF0, data)

	return E
}

// A client application can ask to send connected mode data for a link that
// has never been connected, and the link it gets made for it has to be able
// to carry something: with its maximum information field at zero, every
// frame is too long for it, and the segmentation that follows cannot make
// progress.  Refs #683.
func TestDataRequestForALinkThatWasNeverConnected(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 0
	)

	setupTestEnv(t)

	var E = newDataRequest(MY_CALL, THEIR_CALL, CHANNEL, []byte("Testing"))

	runWithTimeout(t, "dl_data_request", func() { dl_data_request(E) })

	require.NotNil(t, ax25Link.listHead, "the request should have made a link")
	assert.Equal(t, ax25Link.miscConfig.paclen, ax25Link.listHead.n1_paclen,
		"a new link should start out able to carry what this station is configured for")
	assert.Equal(t, ax25Link.miscConfig.maxframe_basic, ax25Link.listHead.k_maxframe)
	assert.Equal(t, ax25Link.miscConfig.retry, ax25Link.listHead.n2_retry)
	assert.Equal(t, modulo_8, ax25Link.listHead.modulo)
}

// A link can end up with a maximum information field that nothing will fit
// in: XID negotiation takes the other end's word for it and an XID can ask
// for less than a byte, and PACLEN 1 is a configuration anyone can write.
// Segmenting data to fit either used to divide it into pieces of nothing,
// for ever, or divide by zero.  Refs #683.
func TestDataRequestForALinkThatCannotCarryAnything(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 0
	)

	var testCases = []struct {
		name     string
		modulo   ax25_modulo_t
		n1Paclen int
	}{
		{"a v2.0 link with no room at all", modulo_8, 0},
		{"a v2.2 link with no room for a segment header", modulo_128, 1},
		{"a v2.2 link with no room for data behind the header", modulo_128, 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setupTestEnv(t)

			var S = get_link_handle([AX25_MAX_ADDRS]string{OWNCALL: MY_CALL, PEERCALL: THEIR_CALL}, 2, CHANNEL, 0, true)
			require.NotNil(t, S)

			S.modulo = testCase.modulo
			S.n1_paclen = testCase.n1Paclen

			var E = newDataRequest(MY_CALL, THEIR_CALL, CHANNEL, []byte("Testing"))

			runWithTimeout(t, "dl_data_request", func() { dl_data_request(E) })

			assert.Nil(t, E.txdata, "data that cannot be sent should have been discarded")
		})
	}
}
