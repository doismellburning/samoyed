// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:	Decode the information part of an INP3 frame.
 *
 * Description:	INP3 is a routing / link-metric extension to NET/ROM
 *		(AX.25 PID 0xCF) used by G8BPQ's BPQ32/LinBPQ node
 *		software and compatible node switches (TheNet-X,
 *		XRouter). It carries three kinds of message inside
 *		the ordinary NET/ROM Level 3 envelope
 *		(L3DEST[7] L3SRCE[7] L3TTL L4FLAGS ...):
 *
 *			RIF        - routing information: a list of
 *			             destination/hop/metric entries,
 *			             identified by L3SRCE[0] == 0xFF.
 *			RTT        - a neighbour link-timing probe or
 *			             reply, identified by L3DEST
 *			             matching the pseudo-callsign
 *			             "L3RTT".
 *			Keepalive  - an otherwise-empty message,
 *			             identified by L3DEST matching the
 *			             pseudo-callsign "L3KEEP".
 *
 *		Samoyed does not participate in INP3 routing (no
 *		NET/ROM Level 4 exists to make that useful - see
 *		docs) - this is a passive decoder for display/logging
 *		only, analogous to decode_aprs.go for APRS.
 *
 *		This was reverse-engineered from the reference
 *		implementation, G8BPQ's LinBPQ BPQINP3.c (GPL), rather
 *		than from a formal specification, so some details -
 *		notably the exact byte length of the NET/ROM Level 3
 *		header preceding a RIF entry list - are best-effort
 *		inferences and may not be byte-perfect for every INP3
 *		implementation in the wild. The RTT message is located
 *		by searching for its literal "L3RTT: " ID field rather
 *		than assuming a fixed header length, which avoids that
 *		particular uncertainty.
 *
 *------------------------------------------------------------------*/

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// inp3_kind_e identifies which of the three INP3 message kinds a frame is.
type inp3_kind_e int

const (
	inp3_kind_rif inp3_kind_e = iota
	inp3_kind_rtt
	inp3_kind_keepalive
)

// inp3_rif_entry_t is one destination entry within an INP3 RIF (routing)
// message: a callsign, hop count, RTT/STT metric, and zero or more decoded
// TLV options.
type inp3_rif_entry_t struct {
	Callsign string
	Hops     int
	RTT      int // 10ms units. >= 60000 means "unreachable" (route withdrawal).

	Alias string // TLV 0x00, "" if absent.

	HasIPv4    bool // TLV 0x01
	IPv4       string
	IPv4Prefix int

	HasPosition bool // TLV 0x10
	Lat, Lon    float64

	HasCapability bool // TLV 0x11
	SWType        byte
	Flags1        byte
	Flags2        byte

	HasTimestamp bool // TLV 0x12
	Timestamp    time.Time

	HasTCPPort bool // TLV 0x13
	TCPPort    int

	HasTZOffset     bool // TLV 0x14
	TZOffsetMinutes int

	Locator string // TLV 0x15, "" if absent.
	QTH     string // TLV 0x16, "" if absent.
	Version string // TLV 0x17, "" if absent.
}

// inp3_rtt_t holds the parsed fields of an INP3 RTT (link-timing) message.
type inp3_rtt_t struct {
	TXTime      string
	SmoothedRTT string
	LastRTT     string
	RTTID       string
	Alias       string
	Version     string
	SWVersion   string
	Flags       string
}

// decode_inp3_t is the result of decoding one INP3 frame.
type decode_inp3_t struct {
	Kind       inp3_kind_e
	RIFEntries []inp3_rif_entry_t // populated only for inp3_kind_rif.
	RTT        *inp3_rtt_t        // populated only for inp3_kind_rtt.
}

// The pseudo-callsigns INP3 uses as the L3 destination address of RTT and
// keepalive control messages, instead of an actual node callsign.
const inp3_pseudo_call_rtt = "L3RTT"
const inp3_pseudo_call_keep = "L3KEEP"

// inp3_rtt_id_marker is the literal ID field of the fixed-format RTT
// message struct ("L3RTT: ", 7 bytes), used to locate the struct within the
// frame regardless of the exact preceding header length.
const inp3_rtt_id_marker = "L3RTT: "

// inp3_decode_addr decodes a 7-byte AX.25-address-encoded callsign field
// (as used both for AX.25 address fields and for INP3's embedded NET/ROM L3
// DEST/SRC/entry-callsign fields) into human-readable text with SSID.
func inp3_decode_addr(b []byte) string {
	if len(b) < 7 {
		return ""
	}

	var sb strings.Builder
	for i := range 6 {
		sb.WriteByte((b[i] >> 1) & 0x7f)
	}

	var call = strings.TrimRight(sb.String(), " ")

	var ssid = int((b[6] & SSID_SSID_MASK) >> SSID_SSID_SHIFT)
	if ssid != 0 {
		call += fmt.Sprintf("-%d", ssid)
	}

	return call
}

