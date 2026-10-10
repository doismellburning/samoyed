// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/testutils"
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

	S.modulo = ax25.Modulo8
	S.srej_enable = SREJNone
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
	assert.True(t, status)

	negotiation_response(S, param)

	assert.Equal(t, ax25.Modulo8, param.Modulo)
	assert.Equal(t, SREJNone, param.SREJ)
	assert.Equal(t, maybe.Just(AX25_N1_PACLEN_DEFAULT), param.IFieldLengthRx)
	assert.Equal(t, maybe.Just(AX25_K_MAXFRAME_BASIC_DEFAULT), param.WindowSizeRx)
	assert.Equal(t, maybe.Just(3000), param.AckTimer)
	assert.Equal(t, maybe.Just(AX25_N2_RETRY_DEFAULT), param.Retries)

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
		modulo     ax25.Modulo
		wantWindow int
	}{
		{"modulo 8", ax25.Modulo8, AX25_K_MAXFRAME_BASIC_MAX},
		{"modulo 128", ax25.Modulo128, AX25_K_MAXFRAME_EXTENDED_MAX},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTestEnv(t)

			ax25Link.miscConfig.frack = 5

			var S = newNegotiationTestLink()

			var param = new(xid_param_s)
			param.SREJ = SREJMulti
			param.Modulo = test.modulo
			param.IFieldLengthRx = maybe.Just(8000) // More than we can hold.
			param.WindowSizeRx = maybe.Just(100)    // Wider than we allow.
			param.AckTimer = maybe.Just(1000)       // Quicker than our FRACK.
			param.Retries = maybe.Just(2)           // Fewer than our N2.

			negotiation_response(S, param)

			assert.Equal(t, maybe.Just(AX25_N1_PACLEN_MAX), param.IFieldLengthRx)
			assert.Equal(t, maybe.Just(test.wantWindow), param.WindowSizeRx)
			assert.Equal(t, maybe.Just(5000), param.AckTimer)
			assert.Equal(t, maybe.Just(5), param.Retries)

			assert.Equal(t, AX25_N1_PACLEN_MAX, S.n1_paclen)
			assert.Equal(t, test.wantWindow, S.k_maxframe)
			assert.Equal(t, 5*time.Second, S.t1v)
			assert.Equal(t, 5, S.n2_retry)
			assert.Equal(t, test.modulo, S.modulo)
			assert.Equal(t, SREJMulti, S.srej_enable)
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
	assert.True(t, status)

	complete_negotiation(S, param)

	assert.Equal(t, ax25.Modulo8, S.modulo)
	assert.Equal(t, SREJNone, S.srej_enable)
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
	assert.True(t, status)

	param.AckTimer = maybe.Just(4500)

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
		modulo     ax25.Modulo
		length     int
		window     int
		wantModulo ax25.Modulo
		wantLength int
		wantWindow int
		parameter  string
		// negotiation_response brings it into range before it gets to
		// complete_negotiation, so a command draws no warning.
		negotiated bool
	}{
		{"I field too short", ax25.Modulo8, 0, 4, ax25.Modulo8, AX25_N1_PACLEN_MIN, 4, "i_field_length_rx", false},
		{"I field negative", ax25.Modulo8, -8, 4, ax25.Modulo8, AX25_N1_PACLEN_MIN, 4, "i_field_length_rx", false},
		{"I field too short for modulo 128", ax25.Modulo128, 2, 4, ax25.Modulo128, 3, 4, "i_field_length_rx", false},
		{"I field too long", ax25.Modulo8, 8000, 4, ax25.Modulo8, AX25_N1_PACLEN_MAX, 4, "i_field_length_rx", true},
		{"window too wide for modulo 8", ax25.Modulo8, 256, 127, ax25.Modulo8, 256, AX25_K_MAXFRAME_BASIC_MAX, "window_size_rx", true},
		{"window too wide for modulo 128", ax25.Modulo128, 256, 127, ax25.Modulo128, 256, AX25_K_MAXFRAME_EXTENDED_MAX, "window_size_rx", true},
		{"window closed", ax25.Modulo8, 256, 0, ax25.Modulo8, 256, AX25_K_MAXFRAME_BASIC_MIN, "window_size_rx", false},
		{"window closed modulo 128", ax25.Modulo128, 256, 0, ax25.Modulo128, 256, AX25_K_MAXFRAME_EXTENDED_MIN, "window_size_rx", false},
		{"modulo we don't implement", 16, 256, 4, ax25.Modulo8, 256, 4, "modulo", false},
	}

	for _, path := range paths {
		for _, tc := range tests {
			t.Run(path.name+"/"+tc.name, func(t *testing.T) {
				setupTestEnv(t)

				var hook = test.NewGlobal()

				t.Cleanup(hook.Reset)

				var S = newNegotiationTestLink()

				var param = new(xid_param_s)
				param.SREJ = SREJNone
				param.Modulo = tc.modulo
				param.IFieldLengthRx = maybe.Just(tc.length)
				param.WindowSizeRx = maybe.Just(tc.window)

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

// A closed window on the wire, not just in a hand-built xid_param_s, is opened
// to the least there is rather than thrown wide: xid_parse used to put 127 in
// place of anything out of range, which complete_negotiation then narrowed to
// the widest window the modulus allows.
func TestNegotiationOpensAParsedClosedWindowToTheLeast(t *testing.T) {
	var paths = []struct {
		name  string
		cr    ax25.CmdRes
		apply func(S *ax25_dlsm_t, param *xid_param_s)
	}{
		{"command", ax25.CRCmd, negotiation_response},
		{"response", ax25.CRRes, complete_negotiation},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			setupTestEnv(t)

			var hook = test.NewGlobal()

			t.Cleanup(hook.Reset)

			var sent = new(xid_param_s)
			sent.SREJ = SREJNone
			sent.Modulo = ax25.Modulo8
			sent.WindowSizeRx = maybe.Just(0)

			var param, _, status = xid_parse(xid_encode(sent, path.cr))
			require.True(t, status)

			var S = newNegotiationTestLink()

			path.apply(S, param)

			assert.Equal(t, AX25_K_MAXFRAME_BASIC_MIN, S.k_maxframe)

			var entry = hook.LastEntry()
			if assert.NotNil(t, entry) {
				assert.Equal(t, logrus.WarnLevel, entry.Level)
				assert.Equal(t, "window_size_rx", entry.Data["parameter"])
				assert.Equal(t, 0, entry.Data["asked"])
			}
		})
	}
}

// Answering an XID command, what we send back is what we now run with, so the
// two ends agree - not the nonsense the other station asked for.
func TestNegotiationResponseSendsBackWhatItApplied(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()

	var param = new(xid_param_s)
	param.SREJ = SREJNone
	param.Modulo = 16
	param.IFieldLengthRx = maybe.Just(0)
	param.WindowSizeRx = maybe.Just(0)

	negotiation_response(S, param)

	assert.Equal(t, S.modulo, param.Modulo)
	assert.Equal(t, maybe.Just(S.n1_paclen), param.IFieldLengthRx)
	assert.Equal(t, maybe.Just(S.k_maxframe), param.WindowSizeRx)
}

// A response that moves the link to modulo 8 but leaves the window size out
// must not keep a window wider than modulo 8 can hold.
func TestCompleteNegotiationNarrowsTheWindowToTheNewModulo(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()
	S.modulo = ax25.Modulo128
	S.k_maxframe = 32

	var param, _, status = xid_parse(nil)
	assert.True(t, status)

	param.Modulo = ax25.Modulo8

	complete_negotiation(S, param)

	assert.Equal(t, ax25.Modulo8, S.modulo)
	assert.Equal(t, AX25_K_MAXFRAME_BASIC_MAX, S.k_maxframe)
}

// A response that moves the link to modulo 128 but leaves the I field length
// out must not keep an N1 too small for V2.2 segmentation to send anything.
func TestCompleteNegotiationRaisesN1ToTheNewModulo(t *testing.T) {
	setupTestEnv(t)

	var S = newNegotiationTestLink()
	S.n1_paclen = 1

	var param, _, status = xid_parse(nil)
	assert.True(t, status)

	param.Modulo = ax25.Modulo128

	complete_negotiation(S, param)

	assert.Equal(t, ax25.Modulo128, S.modulo)
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

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = THEIR_CALL
	addrs[ax25.Destination] = MY_CALL

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil)
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
	assert.Equal(t, ax25.Modulo8, ax25Link.listHead.modulo)
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
		modulo   ax25.Modulo
		n1Paclen int
	}{
		{"a v2.0 link with no room at all", ax25.Modulo8, 0},
		{"a v2.2 link with no room for a segment header", ax25.Modulo128, 1},
		{"a v2.2 link with no room for data behind the header", ax25.Modulo128, 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			setupTestEnv(t)

			var S = get_link_handle([ax25.MaxAddrs]string{OWNCALL: MY_CALL, PEERCALL: THEIR_CALL}, 2, CHANNEL, 0, true)
			require.NotNil(t, S)

			S.modulo = testCase.modulo
			S.n1_paclen = testCase.n1Paclen

			var E = newDataRequest(MY_CALL, THEIR_CALL, CHANNEL, []byte("Testing"))

			runWithTimeout(t, "dl_data_request", func() { dl_data_request(E) })

			assert.Nil(t, E.txdata, "data that cannot be sent should have been discarded")
		})
	}
}

