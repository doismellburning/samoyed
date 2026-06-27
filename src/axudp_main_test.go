// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAXUDPAddCRC(t *testing.T) {
	// A minimal AX.25 frame (14 bytes: dest + src address fields).
	var frame = []byte{
		0x82, 0xA0, 0x6E, 0x98, 0x9C, 0x40, 0xE0, // dest
		0x82, 0xA0, 0x6E, 0x98, 0x9C, 0x42, 0x61, // src
	}

	var got = axudpAddCRC(frame)

	// Must be 2 bytes longer.
	if len(got) != len(frame)+2 {
		t.Fatalf("axudpAddCRC: want len %d, got %d", len(frame)+2, len(got))
	}

	// Frame bytes must be unchanged at the start.
	for i, b := range frame {
		if got[i] != b {
			t.Fatalf("axudpAddCRC: frame byte %d changed: want 0x%02x got 0x%02x", i, b, got[i])
		}
	}

	// CRC is at the end as a LE uint16 and must equal fcs_calc(frame).
	var want = fcs_calc(frame)
	var crc = uint16(got[len(frame)]) | uint16(got[len(frame)+1])<<8
	if crc != want {
		t.Errorf("axudpAddCRC: crc=0x%04x want 0x%04x", crc, want)
	}
}

func TestAXUDPStripCRC(t *testing.T) {
	var frame = []byte{0xAA, 0xBB, 0xCC, 0xDD}
	var withCRC = axudpAddCRC(frame)

	var got, ok = axudpStripCRC(withCRC)
	if !ok {
		t.Fatal("axudpStripCRC: reported invalid checksum for a packet we just built")
	}

	if len(got) != len(frame) {
		t.Fatalf("axudpStripCRC: want len %d, got %d", len(frame), len(got))
	}

	for i, b := range frame {
		if got[i] != b {
			t.Fatalf("axudpStripCRC: byte %d: want 0x%02x got 0x%02x", i, b, got[i])
		}
	}
}

func TestAXUDPStripCRCBadChecksum(t *testing.T) {
	// A bare AX.25 frame with no CRC appended — should be rejected.
	var raw = []byte{
		0x82, 0xA0, 0x6E, 0x98, 0x9C, 0x40, 0xE0,
		0x82, 0xA0, 0x6E, 0x98, 0x9C, 0x42, 0x61,
	}

	var _, ok = axudpStripCRC(raw)
	if ok {
		t.Error("axudpStripCRC: accepted a frame with no CRC appended (should have failed)")
	}
}

func TestAXUDPParseConfig(t *testing.T) {
	var dir = t.TempDir()
	var p = filepath.Join(dir, "axudp.yaml")
	var content = `maps:
  - ax25addr: q1test
    host: 192.0.2.1
    port: 93
  - ax25addr: Q2TEST-7
    host: 192.0.2.2
    port: 20093
`
	var writeErr = os.WriteFile(p, []byte(content), 0600)
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	var entries, err = axudpParseConfig(p)
	if err != nil {
		t.Fatalf("axudpParseConfig: unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("axudpParseConfig: want 2 entries, got %d", len(entries))
	}
	if entries[0].AX25Addr != "Q1TEST" {
		t.Errorf("entries[0].AX25Addr = %q, want %q", entries[0].AX25Addr, "Q1TEST")
	}
	if entries[0].Addr != "192.0.2.1:93" {
		t.Errorf("entries[0].Addr = %q, want %q", entries[0].Addr, "192.0.2.1:93")
	}
	if entries[1].AX25Addr != "Q2TEST-7" {
		t.Errorf("entries[1].AX25Addr = %q, want %q", entries[1].AX25Addr, "Q2TEST-7")
	}
	if entries[1].Addr != "192.0.2.2:20093" {
		t.Errorf("entries[1].Addr = %q, want %q", entries[1].Addr, "192.0.2.2:20093")
	}
}

func TestAXUDPParseConfigNormalisesAX25Addr(t *testing.T) {
	var cases = []struct {
		name     string
		ax25addr string
		wantNorm string
	}{
		{"lowercase", "q1test", "Q1TEST"},
		{"with leading/trailing spaces", "  Q1TEST  ", "Q1TEST"},
		{"ssid zero stripped", "Q1TEST-0", "Q1TEST"},
		{"lowercase with ssid zero", "q1test-0", "Q1TEST"},
		{"non-zero ssid preserved", "Q1TEST-7", "Q1TEST-7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dir = t.TempDir()
			var p = filepath.Join(dir, "axudp.yaml")
			var content = "maps:\n  - ax25addr: " + tc.ax25addr + "\n    host: 192.0.2.1\n    port: 93\n"
			var writeErr = os.WriteFile(p, []byte(content), 0600)
			if writeErr != nil {
				t.Fatal(writeErr)
			}
			var entries, err = axudpParseConfig(p)
			if err != nil {
				t.Fatalf("axudpParseConfig: unexpected error: %v", err)
			}
			if entries[0].AX25Addr != tc.wantNorm {
				t.Errorf("AX25Addr = %q, want %q", entries[0].AX25Addr, tc.wantNorm)
			}
		})
	}
}