// decode_inp3 examines a received packet and, if it is an INP3 frame,
// returns its decoded contents. Returns nil for anything else, including
// plain NET/ROM traffic (e.g. classic NODES broadcasts or connected-mode
// circuit frames) that happens to share PID 0xCF.
func decode_inp3(pp *packet_t) *decode_inp3_t {
	if ax25_get_pid(pp) != AX25_PID_NETROM {
		return nil
	}

	var pinfo = AX25GetInfo(pp)

	if len(pinfo) >= inp3RIFSrceEnd && pinfo[inp3RIFSrceOffset] == 0xff {
		var D = new(decode_inp3_t)
		D.Kind = inp3_kind_rif
		D.RIFEntries = decode_inp3_rif(pinfo[inp3RIFHeaderLen:])

		return D
	}

	if len(pinfo) >= 7 {
		var dest = inp3_decode_addr(pinfo[0:7])

		switch dest {
		case inp3_pseudo_call_rtt:
			var D = new(decode_inp3_t)
			D.Kind = inp3_kind_rtt
			D.RTT = decode_inp3_rtt(pinfo)

			return D
		case inp3_pseudo_call_keep:
			var D = new(decode_inp3_t)
			D.Kind = inp3_kind_keepalive

			return D
		}
	}

	return nil
}

// Offsets within the INP3-embedded NET/ROM L3 envelope: L3DEST[7] L3SRCE[7].
// See the package doc comment above regarding the uncertainty of the header
// length (L3TTL + L4FLAGS, 2 bytes) preceding the RIF entry list.
const inp3RIFSrceOffset = 7
const inp3RIFSrceEnd = 14
const inp3RIFHeaderLen = 16

// decode_inp3_rif parses a sequence of RIF entries: each is a 7-byte
// callsign, 1-byte hop count, 2-byte big-endian RTT/STT, followed by a
// sequence of TLV options terminated by a single 0x00 length byte.
func decode_inp3_rif(data []byte) []inp3_rif_entry_t {
	var entries []inp3_rif_entry_t

	var pos = 0
	for pos+10 <= len(data) {
		var e inp3_rif_entry_t
		e.Callsign = inp3_decode_addr(data[pos : pos+7])
		e.Hops = int(data[pos+7])
		e.RTT = int(data[pos+8])<<8 | int(data[pos+9])
		pos += 10

		pos = decode_inp3_rif_options(data, pos, &e)

		entries = append(entries, e)
	}

	return entries
}

// decode_inp3_rif_options parses the TLV options block for one RIF entry,
// starting at pos, and returns the position just after the terminating
// 0x00 length byte (or the end of data, if the block is malformed/truncated).
func decode_inp3_rif_options(data []byte, pos int, e *inp3_rif_entry_t) int {
	for pos < len(data) {
		var length = int(data[pos])

		if length == 0 {
			return pos + 1
		}

		if pos+length > len(data) {
			return len(data)
		}

		var opcode = data[pos+1]
		var value = data[pos+2 : pos+length]

		decode_inp3_rif_option(opcode, value, e)

		pos += length
	}

	return pos
}

func decode_inp3_rif_option(opcode byte, value []byte, e *inp3_rif_entry_t) {
	switch opcode {
	case 0x00: // Alias
		e.Alias = strings.TrimSpace(string(value))
	case 0x01: // IPv4 address + prefix length
		if len(value) >= 5 {
			e.HasIPv4 = true
			e.IPv4 = fmt.Sprintf("%d.%d.%d.%d", value[0], value[1], value[2], value[3])
			e.IPv4Prefix = int(value[4])
		}
	case 0x10: // Position
		if len(value) >= 8 {
			e.HasPosition = true
			e.Lat = float64(int32(binary.BigEndian.Uint32(value[0:4]))) / 6000.0
			e.Lon = float64(int32(binary.BigEndian.Uint32(value[4:8]))) / 6000.0
		}
	case 0x11: // Capability/flags
		if len(value) >= 3 {
			e.HasCapability = true
			e.SWType = value[0]
			e.Flags1 = value[1]
			e.Flags2 = value[2]
		}
	case 0x12: // Timestamp
		if len(value) >= 4 {
			e.HasTimestamp = true
			e.Timestamp = time.Unix(int64(binary.BigEndian.Uint32(value[0:4])), 0).UTC()
		}
	case 0x13: // TCP service port
		if len(value) >= 2 {
			e.HasTCPPort = true
			e.TCPPort = int(binary.BigEndian.Uint16(value[0:2]))
		}
	case 0x14: // Timezone offset, minutes
		if len(value) >= 2 {
			e.HasTZOffset = true
			e.TZOffsetMinutes = int(int16(binary.BigEndian.Uint16(value[0:2])))
		}
	case 0x15: // Maidenhead locator
		e.Locator = string(value)
	case 0x16: // QTH description
		e.QTH = string(value)
	case 0x17: // Software version string
		e.Version = string(value)
	}
}

