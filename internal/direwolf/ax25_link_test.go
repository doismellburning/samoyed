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
	"github.com/doismellburning/samoyed/internal/xid"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNegotiationTestLink is a link whose parameters are all distinct from the
// defaults negotiation would otherwise reach for, so a test can tell "kept what
// we had" apart from "filled in a default".  It belongs to link.
func newNegotiationTestLink(link *AX25Link) *ax25_dlsm_t {
	var S = new(ax25_dlsm_t)

	S.services = link.linkServices
	S.modulo = ax25.Modulo8
	S.srej_enable = xid.SREJNone
	S.n1_paclen = 128
	S.k_maxframe = 2
	S.n2_retry = 5
	S.t1v = 7 * time.Second

	return S
}

// An XID command that specifies nothing should get back a complete set of
// defaults, per the 2006 revision of the spec.
func TestNegotiationResponseFillsInDefaults(t *testing.T) {
	t.Parallel()

	var link = setupTestEnv(t)

	// Distinct from the 3000 mSec default so the two branches can be told apart.
	link.miscConfig.frack = 5

	var S = newNegotiationTestLink(link)

	var param, _, status = xid.Parse(nil)
	assert.True(t, status)

	negotiation_response(S, param)

	assert.Equal(t, ax25.Modulo8, param.Modulo)
	assert.Equal(t, xid.SREJNone, param.SREJ)
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
	t.Parallel()

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
			t.Parallel()

			var link = setupTestEnv(t)

			link.miscConfig.frack = 5

			var S = newNegotiationTestLink(link)

			var param = new(xid.Param)
			param.SREJ = xid.SREJMulti
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
			assert.Equal(t, xid.SREJMulti, S.srej_enable)
		})
	}
}

// "If this field is not present, the current values are retained."  A response
// that leaves a parameter out must not reset the running configuration to a
// default, or to zero.
func TestCompleteNegotiationKeepsWhatTheResponseOmits(t *testing.T) {
	t.Parallel()

	var link = setupTestEnv(t)

	var S = newNegotiationTestLink(link)

	// Everything absent, as xid.Parse gives for an empty info field.
	var param, _, status = xid.Parse(nil)
	assert.True(t, status)

	complete_negotiation(S, param)

	assert.Equal(t, ax25.Modulo8, S.modulo)
	assert.Equal(t, xid.SREJNone, S.srej_enable)
	assert.Equal(t, 128, S.n1_paclen)
	assert.Equal(t, 2, S.k_maxframe)
	assert.Equal(t, 5, S.n2_retry)
	assert.Equal(t, 7*time.Second, S.t1v)
}

