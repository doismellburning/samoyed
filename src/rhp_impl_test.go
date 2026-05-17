package direwolf

/*
These are Claude-generated tests for the RHP2 server (rhp.go).

They exercise the JSON message framing, socket lifecycle (OPEN/CLOSE/AUTH),
and the SendRecFrame / LinkEstablished / LinkTerminated / RecConnData
callbacks — all of which work without a running modem or transmit queue.

Tests that require a live transmit queue (dgram SEND → tq_append) or a full
ax25_link state machine are not included here.

Failing tests may indicate a bug, or they may reflect implementation-specific
behaviour that changed deliberately — check the context before assuming a bug.
*/

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// freePort returns a TCP port that is free at the moment of the call.
// There is an inherent TOCTOU race, but it is acceptable for tests.
func freePort(t *testing.T) int {
	t.Helper()
	var ln, err = net.Listen("tcp", ":0") //nolint:gosec // G102: intentional, test server binds to all interfaces
	require.NoError(t, err)
	var addr, ok = ln.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected *net.TCPAddr")
	var port = addr.Port
	require.NoError(t, ln.Close())
	return port
}

// startRHP starts an RHPService on a free port and returns the service and port.
// The service is automatically shut down when the test finishes.
func startRHP(t *testing.T) (*RHPService, int) {
	t.Helper()
	var port = freePort(t)
	var mc = new(misc_config_s)
	mc.rhp_port = port
	var svc = NewRHPService(mc)
	t.Cleanup(func() { svc.Close() }) //nolint:errcheck
	return svc, port
}

// dialRHP dials the RHP server, retrying for up to 100 ms while it starts up.
func dialRHP(t *testing.T, port int) net.Conn {
	t.Helper()
	var addr = fmt.Sprintf("localhost:%d", port)
	var conn net.Conn
	var err error
	for range 20 {
		conn, err = net.Dial("tcp", addr)
		if err == nil {
			t.Cleanup(func() { conn.Close() })
			return conn
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.NoError(t, err, "could not connect to RHP server on port %d", port)
	return nil
}

// rhpSend marshals msg and sends it with the 2-byte big-endian length header.
//
//nolint:errchkjson // test helper accepts map[string]any for convenience
func rhpSend(t *testing.T, conn net.Conn, msg map[string]any) {
	t.Helper()
	var data, merr = json.Marshal(msg)
	require.NoError(t, merr)
	var frame = make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(data)))
	copy(frame[2:], data)
	var _, werr = conn.Write(frame)
	require.NoError(t, werr)
}

// rhpRecvRaw reads one framed RHP message and returns the raw bytes.
func rhpRecvRaw(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	var buf, rerr = readRHPMsg(conn)
	require.NoError(t, rerr)
	return buf
}

// rhpRecv reads one framed RHP message and decodes it into a map.
func rhpRecv(t *testing.T, conn net.Conn) map[string]any {
	t.Helper()
	var result map[string]any
	require.NoError(t, json.Unmarshal(rhpRecvRaw(t, conn), &result))
	return result
}

// rhpRecvAs reads one framed RHP message and unmarshals it into T.
//
//nolint:ireturn // generic helper must return T
func rhpRecvAs[T any](t *testing.T, conn net.Conn) T {
	t.Helper()
	var result T
	require.NoError(t, json.Unmarshal(rhpRecvRaw(t, conn), &result))
	return result
}

// rhpMaybeRecv attempts to read one message, returning nil if nothing arrives
// within a short timeout.  Used to assert that no message is sent.
func rhpMaybeRecv(conn net.Conn) map[string]any {
	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)) //nolint:errcheck
	var buf, err = readRHPMsg(conn)
	if err != nil {
		return nil
	}
	var result map[string]any
	json.Unmarshal(buf, &result) //nolint:errcheck
	return result
}

