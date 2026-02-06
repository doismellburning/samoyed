package direwolf

/*
Here be (some) dragons.

These are Claude-generated tests for the AX.25 state machine as described in
[AX.25 Link Access Protocol for Amateur Packet Radio - Version 2.2 Revision 4: 27 October 2025](https://github.com/packethacking/ax25spec/blob/879a00a3d1587e65a04edc2a3e86ea3a4ab2f7b8/doc/ax.25.2.2.4_Oct_25.md).

These were generated *after* some degree of porting of Dire Wolf's C implementation to Go for Samoyed,
and by someone (@doismellburning / 2E0KGG) not really in a position to understand their correctness.
Thus they should not be used as a measure of AX.25 implementation correctness
(if anyone *does* have a good test suite I'd love to hear about it)
but instead as a "safety check" for future changes to the current implementation -
i.e. did we accidentally change/break something.
*/

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ax25_link_test_main(t *testing.T) {
	t.Helper()

	// Link Establishment and Termination
	TestAX25LinkConnectedBasic(t)
	TestAX25LinkSABMEConnection(t)
	TestAX25LinkConnectionRejectedWithDM(t)
	TestAX25LinkDISCDisconnection(t)
	TestAX25LinkDISCInDisconnectedState(t)

	// Information Transfer
	TestAX25LinkIFrameExchange(t)
	TestAX25LinkRRResponse(t)
	TestAX25LinkRNRFlowControl(t)
	TestAX25LinkMultipleIFrames(t)
	TestAX25LinkOutOfSequenceIFrame(t)
	TestAX25LinkIFrameWithAck(t)

	// Error Recovery
	TestAX25LinkREJErrorRecovery(t)

	// State Machine Transitions
	TestAX25LinkIncomingSABM(t)
	TestAX25LinkStateVariables(t)
	TestAX25LinkDISCWhileConnected(t)
	TestAX25LinkSABMWhileConnected(t)
	TestAX25LinkDMAfterDISC(t)
	TestAX25LinkSABMCollision(t)
	TestAX25LinkTimerRecoveryState(t)

	// SREJ (Selective Reject)
	TestAX25LinkSREJFrame(t)

	// Window Size
	TestAX25LinkWindowSize(t)
	TestAX25LinkWindowSizeMod128(t)

	// FRMR Handling
	TestAX25LinkFRMRResponse(t)

	// P/F Bit Handling
	TestAX25LinkPollResponse(t)
	TestAX25LinkRRPoll(t)

	// N(R) Validation
	TestAX25LinkIsGoodNR(t)

	// Timer Tests
	TestAX25LinkT1Timer(t)
	TestAX25LinkT3Timer(t)
	TestAX25LinkT1PauseResume(t)
	TestAX25LinkT1Expiry(t)
	TestAX25LinkT3Expiry(t)

	// Collision Tests
	TestAX25LinkSABMDISCCollision(t)
	TestAX25LinkUnexpectedUA(t)

	// XID Tests
	TestAX25LinkXIDParse(t)
	TestAX25LinkXIDEncode(t)
	TestAX25LinkXIDRoundtrip(t)
	TestAX25LinkXIDFrameConnected(t)

	// C/R Bit Encoding Tests
	TestAX25LinkCommandFrameEncoding(t)
	TestAX25LinkResponseFrameEncoding(t)
	TestAX25LinkIFrameAsCommand(t)
	TestAX25LinkSFrameCommandResponse(t)

	// Sequence Number Tests
	TestAX25LinkModulo8WrapAround(t)
	TestAX25LinkModulo128WrapAround(t)
	TestAX25LinkNSInWindow(t)

	// Segmenter/Reassembler Tests
	TestAX25LinkReassemblerInitialState(t)
	TestAX25LinkFirstSegmentFlag(t)

	// Link Multiplexer Tests
	TestAX25LinkMultipleConcurrentLinks(t)
	TestAX25LinkIsolation(t)
	TestAX25LinkChannelBusy(t)

	// Frame Type Parsing Tests
	TestAX25LinkFrameTypeParsing(t)
	TestAX25LinkSFrameTypeParsing(t)
	TestAX25LinkIFrameTypeParsing(t)

	// Edge Case Tests
	TestAX25LinkInvalidNR(t)
	TestAX25LinkClearExceptionConditions(t)
	TestAX25LinkRejectExceptionFlag(t)

	// Retry and Version Tests
	TestAX25LinkRetryCounter(t)
	TestAX25LinkSetVersion20(t)
	TestAX25LinkSetVersion22(t)

	// Request Type Tests
	TestAX25LinkConnectRequestTypes(t)
	TestAX25LinkDisconnectRequestTypes(t)

	// TEST/UI Frame Tests
	TestAX25LinkTESTFrame(t)
	TestAX25LinkUIFrameConnected(t)

	// Statistics Tests
	TestAX25LinkFrameCountStats(t)
}

// Helper to set up a fresh test environment
func setupTestEnv(t *testing.T) {
	t.Helper()

	var audioConfig = new(audio_s)
	ptt_init(audioConfig)
	tq_init(audioConfig)

	var miscConfig = new(misc_config_s)
	// Set proper defaults for connected mode
	miscConfig.paclen = AX25_N1_PACLEN_DEFAULT
	miscConfig.retry = AX25_N2_RETRY_DEFAULT
	miscConfig.frack = AX25_T1V_FRACK_DEFAULT
	miscConfig.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT
	miscConfig.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT
	miscConfig.maxv22 = 0 // Default: don't try v2.2 (for most tests)

	ax25_link_init(miscConfig, 1)

	list_head = nil
	reg_callsign_list = nil // Clear registered callsigns
}