// A segment's header is the sender's to get wrong: a fragment with nothing in
// it, or a first segment with no room for the PID it carries, is a protocol
// error to report and throw away rather than read past the end of.
func TestDLDataIndicationShortSegments(t *testing.T) {
	var firstSegment = []byte{0x82, 0xF0, 'H', 'i'}

	for _, tc := range []struct {
		name         string
		reassembling bool
		segment      []byte
	}{
		{"empty fragment, ready", false, nil},
		{"first segment with no PID", false, []byte{0x82}},
		{"empty fragment, reassembling", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTestEnv(t)

			var S = establishConnection(t, "Q1TEST", "Q2TEST", 0)

			if tc.reassembling {
				dl_data_indication(S, ax25.PIDSegmentationFragment, firstSegment)
				require.NotNil(t, S.ra_buff)
			}

			var hook = test.NewGlobal()
			t.Cleanup(hook.Reset)

			dl_data_indication(S, ax25.PIDSegmentationFragment, tc.segment)

			assert.Nil(t, S.ra_buff, "a bad segment abandons any reassembly")
			require.NotNil(t, hook.LastEntry())
			assert.Equal(t, logrus.ErrorLevel, hook.LastEntry().Level)
		})
	}
}

// A link coming up turns on its channel's connected indicator, and going down
// turns it off again.
func TestEnterNewStateSetsConnectedIndicator(t *testing.T) {
	var origSetOutput = ax25Link.setOutput

	t.Cleanup(func() { ax25Link.setOutput = origSetOutput })

	var set []string

	ax25Link.setOutput = func(ot int, channel int, state int) {
		set = append(set, fmt.Sprintf("%s %d=%d", octypeName(ot), channel, state))
	}

	var S = new(ax25_dlsm_t)
	S.channel = 1
	S.state = state_1_awaiting_connection

	enter_new_state(S, state_3_connected)
	enter_new_state(S, state_4_timer_recovery) // Still connected, so no change.
	enter_new_state(S, state_0_disconnected)

	assert.Equal(t, []string{"CON 1=1", "CON 1=0"}, set)
}

// recordingLinkClients notes the outstanding frame counts the link layer
// reports, and ignores the rest.
type recordingLinkClients struct {
	noLinkClients

	replies []string
}

func (r *recordingLinkClients) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	r.replies = append(r.replies, fmt.Sprintf("%d/%d %s>%s %d", channel, client, ownCall, remoteCall, count))
}

// A client asking how much is waiting on a link that doesn't exist is told
// nothing is, rather than left waiting for an answer.
func TestOutstandingFramesForNoLinkRepliesNone(t *testing.T) {
	var saved = *ax25Link

	t.Cleanup(func() { *ax25Link = saved })

	*ax25Link = *NewAX25Link()

	var clients = new(recordingLinkClients)
	ax25_link_init(new(misc_config_s), nil, clients, 0)

	var E = new(dlq_item_t)
	E._chan = 0
	E.client = 2
	E.addrs[OWNCALL] = "Q1TEST"
	E.addrs[PEERCALL] = "Q2TEST"
	E.num_addr = 2

	testutils.CaptureOutput(t, func() { dl_outstanding_frames_request(E) })

	assert.Equal(t, []string{"0/2 Q1TEST>Q2TEST 0"}, clients.replies)
}