func TestAXUDPParseConfigResolvesUDPAddr(t *testing.T) {
	var dir = t.TempDir()
	var p = filepath.Join(dir, "axudp.yaml")
	var content = `maps:
  - ax25addr: Q1TEST
    host: 192.0.2.1
    port: 93
`
	var writeErr = os.WriteFile(p, []byte(content), 0600)
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	var entries, err = axudpParseConfig(p)
	if err != nil {
		t.Fatalf("axudpParseConfig: unexpected error: %v", err)
	}
	if entries[0].UDPAddr == nil {
		t.Fatal("axudpParseConfig: UDPAddr is nil, expected resolved address")
	}
	if entries[0].UDPAddr.String() != "192.0.2.1:93" {
		t.Errorf("UDPAddr = %q, want %q", entries[0].UDPAddr.String(), "192.0.2.1:93")
	}
}

func TestAXUDPParseConfigFileNotFound(t *testing.T) {
	var _, err = axudpParseConfig("/nonexistent/axudp.yaml")
	if err == nil {
		t.Error("axudpParseConfig: expected error for missing file, got nil")
	}
}

func TestAXUDPParseConfigValidation(t *testing.T) {
	var cases = []struct {
		name    string
		content string
	}{
		{
			name: "empty ax25addr",
			content: `maps:
  - ax25addr: ""
    host: 192.0.2.1
    port: 93
`,
		},
		{
			name: "empty host",
			content: `maps:
  - ax25addr: Q1TEST
    host: ""
    port: 93
`,
		},
		{
			name: "port zero",
			content: `maps:
  - ax25addr: Q1TEST
    host: 192.0.2.1
    port: 0
`,
		},
		{
			name: "port too high",
			content: `maps:
  - ax25addr: Q1TEST
    host: 192.0.2.1
    port: 65536
`,
		},
		{
			name: "whitespace-only ax25addr",
			content: `maps:
  - ax25addr: "   "
    host: 192.0.2.1
    port: 93
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dir = t.TempDir()
			var p = filepath.Join(dir, "axudp.yaml")
			var writeErr = os.WriteFile(p, []byte(tc.content), 0600)
			if writeErr != nil {
				t.Fatal(writeErr)
			}
			var _, err = axudpParseConfig(p)
			if err == nil {
				t.Errorf("axudpParseConfig: expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestAXUDPParseConfigBadYAML(t *testing.T) {
	var dir = t.TempDir()
	var p = filepath.Join(dir, "axudp.yaml")
	var writeErr = os.WriteFile(p, []byte("{ not valid yaml: ["), 0600)
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	var _, err = axudpParseConfig(p)
	if err == nil {
		t.Error("axudpParseConfig: expected error for malformed YAML, got nil")
	}
}

func TestAXUDPLookupMap(t *testing.T) {
	var b = new(axudpBridge)
	b.maps = []axudpMapEntry{
		{AX25Addr: "Q1TEST", Addr: "192.0.2.1:93", UDPAddr: nil},   // no SSID — should match all SSIDs
		{AX25Addr: "Q2TEST-7", Addr: "192.0.2.2:93", UDPAddr: nil}, // with SSID — exact match only
	}

	var cases = []struct {
		dest      string
		wantAddr  string
		wantFound bool
	}{
		// Q1TEST (no SSID) matches bare address and any SSID
		{"Q1TEST", "192.0.2.1:93", true},
		{"Q1TEST-1", "192.0.2.1:93", true},
		{"Q1TEST-15", "192.0.2.1:93", true},
		// Q2TEST-7 (with SSID) matches only exact
		{"Q2TEST-7", "192.0.2.2:93", true},
		{"Q2TEST", "", false},
		{"Q2TEST-1", "", false},
		// Unknown address
		{"Q3TEST", "", false},
	}

	for _, tc := range cases {
		var entry, ok = b.lookupMap(tc.dest)
		if ok != tc.wantFound {
			t.Errorf("lookupMap(%q): found=%v want %v", tc.dest, ok, tc.wantFound)
			continue
		}
		if ok && entry.Addr != tc.wantAddr {
			t.Errorf("lookupMap(%q): addr=%q want %q", tc.dest, entry.Addr, tc.wantAddr)
		}
	}
}

// TestAXUDPLookupMapExactBeforeWildcard verifies that an exact SSID match takes
// priority over a no-SSID (wildcard) entry regardless of YAML order.
func TestAXUDPLookupMapExactBeforeWildcard(t *testing.T) {
	// Wildcard entry is listed first; specific SSID entry is listed second.
	// A lookup for Q1TEST-7 must return the specific entry, not the wildcard.
	var b = new(axudpBridge)
	b.maps = []axudpMapEntry{
		{AX25Addr: "Q1TEST", Addr: "192.0.2.1:93", UDPAddr: nil},   // wildcard — listed first
		{AX25Addr: "Q1TEST-7", Addr: "192.0.2.2:93", UDPAddr: nil}, // specific SSID-7 — listed second
	}

	var entry, ok = b.lookupMap("Q1TEST-7")
	if !ok {
		t.Fatal("lookupMap(Q1TEST-7): not found")
	}
	if entry.Addr != "192.0.2.2:93" {
		t.Errorf("lookupMap(Q1TEST-7): got addr %q, want specific entry 192.0.2.2:93 (exact match must beat wildcard)", entry.Addr)
	}

	// Wildcard should still match when no exact entry exists.
	var entry2, ok2 = b.lookupMap("Q1TEST-3")
	if !ok2 {
		t.Fatal("lookupMap(Q1TEST-3): not found via wildcard")
	}
	if entry2.Addr != "192.0.2.1:93" {
		t.Errorf("lookupMap(Q1TEST-3): got addr %q, want wildcard entry 192.0.2.1:93", entry2.Addr)
	}
}

// TestKISSExactlyFullBufferDiscarded verifies that a KISS frame that fills the
// accumulator buffer exactly (kf.kiss_len == MAX_KISS_LEN when the closing FEND
// arrives, so no room to append the closing FEND) is treated as overflow and
// discarded rather than forwarded as a truncated/unterminated frame.
//
// The frame is constructed so that, if forwarded, sendAXUDP would be called on a
// bridge with a nil UDP connection, which would panic.  A panic therefore means
// the frame was forwarded; no panic means it was correctly discarded.
func TestKISSExactlyFullBufferDiscarded(t *testing.T) {
	// Build a bridge with a map entry that matches the destination encoded in
	// the test frame below.  The udpConn is intentionally left nil so that any
	// accidental call to sendAXUDP panics immediately.
	var b = new(axudpBridge)
	b.maps = []axudpMapEntry{
		{AX25Addr: "Q1TEST", Addr: "192.0.2.1:93", UDPAddr: nil},
	}

	// The KISS DATA frame payload is: type byte (0x00) followed by an AX.25
	// frame whose first 7 bytes are the encoded destination "Q1TEST\x00" (SSID
	// 0, end-of-address bit set → 0xE0).  We pad the rest to reach exactly
	// MAX_KISS_LEN - 1 content bytes so that together with the opening FEND
	// the accumulator holds exactly MAX_KISS_LEN bytes when the closing FEND
	// arrives (kf.kiss_len == MAX_KISS_LEN, *overflow == false).
	//
	// AX.25 address encoding: each character shifted left one bit.
	// Q=0x51<<1=0xA2, 1=0x31<<1=0x62, T=0x54<<1=0xA8, E=0x45<<1=0x8A, S=0x53<<1=0xA6, T=0x54<<1=0xA8
	// SSID byte: 0xE0 (no SSID, end-of-address-field set for destination)
	var ax25Dest = []byte{0xA2, 0x62, 0xA8, 0x8A, 0xA6, 0xA8, 0xE0}

	// Total content bytes = 1 (type) + len(ax25Dest) + padding = MAX_KISS_LEN - 1
	var padLen = MAX_KISS_LEN - 1 - 1 - len(ax25Dest)
	var content []byte
	content = append(content, KISS_CMD_DATA_FRAME) // type byte
	content = append(content, ax25Dest...)
	for range padLen {
		content = append(content, 0x41)
	}

	// Build stream: FEND + content (MAX_KISS_LEN-1 bytes) + FEND.
	// Opening FEND → kf.kiss_len = 1; content → kf.kiss_len = MAX_KISS_LEN;
	// closing FEND arrives with kf.kiss_len == MAX_KISS_LEN, *overflow == false.
	var buf []byte
	buf = append(buf, FEND)
	buf = append(buf, content...)
	buf = append(buf, FEND)

	// If the frame is forwarded despite being unterminated, sendAXUDP will
	// dereference the nil udpConn and panic — recover it as a test failure.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("exactly-full buffer frame was forwarded (sendAXUDP panicked): %v", r)
		}
	}()

	var kf kiss_frame_t
	var overflow bool
	for _, by := range buf {
		my_kiss_rec_byte_axudp(&kf, &overflow, by, b)
	}

	// After the closing FEND the state machine must reset cleanly.
	if overflow {
		t.Error("overflow flag should be cleared after discarding exactly-full frame")
	}
	if kf.state != KS_SEARCHING {
		t.Errorf("state = %v after exactly-full frame, want KS_SEARCHING", kf.state)
	}
	if kf.kiss_len != 0 {
		t.Errorf("kiss_len = %d after exactly-full frame, want 0", kf.kiss_len)
	}
}

// TestKISSOverflowDiscarded verifies that a frame whose raw KISS bytes exceed
// MAX_KISS_LEN is discarded on the closing FEND rather than forwarded in
// truncated form.  It checks that the state machine resets cleanly.
func TestKISSOverflowDiscarded(t *testing.T) {
	// Empty bridge — no maps, so even an accidentally forwarded frame would
	// just log to stderr rather than panic.
	var b = new(axudpBridge)

	// Build a KISS input: FEND + type byte + MAX_KISS_LEN data bytes + FEND.
	// MAX_KISS_LEN data bytes is enough to trigger the overflow condition.
	var buf []byte
	buf = append(buf, FEND)
	buf = append(buf, KISS_CMD_DATA_FRAME)
	for range MAX_KISS_LEN {
		buf = append(buf, 0x41)
	}
	buf = append(buf, FEND)

	var kf kiss_frame_t
	var overflow bool
	for _, by := range buf {
		my_kiss_rec_byte_axudp(&kf, &overflow, by, b)
	}

	// After the closing FEND the overflow flag should be cleared and the state
	// machine should be back in KS_SEARCHING with kiss_len reset.
	if overflow {
		t.Error("overflow flag should be cleared after discarding frame")
	}
	if kf.state != KS_SEARCHING {
		t.Errorf("state = %v after overflow frame, want KS_SEARCHING", kf.state)
	}
	if kf.kiss_len != 0 {
		t.Errorf("kiss_len = %d after overflow frame, want 0", kf.kiss_len)
	}
}

// fakeConn is a minimal net.Conn implementation that records Write calls.
type fakeConn struct {
	mu     sync.Mutex
	writes [][]byte
}

func (f *fakeConn) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.writes = append(f.writes, append([]byte(nil), b...))
	f.mu.Unlock()
	return len(b), nil
}

func (f *fakeConn) SetWriteDeadline(_ time.Time) error { return nil }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) Read(_ []byte) (int, error)         { return 0, nil }
func (f *fakeConn) LocalAddr() net.Addr                { return nil }
func (f *fakeConn) RemoteAddr() net.Addr               { return nil }
func (f *fakeConn) SetDeadline(_ time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(_ time.Time) error  { return nil }

// singleReadConn is a net.Conn that returns a fixed payload together with
// io.EOF on the first Read call, simulating a TCP connection that delivers its
// last bytes in the same call as EOF (the (n>0, err!=nil) case permitted by the
// io.Reader contract).  Subsequent calls return (0, io.EOF).
type singleReadConn struct {
	fakeConn

	data []byte
	mu   sync.Mutex
	done bool
}

func (c *singleReadConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return 0, io.EOF
	}
	c.done = true
	return copy(b, c.data), io.EOF
}

// TestHandleKISSClientProcessesFinalReadBytes is a regression test for the
// case where conn.Read returns (n>0, io.EOF): bytes in the final read must be
// processed before the loop exits, not silently dropped.
func TestHandleKISSClientProcessesFinalReadBytes(t *testing.T) {
	// Create a UDP socket pair: src is used by the bridge, dst receives frames.
	var srcPkt, srcErr = net.ListenPacket("udp", "127.0.0.1:0")
	if srcErr != nil {
		t.Fatal(srcErr)
	}
	defer srcPkt.Close()

	var dstPkt, dstErr = net.ListenPacket("udp", "127.0.0.1:0")
	if dstErr != nil {
		t.Fatal(dstErr)
	}
	defer dstPkt.Close()

	var dstUDP, dstOK = dstPkt.(*net.UDPConn)
	if !dstOK {
		t.Fatal("dstPkt is not a *net.UDPConn")
	}
	var dstLocalAddr = dstUDP.LocalAddr()
	var dstAddr, dstAddrOK = dstLocalAddr.(*net.UDPAddr)
	if !dstAddrOK {
		t.Fatal("dstPkt local addr is not a *net.UDPAddr")
	}

	var srcUDP, srcOK = srcPkt.(*net.UDPConn)
	if !srcOK {
		t.Fatal("srcPkt is not a *net.UDPConn")
	}

	// Build a minimal KISS DATA frame carrying an AX.25 frame destined for Q1TEST.
	// AX.25 encoding: each callsign character shifted left 1 bit.
	// Q=0xA2 1=0x62 T=0xA8 E=0x8A S=0xA6 T=0xA8  SSID=0xE0 (no SSID, end-of-addr)
	var ax25Dest = []byte{0xA2, 0x62, 0xA8, 0x8A, 0xA6, 0xA8, 0xE0}
	// Arbitrary source address with end-of-address bit set.
	var ax25Src = []byte{0xA4, 0x64, 0xAA, 0x8C, 0xA8, 0xAA, 0x61}
	var ax25frame = append(ax25Dest, ax25Src...)
	var payload = append([]byte{KISS_CMD_DATA_FRAME}, ax25frame...)
	var kissframe = kiss_encapsulate(payload)

	// Set up a bridge with a MAP entry routing Q1TEST to dstPkt.
	var b = new(axudpBridge)
	b.maps = []axudpMapEntry{
		{AX25Addr: "Q1TEST", Addr: dstAddr.String(), UDPAddr: dstAddr},
	}
	b.udpConn = srcUDP

	// fconn delivers the full KISS frame + io.EOF in a single Read call.
	var fconn = new(singleReadConn)
	fconn.data = kissframe

	var done = make(chan struct{})
	go func() {
		b.handleKISSClient(fconn)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleKISSClient did not exit after conn returned io.EOF")
	}

	// An AXUDP datagram must have been forwarded to dstPkt.
	var setErr = dstUDP.SetReadDeadline(time.Now().Add(time.Second))
	if setErr != nil {
		t.Fatal(setErr)
	}
	var rxBuf = make([]byte, 4096)
	var n, _, rxErr = dstUDP.ReadFromUDP(rxBuf)
	if rxErr != nil {
		t.Fatalf("no AXUDP datagram received — bytes from final read were dropped: %v", rxErr)
	}
	if n == 0 {
		t.Fatal("received empty AXUDP datagram")
	}
}

// TestBroadcastKISSDropsOversizedFrame verifies that broadcastKISS does not
// write a KISS-encoded frame to clients when the on-wire length would exceed
// MAX_KISS_LEN.  Oversized frames would be silently truncated by the nettnc.go
// KISS reader, producing corrupt AX.25 data.
func TestBroadcastKISSDropsOversizedFrame(t *testing.T) {
	var b = new(axudpBridge)
	var fc = new(fakeConn)
	b.clients = []net.Conn{fc}

	// An AX.25 frame large enough that kiss_encapsulate produces > MAX_KISS_LEN
	// bytes: payload = 1 type byte + ax25frame, encapsulated with 2 FENDs.
	// With no bytes needing escaping, output = len(payload) + 2 bytes.
	// We need len(payload) > MAX_KISS_LEN - 2, so len(ax25frame) >= MAX_KISS_LEN - 2.
	var ax25frame = make([]byte, MAX_KISS_LEN)
	b.broadcastKISS(ax25frame)

	fc.mu.Lock()
	var writeCount = len(fc.writes)
	fc.mu.Unlock()

	if writeCount != 0 {
		t.Errorf("broadcastKISS wrote %d frame(s) to client for oversized frame, want 0", writeCount)
	}
}
