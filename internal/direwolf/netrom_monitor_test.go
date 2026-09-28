// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// netromMonitorPacket wraps info in an AX.25 frame from Q1TEST-7 to dest,
// UI or I as a NET/ROM node would send it.
func netromMonitorPacket(t *testing.T, dest string, ui bool, pid int, info []byte) *ax25.Packet {
	t.Helper()

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = dest
	addrs[ax25.Source] = "Q1TEST-7"

	var pp *ax25.Packet
	if ui {
		pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, pid, info)
	} else {
		pp = ax25.IFrame(addrs, 2, ax25.CRCmd, ax25.Modulo8, 0, 0, 0, pid, info)
	}

	require.NotNil(t, pp)

	return pp
}

func TestNetromMonitorNodes(t *testing.T) {
	var frames, err = encodeNetromNodes("QNODEA", []netromNodesEntry{
		{callsign: "Q2TEST-7", alias: "QNODEB", neighbour: "Q2TEST-7", quality: 192},
		{callsign: "Q3TEST", alias: "", neighbour: "Q2TEST-7", quality: 144},
	})
	require.NoError(t, err)

	var text, ok = netromMonitorText(netromMonitorPacket(t, "NODES", true, ax25.PIDNetROM, frames[0]))
	require.True(t, ok)
	assert.Equal(t, "NET/ROM NODES from QNODEA: Q2TEST-7 (QNODEB) via Q2TEST-7 q=192, Q3TEST via Q2TEST-7 q=144", text)
}

func TestNetromMonitorFrames(t *testing.T) {
	var connReq = testNetromFrame(netromOpConnReq)
	connReq.window = 4
	connReq.user = "Q1TEST"
	connReq.originNode = "Q1TEST-7"
	connReq.timeout = maybe.Just(120)

	var connAck = testNetromFrame(netromOpConnAck)
	connAck.txSeq, connAck.rxSeq, connAck.window = 0x05, 0x06, 4

	var refusal = testNetromFrame(netromOpConnAck)
	refusal.flags = netromFlagChoke

	var info = testNetromFrame(netromOpInfo)
	info.txSeq, info.rxSeq, info.flags = 3, 5, netromFlagMore
	info.info = []byte("hi\r\xff")

	var infoAck = testNetromFrame(netromOpInfoAck)
	infoAck.rxSeq, infoAck.flags = 9, netromFlagChoke|netromFlagNAK

	var testCases = []struct {
		frame *netromFrame
		want  string
	}{
		{connReq, "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: CONN REQ circuit 01/02 window=4 user=Q1TEST node=Q1TEST-7 timeout=120s"},
		{connAck, "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: CONN ACK circuit 01/02 from 05/06 window=4"},
		{refusal, "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: CONN ACK circuit 01/02 REFUSED"},
		{info, "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: INFO circuit 01/02 s=3 r=5 MORE len=4: hi<0x0d><0xff>"},
		{infoAck, "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: INFO ACK circuit 01/02 r=9 CHOKE NAK"},
		{testNetromFrame(netromOpDiscReq), "NET/ROM Q1TEST-7 to Q2TEST-7 ttl=16: DISC REQ circuit 01/02"},
	}

	for _, tc := range testCases {
		t.Run(tc.frame.opcode.String(), func(t *testing.T) {
			var b, err = tc.frame.encode()
			require.NoError(t, err)

			var text, ok = netromMonitorText(netromMonitorPacket(t, "Q2TEST-7", false, ax25.PIDNetROM, b))
			require.True(t, ok)
			assert.Equal(t, tc.want, text)
		})
	}
}

// What is not NET/ROM, or does not decode as it, is left to be shown as it
// always was.
func TestNetromMonitorLeavesOtherFramesAlone(t *testing.T) {
	var testCases = []struct {
		name string
		pp   *ax25.Packet
	}{
		{"another PID", netromMonitorPacket(t, "Q2TEST-7", false, ax25.PIDNoLayer3, []byte("text"))},
		{"a NET/ROM frame too short to be one", netromMonitorPacket(t, "Q2TEST-7", false, ax25.PIDNetROM, []byte("short"))},
		{"a NODES broadcast without its signature", netromMonitorPacket(t, "NODES", true, ax25.PIDNetROM, []byte("QNODEA"))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var _, ok = netromMonitorText(tc.pp)
			assert.False(t, ok)
			assert.Equal(t, tc.pp.Info(), netromMonitorInfo(tc.pp, tc.pp.Info()))
		})
	}
}

// A frame for a virtual channel is printed by the transmit queue itself,
// never reaching the monitor code in xmit.go, and a NET/ROM node can sit on
// one.
func TestTransmitQueueDecodesNetromOnAVirtualChannel(t *testing.T) {
	const channel = MAX_RADIO_CHANS

	var audio = new(AudioConfig)
	audio.chan_medium[channel] = MEDIUM_NETTNC

	var tq = NewTransmitQueue()
	tq.Init(audio)

	var frames, err = encodeNetromNodes("QNODEA", nil)
	require.NoError(t, err)

	var pp = netromMonitorPacket(t, "NODES", true, ax25.PIDNetROM, frames[0])

	var out = captureStdout(t, func() { tq.Append(channel, TQ_PRIO_1_LO, pp) })

	assert.Contains(t, out, "NET/ROM NODES from QNODEA")
}
