// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strings"
)

// netromFrameToText renders a NET/ROM frame as readable text for the monitor,
// in the same spirit as xid_parse does for XID frames. A NET/ROM information
// field is binary — AX.25-shifted callsigns, opcodes and sequence numbers —
// so printing it raw says nothing about which nodes were advertised or what
// stage a circuit has reached, which is exactly what you want when working
// out why routing has not converged.
//
// Returns false for anything that is not a NET/ROM frame, and for one that
// does not parse, so the caller falls back to printing the raw bytes rather
// than hiding a malformed frame behind an error message.
//
// This is display-only: it is called for every monitored frame, including
// ones addressed to somebody else, and must not touch routing or circuit
// state.
func netromFrameToText(pp *packet_t, info []byte) (string, bool) {
	if pp == nil || ax25_get_pid(pp) != AX25_PID_NETROM {
		return "", false
	}

	// Same split as netrom_rx: the AX.25 destination says whether this is a
	// routing broadcast or a transport frame.
	var dst = strings.TrimRight(ax25_get_addr_with_ssid(pp, AX25_DESTINATION), " ")
	if strings.EqualFold(dst, NETROM_BROADCAST_CALLSIGN) {
		return netromNodesToText(info)
	}

	return netromTransportToText(info)
}

// netromNodesToText renders a NODES routing broadcast: who is advertising,
// and what they say they can reach.
func netromNodesToText(info []byte) (string, bool) {
	var bc, err = netromParseRoutingBroadcast(info)
	if err != nil {
		return "", false
	}

	var text strings.Builder
	fmt.Fprintf(&text, "NET/ROM NODES from %s", netromAliasText(bc.srcAlias))

	if len(bc.entries) == 0 {
		text.WriteString(" (no routes)")

		return text.String(), true
	}

	for _, e := range bc.entries {
		fmt.Fprintf(&text, " [%s %s via %s q=%d]",
			e.dstCallsign, netromAliasText(e.dstAlias), e.neighbor, e.quality)
	}

	return text.String(), true
}

// netromTransportToText renders a transport frame as its opcode plus the
// fields that mean something for that opcode.
func netromTransportToText(info []byte) (string, bool) {
	var f, err = netromParseTransportFrame(info)
	if err != nil {
		return "", false
	}

	var text strings.Builder

	// The network-layer endpoints are worth showing even though the line
	// already carries AX.25 addresses: on a multi-hop circuit the two
	// differ, one being the radio neighbour and the other the far end.
	fmt.Fprintf(&text, "NET/ROM %s %s>%s ttl=%d",
		netromOpcodeText(f.opcode), f.net.src, f.net.dst, f.net.ttl)

	switch f.opcode {
	case netromOpcodeConnect:
		fmt.Fprintf(&text, " orig=%d/%d %s to %s win=%d",
			f.origIdx, f.origID, f.origCallsign, f.dstCallsign, f.windowSize)
	case netromOpcodeConnAck:
		fmt.Fprintf(&text, " ckt=%d/%d accept=%d/%d win=%d",
			f.cktIdx, f.cktID, f.acceptIdx, f.acceptID, f.windowSize)
	case netromOpcodeInfo:
		// Keep the payload visible: connected-mode traffic is often text,
		// and the whole point of decoding the header is not to lose sight
		// of what is being carried.
		fmt.Fprintf(&text, " ckt=%d/%d n(s)=%d n(r)=%d len=%d %s",
			f.cktIdx, f.cktID, f.txSeq, f.rxSeq, len(f.info), netromPayloadText(f.info))
	case netromOpcodeInfoAck, netromOpcodeDisconnect:
		fmt.Fprintf(&text, " ckt=%d/%d n(r)=%d", f.cktIdx, f.cktID, f.rxSeq)
	default:
		fmt.Fprintf(&text, " ckt=%d/%d", f.cktIdx, f.cktID)
	}

	for _, flag := range []struct {
		bit  byte
		name string
	}{
		{netromFlagChoke, "CHOKE"},
		{netromFlagNAK, "NAK"},
		{netromFlagMore, "MORE"},
	} {
		if f.flags&flag.bit != 0 {
			fmt.Fprintf(&text, " %s", flag.name)
		}
	}

	return text.String(), true
}

// netromOpcodeText names an opcode, falling back to its number so an
// unrecognised one is still reported rather than silently blank.
func netromOpcodeText(opcode byte) string {
	switch opcode {
	case netromOpcodeConnect:
		return "CONNECT REQ"
	case netromOpcodeConnAck:
		return "CONNECT ACK"
	case netromOpcodeDisconnect:
		return "DISCONNECT REQ"
	case netromOpcodeDiscAck:
		return "DISCONNECT ACK"
	case netromOpcodeInfo:
		return "INFO"
	case netromOpcodeInfoAck:
		return "INFO ACK"
	default:
		return fmt.Sprintf("opcode 0x%02x", opcode)
	}
}

// netromPayloadText renders an INFO payload, showing printable ASCII as
// itself and anything else as its actual byte value.
//
// It deliberately works a byte at a time rather than reusing AX25SafePrint,
// which ranges over runes: a byte that is not valid UTF-8 decodes to U+FFFD
// there and prints as the literal "<0xfffd>" instead of the byte that was
// actually on the air.
func netromPayloadText(info []byte) string {
	var text strings.Builder
	text.WriteByte('"')

	for _, b := range info {
		if b >= ' ' && b < 0x7f {
			text.WriteByte(b)
		} else {
			fmt.Fprintf(&text, "<0x%02x>", b)
		}
	}

	text.WriteByte('"')

	return text.String()
}

// netromAliasText trims a fixed-width alias to something printable.
func netromAliasText(alias [netromAliasLen]byte) string {
	return strings.TrimRight(string(alias[:]), " \x00")
}