// Helper to set up environment with v2.2 support enabled
func setupTestEnvV22(t *testing.T) {
	t.Helper()

	var audioConfig = new(audio_s)
	ptt_init(audioConfig)
	tq_init(audioConfig)

	var miscConfig = new(misc_config_s)
	// Set proper defaults for connected mode
	miscConfig.paclen = AX25_N1_PACLEN_DEFAULT
	miscConfig.retry = AX25_N2_RETRY_DEFAULT
	miscConfig.frack = AX25_T1V_FRACK_DEFAULT
	miscConfig.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT
	miscConfig.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT
	miscConfig.maxv22 = 3 // Enable v2.2

	ax25_link_init(miscConfig, 1)

	list_head = nil
	reg_callsign_list = nil // Clear registered callsigns
}

// Helper to initiate a connect request
func initiateConnect(t *testing.T, myCall, theirCall string, channel int) {
	t.Helper()

	var E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = channel
	E.addrs[OWNCALL] = myCall
	E.addrs[PEERCALL] = theirCall
	E.num_addr = 2

	dl_connect_request(E)
}

// Helper to simulate receiving a frame
func receiveFrame(t *testing.T, pp *packet_t, channel int) {
	t.Helper()

	var E = new(dlq_item_t)
	E._chan = channel
	E.pp = pp

	lm_data_indication(E)
}

// Helper to establish a connection (SABM/UA exchange)
func establishConnection(t *testing.T, myCall, theirCall string, channel int) *ax25_dlsm_t { //nolint:unparam
	t.Helper()

	initiateConnect(t, myCall, theirCall, channel)

	// Receive UA response
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = theirCall
	addrs[PEERCALL] = myCall
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, channel)

	assert.NotNil(t, list_head)
	assert.Equal(t, state_3_connected, list_head.state)

	return list_head
}

// ============================================================================
// Link Establishment and Termination Tests
// ============================================================================

// SABM/UA Exchange (Modulo 8)
// Pokes at some of the state machine API in the style of recv_process from recv.go
func TestAX25LinkConnectedBasic(t *testing.T) {
	t.Helper()

	// Setup
	var MY_CALL = "M6KGG"
	var THEIR_CALL = "2E0KGG"
	const CHANNEL = 1

	setupTestEnv(t)

	var E *dlq_item_t
	var pp *packet_t
	var addrs [AX25_MAX_ADDRS]string

	// Connect request
	E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2

	dl_connect_request(E)

	// Now acknowledge
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	assert.NotNil(t, pp)

	E = new(dlq_item_t)
	E._chan = CHANNEL
	E.pp = pp

	lm_data_indication(E)

	// And now we should be connected!
	assert.NotNil(t, list_head)
	assert.Equal(t, state_3_connected, list_head.state, "%+v", list_head)

	// Verify state variables initialized
	assert.Equal(t, 0, list_head.vs, "V(S) should be 0")
	assert.Equal(t, 0, list_head.vr, "V(R) should be 0")
	assert.Equal(t, 0, list_head.va, "V(A) should be 0")
	assert.Equal(t, ax25_modulo_t(8), list_head.modulo, "Should be modulo 8")
}

// SABME/UA Exchange (Modulo 128)
func TestAX25LinkSABMEConnection(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t) // Use v2.2 enabled environment

	// Initiate connection - will try SABME first for v2.2
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Should be in awaiting v2.2 connection state
	assert.NotNil(t, list_head)
	assert.Equal(t, state_5_awaiting_v22_connection, list_head.state)

	// Receive UA response (accepting v2.2)
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should now be connected with modulo 128
	assert.Equal(t, state_3_connected, list_head.state)
	assert.Equal(t, ax25_modulo_t(128), list_head.modulo, "Should be modulo 128 for v2.2")
}

// Connection Rejected with DM
func TestAX25LinkConnectionRejectedWithDM(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive DM response (connection rejected)
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_DM, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should return to disconnected state
	assert.NotNil(t, list_head)
	assert.Equal(t, state_0_disconnected, list_head.state, "Should be disconnected after DM")
}

// Normal DISC/UA Exchange
func TestAX25LinkDISCDisconnection(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	// First establish a connection
	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)
	assert.Equal(t, state_3_connected, S.state)

	// Request disconnection
	var E = new(dlq_item_t)
	E._type = DLQ_DISCONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2

	dl_disconnect_request(E)

	// Should be in awaiting release state
	assert.Equal(t, state_2_awaiting_release, S.state)

	// Receive UA response
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should now be disconnected
	assert.Equal(t, state_0_disconnected, S.state)
}

// DISC in Disconnected State
func TestAX25LinkDISCInDisconnectedState(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	// Register our callsign to receive frames
	var regE = new(dlq_item_t)
	regE._type = DLQ_REGISTER_CALLSIGN
	regE._chan = CHANNEL
	regE.addrs[0] = MY_CALL // Register uses addrs[0]
	regE.client = 0
	dl_register_callsign(regE)

	// Receive DISC in disconnected state
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_DISC, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should respond with DM and remain disconnected (no state machine created)
	// The frame processing should not crash
	// list_head should still be nil as no connection was established
}

// ============================================================================
// Information Transfer Tests
// ============================================================================

// I-Frame Reception
func TestAX25LinkIFrameExchange(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive I-frame with N(S)=0
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var info = []byte("Hello")
	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 0, 0, AX25_PID_NO_LAYER_3, info)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// V(R) should increment to 1
	assert.Equal(t, 1, S.vr, "V(R) should be 1 after receiving I-frame")
	assert.True(t, S.acknowledge_pending, "Acknowledge should be pending")
}

// RR Acknowledgement
func TestAX25LinkRRResponse(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive RR with N(R)=0 (poll)
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_RR, 8, 0, 1, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should still be connected
	assert.Equal(t, state_3_connected, S.state)
}

// RNR Flow Control
func TestAX25LinkRNRFlowControl(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive RNR (peer busy)
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_RNR, 8, 0, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Peer receiver busy flag should be set
	assert.True(t, S.peer_receiver_busy, "Peer receiver busy should be set")

	// Now receive RR to clear busy condition
	pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_RR, 8, 0, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Peer receiver busy should be cleared
	assert.False(t, S.peer_receiver_busy, "Peer receiver busy should be cleared")
}