// A response that specifies one parameter applies that one and leaves the rest,
// including converting the acknowledge timer from mSec to a Duration.
func TestCompleteNegotiationAppliesOnlyWhatTheResponseSpecifies(t *testing.T) {
	t.Parallel()

	var link = setupTestEnv(t)

	var S = newNegotiationTestLink(link)

	var param, _, status = xid.Parse(nil)
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
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestNegotiationBoundsWhatMakesNoSense(t *testing.T) {
	var paths = []struct {
		name    string
		apply   func(S *ax25_dlsm_t, param *xid.Param)
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
				var link = setupTestEnv(t)

				var hook = test.NewGlobal()

				t.Cleanup(hook.Reset)

				var S = newNegotiationTestLink(link)

				var param = new(xid.Param)
				param.SREJ = xid.SREJNone
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

// A closed window on the wire, not just in a hand-built xid.Param, is opened
// to the least there is rather than thrown wide: xid.Parse used to put 127 in
// place of anything out of range, which complete_negotiation then narrowed to
// the widest window the modulus allows.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestNegotiationOpensAParsedClosedWindowToTheLeast(t *testing.T) {
	var paths = []struct {
		name  string
		cr    ax25.CmdRes
		apply func(S *ax25_dlsm_t, param *xid.Param)
	}{
		{"command", ax25.CRCmd, negotiation_response},
		{"response", ax25.CRRes, complete_negotiation},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			var link = setupTestEnv(t)

			var hook = test.NewGlobal()

			t.Cleanup(hook.Reset)

			var sent = new(xid.Param)
			sent.SREJ = xid.SREJNone
			sent.Modulo = ax25.Modulo8
			sent.WindowSizeRx = maybe.Just(0)

			var param, _, status = xid.Parse(xid.Encode(sent, path.cr))
			require.True(t, status)

			var S = newNegotiationTestLink(link)

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
	t.Parallel()

	var link = setupTestEnv(t)

	var S = newNegotiationTestLink(link)

	var param = new(xid.Param)
	param.SREJ = xid.SREJNone
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
	t.Parallel()

	var link = setupTestEnv(t)

	var S = newNegotiationTestLink(link)
	S.modulo = ax25.Modulo128
	S.k_maxframe = 32

	var param, _, status = xid.Parse(nil)
	assert.True(t, status)

	param.Modulo = ax25.Modulo8

	complete_negotiation(S, param)

	assert.Equal(t, ax25.Modulo8, S.modulo)
	assert.Equal(t, AX25_K_MAXFRAME_BASIC_MAX, S.k_maxframe)
}

// A response that moves the link to modulo 128 but leaves the I field length
// out must not keep an N1 too small for V2.2 segmentation to send anything.
func TestCompleteNegotiationRaisesN1ToTheNewModulo(t *testing.T) {
	t.Parallel()

	var link = setupTestEnv(t)

	var S = newNegotiationTestLink(link)
	S.n1_paclen = 1

	var param, _, status = xid.Parse(nil)
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
//
//nolint:paralleltest // Serial: changes logrus's global logger.
func TestIgnoredConnectRequestIsLogged(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 1
	)

	var link = setupTestEnv(t) // Registers no callsigns.

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

	receiveFrame(t, link, pp, CHANNEL)

	assert.Nil(t, link.listHead, "an unregistered callsign gets no link state machine")

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
	E.txdata = NewCData(0xF0, data)

	return E
}

// A client application can ask to send connected mode data for a link that
// has never been connected, and the link it gets made for it has to be able
// to carry something: with its maximum information field at zero, every
// frame is too long for it, and the segmentation that follows cannot make
// progress.  Refs #683.
func TestDataRequestForALinkThatWasNeverConnected(t *testing.T) {
	t.Parallel()

	const (
		MY_CALL    = "Q1TEST"
		THEIR_CALL = "Q2TEST"
		CHANNEL    = 0
	)

	var link = setupTestEnv(t)

	var E = newDataRequest(MY_CALL, THEIR_CALL, CHANNEL, []byte("Testing"))

	runWithTimeout(t, "dl_data_request", func() { link.dl_data_request(E) })

	require.NotNil(t, link.listHead, "the request should have made a link")
	assert.Equal(t, link.miscConfig.paclen, link.listHead.n1_paclen,
		"a new link should start out able to carry what this station is configured for")
	assert.Equal(t, link.miscConfig.maxframe_basic, link.listHead.k_maxframe)
	assert.Equal(t, link.miscConfig.retry, link.listHead.n2_retry)
	assert.Equal(t, ax25.Modulo8, link.listHead.modulo)
}

// A link can end up with a maximum information field that nothing will fit
// in: XID negotiation takes the other end's word for it and an XID can ask
// for less than a byte, and PACLEN 1 is a configuration anyone can write.
// Segmenting data to fit either used to divide it into pieces of nothing,
// for ever, or divide by zero.  Refs #683.
func TestDataRequestForALinkThatCannotCarryAnything(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			var link = setupTestEnv(t)

			var S = link.get_link_handle([ax25.MaxAddrs]string{OWNCALL: MY_CALL, PEERCALL: THEIR_CALL}, 2, CHANNEL, 0, true)
			require.NotNil(t, S)

			S.modulo = testCase.modulo
			S.n1_paclen = testCase.n1Paclen

			var E = newDataRequest(MY_CALL, THEIR_CALL, CHANNEL, []byte("Testing"))

			runWithTimeout(t, "dl_data_request", func() { link.dl_data_request(E) })

			assert.Nil(t, E.txdata, "data that cannot be sent should have been discarded")
		})
	}
}

// A segment's header is the sender's to get wrong: a fragment with nothing in
// it, or a first segment with no room for the PID it carries, is a protocol
// error to report and throw away rather than read past the end of.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
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
			var link = setupTestEnv(t)

			var S = establishConnection(t, link, "Q1TEST", "Q2TEST", 0)

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
	t.Parallel()

	var set []string

	var link = NewAX25Link(new(misc_config_s), func(channel int, on bool) {
		set = append(set, fmt.Sprintf("%d=%t", channel, on))
	}, nil, nil, 0)

	var S = new(ax25_dlsm_t)
	S.services = link.linkServices
	S.channel = 1
	S.state = state_1_awaiting_connection

	enter_new_state(S, state_3_connected)
	enter_new_state(S, state_4_timer_recovery) // Still connected, so no change.
	enter_new_state(S, state_0_disconnected)

	assert.Equal(t, []string{"1=true", "1=false"}, set)
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
//
//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestOutstandingFramesForNoLinkRepliesNone(t *testing.T) {
	var clients = new(recordingLinkClients)
	var link = NewAX25Link(new(misc_config_s), nil, clients, nil, 0)

	var E = new(dlq_item_t)
	E._chan = 0
	E.client = 2
	E.addrs[OWNCALL] = "Q1TEST"
	E.addrs[PEERCALL] = "Q2TEST"
	E.num_addr = 2

	testutils.CaptureOutput(t, func() { link.dl_outstanding_frames_request(E) })

	assert.Equal(t, []string{"0/2 Q1TEST>Q2TEST 0"}, clients.replies)
}