// openSocket sends an OPEN message and returns the handle from OPENREPLY.
// Fails the test if errcode != 0.
func openSocket(t *testing.T, conn net.Conn, mode, port string, flags int) int {
	t.Helper()
	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"id":    1,
		"pfam":  "ax25",
		"mode":  mode,
		"port":  port,
		"flags": flags,
	})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)
	require.Equal(t, "openReply", reply.Type, "expected openReply")
	require.Equal(t, 0, reply.ErrCode, "expected errcode 0, got %v (%s)", reply.ErrCode, reply.ErrText)
	return reply.Handle
}

// ---------------------------------------------------------------------------
// AUTH
// ---------------------------------------------------------------------------

func TestRHP_Auth_ReturnsOK(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "auth", "user": "Q1TEST", "pass": "hunter2"})
	var reply = rhpRecvAs[rhpAuthReply](t, conn)

	assert.Equal(t, "authReply", reply.Type)
	assert.Equal(t, 0, reply.ErrCode)
	assert.Equal(t, "Ok", reply.ErrText)
}

func TestRHP_Auth_EchoesID(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "auth", "id": 42, "user": "Q1TEST", "pass": "x"})
	var reply = rhpRecvAs[rhpAuthReply](t, conn)

	assert.Equal(t, "authReply", reply.Type)
	assert.Equal(t, 42, reply.ID)
}

// ---------------------------------------------------------------------------
// OPEN / OPENREPLY
// ---------------------------------------------------------------------------

func TestRHP_Open_TraceReturnsHandle(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)
	assert.GreaterOrEqual(t, handle, 0)
}

func TestRHP_Open_RawReturnsHandle(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "raw", "0", 3)
	assert.GreaterOrEqual(t, handle, 0)
}

func TestRHP_Open_DgramReturnsHandle(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "dgram", "0", 0)
	assert.GreaterOrEqual(t, handle, 0)
}

func TestRHP_Open_HandlesAreSequential(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var h0 = openSocket(t, conn, "trace", "0", 3)
	var h1 = openSocket(t, conn, "raw", "0", 3)
	var h2 = openSocket(t, conn, "dgram", "0", 0)
	assert.Equal(t, 0, h0)
	assert.Equal(t, 1, h1)
	assert.Equal(t, 2, h2)
}

func TestRHP_Open_InvalidPfamReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "open", "pfam": "inet", "mode": "dgram"})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode)
}

func TestRHP_Open_InvalidModeReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "open", "pfam": "ax25", "mode": "custom"})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode)
}

func TestRHP_Open_StreamPassiveReturnsHandle(t *testing.T) {
	// Passive stream open registers a callsign with the dlq.
	// dlq auto-initialises, so this is safe without a running recv loop.
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"id":    5,
		"pfam":  "ax25",
		"mode":  "stream",
		"port":  "0",
		"local": "Q1TEST",
		"flags": 0, // passive (no 0x80)
	})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 0, reply.ErrCode)
	assert.Equal(t, 5, reply.ID)
}

func TestRHP_Open_StreamPassiveMissingLocalReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"pfam":  "ax25",
		"mode":  "stream",
		"port":  "0",
		"flags": 0,
		// no "local"
	})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode)
}

func TestRHP_Open_StreamActiveReturnsHandle(t *testing.T) {
	// Active stream open enqueues a connect request via dlq.
	// dlq auto-initialises; the request simply sits in the queue unprocessed.
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{
		"type":   "open",
		"pfam":   "ax25",
		"mode":   "stream",
		"port":   "0",
		"local":  "Q1TEST",
		"remote": "Q2TEST",
		"flags":  0x80, // active open
	})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 0, reply.ErrCode)
}

func TestRHP_Open_StreamActiveMissingRemoteReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"pfam":  "ax25",
		"mode":  "stream",
		"port":  "0",
		"local": "Q1TEST",
		"flags": 0x80,
		// no "remote"
	})
	var reply = rhpRecvAs[rhpOpenReply](t, conn)

	assert.Equal(t, "openReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode)
}

// ---------------------------------------------------------------------------
// CLOSE / CLOSEREPLY
// ---------------------------------------------------------------------------

