// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"testing"
)

func TestNetromEncodeDecodeAddr(t *testing.T) {
	var cases = []string{"Q1TEST", "Q2TEST", "Q1TEST-7", "Q2TEST-15"}
	for _, call := range cases {
		var encoded = netromEncodeAddr(call)
		var decoded = netromDecodeAddr(encoded)
		if decoded != call {
			t.Errorf("callsign roundtrip: got %q, want %q", decoded, call)
		}
	}
}

func TestNetromRoutingBroadcastRoundtrip(t *testing.T) {
	var srcAlias = netromPadAlias("QNODEA")
	var entries = []netromNodesEntry{
		{
			dstCallsign: "Q2TEST",
			dstAlias:    netromPadAlias("QNODEB"),
			neighbor:    "Q3TEST",
			quality:     192,
		},
		{
			dstCallsign: "Q4TEST",
			dstAlias:    netromPadAlias("QNODEC"),
			neighbor:    "Q3TEST",
			quality:     128,
		},
	}

	var payload = netromBuildRoutingBroadcast(srcAlias, entries)

	var bc, err = netromParseRoutingBroadcast(payload)
	if err != nil {
		t.Fatalf("parse routing broadcast: %v", err)
	}

	if bc.srcAlias != srcAlias {
		t.Errorf("srcAlias: got %v, want %v", bc.srcAlias, srcAlias)
	}
	if len(bc.entries) != len(entries) {
		t.Fatalf("entry count: got %d, want %d", len(bc.entries), len(entries))
	}
	for i, want := range entries {
		var got = bc.entries[i]
		if got.dstCallsign != want.dstCallsign {
			t.Errorf("entry[%d].dstCallsign: got %q, want %q", i, got.dstCallsign, want.dstCallsign)
		}
		if got.dstAlias != want.dstAlias {
			t.Errorf("entry[%d].dstAlias: got %v, want %v", i, got.dstAlias, want.dstAlias)
		}
		if got.neighbor != want.neighbor {
			t.Errorf("entry[%d].neighbor: got %q, want %q", i, got.neighbor, want.neighbor)
		}
		if got.quality != want.quality {
			t.Errorf("entry[%d].quality: got %d, want %d", i, got.quality, want.quality)
		}
	}
}

func TestNetromConnectRoundtrip(t *testing.T) {
	var payload = netromBuildConnect(
		"Q2TEST", "Q1TEST", NETROM_TTL_DEFAULT,
		0x01, 0x42,
		0x10, 0x20,
		"Q1TEST-1", "QNODEA",
		"Q2TEST-1", "QNODEB",
		NETROM_WINDOW_DEFAULT,
	)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse connect: %v", err)
	}
	if f.opcode != netromOpcodeConnect {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeConnect)
	}
	if f.net.dst != "Q2TEST" {
		t.Errorf("net.dst: got %q, want %q", f.net.dst, "Q2TEST")
	}
	if f.net.src != "Q1TEST" {
		t.Errorf("net.src: got %q, want %q", f.net.src, "Q1TEST")
	}
	if f.net.ttl != NETROM_TTL_DEFAULT {
		t.Errorf("net.ttl: got %d, want %d", f.net.ttl, NETROM_TTL_DEFAULT)
	}
	if f.cktIdx != 0x01 {
		t.Errorf("cktIdx: got 0x%02x, want 0x01", f.cktIdx)
	}
	if f.cktID != 0x42 {
		t.Errorf("cktID: got 0x%02x, want 0x42", f.cktID)
	}
	if f.origIdx != 0x10 {
		t.Errorf("origIdx: got 0x%02x, want 0x10", f.origIdx)
	}
	if f.origID != 0x20 {
		t.Errorf("origID: got 0x%02x, want 0x20", f.origID)
	}
	if f.origCallsign != "Q1TEST-1" {
		t.Errorf("origCallsign: got %q, want %q", f.origCallsign, "Q1TEST-1")
	}
	if f.origAlias != "QNODEA" {
		t.Errorf("origAlias: got %q, want %q", f.origAlias, "QNODEA")
	}
	if f.dstCallsign != "Q2TEST-1" {
		t.Errorf("dstCallsign: got %q, want %q", f.dstCallsign, "Q2TEST-1")
	}
	if f.dstAlias != "QNODEB" {
		t.Errorf("dstAlias: got %q, want %q", f.dstAlias, "QNODEB")
	}
	if f.windowSize != NETROM_WINDOW_DEFAULT {
		t.Errorf("windowSize: got %d, want %d", f.windowSize, NETROM_WINDOW_DEFAULT)
	}
}