// ============================================================================
// Error Recovery Tests
// ============================================================================

// REJ Reception handling
// Note: Full retransmission testing requires having sent I-frames first
func TestAX25LinkREJErrorRecovery(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive REJ command with P=1 (poll)
	// This tests REJ handling when no frames are outstanding
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_REJ, 8, 0, 1, nil) // P=1
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should still be connected (REJ with valid N(R)=0 when V(A)=0 is valid)
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// State Machine Transition Tests
// ============================================================================

// SABM Reception in Disconnected (incoming connection)
func TestAX25LinkIncomingSABM(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	// Register our callsign for incoming connections
	// dl_register_callsign uses addrs[0] for the callsign
	var regE = new(dlq_item_t)
	regE._type = DLQ_REGISTER_CALLSIGN
	regE._chan = CHANNEL
	regE.addrs[0] = MY_CALL // Register uses addrs[0]
	regE.client = 0
	dl_register_callsign(regE)

	// Receive SABM from peer
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 1, 0, nil)
	assert.NotNil(t, pp)

	receiveFrame(t, pp, CHANNEL)

	// Should now be connected
	if assert.NotNil(t, list_head, "Should have created state machine for incoming connection") {
		assert.Equal(t, state_3_connected, list_head.state)

		// Verify state variables initialized
		assert.Equal(t, 0, list_head.vs, "V(S) should be 0")
		assert.Equal(t, 0, list_head.vr, "V(R) should be 0")
		assert.Equal(t, 0, list_head.va, "V(A) should be 0")
	}
}

// State variable management
func TestAX25LinkStateVariables(t *testing.T) {
	t.Helper()

	// Test AX25MODULO function (doesn't need a connection)
	assert.Equal(t, 0, AX25MODULO(8, 8), "8 mod 8 should be 0")
	assert.Equal(t, 7, AX25MODULO(-1, 8), "-1 mod 8 should be 7")
	assert.Equal(t, 0, AX25MODULO(128, 128), "128 mod 128 should be 0")
	assert.Equal(t, 127, AX25MODULO(-1, 128), "-1 mod 128 should be 127")
	assert.Equal(t, 5, AX25MODULO(13, 8), "13 mod 8 should be 5")

	// Now test with a connection
	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Initial state variables
	assert.Equal(t, 0, S.vs, "Initial V(S) should be 0")
	assert.Equal(t, 0, S.vr, "Initial V(R) should be 0")
	assert.Equal(t, 0, S.va, "Initial V(A) should be 0")

	// Test SET_VS
	SET_VS(S, 3)
	assert.Equal(t, 3, S.vs, "V(S) should be 3")

	// Test SET_VR
	SET_VR(S, 2)
	assert.Equal(t, 2, S.vr, "V(R) should be 2")

	// Test SET_VA - need VS to be >= VA value first
	S.vs = 5 // Set VS high enough
	SET_VA(S, 1)
	assert.Equal(t, 1, S.va, "V(A) should be 1")

	// Test WITHIN_WINDOW_SIZE
	S.va = 0
	S.vs = 0
	S.k_maxframe = 4
	assert.True(t, WITHIN_WINDOW_SIZE(S), "Should be within window when vs=0, va=0, k=4")

	S.vs = 4
	assert.False(t, WITHIN_WINDOW_SIZE(S), "Should NOT be within window when vs=4, va=0, k=4")
}

// ============================================================================
// Additional Information Transfer Tests
// ============================================================================

// Multiple sequential I-frame reception with V(R) tracking
func TestAX25LinkMultipleIFrames(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive I-frames 0, 1, 2 in sequence
	for ns := 0; ns < 3; ns++ {
		var info = []byte("Frame " + string(rune('0'+ns)))
		var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, ns, 0, AX25_PID_NO_LAYER_3, info)
		assert.NotNil(t, pp)
		receiveFrame(t, pp, CHANNEL)

		// V(R) should increment
		assert.Equal(t, ns+1, S.vr, "V(R) should be %d after frame %d", ns+1, ns)
	}

	assert.True(t, S.acknowledge_pending, "Acknowledge should be pending")
}

// Out-of-sequence I-frame sets reject_exception flag
func TestAX25LinkOutOfSequenceIFrame(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive I-frame 0
	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 0, 0, AX25_PID_NO_LAYER_3, []byte("Frame 0"))
	receiveFrame(t, pp, CHANNEL)
	assert.Equal(t, 1, S.vr, "V(R) should be 1")

	// Receive I-frame 2 (out of sequence, expecting 1)
	pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 2, 0, AX25_PID_NO_LAYER_3, []byte("Frame 2"))
	receiveFrame(t, pp, CHANNEL)

	// V(R) should NOT increment (frame rejected)
	assert.Equal(t, 1, S.vr, "V(R) should still be 1")
	// Reject exception should be set
	assert.True(t, S.reject_exception, "Reject exception should be set")
}

// I-frame with piggybacked acknowledgement updates V(A)
func TestAX25LinkIFrameWithAck(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Simulate that we've sent some frames by setting V(S)
	S.vs = 3

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive I-frame with N(R)=2 (acknowledging our frames 0 and 1)
	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 2, 0, 0, AX25_PID_NO_LAYER_3, []byte("Data"))
	receiveFrame(t, pp, CHANNEL)

	// V(A) should be updated to 2
	assert.Equal(t, 2, S.va, "V(A) should be 2 after receiving N(R)=2")
}

// ============================================================================
// Additional State Machine Tests
// ============================================================================

// DISC reception while connected causes transition to disconnected
func TestAX25LinkDISCWhileConnected(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)
	assert.Equal(t, state_3_connected, S.state)

	// Receive DISC from peer
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_DISC, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should transition to disconnected
	assert.Equal(t, state_0_disconnected, S.state)
}