// An I frame must have a PID.  One that stops before it is a protocol error,
// and its information part - nothing at all - is not handed to the client as
// though it were data with some PID.
func TestIFrameWithoutPIDIgnored(t *testing.T) {
	t.Parallel()

	var link = setupTestEnv(t)

	var S = establishConnection(t, link, "Q1TEST", "Q2TEST", 0)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "Q1TEST"
	addrs[ax25.Source] = "Q2TEST"

	var whole = ax25.IFrame(addrs, 2, ax25.CRCmd, ax25.Modulo8, 0, 0, 0, ax25.PIDNoLayer3, nil)
	require.NotNil(t, whole)

	var pp = ax25.FromFrame(whole.FrameData()[:15], ax25.ALevel{Rec: 50, Mark: 50, Space: 50})
	require.NotNil(t, pp)
	require.Equal(t, ax25.FrameTypeI, pp.FrameTypeOnly())

	receiveFrame(t, link, pp, 0)

	assert.Equal(t, 0, S.vr, "the I frame without a PID was accepted")
	assert.False(t, S.acknowledge_pending)
}

// warningsDuring runs fn and returns the warnings it logged.
func warningsDuring(t *testing.T, fn func()) []string {
	t.Helper()

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	fn()

	var warnings []string

	for _, entry := range hook.AllEntries() {
		if entry.Level == logrus.WarnLevel {
			warnings = append(warnings, entry.Message)
		}
	}

	return warnings
}

// sharedCall is the callsign the clients below both want.
const sharedCall = "Q1TEST-7"

// newRegisterRequest is a client application registering sharedCall.
func newRegisterRequest(channel int, client int) *dlq_item_t {
	var E = new(dlq_item_t)

	E._type = DLQ_REGISTER_CALLSIGN
	E._chan = channel
	E.addrs[0] = sharedCall
	E.num_addr = 1
	E.client = client

	return E
}

// newConnectRequest is a client application asking to connect from
// sharedCall to Q3TEST.
func newConnectRequest(channel int, client int) *dlq_item_t {
	var E = new(dlq_item_t)

	E._type = DLQ_CONNECT_REQUEST
	E._chan = channel
	E.addrs[OWNCALL] = sharedCall
	E.addrs[PEERCALL] = "Q3TEST"
	E.num_addr = 2
	E.client = client

	return E
}

// Regression test: a callsign another client had already registered on the
// channel was taken without a word, though connections to it then go to one
// client only - the node shell became unreachable once an AGW application
// registered its callsign.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestRegisteringATakenCallsignIsWarned(t *testing.T) {
	var link = setupTestEnv(t)

	var warnings = warningsDuring(t, func() {
		link.dl_register_callsign(newRegisterRequest(1, nodeClient))
		link.dl_register_callsign(newRegisterRequest(2, 0)) // Another channel.
		link.dl_register_callsign(newRegisterRequest(1, nodeClient))
	})
	assert.Empty(t, warnings)

	warnings = warningsDuring(t, func() {
		link.dl_register_callsign(newRegisterRequest(1, 0))
	})
	assert.Equal(t, []string{"Callsign is already registered on this channel by another client application, which may get its connections instead"}, warnings)
}

// Regression test: a client asking to connect to a station another client
// already has a link with, between the same two callsigns on the same
// channel, got a second link without a word, though frames from the station
// can only reach one of them.
//
//nolint:paralleltest // Serial: records logrus's global logger through a hook.
func TestSecondLinkBetweenTheSameStationsIsWarned(t *testing.T) {
	var link = setupTestEnv(t)

	var warnings = warningsDuring(t, func() {
		link.dl_connect_request(newConnectRequest(1, 0))
		link.dl_connect_request(newConnectRequest(1, 0)) // The same client again.
		link.dl_connect_request(newConnectRequest(2, nodeClient))
	})
	assert.Empty(t, warnings)

	warnings = warningsDuring(t, func() {
		link.dl_connect_request(newConnectRequest(1, nodeClient))
	})
	assert.Equal(t, []string{"Another client application already has a link between these stations on this channel, which may get its frames instead"}, warnings)
}

// establishedLinkClients counts the connect indications the link layer gives
// a client, and ignores the rest.
type establishedLinkClients struct {
	noLinkClients

	established int
}

func (c *establishedLinkClients) LinkEstablished(int, int, string, string, bool) {
	c.established++
}