func TestNetromConnAckRoundtrip(t *testing.T) {
	var payload = netromBuildConnAck("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0x10, 0x20, 0x01, 0x42, NETROM_WINDOW_DEFAULT, false)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse conn ack: %v", err)
	}
	if f.opcode != netromOpcodeConnAck {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeConnAck)
	}
	if f.cktIdx != 0x10 {
		t.Errorf("cktIdx: got 0x%02x, want 0x10", f.cktIdx)
	}
	if f.acceptIdx != 0x01 {
		t.Errorf("acceptIdx: got 0x%02x, want 0x01", f.acceptIdx)
	}
	if f.acceptID != 0x42 {
		t.Errorf("acceptID: got 0x%02x, want 0x42", f.acceptID)
	}
	if f.windowSize != NETROM_WINDOW_DEFAULT {
		t.Errorf("windowSize: got %d, want %d", f.windowSize, NETROM_WINDOW_DEFAULT)
	}
}

func TestNetromDisconnectRoundtrip(t *testing.T) {
	var payload = netromBuildDisconnect("Q2TEST", "Q1TEST", NETROM_TTL_DEFAULT, 0x05, 0x06, 0x03)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse disconnect: %v", err)
	}
	if f.opcode != netromOpcodeDisconnect {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeDisconnect)
	}
}

func TestNetromDiscAckRoundtrip(t *testing.T) {
	var payload = netromBuildDiscAck("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0x05, 0x06)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse disc ack: %v", err)
	}
	if f.opcode != netromOpcodeDiscAck {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeDiscAck)
	}
}

func TestNetromInfoRoundtrip(t *testing.T) {
	var data = []byte("hello NET/ROM")
	var payload = netromBuildInfo("Q2TEST", "Q1TEST", NETROM_TTL_DEFAULT, 0x01, 0x02, 0x03, 0x04, false, false, false, data)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse info: %v", err)
	}
	if f.opcode != netromOpcodeInfo {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeInfo)
	}
	if f.txSeq != 0x03 {
		t.Errorf("txSeq: got %d, want 3", f.txSeq)
	}
	if f.rxSeq != 0x04 {
		t.Errorf("rxSeq: got %d, want 4", f.rxSeq)
	}
	if !bytes.Equal(f.info, data) {
		t.Errorf("info: got %q, want %q", f.info, data)
	}
}

func TestNetromInfoFlagsRoundtrip(t *testing.T) {
	var payload = netromBuildInfo("Q2TEST", "Q1TEST", NETROM_TTL_DEFAULT, 0x01, 0x02, 0x05, 0x06, true, false, true, []byte("x"))

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse info with flags: %v", err)
	}
	if f.flags&netromFlagChoke == 0 {
		t.Error("expected CHOKE flag set")
	}
	if f.flags&netromFlagMore == 0 {
		t.Error("expected MORE flag set")
	}
	if f.flags&netromFlagNAK != 0 {
		t.Error("expected NAK flag clear")
	}
}

func TestNetromInfoAckRoundtrip(t *testing.T) {
	var payload = netromBuildInfoAck("Q1TEST", "Q2TEST", NETROM_TTL_DEFAULT, 0x01, 0x02, 0x07, false, false)

	var f, err = netromParseTransportFrame(payload)
	if err != nil {
		t.Fatalf("parse info ack: %v", err)
	}
	if f.opcode != netromOpcodeInfoAck {
		t.Errorf("opcode: got 0x%02x, want 0x%02x", f.opcode, netromOpcodeInfoAck)
	}
	if f.rxSeq != 0x07 {
		t.Errorf("rxSeq: got %d, want 7", f.rxSeq)
	}
}

func TestNetromParseTooShort(t *testing.T) {
	var _, err = netromParseTransportFrame([]byte{0x01, 0x02})
	if err == nil {
		t.Error("expected error for too-short frame")
	}
}

func TestNetromRoutingBroadcastEmpty(t *testing.T) {
	var srcAlias = netromPadAlias("QNODE1")
	var payload = netromBuildRoutingBroadcast(srcAlias, nil)
	var bc, err = netromParseRoutingBroadcast(payload)
	if err != nil {
		t.Fatalf("parse empty broadcast: %v", err)
	}
	if len(bc.entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(bc.entries))
	}
}