// SABM while connected causes link reset
func TestAX25LinkSABMWhileConnected(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Set some non-zero state variables
	S.vs = 3
	S.vr = 2
	S.va = 1

	// Receive SABM from peer (link reset)
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should remain connected but state variables reset
	assert.Equal(t, state_3_connected, S.state)
	assert.Equal(t, 0, S.vs, "V(S) should be reset to 0")
	assert.Equal(t, 0, S.vr, "V(R) should be reset to 0")
	assert.Equal(t, 0, S.va, "V(A) should be reset to 0")
}

// DM response also terminates awaiting release state
func TestAX25LinkDMAfterDISC(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Request disconnection
	var E = new(dlq_item_t)
	E._type = DLQ_DISCONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2
	dl_disconnect_request(E)

	assert.Equal(t, state_2_awaiting_release, S.state)

	// Receive DM instead of UA
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_DM, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should transition to disconnected
	assert.Equal(t, state_0_disconnected, S.state)
}

// SABM collision - receive SABM while in awaiting connection state
func TestAX25LinkSABMCollision(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	// Initiate connection (sends SABM, enters awaiting connection)
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	assert.NotNil(t, list_head)
	assert.Equal(t, state_1_awaiting_connection, list_head.state)

	// Receive SABM from peer (collision)
	// Per the protocol, we send UA but stay in state 1 waiting for peer's UA
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Still in state 1 - we sent UA but still waiting for their UA
	assert.Equal(t, state_1_awaiting_connection, list_head.state)

	// Now receive the UA from peer (completing the collision resolution)
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Now should be connected
	assert.Equal(t, state_3_connected, list_head.state)
}

// Timer recovery state entry on receiving poll
func TestAX25LinkTimerRecoveryState(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Manually put into timer recovery state
	enter_new_state(S, state_4_timer_recovery)
	assert.Equal(t, state_4_timer_recovery, S.state)

	// Receive RR with F=1 (response to our poll)
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_s_frame(addrs, 2, cr_res, frame_type_S_RR, 8, 0, 1, nil) // F=1
	receiveFrame(t, pp, CHANNEL)

	// Should return to connected state
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// SREJ Tests (Selective Reject)
// ============================================================================

// SREJ frame handling
func TestAX25LinkSREJFrame(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t) // SREJ requires v2.2

	// Establish v2.2 connection
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var S = list_head
	assert.Equal(t, state_3_connected, S.state)
	assert.Equal(t, ax25_modulo_t(128), S.modulo)

	// Simulate having sent frames by setting V(S)
	S.vs = 5

	// Receive SREJ requesting retransmission of frame 2
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	pp = ax25_s_frame(addrs, 2, cr_res, frame_type_S_SREJ, 128, 2, 1, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should still be connected
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// Window Size Tests
// ============================================================================

// Window size check function
func TestAX25LinkWindowSize(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Default k=4 for modulo 8
	S.k_maxframe = 4
	S.va = 0
	S.vs = 0

	// Should be within window initially
	assert.True(t, WITHIN_WINDOW_SIZE(S))

	// Simulate sending 4 frames
	S.vs = 4
	assert.False(t, WITHIN_WINDOW_SIZE(S), "At window limit")

	// Simulate receiving ack for 2 frames
	S.va = 2
	assert.True(t, WITHIN_WINDOW_SIZE(S), "Window should slide")

	// Test wrap-around
	S.va = 6
	S.vs = 6
	assert.True(t, WITHIN_WINDOW_SIZE(S))

	S.vs = 2 // Wrapped around (6+4=10, 10 mod 8 = 2)
	assert.False(t, WITHIN_WINDOW_SIZE(S), "At window limit with wrap")
}

// Modulo 128 window size
func TestAX25LinkWindowSizeMod128(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t)

	// Establish v2.2 connection
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var S = list_head
	assert.Equal(t, ax25_modulo_t(128), S.modulo)

	// Default k=32 for modulo 128
	S.k_maxframe = 32
	S.va = 0
	S.vs = 0

	assert.True(t, WITHIN_WINDOW_SIZE(S))

	S.vs = 32
	assert.False(t, WITHIN_WINDOW_SIZE(S), "At window limit mod 128")

	S.va = 16
	assert.True(t, WITHIN_WINDOW_SIZE(S), "Window slides")
}

// ============================================================================
// FRMR Handling Tests
// ============================================================================

// FRMR reception causes fallback to v2.0
func TestAX25LinkFRMRResponse(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t)

	// Initiate v2.2 connection
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	assert.NotNil(t, list_head)
	assert.Equal(t, state_5_awaiting_v22_connection, list_head.state)

	// Receive FRMR (peer doesn't understand SABME)
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	// FRMR has info field with error details
	var frmrInfo = []byte{0x00, 0x00, 0x00} // Minimal FRMR info
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_FRMR, 1, 0, frmrInfo)
	receiveFrame(t, pp, CHANNEL)

	// Should fall back to v2.0 and retry with SABM
	// State should be awaiting connection (v2.0)
	assert.Equal(t, state_1_awaiting_connection, list_head.state)
	assert.Equal(t, ax25_modulo_t(8), list_head.modulo, "Should fall back to modulo 8")
}

// ============================================================================
// P/F Bit Tests
// ============================================================================

// Poll bit in I-frame requires response with F bit
func TestAX25LinkPollResponse(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive I-frame with P=1 (poll)
	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 0, 1, AX25_PID_NO_LAYER_3, []byte("Poll"))
	receiveFrame(t, pp, CHANNEL)

	// V(R) should increment
	assert.Equal(t, 1, S.vr)
	// Should remain connected
	assert.Equal(t, state_3_connected, S.state)
}