// A SABM(E) on a link that is already up resets it.  The AX.25 2.2 SDL, and
// Dire Wolf, throw away whatever was sent and not yet acknowledged, and
// whatever was still waiting to go, and tell the client the link was
// established afresh.  When the SABM(E) is just the other station's retry,
// because our UA was slow to reach it, that loses the first thing we said - a
// node's greeting, say - and leaves a plain AX.25 client with a second connect
// indication and nothing else.  Like the Linux kernel's AX.25, we put the
// unacknowledged frames back at the front of the queue and send them again
// from N(S) 0, so there is nothing lost to tell the client about.  Refs #1052.
func TestLinkResetResendsUnacknowledgedFrames(t *testing.T) {
	t.Parallel()

	const (
		myCall    = "Q3TEST"
		theirCall = "Q4TEST"
		channel   = 1
	)

	var miscConfig = new(misc_config_s)
	miscConfig.paclen = AX25_N1_PACLEN_DEFAULT
	miscConfig.retry = AX25_N2_RETRY_DEFAULT
	miscConfig.frack = AX25_T1V_FRACK_DEFAULT
	miscConfig.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT
	miscConfig.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

	var clients = new(establishedLinkClients)
	var link = NewAX25Link(miscConfig, nil, clients, newTestTransmitQueue(new(RadioConfig)), 1)

	var S = establishConnection(t, link, myCall, theirCall, channel)

	require.Equal(t, 1, clients.established)

	// The transmitter is ready for whatever the link has to send.
	var seize = new(dlq_item_t)
	seize._chan = channel

	for _, data := range []string{"one", "two", "three"} {
		link.dl_data_request(newDataRequest(myCall, theirCall, channel, []byte(data)))
	}

	link.lm_seize_confirm(seize)
	require.Equal(t, 3, S.vs, "all three should have been sent")

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = myCall
	addrs[ax25.Source] = theirCall

	// The other end acknowledges the first.
	receiveFrame(t, link, ax25.SFrame(addrs, 2, ax25.CRRes, ax25.FrameTypeSRR, ax25.Modulo8, 1, 0, nil), channel)
	require.Equal(t, 1, S.va)

	// And something else is waiting to go.
	link.dl_data_request(newDataRequest(myCall, theirCall, channel, []byte("four")))

	receiveFrame(t, link, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil), channel)

	assert.Equal(t, state_3_connected, S.state)
	assert.Equal(t, 0, S.vs)
	assert.Equal(t, 0, S.va)
	assert.Equal(t, 0, S.vr)

	for ns, txdata := range S.txdata_by_ns {
		assert.Nil(t, txdata, "nothing should be left as sent with N(S) %d", ns)
	}

	var queued []string
	for txdata := S.i_frame_queue; txdata != nil; txdata = txdata.next {
		queued = append(queued, string(txdata.data[:txdata.len]))
	}

	assert.Equal(t, []string{"two", "three", "four"}, queued,
		"the unacknowledged frames should go again, ahead of what had not been sent")
	assert.Equal(t, 1, clients.established, "nothing was lost, so there is no new connection to report")

	link.lm_seize_confirm(seize)

	assert.Equal(t, 3, S.vs, "they should be sent again from N(S) 0")
	require.NotNil(t, S.txdata_by_ns[0])
	assert.Equal(t, "two", string(S.txdata_by_ns[0].data[:S.txdata_by_ns[0].len]))
}

// A SABM in timer recovery takes a v2.2 link back to v2.0, and so from
// modulo 128 to modulo 8.  The unacknowledged frames are numbered in the old
// sequence space, so re-queuing them must happen before the change: walking
// from V(A) to a V(S) of 8 or more in modulo 8 never gets there.  Refs #1052.
func TestLinkResetToV20ResendsUnacknowledgedFrames(t *testing.T) {
	t.Parallel()

	const (
		myCall    = "Q3TEST"
		theirCall = "Q4TEST"
		channel   = 1
	)

	var link = setupTestEnvV22(t)

	var S = establishConnection(t, link, myCall, theirCall, channel)

	require.Equal(t, ax25.Modulo128, S.modulo)

	var want []string

	for ns := 2; ns < 12; ns++ {
		var data = fmt.Sprintf("frame %d", ns)
		S.txdata_by_ns[ns] = NewCData(int(ax25.PIDNoLayer3), []byte(data))

		want = append(want, data)
	}

	S.va = 2
	S.vs = 12
	S.state = state_4_timer_recovery

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = myCall
	addrs[ax25.Source] = theirCall

	runWithTimeout(t, "a SABM in timer recovery", func() {
		receiveFrame(t, link, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil), channel)
	})

	assert.Equal(t, ax25.Modulo8, S.modulo)

	var queued []string
	for txdata := S.i_frame_queue; txdata != nil; txdata = txdata.next {
		queued = append(queued, string(txdata.data[:txdata.len]))
	}

	assert.Equal(t, want, queued)
}
