// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ax25EncodeAddr7 encodes a callsign (no SSID) into a 7-byte AX.25 address
// field: 6 bytes of shifted-left ASCII, space-padded, plus a trailing byte
// with SSID 0 (only the reserved bits are set, matching real AX.25 frames -
// decode_inp3 only looks at the SSID bits within it).
func ax25EncodeAddr7(call string) []byte {
	var padded = call
	for len(padded) < 6 {
		padded += " "
	}

	var b = make([]byte, 7)
	for i := range 6 {
		b[i] = padded[i] << 1
	}

	b[6] = 0x60

	return b
}

// padRight space-pads (or truncates) s to exactly n bytes.
func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}

	return s + strings.Repeat(" ", n-len(s))
}

// inp3PacketFrom builds a NET/ROM (PID 0xCF) UI frame carrying pinfo as its
// information part.
func inp3PacketFrom(t *testing.T, pinfo []byte) *packet_t {
	t.Helper()

	var pp = AX25FromText("Q1TEST>Q2TEST:", true)
	require.NotNil(t, pp)
	ax25_set_info(pp, pinfo)
	ax25_set_pid(pp, AX25_PID_NETROM)

	return pp
}

func Test_decode_inp3_rif(t *testing.T) {
	// L3 envelope: DEST (arbitrary), SRCE with the 0xFF RIF sentinel, then
	// 2 header bytes (TTL, flags) before the entry list.
	var header = ax25EncodeAddr7("NODES")
	var srce = ax25EncodeAddr7("Q1TEST")
	srce[0] = 0xff
	header = append(header, srce...)
	header = append(header, 0x01, 0x00)

	// Entry 1: a live route with an Alias TLV option.
	var entry1 = ax25EncodeAddr7("Q1TEST")
	entry1 = append(entry1, 3)          // hops
	entry1 = append(entry1, 0x00, 0xfa) // RTT/STT = 250 (10ms units = 2500ms)
	entry1 = append(entry1, 0x08, 0x00) // TLV: len=8 (2 header + 6 value), opcode 0x00 = Alias
	entry1 = append(entry1, []byte("TESTND")...)
	entry1 = append(entry1, 0x00) // options terminator

	// Entry 2: a withdrawn/unreachable route (RTT >= 60000), no options.
	var entry2 = ax25EncodeAddr7("Q2TEST")
	entry2 = append(entry2, 99)         // hops
	entry2 = append(entry2, 0xea, 0x60) // RTT/STT = 60000 (withdrawal sentinel)
	entry2 = append(entry2, 0x00)       // options terminator

	var pinfo = append(header, entry1...)
	pinfo = append(pinfo, entry2...)

	var pp = inp3PacketFrom(t, pinfo)

	var D = decode_inp3(pp)
	require.NotNil(t, D)
	assert.Equal(t, inp3_kind_rif, D.Kind)
	require.Len(t, D.RIFEntries, 2)

	assert.Equal(t, "Q1TEST", D.RIFEntries[0].Callsign)
	assert.Equal(t, 3, D.RIFEntries[0].Hops)
	assert.Equal(t, 250, D.RIFEntries[0].RTT)
	assert.Equal(t, "TESTND", D.RIFEntries[0].Alias)

	assert.Equal(t, "Q2TEST", D.RIFEntries[1].Callsign)
	assert.Equal(t, 99, D.RIFEntries[1].Hops)
	assert.Equal(t, 60000, D.RIFEntries[1].RTT)
	assert.Empty(t, D.RIFEntries[1].Alias)

	// Must not panic when printed.
	decode_inp3_print(D)
}

