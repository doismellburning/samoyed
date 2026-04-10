package direwolf

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// readReplyFrom reads one AGWPEMessage (header + data) from conn.
func readReplyFrom(conn net.Conn) (*AGWPEMessage, error) {
	var hdr AGWPEHeader
	err := binary.Read(conn, binary.LittleEndian, &hdr)
	if err != nil {
		return nil, err
	}
	var msg = &AGWPEMessage{Header: hdr, Data: nil}
	if hdr.DataLen > 0 {
		msg.Data = make([]byte, hdr.DataLen)
		_, err = io.ReadFull(conn, msg.Data)
		if err != nil {
			return nil, err
		}
	}
	return msg, nil
}

// setupClientPipe wires client_sock[0] to one end of an in-memory net.Pipe
// and returns the other end for reading replies.
func setupClientPipe(t *testing.T) net.Conn {
	t.Helper()
	var server, client = net.Pipe()
	client_sock[0] = server
	t.Cleanup(func() {
		server.Close()
		client.Close()
		client_sock[0] = nil
	})
	return client
}

// asyncReply starts reading one reply from conn in a goroutine and returns
// a channel that delivers the result. Used to avoid deadlocking on
// net.Pipe's unbuffered writes.
func asyncReply(conn net.Conn) <-chan *AGWPEMessage {
	var ch = make(chan *AGWPEMessage, 1)
	go func() {
		msg, _ := readReplyFrom(conn)
		ch <- msg
	}()
	return ch
}

func TestHandleClientCommand_R_VersionReply(t *testing.T) {
	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'R'
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('R'), reply.Header.DataKind)
	assert.Equal(t, uint32(8), reply.Header.DataLen)
	require.Len(t, reply.Data, 8)
	assert.Equal(t, uint32(2005), binary.LittleEndian.Uint32(reply.Data[0:4]))
	assert.Equal(t, uint32(127), binary.LittleEndian.Uint32(reply.Data[4:8]))
}

func TestHandleClientCommand_k_TogglesRawFrames(t *testing.T) {
	t.Cleanup(func() { enable_send_raw_to_client[0] = false })

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'k'

	assert.False(t, enable_send_raw_to_client[0])
	handleClientCommand(0, cmd)
	assert.True(t, enable_send_raw_to_client[0])
	handleClientCommand(0, cmd)
	assert.False(t, enable_send_raw_to_client[0])
}

func TestHandleClientCommand_m_TogglesMonitorFrames(t *testing.T) {
	t.Cleanup(func() { enable_send_monitor_to_client[0] = false })

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'm'

	assert.False(t, enable_send_monitor_to_client[0])
	handleClientCommand(0, cmd)
	assert.True(t, enable_send_monitor_to_client[0])
	handleClientCommand(0, cmd)
	assert.False(t, enable_send_monitor_to_client[0])
}

func TestHandleClientCommand_g_PortCapabilitiesReply(t *testing.T) {
	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'g'
	cmd.Header.Portx = 2
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('g'), reply.Header.DataKind)
	assert.Equal(t, byte(2), reply.Header.Portx)
	assert.Equal(t, uint32(12), reply.Header.DataLen)
	require.Len(t, reply.Data, 12)
	assert.Equal(t, byte(0), reply.Data[0])    // on_air_baud_rate
	assert.Equal(t, byte(1), reply.Data[1])    // traffic_level
	assert.Equal(t, byte(0x19), reply.Data[2]) // tx_delay
	assert.Equal(t, byte(4), reply.Data[3])    // tx_tail
	assert.Equal(t, byte(0xc8), reply.Data[4]) // persist
	assert.Equal(t, byte(4), reply.Data[5])    // slottime
	assert.Equal(t, byte(7), reply.Data[6])    // maxframe
	assert.Equal(t, byte(0), reply.Data[7])    // active_connections
	assert.Equal(t, uint32(1), binary.LittleEndian.Uint32(reply.Data[8:12]))
}

