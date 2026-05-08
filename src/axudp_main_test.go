// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"os"
	"path/filepath"
	"testing"
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

func TestAXUDPParseConfigFileNotFound(t *testing.T) {
	var _, err = axudpParseConfig("/nonexistent/axudp.yaml")
	if err == nil {
		t.Error("axudpParseConfig: expected error for missing file, got nil")
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
		{AX25Addr: "Q1TEST", Addr: "192.0.2.1:93"},   // no SSID — should match all SSIDs
		{AX25Addr: "Q2TEST-7", Addr: "192.0.2.2:93"}, // with SSID — exact match only
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