// Fixed field widths and offsets (relative to the "L3RTT: " marker) of the
// 236-byte RTT message struct, per BPQINP3.c's _RTTMSG.
const (
	inp3RTTTxTimeOff      = 7
	inp3RTTSmoothedRTTOff = 18
	inp3RTTLastRTTOff     = 29
	inp3RTTIDOff          = 40
	inp3RTTAliasOff       = 51
	inp3RTTVersionOff     = 58
	inp3RTTSWVersionOff   = 70
	inp3RTTFlagsOff       = 79
	inp3RTTFieldWidth     = 10
	inp3RTTAliasWidth     = 7
	inp3RTTVersionWidth   = 12
	inp3RTTSWVersionWidth = 9
	inp3RTTFlagsWidth     = 20
)

// decode_inp3_rtt locates and parses the fixed-format RTT message struct
// within an INP3 frame's info part, by searching for its literal
// "L3RTT: " ID field. Returns a struct with whatever fields fit in the
// available data - a truncated/malformed frame yields empty fields rather
// than a panic.
func decode_inp3_rtt(pinfo []byte) *inp3_rtt_t {
	var idx = strings.Index(string(pinfo), inp3_rtt_id_marker)
	if idx < 0 {
		return nil
	}

	var body = pinfo[idx:]
	var field = func(off, width int) string {
		if off+width > len(body) {
			return ""
		}

		return strings.TrimSpace(string(body[off : off+width]))
	}

	return &inp3_rtt_t{
		TXTime:      field(inp3RTTTxTimeOff, inp3RTTFieldWidth),
		SmoothedRTT: field(inp3RTTSmoothedRTTOff, inp3RTTFieldWidth),
		LastRTT:     field(inp3RTTLastRTTOff, inp3RTTFieldWidth),
		RTTID:       field(inp3RTTIDOff, inp3RTTFieldWidth),
		Alias:       field(inp3RTTAliasOff, inp3RTTAliasWidth),
		Version:     field(inp3RTTVersionOff, inp3RTTVersionWidth),
		SWVersion:   field(inp3RTTSWVersionOff, inp3RTTSWVersionWidth),
		Flags:       field(inp3RTTFlagsOff, inp3RTTFlagsWidth),
	}
}

// decode_inp3_print prints a decoded INP3 frame in human-readable form,
// following the same dw_printf/text_color_set conventions used by
// decode_aprs_print.
func decode_inp3_print(D *decode_inp3_t) {
	text_color_set(DW_COLOR_DECODED)

	switch D.Kind {
	case inp3_kind_rif:
		dw_printf("INP3 routing information, %d entr%s:\n", len(D.RIFEntries), plural_ies(len(D.RIFEntries)))

		for _, e := range D.RIFEntries {
			decode_inp3_print_rif_entry(e)
		}
	case inp3_kind_rtt:
		if D.RTT == nil {
			dw_printf("INP3 RTT message (could not locate fixed fields)\n")

			return
		}

		dw_printf("INP3 RTT: alias=%s sw=%s ver=%s smoothedRTT=%s lastRTT=%s id=%s flags=%s\n",
			D.RTT.Alias, D.RTT.SWVersion, D.RTT.Version, D.RTT.SmoothedRTT, D.RTT.LastRTT, D.RTT.RTTID, D.RTT.Flags)
	case inp3_kind_keepalive:
		dw_printf("INP3 keepalive\n")
	}
}

func decode_inp3_print_rif_entry(e inp3_rif_entry_t) {
	if e.RTT >= 60000 {
		dw_printf("  %-9s hops=%d  withdrawn/unreachable", e.Callsign, e.Hops)
	} else {
		dw_printf("  %-9s hops=%d  rtt=%dms", e.Callsign, e.Hops, e.RTT*10)
	}

	if e.Alias != "" {
		dw_printf("  alias=%s", e.Alias)
	}

	if e.HasIPv4 {
		dw_printf("  ip=%s/%d", e.IPv4, e.IPv4Prefix)
	}

	if e.HasPosition {
		dw_printf("  pos=%.4f,%.4f", e.Lat, e.Lon)
	}

	if e.HasCapability {
		dw_printf("  swtype=0x%02x flags=0x%02x,0x%02x", e.SWType, e.Flags1, e.Flags2)
	}

	if e.HasTimestamp {
		dw_printf("  time=%s", e.Timestamp.Format(time.RFC3339))
	}

	if e.HasTCPPort {
		dw_printf("  tcp=%d", e.TCPPort)
	}

	if e.HasTZOffset {
		dw_printf("  tz=%+dmin", e.TZOffsetMinutes)
	}

	if e.Locator != "" {
		dw_printf("  loc=%s", e.Locator)
	}

	if e.QTH != "" {
		dw_printf("  qth=%s", e.QTH)
	}

	if e.Version != "" {
		dw_printf("  version=%s", e.Version)
	}

	dw_printf("\n")
}

func plural_ies(n int) string {
	if n == 1 {
		return "y"
	}

	return "ies"
}