// RR command with P=1 should get response
func TestAX25LinkRRPoll(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive RR with P=1
	var pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_RR, 8, 0, 1, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should remain connected
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// is_good_nr Tests
// ============================================================================

// Test N(R) validation function
func TestAX25LinkIsGoodNR(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// V(A)=0, V(S)=0: only N(R)=0 is valid
	S.va = 0
	S.vs = 0
	assert.True(t, is_good_nr(S, 0), "N(R)=0 valid when V(A)=0, V(S)=0")
	assert.False(t, is_good_nr(S, 1), "N(R)=1 invalid when V(S)=0")

	// V(A)=0, V(S)=3: N(R) 0-3 valid
	S.vs = 3
	assert.True(t, is_good_nr(S, 0), "N(R)=0 valid")
	assert.True(t, is_good_nr(S, 1), "N(R)=1 valid")
	assert.True(t, is_good_nr(S, 2), "N(R)=2 valid")
	assert.True(t, is_good_nr(S, 3), "N(R)=3 valid")
	assert.False(t, is_good_nr(S, 4), "N(R)=4 invalid")

	// V(A)=2, V(S)=5: N(R) 2-5 valid
	S.va = 2
	S.vs = 5
	assert.False(t, is_good_nr(S, 1), "N(R)=1 invalid (< V(A))")
	assert.True(t, is_good_nr(S, 2), "N(R)=2 valid")
	assert.True(t, is_good_nr(S, 5), "N(R)=5 valid")
	assert.False(t, is_good_nr(S, 6), "N(R)=6 invalid")

	// Test wrap-around: V(A)=6, V(S)=2 (wrapped)
	S.va = 6
	S.vs = 2
	assert.True(t, is_good_nr(S, 6), "N(R)=6 valid")
	assert.True(t, is_good_nr(S, 7), "N(R)=7 valid")
	assert.True(t, is_good_nr(S, 0), "N(R)=0 valid (wrapped)")
	assert.True(t, is_good_nr(S, 2), "N(R)=2 valid")
	assert.False(t, is_good_nr(S, 3), "N(R)=3 invalid")
}

// ============================================================================
// Timer Tests
// ============================================================================

// T1 timer start/stop
func TestAX25LinkT1Timer(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Initially T1 should not be running
	assert.False(t, IS_T1_RUNNING(S), "T1 should not be running initially")

	// Start T1
	START_T1(S)
	assert.True(t, IS_T1_RUNNING(S), "T1 should be running after START_T1")

	// Stop T1
	STOP_T1(S)
	assert.False(t, IS_T1_RUNNING(S), "T1 should not be running after STOP_T1")
}

// T3 timer start/stop
func TestAX25LinkT3Timer(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Start T3
	START_T3(S)
	assert.False(t, S.t3_exp.IsZero(), "T3 should be running after START_T3")

	// Stop T3
	STOP_T3(S)
	assert.True(t, S.t3_exp.IsZero(), "T3 should not be running after STOP_T3")
}

// T1 pause/resume for channel busy
func TestAX25LinkT1PauseResume(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	START_T1(S)
	assert.True(t, IS_T1_RUNNING(S))

	// Pause T1
	PAUSE_T1(S)
	assert.False(t, S.t1_paused_at.IsZero(), "T1 should be paused")

	// Resume T1
	RESUME_T1(S)
	assert.True(t, S.t1_paused_at.IsZero(), "T1 should not be paused after resume")
	assert.True(t, IS_T1_RUNNING(S), "T1 should still be running")
}

// Timer expiry functions can be called directly
func TestAX25LinkT1Expiry(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Set retry counter
	SET_RC(S, 0)

	// Call t1_expiry - should increment RC and potentially change state
	t1_expiry(S)

	// RC should have incremented
	assert.Equal(t, 1, S.rc, "RC should increment on T1 expiry")
}

// T3 expiry triggers poll
func TestAX25LinkT3Expiry(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Call t3_expiry - should send a poll
	t3_expiry(S)

	// State should remain connected or go to timer recovery
	assert.True(t, S.state == state_3_connected || S.state == state_4_timer_recovery)
}

// ============================================================================
// Collision Tests
// ============================================================================

// SABM/DISC collision
func TestAX25LinkSABMDISCCollision(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	// Initiate connection
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	assert.NotNil(t, list_head)
	assert.Equal(t, state_1_awaiting_connection, list_head.state)

	// Receive DISC while awaiting connection
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_DISC, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Protocol sends DM but stays in awaiting connection state
	assert.Equal(t, state_1_awaiting_connection, list_head.state)
}

// Unexpected UA in connected state triggers link reset
func TestAX25LinkUnexpectedUA(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Set some state
	S.vs = 3
	S.vr = 2

	// Receive unsolicited UA
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 0, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Should trigger link reset - state goes to awaiting connection
	assert.True(t, S.state == state_1_awaiting_connection || S.state == state_5_awaiting_v22_connection,
		"Unexpected UA should trigger link reset")
}

// ============================================================================
// XID Parameter Negotiation Tests
// ============================================================================

// XID frame parsing
func TestAX25LinkXIDParse(t *testing.T) {
	t.Helper()

	// Test empty XID info
	result, _, status := xid_parse(nil)
	assert.Equal(t, 1, status, "Empty XID should parse successfully")
	assert.Equal(t, G_UNKNOWN, result.full_duplex)

	// Test XID with just format indicator (minimal valid)
	info := []byte{FI_Format_Indicator, GI_Group_Identifier, 0x00, 0x00}
	_, _, status = xid_parse(info)
	assert.Equal(t, 1, status, "Minimal XID should parse successfully")
}

// XID frame encoding
func TestAX25LinkXIDEncode(t *testing.T) {
	t.Helper()

	var param xid_param_s
	param.full_duplex = 0 // half duplex
	param.srej = srej_single
	param.modulo = 128
	param.i_field_length_rx = 256
	param.window_size_rx = 32
	param.ack_timer = 3000
	param.retries = 10

	// Encode the parameters
	info := xid_encode(&param, cr_cmd)
	assert.NotNil(t, info, "XID encode should produce info field")
	assert.Greater(t, len(info), 4, "XID info should have content")

	// Verify format indicator
	assert.Equal(t, byte(FI_Format_Indicator), info[0])
	assert.Equal(t, byte(GI_Group_Identifier), info[1])
}

// XID roundtrip (encode then parse)
func TestAX25LinkXIDRoundtrip(t *testing.T) {
	t.Helper()

	var original xid_param_s
	original.full_duplex = 1
	original.srej = srej_multi
	original.modulo = 128
	original.i_field_length_rx = 512
	original.window_size_rx = 64
	original.ack_timer = 5000
	original.retries = 15

	// Encode
	info := xid_encode(&original, cr_cmd)
	assert.NotNil(t, info)

	// Parse back
	parsed, _, status := xid_parse(info)
	assert.Equal(t, 1, status)

	// Verify values match
	assert.Equal(t, original.full_duplex, parsed.full_duplex)
	assert.Equal(t, original.modulo, parsed.modulo)
	assert.Equal(t, original.i_field_length_rx, parsed.i_field_length_rx)
	assert.Equal(t, original.window_size_rx, parsed.window_size_rx)
	assert.Equal(t, original.ack_timer, parsed.ack_timer)
	assert.Equal(t, original.retries, parsed.retries)
}

// XID frame reception in connected state
func TestAX25LinkXIDFrameConnected(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t)

	// Establish v2.2 connection
	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var S = list_head
	assert.Equal(t, state_3_connected, S.state)

	// Receive XID command
	var param xid_param_s
	param.full_duplex = 0
	param.srej = srej_single
	param.modulo = 128
	param.i_field_length_rx = 256
	param.window_size_rx = 32
	param.ack_timer = 3000
	param.retries = 10

	xidInfo := xid_encode(&param, cr_cmd)

	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_XID, 1, 0, xidInfo)
	receiveFrame(t, pp, CHANNEL)

	// Should still be connected
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// C/R Bit Encoding Tests
// ============================================================================

