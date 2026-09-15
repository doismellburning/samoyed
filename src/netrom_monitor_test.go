// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"testing"
)

// netromMonitorFrame wraps a NET/ROM payload in the AX.25 UI frame the
// monitor would actually see, addressed over the air to axDst.
func netromMonitorFrame(t *testing.T, axDst string, payload []byte) *packet_t {
	t.Helper()

	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = axDst
	addrs[AX25_SOURCE] = testNodeCallQ1

	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NETROM, payload)
	if pp == nil {
		t.Fatal("failed to build test AX.25 UI frame")
	}

	return pp
}

func TestNetromFrameToTextNodes(t *testing.T) {
	var entries = []netromNodesEntry{
		{
			dstCallsign: testNodeCallQ2,
			dstAlias:    netromPadAlias("QNODE2"),
			neighbor:    testNodeCallQ2,
			quality:     255,
		},
		{
			dstCallsign: testNodeCallQ3,
			dstAlias:    netromPadAlias("QNODE3"),
			neighbor:    testNodeCallQ2,
			quality:     180,
		},
	}
	var payload = netromBuildRoutingBroadcast(netromPadAlias("QNODE1"), entries)

	var got, ok = netromFrameToText(netromMonitorFrame(t, NETROM_BROADCAST_CALLSIGN, payload), payload)
	if !ok {
		t.Fatal("expected a NODES broadcast to be recognised")
	}

	var want = "NET/ROM NODES from QNODE1" +
		" [Q2TEST QNODE2 via Q2TEST q=255]" +
		" [Q3TEST QNODE3 via Q2TEST q=180]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestNetromFrameToTextNodesEmpty covers a node that knows no routes yet but
// is still announcing itself.
func TestNetromFrameToTextNodesEmpty(t *testing.T) {
	var payload = netromBuildRoutingBroadcast(netromPadAlias("QNODE1"), nil)

	var got, ok = netromFrameToText(netromMonitorFrame(t, NETROM_BROADCAST_CALLSIGN, payload), payload)
	if !ok {
		t.Fatal("expected an empty NODES broadcast to be recognised")
	}
	if got != "NET/ROM NODES from QNODE1 (no routes)" {
		t.Errorf("got %q", got)
	}
}

func TestNetromFrameToTextTransport(t *testing.T) {
	var tests = []struct {
		name    string
		payload []byte
		want    string
	}{
		{
			name: "connect request",
			payload: netromBuildConnect(
				testNodeCallQ2, testNodeCallQ1, 7,
				0, 0, 0x11, 0x22,
				"Q4TEST", "QNODE1",
				testNodeCallQ2, "QNODE2",
				4,
			),
			want: "NET/ROM CONNECT REQ Q1TEST>Q2TEST ttl=7 orig=17/34 Q4TEST to Q2TEST win=4",
		},
		{
			name: "connect ack",
			payload: netromBuildConnAck(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 0x33, 0x44, 4, false,
			),
			want: "NET/ROM CONNECT ACK Q1TEST>Q2TEST ttl=7 ckt=17/34 accept=51/68 win=4",
		},
		{
			name: "connect ack refused",
			payload: netromBuildConnAck(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 0, 0, 4, true,
			),
			want: "NET/ROM CONNECT ACK Q1TEST>Q2TEST ttl=7 ckt=17/34 accept=0/0 win=4 CHOKE",
		},
		{
			name: "info",
			payload: netromBuildInfo(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 3, 5,
				false, false, false, []byte("hello"),
			),
			want: "NET/ROM INFO Q1TEST>Q2TEST ttl=7 ckt=17/34 n(s)=3 n(r)=5 len=5 \"hello\"",
		},
		{
			name: "info fragment",
			payload: netromBuildInfo(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 3, 5,
				false, false, true, []byte("hello"),
			),
			want: "NET/ROM INFO Q1TEST>Q2TEST ttl=7 ckt=17/34 n(s)=3 n(r)=5 len=5 \"hello\" MORE",
		},
		{
			name: "info ack with nak",
			payload: netromBuildInfoAck(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 5, false, true,
			),
			want: "NET/ROM INFO ACK Q1TEST>Q2TEST ttl=7 ckt=17/34 n(r)=5 NAK",
		},
		{
			name: "disconnect request",
			payload: netromBuildDisconnect(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22, 5,
			),
			want: "NET/ROM DISCONNECT REQ Q1TEST>Q2TEST ttl=7 ckt=17/34 n(r)=5",
		},
		{
			name: "disconnect ack",
			payload: netromBuildDiscAck(
				testNodeCallQ2, testNodeCallQ1, 7,
				0x11, 0x22,
			),
			want: "NET/ROM DISCONNECT ACK Q1TEST>Q2TEST ttl=7 ckt=17/34",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got, ok = netromFrameToText(netromMonitorFrame(t, testNodeCallQ2, tt.payload), tt.payload)
			if !ok {
				t.Fatal("expected the transport frame to be recognised")
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestNetromFrameToTextIgnoresOtherProtocols checks that the monitor hook
// keeps its hands off frames that are not NET/ROM, so they still print their
// own contents.
func TestNetromFrameToTextIgnoresOtherProtocols(t *testing.T) {
	var addrs [AX25_MAX_ADDRS]string
	addrs[AX25_DESTINATION] = testNodeCallQ2
	addrs[AX25_SOURCE] = testNodeCallQ1

	var payload = []byte("plain APRS-ish text")
	var pp = ax25_u_frame(addrs, 2, cr_cmd, frame_type_U_UI, 0, AX25_PID_NO_LAYER_3, payload)
	if pp == nil {
		t.Fatal("failed to build test AX.25 UI frame")
	}

	if _, ok := netromFrameToText(pp, payload); ok {
		t.Error("a frame with a non-NET/ROM PID should not be decoded as NET/ROM")
	}
}

// TestNetromFrameToTextMalformed checks that a NET/ROM frame too short to
// parse falls back to the raw print rather than panicking or reporting a
// decode error in place of the data.
func TestNetromFrameToTextMalformed(t *testing.T) {
	var tests = []struct {
		name  string
		axDst string
	}{
		{"truncated transport frame", testNodeCallQ2},
		{"truncated nodes broadcast", NETROM_BROADCAST_CALLSIGN},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload = []byte{0x01, 0x02}
			var got, ok = netromFrameToText(netromMonitorFrame(t, tt.axDst, payload), payload)
			if ok {
				t.Errorf("expected a malformed frame to be left to the raw printer, got %q", got)
			}
		})
	}
}

// TestNetromFrameToTextNilPacket guards the monitor hook against being handed
// no packet at all.
func TestNetromFrameToTextNilPacket(t *testing.T) {
	if _, ok := netromFrameToText(nil, []byte{0x01}); ok {
		t.Error("expected no decode for a nil packet")
	}
}

// TestNetromPayloadTextShowsRealBytes guards the payload rendering against
// the trap AX25SafePrint falls into: ranging over runes turns any byte that
// is not valid UTF-8 into U+FFFD, which then prints as "<0xfffd>" rather
// than the byte that was actually received.
func TestNetromPayloadTextShowsRealBytes(t *testing.T) {
	var got = netromPayloadText([]byte{'h', 'i', 0xa2, 0x00, 0x7f})

	var want = `"hi<0xa2><0x00><0x7f>"`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestTqAppendDecodesNetromOnVirtualChannel covers the monitor output for a
// channel that is not a radio: tq_append prints IGate and network-TNC frames
// itself rather than handing them to the transmit queue, so it never reaches
// the monitor code in xmit.go.  A NET/ROM node can sit on an NCHANNEL, and its
// NODES broadcasts should be as readable there as on the air.
func TestTqAppendDecodesNetromOnVirtualChannel(t *testing.T) {
	var prevAudio = save_audio_config_p
	t.Cleanup(func() { save_audio_config_p = prevAudio })

	const channel = MAX_RADIO_CHANS

	var audio = new(audio_s)
	audio.chan_medium[channel] = MEDIUM_NETTNC
	save_audio_config_p = audio

	var entries = []netromNodesEntry{
		{
			dstCallsign: testNodeCallQ2,
			dstAlias:    netromPadAlias("QNODE2"),
			neighbor:    testNodeCallQ2,
			quality:     255,
		},
	}
	var payload = netromBuildRoutingBroadcast(netromPadAlias("QNODE1"), entries)
	var pp = netromMonitorFrame(t, NETROM_BROADCAST_CALLSIGN, payload)

	// nettnc_send_packet is a no-op for a channel with no connection, so this
	// exercises the print path without needing a live network TNC.
	var out = captureStdout(t, func() { tq_append(channel, TQ_PRIO_1_LO, pp) })

	if !strings.Contains(out, "NET/ROM NODES from QNODE1") {
		t.Errorf("expected a decoded NODES broadcast, got %q", out)
	}
	if !strings.Contains(out, "[6>nt]") {
		t.Errorf("expected the network-TNC channel prefix, got %q", out)
	}
}