func TestRHP_Close_ValidHandleReturnsOK(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)

	rhpSend(t, conn, map[string]any{"type": "close", "id": 9, "handle": handle})
	var reply = rhpRecvAs[rhpCloseReply](t, conn)

	assert.Equal(t, "closeReply", reply.Type)
	assert.Equal(t, 0, reply.ErrCode)
	assert.Equal(t, 9, reply.ID)
	assert.Equal(t, handle, reply.Handle)
}

func TestRHP_Close_InvalidHandleReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "close", "handle": 999})
	var reply = rhpRecvAs[rhpCloseReply](t, conn)

	assert.Equal(t, "closeReply", reply.Type)
	assert.Equal(t, 3, reply.ErrCode) // errInvalidHandle
}

func TestRHP_Close_MissingHandleReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "close"})
	var reply = rhpRecvAs[rhpCloseReply](t, conn)

	assert.Equal(t, "closeReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode) // errBadParam
}

// ---------------------------------------------------------------------------
// SEND (stream only — dgram SEND is excluded as it requires a live tx queue)
// ---------------------------------------------------------------------------

func TestRHP_Send_StreamQueuesData(t *testing.T) {
	// SEND on a stream socket calls dlq_xmit_data_request, which appends to
	// the dlq (auto-initialised). We just verify the SENDREPLY is OK and
	// that no panic occurs.
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	// Active-open stream socket: the connect request sits in the queue unprocessed,
	// but local/remote are set, which is all SEND needs.
	rhpSend(t, conn, map[string]any{
		"type":   "open",
		"pfam":   "ax25",
		"mode":   "stream",
		"port":   "0",
		"local":  "Q1TEST",
		"remote": "Q2TEST",
		"flags":  0x80,
	})
	var openReply = rhpRecvAs[rhpOpenReply](t, conn)
	var handle = openReply.Handle

	rhpSend(t, conn, map[string]any{
		"type":   "send",
		"id":     7,
		"handle": handle,
		"data":   "Hello world",
	})
	var reply = rhpRecvAs[rhpSendReply](t, conn)

	assert.Equal(t, "sendReply", reply.Type)
	assert.Equal(t, 0, reply.ErrCode)
	assert.Equal(t, 7, reply.ID)
}

func TestRHP_Send_InvalidHandleReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "send", "handle": 42, "data": "hi"})
	var reply = rhpRecvAs[rhpSendReply](t, conn)

	assert.Equal(t, "sendReply", reply.Type)
	assert.Equal(t, 3, reply.ErrCode) // errInvalidHandle
}

func TestRHP_Send_TraceSocketReturnsError(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)
	rhpSend(t, conn, map[string]any{"type": "send", "handle": handle, "data": "hi"})
	var reply = rhpRecvAs[rhpSendReply](t, conn)

	assert.Equal(t, "sendReply", reply.Type)
	assert.Equal(t, 12, reply.ErrCode) // errBadParam: trace mode
}

// ---------------------------------------------------------------------------
// SendRecFrame routing
// ---------------------------------------------------------------------------

// makeTestFrame builds a simple UI frame for use in SendRecFrame tests.
func makeTestFrame(dst string) (*packet_t, []byte) {
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_SOURCE] = "Q1TEST"
	addrs[AX25_DESTINATION] = dst
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NO_LAYER_3, []byte("test payload"))
	var fbuf = ax25_pack(pp)
	return pp, fbuf
}

func TestRHP_SendRecFrame_DeliveredToTraceSocket(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)
	openSocket(t, conn, "trace", "0", rhpTraceFlagIncoming)

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf)

	var recv = rhpRecv(t, conn)
	assert.Equal(t, "recv", recv["type"])
	assert.Equal(t, "Q1TEST", recv["srce"])
	assert.Equal(t, "Q2TEST", recv["dest"])
	assert.Equal(t, "rcvd", recv["action"])
	assert.Equal(t, "0", recv["port"])
}