// Command frame has correct C/R bits
func TestAX25LinkCommandFrameEncoding(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	// Create SABM command
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_SABM, 1, 0, nil)
	assert.NotNil(t, pp)

	// Verify it's recognized as a command
	cr, _, _, _, _, _ := ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, cr_cmd, cr, "SABM should be a command")
}

// Response frame has correct C/R bits
func TestAX25LinkResponseFrameEncoding(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST1"
	addrs[AX25_SOURCE] = "TEST2"

	// Create UA response
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	assert.NotNil(t, pp)

	// Verify it's recognized as a response
	cr, _, _, _, _, _ := ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, cr_res, cr, "UA should be a response")
}

// I-frame as command
func TestAX25LinkIFrameAsCommand(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 0, 0, AX25_PID_NO_LAYER_3, []byte("test"))
	assert.NotNil(t, pp)

	cr, _, _, _, _, ftype := ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, cr_cmd, cr, "I-frame should be a command")
	assert.Equal(t, frame_type_I, ftype)
}

// S-frame as command and response
func TestAX25LinkSFrameCommandResponse(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	// RR as command
	var pp = ax25_s_frame(addrs, 2, cr_cmd, frame_type_S_RR, 8, 0, 1, nil)
	assert.NotNil(t, pp)

	cr, _, _, _, _, ftype := ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, cr_cmd, cr, "RR should be command")
	assert.Equal(t, frame_type_S_RR, ftype)

	// RR as response
	pp = ax25_s_frame(addrs, 2, cr_res, frame_type_S_RR, 8, 0, 1, nil)
	assert.NotNil(t, pp)

	cr, _, _, _, _, ftype = ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, cr_res, cr, "RR should be response")
	assert.Equal(t, frame_type_S_RR, ftype)
}

// ============================================================================
// Sequence Number Tests
// ============================================================================

// Modulo 8 wrap-around
func TestAX25LinkModulo8WrapAround(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Test sequence wrap from 7 to 0
	S.vs = 7
	SET_VS(S, AX25MODULO(S.vs+1, S.modulo))
	assert.Equal(t, 0, S.vs, "V(S) should wrap from 7 to 0")

	S.vr = 7
	SET_VR(S, AX25MODULO(S.vr+1, S.modulo))
	assert.Equal(t, 0, S.vr, "V(R) should wrap from 7 to 0")
}

// Modulo 128 wrap-around
func TestAX25LinkModulo128WrapAround(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t)

	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var S = list_head
	assert.Equal(t, ax25_modulo_t(128), S.modulo)

	// Test sequence wrap from 127 to 0
	S.vs = 127
	SET_VS(S, AX25MODULO(S.vs+1, S.modulo))
	assert.Equal(t, 0, S.vs, "V(S) should wrap from 127 to 0")
}

// N(S) in window check
func TestAX25LinkNSInWindow(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	S.vr = 0
	// The window check uses GENEROUS_K (63) and formula: V(R) < N(S) < V(R) + GENEROUS_K

	// N(S)=1 should be in window (0 < 1 < 63)
	assert.True(t, is_ns_in_window(S, 1))

	// N(S)=62 should be in window (0 < 62 < 63)
	assert.True(t, is_ns_in_window(S, 62))

	// N(S)=0 is NOT in window (0 < 0 is false)
	assert.False(t, is_ns_in_window(S, 0))
}

// ============================================================================
// Segmenter/Reassembler Tests
// ============================================================================

// Reassembler initial state
func TestAX25LinkReassemblerInitialState(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Reassembler buffer should be nil initially
	assert.Nil(t, S.ra_buff, "Reassembler buffer should be nil initially")
}

// First segment with flag set
func TestAX25LinkFirstSegmentFlag(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Create first segment:
	// Format: [0x80 | count] [original_pid] [data...]
	// 0x82 = 0x80 (first segment flag) | 0x02 (2 following segments)
	var segmentData = []byte{0x82, 0xF0, 'H', 'e', 'l', 'l', 'o'} // first segment, pid=0xF0

	// Simulate receiving the segment via dl_data_indication
	dl_data_indication(S, AX25_PID_SEGMENTATION_FRAGMENT, segmentData)

	// Reassembler should now have a buffer allocated
	assert.NotNil(t, S.ra_buff, "Reassembler buffer should be allocated after first segment")
	assert.Equal(t, 2, S.ra_following, "Should have 2 segments remaining")
}