func TestHandleClientCommand_G_NoPorts(t *testing.T) {
	var cfg audio_s
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'G'
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('G'), reply.Header.DataKind)
	assert.Equal(t, "0;", string(reply.Data))
}

func TestHandleClientCommand_G_RadioChannelMono(t *testing.T) {
	var cfg audio_s
	cfg.chan_medium[0] = MEDIUM_RADIO
	cfg.adev[0].num_channels = 1
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'G'
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('G'), reply.Header.DataKind)
	assert.Equal(t, "1;Port1 first soundcard mono;", string(reply.Data))
}

func TestHandleClientCommand_y_EmptyQueueReturnsZero(t *testing.T) {
	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'y'
	cmd.Header.Portx = 0
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('y'), reply.Header.DataKind)
	assert.Equal(t, byte(0), reply.Header.Portx)
	assert.Equal(t, uint32(4), reply.Header.DataLen)
	require.Len(t, reply.Data, 4)
	assert.Equal(t, uint32(0), binary.LittleEndian.Uint32(reply.Data))
}

func TestHandleClientCommand_X_InvalidChannelReportsFailure(t *testing.T) {
	var cfg audio_s
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'X'
	cmd.Header.Portx = MAX_RADIO_CHANS // out of range
	copy(cmd.Header.CallFrom[:], "Q1TEST")
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('X'), reply.Header.DataKind)
	assert.Equal(t, uint32(1), reply.Header.DataLen)
	require.Len(t, reply.Data, 1)
	assert.Equal(t, byte(0), reply.Data[0]) // failure
}

func TestHandleClientCommand_X_ValidRadioChannelReportsSuccess(t *testing.T) {
	var cfg audio_s
	cfg.chan_medium[0] = MEDIUM_RADIO
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	var client = setupClientPipe(t)
	var replyCh = asyncReply(client)

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'X'
	cmd.Header.Portx = 0
	copy(cmd.Header.CallFrom[:], "Q1TEST")
	handleClientCommand(0, cmd)

	var reply = <-replyCh
	require.NotNil(t, reply)
	assert.Equal(t, byte('X'), reply.Header.DataKind)
	assert.Equal(t, uint32(1), reply.Header.DataLen)
	require.Len(t, reply.Data, 1)
	assert.Equal(t, byte(1), reply.Data[0]) // success
}

// dlqAppended clears the DLQ, calls f, returns the first item appended
// during f (or nil if nothing was appended), then restores the original queue.
func dlqAppended(f func()) *dlq_item_t {
	dlq_mutex.Lock()
	var savedHead = dlq_queue_head
	dlq_queue_head = nil
	dlq_mutex.Unlock()

	f()

	dlq_mutex.Lock()
	var newItem = dlq_queue_head
	dlq_queue_head = savedHead
	dlq_mutex.Unlock()
	return newItem
}

// Property: 'V' handler tolerates any cmd.Data without panicking.
// Before the bounds-check fix, data[0] could be read on an empty slice,
// and the digipeater slice could go out of bounds.
func TestHandleClientCommand_V_ArbitraryDataNoPanic(t *testing.T) {
	var cfg audio_s
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	rapid.Check(t, func(t *rapid.T) {
		var cmd = new(AGWPEMessage)
		cmd.Header.DataKind = 'V'
		copy(cmd.Header.CallFrom[:], "Q1TEST")
		copy(cmd.Header.CallTo[:], "Q2TEST")
		cmd.Data = rapid.SliceOf(rapid.Byte()).Draw(t, "data")
		cmd.Header.DataLen = uint32(len(cmd.Data))
		handleClientCommand(0, cmd)
	})
}

// Property: 'K' handler tolerates any combination of DataLen and Data without panicking.
// Before the bounds-check fix, cmd.Data[1:cmd.Header.DataLen] would panic
// when DataLen==0 or DataLen exceeded len(cmd.Data).
func TestHandleClientCommand_K_ArbitraryDataLenNoPanic(t *testing.T) {
	var cfg audio_s
	save_audio_config_p = &cfg
	t.Cleanup(func() { save_audio_config_p = nil })

	rapid.Check(t, func(t *rapid.T) {
		var cmd = new(AGWPEMessage)
		cmd.Header.DataKind = 'K'
		cmd.Data = rapid.SliceOf(rapid.Byte()).Draw(t, "data")
		cmd.Header.DataLen = rapid.Uint32().Draw(t, "dataLen")
		handleClientCommand(0, cmd)
	})
}

