// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"strings"

	"github.com/doismellburning/samoyed/internal/ax25"
)

// netromMonitorText describes a frame carrying NET/ROM for the monitor
// output, as xid_parse describes an XID, rather than leaving its binary
// information field to be printed as it is.  It reports false for a frame
// that is not NET/ROM, or does not decode as it.
func netromMonitorText(pp *ax25.Packet) (string, bool) {
	if pp.PID() != ax25.PIDNetROM {
		return "", false
	}

	var info = pp.Info()

	if pp.FrameTypeOnly() == ax25.FrameTypeUUI &&
		strings.EqualFold(pp.AddrNoSSID(ax25.Destination), netromNodesCallsign) {
		var nodes, err = decodeNetromNodes(info)
		if err != nil {
			return "", false
		}

		return describeNetromNodes(nodes), true
	}

	var f, err = decodeNetromFrame(info)
	if err != nil {
		return "", false
	}

	return describeNetromFrame(f), true
}

// netromMonitorInfo is what the monitor should show for pp's information
// field: its description if it is NET/ROM, otherwise the field itself.
func netromMonitorInfo(pp *ax25.Packet, info []byte) []byte {
	if text, ok := netromMonitorText(pp); ok {
		return []byte(text)
	}

	return info
}

func describeNetromNodes(n *netromNodes) string {
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

func describeNetromFrame(f *netromFrame) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "NET/ROM %s to %s ttl=%d: %s circuit %02X/%02X", f.origin, f.destination, f.ttl, f.opcode, f.index, f.id)

	switch f.opcode {
	case netromOpConnReq:
		fmt.Fprintf(&sb, " window=%d user=%s node=%s", f.window, f.user, f.originNode)

		if timeout, ok := f.timeout.Get(); ok {
			fmt.Fprintf(&sb, " timeout=%ds", timeout)
		}

	case netromOpConnAck:
		if f.has(netromFlagChoke) {
			sb.WriteString(" REFUSED")

			return sb.String()
		}

		fmt.Fprintf(&sb, " from %02X/%02X window=%d", f.peerIndex(), f.peerID(), f.window)

	case netromOpInfo:
		fmt.Fprintf(&sb, " s=%d r=%d%s len=%d: %s", f.txSeq, f.rxSeq, describeNetromFlags(f), len(f.info), netromSafeText(f.info))

	case netromOpInfoAck:
		fmt.Fprintf(&sb, " r=%d%s", f.rxSeq, describeNetromFlags(f))

	case netromOpProtoExt:
		fmt.Fprintf(&sb, " len=%d", len(f.info))

	case netromOpDiscReq, netromOpDiscAck:
	}

	return sb.String()
}

func describeNetromFlags(f *netromFrame) string {
	var flags string

	if f.has(netromFlagChoke) {
		flags += " CHOKE"
	}

	if f.has(netromFlagNAK) {
		flags += " NAK"
	}

	if f.has(netromFlagMore) {
		flags += " MORE"
	}

	return flags
}

// netromSafeText renders data a byte at a time, printable ASCII as it is and
// anything else as <0xNN>.  Going by runes instead would turn every byte of
// a binary payload that is not valid UTF-8 into the same replacement
// character, hiding what it was.
func netromSafeText(data []byte) string {
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