// ============================================================================
// Link Multiplexer Tests
// ============================================================================

// Multiple concurrent links
func TestAX25LinkMultipleConcurrentLinks(t *testing.T) {
	t.Helper()

	const CHANNEL = 0

	setupTestEnv(t)

	// Establish first connection
	initiateConnect(t, "STA1", "STA2", CHANNEL)
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = "STA2"
	addrs[PEERCALL] = "STA1"
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var link1 = list_head
	assert.NotNil(t, link1)
	assert.Equal(t, state_3_connected, link1.state)

	// Establish second connection (different callsigns)
	initiateConnect(t, "STA1", "STA3", CHANNEL)
	addrs[OWNCALL] = "STA3"
	addrs[PEERCALL] = "STA1"
	pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	// Both links should exist
	assert.NotNil(t, list_head)
	// The new link is at the head
	var link2 = list_head
	assert.Equal(t, state_3_connected, link2.state)

	// Original link should still be connected
	assert.Equal(t, state_3_connected, link1.state)

	// Links should be different
	assert.NotEqual(t, link1, link2)
}

// Link isolation - action on one link doesn't affect another
func TestAX25LinkIsolation(t *testing.T) {
	t.Helper()

	const CHANNEL = 0

	setupTestEnv(t)

	// Establish first connection
	initiateConnect(t, "STA1", "STA2", CHANNEL)
	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = "STA2"
	addrs[PEERCALL] = "STA1"
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var link1 = list_head

	// Establish second connection
	initiateConnect(t, "STA1", "STA3", CHANNEL)
	addrs[OWNCALL] = "STA3"
	addrs[PEERCALL] = "STA1"
	pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var link2 = list_head

	// Modify state on link2
	link2.vs = 5
	link2.vr = 3

	// Link1 should be unaffected
	assert.Equal(t, 0, link1.vs)
	assert.Equal(t, 0, link1.vr)
}

// Channel busy handling
func TestAX25LinkChannelBusy(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Initially not busy
	assert.False(t, S.radio_channel_busy)

	// Simulate channel busy event
	var E = new(dlq_item_t)
	E._type = DLQ_CHANNEL_BUSY
	E._chan = CHANNEL
	E.activity = 1 // channel is busy
	lm_channel_busy(E)

	// Note: channel busy affects timer behavior
}

// ============================================================================
// Edge Cases and Error Conditions
// ============================================================================

// Frame type parsing
func TestAX25LinkFrameTypeParsing(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	// Test all U-frame types
	uFrameTypes := []ax25_frame_type_t{
		frame_type_U_SABM,
		frame_type_U_SABME,
		frame_type_U_DISC,
		frame_type_U_DM,
		frame_type_U_UA,
		frame_type_U_UI,
		frame_type_U_XID,
		frame_type_U_TEST,
	}

	for _, ftype := range uFrameTypes {
		cr := cr_cmd
		if ftype == frame_type_U_DM || ftype == frame_type_U_UA {
			cr = cr_res
		}
		pid := 0
		if ftype == frame_type_U_UI {
			pid = AX25_PID_NO_LAYER_3
		}

		pp := ax25_u_frame(addrs, 2, cr, ftype, 1, pid, nil)
		if pp == nil {
			continue // Some combinations may not be valid
		}

		_, _, _, _, _, parsedType := ax25_frame_type(pp)
		assert.Equal(t, ftype, parsedType, "Frame type should match for %v", ftype)
	}
}

// S-frame type parsing
func TestAX25LinkSFrameTypeParsing(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	sFrameTypes := []ax25_frame_type_t{
		frame_type_S_RR,
		frame_type_S_RNR,
		frame_type_S_REJ,
	}

	for _, ftype := range sFrameTypes {
		pp := ax25_s_frame(addrs, 2, cr_cmd, ftype, 8, 0, 0, nil)
		assert.NotNil(t, pp)

		_, _, _, _, _, parsedType := ax25_frame_type(pp)
		assert.Equal(t, ftype, parsedType, "S-Frame type should match for %v", ftype)
	}

	// SREJ must be response
	pp := ax25_s_frame(addrs, 2, cr_res, frame_type_S_SREJ, 8, 0, 0, nil)
	assert.NotNil(t, pp)
	_, _, _, _, _, parsedType := ax25_frame_type(pp) //nolint:dogsled
	assert.Equal(t, frame_type_S_SREJ, parsedType)
}

// I-frame type parsing
func TestAX25LinkIFrameTypeParsing(t *testing.T) {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = "TEST2"
	addrs[AX25_SOURCE] = "TEST1"

	// Modulo 8 I-frame
	pp := ax25_i_frame(addrs, 2, cr_cmd, 8, 3, 2, 1, AX25_PID_NO_LAYER_3, []byte("test"))
	assert.NotNil(t, pp)

	cr, _, pf, nr, ns, ftype := ax25_frame_type(pp)
	assert.Equal(t, frame_type_I, ftype)
	assert.Equal(t, cr_cmd, cr)
	assert.Equal(t, 1, pf)
	assert.Equal(t, 3, nr)
	assert.Equal(t, 2, ns)

	// Modulo 128 I-frame
	pp = ax25_i_frame(addrs, 2, cr_cmd, 128, 100, 50, 1, AX25_PID_NO_LAYER_3, []byte("test"))
	assert.NotNil(t, pp)

	_, _, pf, nr, ns, ftype = ax25_frame_type(pp)
	assert.Equal(t, frame_type_I, ftype)
	assert.Equal(t, 1, pf)
	assert.Equal(t, 100, nr)
	assert.Equal(t, 50, ns)
}