func TestRHP_SendRecFrame_NotDeliveredToTraceWithoutIncomingFlag(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)
	openSocket(t, conn, "trace", "0", 0) // flags=0: no incoming flag

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf)

	assert.Nil(t, rhpMaybeRecv(conn), "trace socket without incoming flag should not receive")
}

func TestRHP_SendRecFrame_DeliveredToRawSocket(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)
	openSocket(t, conn, "raw", "0", rhpTraceFlagIncoming)

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf)

	var recv = rhpRecv(t, conn)
	assert.Equal(t, "recv", recv["type"])
	assert.Equal(t, "rcvd", recv["action"])
	// data field is base64-encoded — just check it's present and non-empty
	assert.NotEmpty(t, recv["data"])
}

func TestRHP_SendRecFrame_DeliveredToDgramSocket_UnboundReceivesAll(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)
	openSocket(t, conn, "dgram", "0", 0) // no local = receive everything

	var pp, fbuf = makeTestFrame("APRS")
	svc.SendRecFrame(0, pp, fbuf)

	var recv = rhpRecv(t, conn)
	assert.Equal(t, "recv", recv["type"])
	assert.Equal(t, "test payload", recv["data"])
}

func TestRHP_SendRecFrame_DeliveredToDgramSocket_BoundMatchingDest(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	// Open a dgram socket bound to "APRS" as local (i.e. we receive frames destined to APRS)
	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"pfam":  "ax25",
		"mode":  "dgram",
		"port":  "0",
		"local": "APRS",
		"flags": 0,
	})
	var openReply = rhpRecvAs[rhpOpenReply](t, conn)
	require.Equal(t, 0, openReply.ErrCode)

	var pp, fbuf = makeTestFrame("APRS")
	svc.SendRecFrame(0, pp, fbuf)

	var recv = rhpRecv(t, conn)
	assert.Equal(t, "recv", recv["type"])
	assert.Equal(t, "test payload", recv["data"])
}

func TestRHP_SendRecFrame_NotDeliveredToDgramSocket_WrongDest(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"pfam":  "ax25",
		"mode":  "dgram",
		"port":  "0",
		"local": "APRS",
		"flags": 0,
	})
	rhpRecv(t, conn) // discard openReply

	var pp, fbuf = makeTestFrame("CQ") // wrong destination
	svc.SendRecFrame(0, pp, fbuf)

	assert.Nil(t, rhpMaybeRecv(conn), "dgram socket bound to APRS should not receive frames destined to CQ")
}

func TestRHP_SendRecFrame_ChannelFilter(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)
	openSocket(t, conn, "trace", "1", rhpTraceFlagIncoming) // bound to channel 1

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf) // frame on channel 0 — should not be delivered

	assert.Nil(t, rhpMaybeRecv(conn), "trace socket on channel 1 should not receive channel 0 frames")
}

func TestRHP_SendRecFrame_AllChannels(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	// Open trace socket with port="" (all channels)
	rhpSend(t, conn, map[string]any{
		"type":  "open",
		"pfam":  "ax25",
		"mode":  "trace",
		"flags": rhpTraceFlagIncoming,
		// no "port" field → all channels
	})
	rhpRecv(t, conn) // discard openReply

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(3, pp, fbuf) // arbitrary channel

	var recv = rhpRecv(t, conn)
	assert.Equal(t, "recv", recv["type"])
	assert.Equal(t, "3", recv["port"])
}

func TestRHP_SendRecFrame_MultipleClients(t *testing.T) {
	var svc, port = startRHP(t)
	var conn1 = dialRHP(t, port)
	var conn2 = dialRHP(t, port)
	// Let the second connection be accepted
	time.Sleep(10 * time.Millisecond)

	openSocket(t, conn1, "trace", "0", rhpTraceFlagIncoming)
	openSocket(t, conn2, "trace", "0", rhpTraceFlagIncoming)

	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf)

	var recv1 = rhpRecv(t, conn1)
	var recv2 = rhpRecv(t, conn2)
	assert.Equal(t, "recv", recv1["type"])
	assert.Equal(t, "recv", recv2["type"])
}