// Property: 'v' handler with numDigi outside [1,7] must not enqueue a connect request.
// Before the fix, the invalid-numDigi else branch fell through to dlq_connect_request,
// silently treating the malformed frame as a direct connect.
func TestHandleClientCommand_v_InvalidNumDigiNoDLQAppend(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var numDigi = rapid.OneOf(
			rapid.Just(byte(0)),
			rapid.ByteRange(8, 255),
		).Draw(t, "numDigi")

		// Provide enough data to reach the numDigi validation, not the len check.
		var data = make([]byte, 1+10*7+1)
		data[0] = numDigi

		var cmd = new(AGWPEMessage)
		cmd.Header.DataKind = 'v'
		cmd.Header.Portx = 0 // valid radio port
		copy(cmd.Header.CallFrom[:], "Q1TEST")
		copy(cmd.Header.CallTo[:], "Q2TEST")
		cmd.Data = data
		cmd.Header.DataLen = uint32(len(data))

		var item = dlqAppended(func() { handleClientCommand(0, cmd) })
		if item != nil {
			t.Errorf("expected no DLQ append for out-of-range numDigi %d, got %+v", numDigi, item)
		}
	})
}

// Property: connected-mode handlers ('C','v','c','D','d','Y') must not enqueue
// anything when Portx is not a radio channel.  Before the fix, the DLQ functions
// would Assert-panic on channel >= MAX_RADIO_CHANS.
func TestHandleClientCommand_ConnectedMode_NonRadioPortxNoDLQAppend(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var dataKind = rapid.SampledFrom([]byte{'C', 'v', 'c', 'D', 'd', 'Y'}).Draw(t, "dataKind")
		var portx = rapid.ByteRange(MAX_RADIO_CHANS, 255).Draw(t, "portx")

		var cmd = new(AGWPEMessage)
		cmd.Header.DataKind = dataKind
		cmd.Header.Portx = portx
		copy(cmd.Header.CallFrom[:], "Q1TEST")
		copy(cmd.Header.CallTo[:], "Q2TEST")

		var item = dlqAppended(func() { handleClientCommand(0, cmd) })
		if item != nil {
			t.Errorf("expected no DLQ append for non-radio Portx %d with command '%c'", portx, dataKind)
		}
	})
}

func TestHandleClientCommand_v_PopulatesDigipeaters(t *testing.T) {
	// Encode the via_info payload: num_digi + 7 x 10-byte callsign slots.
	var via struct {
		NumDigi byte
		Dcall   [7][10]byte
	}
	via.NumDigi = 2
	copy(via.Dcall[0][:], "Q3TEST")
	copy(via.Dcall[1][:], "Q4TEST")

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, via))

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'v'
	cmd.Header.Portx = 0
	copy(cmd.Header.CallFrom[:], "Q1TEST")
	copy(cmd.Header.CallTo[:], "Q2TEST")
	cmd.Header.DataLen = uint32(via.NumDigi)*10 + 1 // expected size per protocol
	cmd.Data = buf.Bytes()[:int(cmd.Header.DataLen)]

	var item = dlqAppended(func() { handleClientCommand(0, cmd) })

	require.NotNil(t, item)
	assert.Equal(t, DLQ_CONNECT_REQUEST, item._type)
	assert.Equal(t, 4, item.num_addr) // source + destination + 2 digipeaters
	assert.Equal(t, "Q1TEST", item.addrs[AX25_SOURCE])
	assert.Equal(t, "Q2TEST", item.addrs[AX25_DESTINATION])
	assert.Equal(t, "Q3TEST", item.addrs[AX25_REPEATER_1])
	assert.Equal(t, "Q4TEST", item.addrs[AX25_REPEATER_1+1])
}
