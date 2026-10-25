// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

// INP3, the Internode Protocol: TheNetNode's routing extension to NET/ROM,
// after its specification, "A New Routing Specification for Packet Radio
// Datagram Networks" (docs/source/glossary/netrom.rst has the reference).
// Neighbours exchange these over the link between them, in the information
// field of an I frame with PID 0xCF:
//
//   - A routing information frame (RIF): a 0xFF signature in place of a
//     layer 3 frame's origin, then routing information packets (RIPs) of
//     destination callsign, hop count, a two-byte target time in 10ms units,
//     and options, each a length (counting itself and the type), a type and
//     a value, ending in a zero length.  A target time of 600s is the
//     horizon: the destination is unreachable.
//
//   - An RTT probe: a layer 3 INFO frame to the pseudo-callsign L3RTT whose
//     data is text, padded with spaces.  The neighbour sends it back, so the
//     origin times the round trip.  The text is the implementation's own,
//     save for its flags, each a "$" and a letter, such as "$N" for INP
//     capable.
//
// This only describes the frames for the monitor: the node takes no part in
// INP3 routing.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	rifSignature    = 0xFF
	ripLen          = callLen + 1 + 2 // Destination, hops, target time.
	rifOptionHeader = 2               // Length and type.
	rifHorizon      = 60000           // A target time of 600s: unreachable.

	RTTCallsign = "L3RTT"
)

// The RIP option types the specification defines.  Any other is shown as it
// is.
const (
	rifOptAlias = 0x00
	rifOptIPv4  = 0x01 // An address and the length of its network prefix.
)

var errRIF = errors.New("malformed INP3 routing information")

// RIP is a routing information packet, one route in a routing information
// frame.
type RIP struct {
	callsign   string
	hops       byte
	targetTime int // In 10ms intervals.
	options    []RIFOption
}

// RIFOption is one of a route's options: a type and its value.
type RIFOption struct {
	code  byte
	value []byte
}

// DecodeRIF decodes an INP3 routing information frame from the information
// field of an AX.25 frame with PID 0xCF.
func DecodeRIF(b []byte) ([]RIP, error) {
	if len(b) == 0 || b[0] != rifSignature {
		return nil, fmt.Errorf("%w: no 0xFF signature", errRIF)
	}

	var rips []RIP

	var rest = b[1:]

	for len(rest) > 0 {
		if len(rest) < ripLen {
			return nil, fmt.Errorf("%w: %d bytes left, a RIP needs %d", errRIF, len(rest), ripLen)
		}

		var e RIP

		var err error

		e.callsign, err = decodeCall(rest[:callLen])
		if err != nil {
			return nil, fmt.Errorf("%w: destination: %w", errRIF, err)
		}

		e.hops = rest[callLen]
		e.targetTime = int(rest[callLen+1])<<8 | int(rest[callLen+2])
		rest = rest[ripLen:]

		for {
			if len(rest) == 0 {
				return nil, fmt.Errorf("%w: options for %s do not end", errRIF, e.callsign)
			}

			var length = int(rest[0])
			if length == 0 {
				rest = rest[1:]

				break
			}

			if length < rifOptionHeader || length > len(rest) {
				return nil, fmt.Errorf("%w: option of length %d with %d bytes left", errRIF, length, len(rest))
			}

			e.options = append(e.options, RIFOption{code: rest[1], value: append([]byte(nil), rest[rifOptionHeader:length]...)})
			rest = rest[length:]
		}

		rips = append(rips, e)
	}

	return rips, nil
}

func describeRIF(rips []RIP) string {
	var sb strings.Builder

	sb.WriteString("INP3 RIF")

	for i, e := range rips {
		if i == 0 {
			sb.WriteString(":")
		} else {
			sb.WriteString(",")
		}

		fmt.Fprintf(&sb, " %s", e.callsign)

		var others []RIFOption

		for _, o := range e.options {
			if o.code == rifOptAlias {
				fmt.Fprintf(&sb, " (%s)", safeText(o.value))
			} else {
				others = append(others, o)
			}
		}

		if e.targetTime >= rifHorizon {
			fmt.Fprintf(&sb, " hops=%d unreachable", e.hops)
		} else {
			fmt.Fprintf(&sb, " hops=%d tt=%dms", e.hops, e.targetTime*10)
		}

		for _, o := range others {
			sb.WriteString(" " + describeRIFOption(o))
		}
	}

	return sb.String()
}

func describeRIFOption(o RIFOption) string {
	var v = o.value

	if o.code == rifOptIPv4 && len(v) == 5 {
		return fmt.Sprintf("ip=%d.%d.%d.%d/%d", v[0], v[1], v[2], v[3], v[4])
	}

	return fmt.Sprintf("opt%02x=%s", o.code, hex.EncodeToString(v))
}

// describeRTT describes f if it is an RTT probe, by its flags, the only part
// of its text the specification settles.
func describeRTT(f *Frame) (string, bool) {
	if f.opcode != OpInfo || f.destination != RTTCallsign {
		return "", false
	}

	var flags []string

	for word := range strings.FieldsSeq(string(f.info)) {
		if len(word) > 1 && word[0] == '$' {
			flags = append(flags, safeText([]byte(word)))
		}
	}

	var text = fmt.Sprintf("INP3 RTT from %s ttl=%d", f.origin, f.ttl)
	if len(flags) > 0 {
		text += ": flags=" + strings.Join(flags, " ")
	}

	return text, true
}