// ---------------------------------------------------------------------------
// Connected-mode callbacks (LinkEstablished, LinkTerminated, RecConnData)
// ---------------------------------------------------------------------------

func TestRHP_LinkEstablished_OutgoingSendsStatus(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	// We use a trace socket as a stand-in — LinkEstablished just needs any
	// socket to exist with that handle; it doesn't check the mode.
	var handle = openSocket(t, conn, "trace", "0", 3)

	svc.LinkEstablished(0, handle, "Q2TEST", "Q1TEST", false /* outgoing */)

	var msg = rhpRecvAs[rhpStatusMsg](t, conn)
	assert.Equal(t, "status", msg.Type)
	assert.Equal(t, rhpFlagConnected, msg.Flags)
	assert.Equal(t, handle, msg.Handle)
}

func TestRHP_LinkEstablished_IncomingSendsAcceptThenStatus(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)

	svc.LinkEstablished(0, handle, "Q2TEST", "Q1TEST", true /* incoming */)

	var accept = rhpRecvAs[rhpAcceptMsg](t, conn)
	assert.Equal(t, "accept", accept.Type)
	assert.Equal(t, "Q2TEST", accept.Remote)
	assert.Equal(t, "Q1TEST", accept.Local)
	assert.Equal(t, handle, accept.Handle)

	var status = rhpRecvAs[rhpStatusMsg](t, conn)
	assert.Equal(t, "status", status.Type)
	assert.Equal(t, rhpFlagConnected, status.Flags)
}

func TestRHP_LinkTerminated_SendsClose(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)

	svc.LinkTerminated(0, handle, "Q2TEST", "Q1TEST", false)

	var msg = rhpRecvAs[rhpCloseMsg](t, conn)
	assert.Equal(t, "close", msg.Type)
	assert.Equal(t, handle, msg.Handle)
}

func TestRHP_RecConnData_SendsRecv(t *testing.T) {
	var svc, port = startRHP(t)
	var conn = dialRHP(t, port)

	var handle = openSocket(t, conn, "trace", "0", 3)

	svc.RecConnData(0, handle, "Q2TEST", "Q1TEST", AX25_PID_NO_LAYER_3, []byte("connected data"))

	var msg = rhpRecvAs[rhpRecvMsg](t, conn)
	assert.Equal(t, "recv", msg.Type)
	assert.Equal(t, handle, msg.Handle)
	var msgData string
	require.NoError(t, json.Unmarshal(msg.Data, &msgData))
	assert.Equal(t, "connected data", msgData)
}

// ---------------------------------------------------------------------------
// Disabled service
// ---------------------------------------------------------------------------

func TestRHP_Disabled_SendRecFrameIsNoop(t *testing.T) {
	var mc = new(misc_config_s)
	mc.rhp_port = 0
	var svc = NewRHPService(mc)

	// None of these should panic.
	var pp, fbuf = makeTestFrame("Q2TEST")
	svc.SendRecFrame(0, pp, fbuf)
	svc.LinkEstablished(0, 0, "Q2TEST", "Q1TEST", false)
	svc.LinkTerminated(0, 0, "Q2TEST", "Q1TEST", false)
	svc.RecConnData(0, 0, "Q2TEST", "Q1TEST", AX25_PID_NO_LAYER_3, []byte("x"))
}

// ---------------------------------------------------------------------------
// Unknown message type
// ---------------------------------------------------------------------------

func TestRHP_UnknownMessageType_DoesNotCrash(t *testing.T) {
	var _, port = startRHP(t)
	var conn = dialRHP(t, port)

	rhpSend(t, conn, map[string]any{"type": "frobnicate", "data": "whatever"})
	// No reply is sent for unknown types; verify the connection is still usable.
	time.Sleep(20 * time.Millisecond)
	rhpSend(t, conn, map[string]any{"type": "auth", "user": "Q1TEST", "pass": "x"})
	var reply = rhpRecv(t, conn)
	assert.Equal(t, "authReply", reply["type"])
}