func Test_decode_inp3_rif_all_tlv_options(t *testing.T) {
	var header = ax25EncodeAddr7("NODES")
	var srce = ax25EncodeAddr7("Q1TEST")
	srce[0] = 0xff
	header = append(header, srce...)
	header = append(header, 0x01, 0x00)

	var entry = ax25EncodeAddr7("Q1TEST")
	entry = append(entry, 1, 0x00, 0x05) // hops=1, RTT=5

	// 0x01 IPv4 + prefix.
	entry = append(entry, 0x07, 0x01, 192, 168, 1, 1, 24)
	// 0x10 Position: lat=51.5 deg, lon=-0.1 deg, in 1/100 arc-min units (*6000).
	// latraw = 51.5*6000 = 309000 = 0x0004b708, lonraw = -0.1*6000 = -600 = 0xfffffda8.
	entry = append(entry, 0x0a, 0x10, 0x00, 0x04, 0xb7, 0x08, 0xff, 0xff, 0xfd, 0xa8)
	// 0x11 Capability flags.
	entry = append(entry, 0x05, 0x11, 0x01, 0x02, 0x03)
	// 0x12 Timestamp (Unix time 1700000000 = 0x6553f100).
	entry = append(entry, 0x06, 0x12, 0x65, 0x53, 0xf1, 0x00)
	// 0x13 TCP port 8000.
	entry = append(entry, 0x04, 0x13, 0x1f, 0x40)
	// 0x14 TZ offset -60 minutes.
	entry = append(entry, 0x04, 0x14, 0xff, 0xc4)
	// 0x15 Maidenhead locator.
	entry = append(entry, byte(2+len("IO91")), 0x15)
	entry = append(entry, []byte("IO91")...)
	// 0x16 QTH description.
	entry = append(entry, byte(2+len("Test City")), 0x16)
	entry = append(entry, []byte("Test City")...)
	// 0x17 Software version.
	entry = append(entry, byte(2+len("Samoyed")), 0x17)
	entry = append(entry, []byte("Samoyed")...)
	entry = append(entry, 0x00) // terminator

	var pinfo = append(header, entry...)

	var pp = inp3PacketFrom(t, pinfo)

	var D = decode_inp3(pp)
	require.NotNil(t, D)
	require.Len(t, D.RIFEntries, 1)

	var e = D.RIFEntries[0]
	assert.True(t, e.HasIPv4)
	assert.Equal(t, "192.168.1.1", e.IPv4)
	assert.Equal(t, 24, e.IPv4Prefix)

	assert.True(t, e.HasPosition)
	assert.InDelta(t, 51.5, e.Lat, 0.01)
	assert.InDelta(t, -0.1, e.Lon, 0.01)

	assert.True(t, e.HasCapability)
	assert.Equal(t, byte(1), e.SWType)
	assert.Equal(t, byte(2), e.Flags1)
	assert.Equal(t, byte(3), e.Flags2)

	assert.True(t, e.HasTimestamp)
	assert.Equal(t, int64(1700000000), e.Timestamp.Unix())

	assert.True(t, e.HasTCPPort)
	assert.Equal(t, 8000, e.TCPPort)

	assert.True(t, e.HasTZOffset)
	assert.Equal(t, -60, e.TZOffsetMinutes)

	assert.Equal(t, "IO91", e.Locator)
	assert.Equal(t, "Test City", e.QTH)
	assert.Equal(t, "Samoyed", e.Version)

	decode_inp3_print(D)
}

func Test_decode_inp3_rtt(t *testing.T) {
	var header = ax25EncodeAddr7(inp3_pseudo_call_rtt)
	header = append(header, ax25EncodeAddr7("Q1TEST")...)
	header = append(header, 0x02, 0x00) // TTL=2, flags

	var body = "L3RTT:" + " " +
		fmt.Sprintf("%10d", 12345) + " " +
		fmt.Sprintf("%10d", 42) + " " +
		fmt.Sprintf("%10d", 41) + " " +
		fmt.Sprintf("%10d", 7) + " " +
		padRight("TEST", 7) +
		padRight("LEVEL3_V2.1", 12) +
		padRight("BPQ32004", 9) +
		padRight("$M90 $H4", 20) +
		padRight("", 137)
	require.Len(t, body, 236)

	var pinfo = append(header, []byte(body)...)

	var pp = inp3PacketFrom(t, pinfo)

	var D = decode_inp3(pp)
	require.NotNil(t, D)
	assert.Equal(t, inp3_kind_rtt, D.Kind)
	require.NotNil(t, D.RTT)
	assert.Equal(t, "12345", D.RTT.TXTime)
	assert.Equal(t, "42", D.RTT.SmoothedRTT)
	assert.Equal(t, "41", D.RTT.LastRTT)
	assert.Equal(t, "7", D.RTT.RTTID)
	assert.Equal(t, "TEST", D.RTT.Alias)
	assert.Equal(t, "LEVEL3_V2.1", D.RTT.Version)
	assert.Equal(t, "BPQ32004", D.RTT.SWVersion)
	assert.Equal(t, "$M90 $H4", D.RTT.Flags)

	decode_inp3_print(D)
}

func Test_decode_inp3_keepalive(t *testing.T) {
	var header = ax25EncodeAddr7(inp3_pseudo_call_keep)
	header = append(header, ax25EncodeAddr7("Q1TEST")...)
	header = append(header, 0x01, 0x00)

	var pp = inp3PacketFrom(t, header)

	var D = decode_inp3(pp)
	require.NotNil(t, D)
	assert.Equal(t, inp3_kind_keepalive, D.Kind)

	decode_inp3_print(D)
}

// Test_decode_inp3_not_inp3 confirms plain NET/ROM traffic (any PID-0xCF
// frame not matching one of the INP3 sentinels) is not misdetected as INP3.
func Test_decode_inp3_not_inp3(t *testing.T) {
	var pinfo = []byte(strings.Repeat("X", 20))

	var pp = inp3PacketFrom(t, pinfo)

	var D = decode_inp3(pp)
	assert.Nil(t, D)
}

// Test_decode_inp3_wrong_pid confirms a non-NET/ROM PID is never treated as
// INP3, even if its payload happens to match an INP3 sentinel byte pattern.
func Test_decode_inp3_wrong_pid(t *testing.T) {
	var header = ax25EncodeAddr7(inp3_pseudo_call_keep)
	header = append(header, ax25EncodeAddr7("Q1TEST")...)
	header = append(header, 0x01, 0x00)

	var pp = AX25FromText("Q1TEST>Q2TEST:", true)
	require.NotNil(t, pp)
	ax25_set_info(pp, header)
	ax25_set_pid(pp, AX25_PID_NO_LAYER_3)

	var D = decode_inp3(pp)
	assert.Nil(t, D)
}
