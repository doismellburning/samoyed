// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inp3RIFPayload builds a synthetic INP3 RIF (routing) message payload as
// carried in the info part of a NET/ROM (PID 0xCF) frame: a leading 0xFF
// sentinel byte (in place of what would normally be a source callsign),
// followed by a route entry whose RTT field happens to contain an embedded
// 0x00 byte, and terminated by the 0x00 TLV-options terminator.
//
// This is the kind of binary data that a naive C-string-oriented pass-through
// (strlcat/strlcpy, NUL-terminated copies) would truncate or corrupt, which
// is exactly what issue #527's NET/ROM fixes in server.go were guarding
// against. INP3 is layered on the same NET/ROM PID and inherits the same
// risk, plus its own 0xFF sentinel byte.
func inp3RIFPayload() []byte {
	return []byte{
		0xff,                              // RIF sentinel (replaces source call byte 0)
		'Q', '1', 'T', 'E', 'S', 'T', ' ', // callsign (7 bytes, AX.25-style, space padded)
		3,          // hops
		0x00, 0x2a, // RTT/STT big-endian uint16, low byte deliberately 0x00
		0x00, // TLV options terminator
	}
}

// setupINP3TransmitTest returns an AGW server whose 'V'/'M' handlers queue
// onto radio channel 0, with that channel's transmit queue empty before and
// after the test.
func setupINP3TransmitTest(t *testing.T) *AGWServer {
	t.Helper()

	var drain = func() {
		for transmitQueue.Remove(0, TQ_PRIO_1_LO) != nil { //revive:disable-line:empty-block
		}
	}

	var cfg = new(audio_s)
	cfg.chan_medium[0] = MEDIUM_RADIO

	// TransmitQueue.Append reads the audio configuration for itself.
	save_audio_config_p = cfg
	transmitQueue.Init(cfg)
	drain()

	t.Cleanup(func() {
		drain()
		save_audio_config_p = nil
	})

	var s = new(AGWServer)
	s.audioConfigP = cfg

	return s
}

// TestHandleClientCommand_V_PreservesINP3BinaryPayload is a regression test
// proving the AGW 'V' (transmit UI frame) handler preserves an INP3-shaped
// binary payload - including the leading 0xFF sentinel and an embedded 0x00
// byte - byte-for-byte, along with PID 0xCF, onto the packet it enqueues for
// transmission.
func TestHandleClientCommand_V_PreservesINP3BinaryPayload(t *testing.T) {
	var s = setupINP3TransmitTest(t)

	var payload = inp3RIFPayload()

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'V'
	cmd.Header.PID = AX25_PID_NETROM
	copy(cmd.Header.CallFrom[:], "Q1TEST")
	copy(cmd.Header.CallTo[:], "Q2TEST")

	var data = make([]byte, 1+len(payload)) // 0 digipeaters, then payload
	data[0] = 0
	copy(data[1:], payload)
	cmd.Data = data
	cmd.Header.DataLen = uint32(len(data))

	s.handleClientCommand(0, cmd)

	var pp = transmitQueue.Remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, pp, "expected a packet to be enqueued for transmission")
	assert.Equal(t, byte(AX25_PID_NETROM), byte(ax25_get_pid(pp)))
	assert.Equal(t, payload, AX25GetInfo(pp))
}

// TestHandleClientCommand_M_PreservesINP3BinaryPayload is the same
// regression as above for the AGW 'M' (send UNPROTO information, no
// digipeater path) handler.
func TestHandleClientCommand_M_PreservesINP3BinaryPayload(t *testing.T) {
	var s = setupINP3TransmitTest(t)

	var payload = inp3RIFPayload()

	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = 'M'
	cmd.Header.PID = AX25_PID_NETROM
	copy(cmd.Header.CallFrom[:], "Q1TEST")
	copy(cmd.Header.CallTo[:], "Q2TEST")
	cmd.Data = payload
	cmd.Header.DataLen = uint32(len(payload))

	s.handleClientCommand(0, cmd)

	var pp = transmitQueue.Remove(0, TQ_PRIO_1_LO)
	require.NotNil(t, pp, "expected a packet to be enqueued for transmission")
	assert.Equal(t, byte(AX25_PID_NETROM), byte(ax25_get_pid(pp)))
	assert.Equal(t, payload, AX25GetInfo(pp))
}

// TestServerSendMonitored_PreservesINP3BinaryPayload is a regression test
// proving that AGWServer.SendMonitored (the AGWPE MONITOR-frame path fixed
// under issue #367 for NET/ROM) also preserves an INP3-shaped binary
// payload - including the embedded 0x00 byte - byte-for-byte in the message
// sent to a monitoring client, rather than truncating at the first NUL as a
// C-string-oriented implementation would.
func TestServerSendMonitored_PreservesINP3BinaryPayload(t *testing.T) {
	var s = new(AGWServer)
	s.clients[0].sendMonitor = true

	var conn = setupClientPipe(t, s)
	var replyCh = asyncReply(conn)

	var payload = inp3RIFPayload()
	var pp = AX25FromText("Q1TEST>Q2TEST:", true)
	require.NotNil(t, pp)
	ax25_set_info(pp, payload)
	ax25_set_pid(pp, AX25_PID_NETROM)

	s.SendMonitored(0, pp, 0)

	var reply = <-replyCh
	require.NotNil(t, reply, "expected a MONITOR message to be sent to the client")

	// The info part is embedded between the "[HH:MM:SS]\r" timestamp and the
	// trailing "\r\x00" added by SendMonitored - confirm the exact
	// payload bytes, including the embedded 0x00, appear intact and are not
	// truncated.
	assert.Contains(t, string(reply.Data), string(payload))
}
