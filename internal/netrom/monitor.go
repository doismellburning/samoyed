// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"fmt"
	"strings"

	"github.com/doismellburning/samoyed/internal/ax25"
)

// Describe describes a frame carrying NET/ROM for the monitor
// output, as xid_parse describes an XID, rather than leaving its binary
// information field to be printed as it is.  It reports false for a frame
// that is not NET/ROM, or does not decode as it.
func Describe(pp *ax25.Packet) (string, bool) {
	if pp.PID() != ax25.PIDNetROM {
		return "", false
	}

	var info = pp.Info()

	if pp.FrameTypeOnly() == ax25.FrameTypeUUI &&
		strings.EqualFold(pp.AddrNoSSID(ax25.Destination), NodesCallsign) {
		var nodes, err = DecodeNodes(info)
		if err != nil {
			return "", false
		}

		return describeNodes(nodes), true
	}

	// No layer 3 frame starts with 0xFF, which cannot be in a callsign.
	if len(info) > 0 && info[0] == rifSignature {
		var entries, err = DecodeRIF(info)
		if err != nil {
			return "", false
		}

		return describeRIF(entries), true
	}

	var f, err = DecodeFrame(info)
	if err != nil {
		return "", false
	}

	if text, ok := describeRTT(f); ok {
		return text, true
	}

	return describeFrame(f), true
}

// MonitorInfo is what the monitor should show for pp's information
// field: its description if it is NET/ROM, otherwise the field itself.
func MonitorInfo(pp *ax25.Packet, info []byte) []byte {
	if text, ok := Describe(pp); ok {
		return []byte(text)
	}

	return info
}

func describeNodes(n *Nodes) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "NET/ROM NODES from %s", n.alias)

	for i, e := range n.entries {
		if i == 0 {
			sb.WriteString(":")
		} else {
			sb.WriteString(",")
		}

		fmt.Fprintf(&sb, " %s", e.callsign)

		if e.alias != "" {
			fmt.Fprintf(&sb, " (%s)", e.alias)
		}

		fmt.Fprintf(&sb, " via %s q=%d", e.neighbour, e.quality)
	}

	return sb.String()
}

func describeFrame(f *Frame) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "NET/ROM %s to %s ttl=%d: %s circuit %02X/%02X", f.origin, f.destination, f.ttl, f.opcode, f.index, f.id)

	switch f.opcode {
	case OpConnReq:
		fmt.Fprintf(&sb, " window=%d user=%s node=%s", f.window, f.user, f.originNode)

		if timeout, ok := f.timeout.Get(); ok {
			fmt.Fprintf(&sb, " timeout=%ds", timeout)
		}

	case OpConnAck:
		if f.has(FlagChoke) {
			sb.WriteString(" REFUSED")

			return sb.String()
		}

		fmt.Fprintf(&sb, " from %02X/%02X window=%d", f.peerIndex(), f.peerID(), f.window)

	case OpInfo:
		fmt.Fprintf(&sb, " s=%d r=%d%s len=%d: %s", f.txSeq, f.rxSeq, describeFlags(f), len(f.info), safeText(f.info))

	case OpInfoAck:
		fmt.Fprintf(&sb, " r=%d%s", f.rxSeq, describeFlags(f))

	case OpProtoExt:
		fmt.Fprintf(&sb, " len=%d", len(f.info))

	case OpDiscReq, OpDiscAck:
	}

	return sb.String()
}

func describeFlags(f *Frame) string {
	var flags string

	if f.has(FlagChoke) {
		flags += " CHOKE"
	}

	if f.has(FlagNAK) {
		flags += " NAK"
	}

	if f.has(FlagMore) {
		flags += " MORE"
	}

	return flags
}

// safeText renders data a byte at a time, printable ASCII as it is and
// anything else as <0xNN>.  Going by runes instead would turn every byte of
// a binary payload that is not valid UTF-8 into the same replacement
// character, hiding what it was.
func safeText(data []byte) string {
	var sb strings.Builder

	for _, b := range data {
		if b >= ' ' && b <= '~' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "<0x%02x>", b)
		}
	}

	return sb.String()
}