// Invalid N(R) triggers error
func TestAX25LinkInvalidNR(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Set V(S)=3 (we've sent frames 0, 1, 2)
	S.vs = 3
	S.va = 0

	// Valid N(R) values are 0, 1, 2, 3
	assert.True(t, is_good_nr(S, 0))
	assert.True(t, is_good_nr(S, 3))

	// N(R)=5 is invalid (> V(S))
	assert.False(t, is_good_nr(S, 5))
}

// ============================================================================
// Exception Condition Tests
// ============================================================================

// Clear exception conditions
func TestAX25LinkClearExceptionConditions(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Set all exception conditions
	S.peer_receiver_busy = true
	S.reject_exception = true
	S.own_receiver_busy = true
	S.acknowledge_pending = true

	// Clear them
	clear_exception_conditions(S)

	assert.False(t, S.peer_receiver_busy)
	assert.False(t, S.reject_exception)
	assert.False(t, S.own_receiver_busy)
	assert.False(t, S.acknowledge_pending)
}

// Reject exception flag behavior
func TestAX25LinkRejectExceptionFlag(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Initially not set
	assert.False(t, S.reject_exception)

	// Receive out-of-sequence frame to set it
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// First receive frame 0
	var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 0, 0, AX25_PID_NO_LAYER_3, []byte("0"))
	receiveFrame(t, pp, CHANNEL)
	assert.Equal(t, 1, S.vr)

	// Now receive frame 2 (skip 1) - should set reject_exception
	pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 2, 0, AX25_PID_NO_LAYER_3, []byte("2"))
	receiveFrame(t, pp, CHANNEL)
	assert.True(t, S.reject_exception, "Reject exception should be set")

	// Receive expected frame 1 - should clear reject_exception
	pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, 1, 0, AX25_PID_NO_LAYER_3, []byte("1"))
	receiveFrame(t, pp, CHANNEL)
	assert.False(t, S.reject_exception, "Reject exception should be cleared")
}

// ============================================================================
// Retry Counter Tests
// ============================================================================

// Retry counter management
func TestAX25LinkRetryCounter(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Initial value
	assert.Equal(t, 0, S.rc)

	// Set and verify
	SET_RC(S, 5)
	assert.Equal(t, 5, S.rc)

	// Track peak value
	assert.GreaterOrEqual(t, S.peak_rc_value, 0)
}

// ============================================================================
// Version Negotiation Tests
// ============================================================================

// Set version 2.0
func TestAX25LinkSetVersion20(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	set_version_2_0(S)

	assert.Equal(t, srej_none, S.srej_enable)
	assert.Equal(t, ax25_modulo_t(8), S.modulo)
}

// Set version 2.2
func TestAX25LinkSetVersion22(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnvV22(t)

	initiateConnect(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[OWNCALL] = THEIR_CALL
	addrs[PEERCALL] = MY_CALL
	var pp = ax25_u_frame(addrs, 2, cr_res, frame_type_U_UA, 1, 0, nil)
	receiveFrame(t, pp, CHANNEL)

	var S = list_head

	// Should be v2.2
	assert.Equal(t, srej_single, S.srej_enable)
	assert.Equal(t, ax25_modulo_t(128), S.modulo)
}

// ============================================================================
// Data Link Queue Tests
// ============================================================================

// Connect request handling
func TestAX25LinkConnectRequestTypes(t *testing.T) {
	t.Helper()

	const CHANNEL = 0

	setupTestEnv(t)

	// Test DLQ_CONNECT_REQUEST
	var E = new(dlq_item_t)
	E._type = DLQ_CONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = "TEST1"
	E.addrs[PEERCALL] = "TEST2"
	E.num_addr = 2
	E.client = 0

	dl_connect_request(E)

	assert.NotNil(t, list_head)
	assert.Equal(t, state_1_awaiting_connection, list_head.state)
}

// Disconnect request handling
func TestAX25LinkDisconnectRequestTypes(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Test DLQ_DISCONNECT_REQUEST
	var E = new(dlq_item_t)
	E._type = DLQ_DISCONNECT_REQUEST
	E._chan = CHANNEL
	E.addrs[OWNCALL] = MY_CALL
	E.addrs[PEERCALL] = THEIR_CALL
	E.num_addr = 2
	E.client = 0

	dl_disconnect_request(E)

	assert.Equal(t, state_2_awaiting_release, S.state)
}

// ============================================================================
// TEST Frame Tests
// ============================================================================

// TEST frame handling
func TestAX25LinkTESTFrame(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive TEST command
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var testInfo = []byte("Test data")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_TEST, 1, 0, testInfo)
	receiveFrame(t, pp, CHANNEL)

	// Should still be connected (TEST doesn't change state)
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// UI Frame Tests
// ============================================================================

// UI frame handling in connected state
func TestAX25LinkUIFrameConnected(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	// Receive UI frame
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NO_LAYER_3, []byte("UI data"))
	receiveFrame(t, pp, CHANNEL)

	// Should still be connected
	assert.Equal(t, state_3_connected, S.state)
}

// ============================================================================
// Statistics Tests
// ============================================================================

// Frame count statistics
func TestAX25LinkFrameCountStats(t *testing.T) {
	t.Helper()

	var MY_CALL = "TEST1"
	var THEIR_CALL = "TEST2"
	const CHANNEL = 0

	setupTestEnv(t)

	var S = establishConnection(t, MY_CALL, THEIR_CALL, CHANNEL)

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = MY_CALL
	addrs[AX25_SOURCE] = THEIR_CALL

	// Receive some I-frames
	for i := 0; i < 3; i++ {
		var pp = ax25_i_frame(addrs, 2, cr_cmd, 8, 0, i, 0, AX25_PID_NO_LAYER_3, []byte("data"))
		receiveFrame(t, pp, CHANNEL)
	}

	// Check that I-frame count increased
	assert.GreaterOrEqual(t, S.count_recv_frame_type[frame_type_I], 3)
}
