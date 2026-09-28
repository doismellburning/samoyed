// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// NET/ROM frame formats.
//
// Everything NET/ROM puts on the air goes in the information field of an
// AX.25 frame with PID 0xCF.  There are two kinds:
//
//   - A NODES routing broadcast, a UI frame addressed to "NODES": a 0xFF
//     signature, the sender's six-character alias, then up to eleven 21-byte
//     entries of destination callsign, destination alias, best neighbour and
//     quality.
//
//   - A layer 3 frame, carried between neighbours over a connected-mode AX.25
//     link: a 15-byte network header (origin and destination node callsigns
//     and a time to live) and a 5-byte transport header (two bytes naming a
//     circuit, two of sequence numbers or a second circuit, and an opcode with
//     flags), followed by whatever that opcode carries.
//
// The layouts follow the Linux NET/ROM stack (net/netrom in the kernel) and
// its routing daemon (netromd in ax25tools), which interoperate with the other
// NET/ROM implementations on the air.  docs/source/protocols.rst has the
// detail and the references.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	netromCallLen            = 7  // An AX.25 address as it appears in an address field.
	netromAliasLen           = 6  // Space padded.
	netromNetworkHeaderLen   = 15 // Origin, destination, TTL.
	netromTransportHeaderLen = 5
	netromHeaderLen          = netromNetworkHeaderLen + netromTransportHeaderLen

	netromNodesSignature  = 0xFF
	netromNodesHeaderLen  = 1 + netromAliasLen
	netromNodesEntryLen   = netromCallLen + netromAliasLen + netromCallLen + 1
	netromNodesMaxEntries = 11 // What fits in the 256 bytes netromd allows itself.
	netromNodesCallsign   = "NODES"

	// The BPQ extensions: a CONNECT REQUEST may carry the originator's
	// transport timeout, and a CONNECT ACKNOWLEDGE the answering node's TTL.
	netromConnReqLen      = netromHeaderLen + 1 + netromCallLen + netromCallLen
	netromConnReqBPQLen   = netromConnReqLen + 2
	netromConnAckLen      = netromHeaderLen + 1
	netromConnAckBPQLen   = netromConnAckLen + 1
	netromSSIDSpareBits   = 0x60 // The two reserved bits of an SSID byte, set as AX.25 sets them.
	netromMaxSSID         = 15
	netromMaxCallsignBase = 6
)

// netromOpcode is the low nibble of the fifth transport header byte.
type netromOpcode byte

const (
	netromOpProtoExt netromOpcode = 0 // Protocol extension, e.g. IP over NET/ROM; not handled.
	netromOpConnReq  netromOpcode = 1
	netromOpConnAck  netromOpcode = 2
	netromOpDiscReq  netromOpcode = 3
	netromOpDiscAck  netromOpcode = 4
	netromOpInfo     netromOpcode = 5
	netromOpInfoAck  netromOpcode = 6

	netromOpcodeMask = 0x0F
)

func (op netromOpcode) String() string {
	switch op {
	case netromOpProtoExt:
		return "PROTOEXT"
	case netromOpConnReq:
		return "CONN REQ"
	case netromOpConnAck:
		return "CONN ACK"
	case netromOpDiscReq:
		return "DISC REQ"
	case netromOpDiscAck:
		return "DISC ACK"
	case netromOpInfo:
		return "INFO"
	case netromOpInfoAck:
		return "INFO ACK"
	}

	return "opcode " + strconv.Itoa(int(op))
}

// The flags in the high nibble of the fifth transport header byte.
const (
	netromFlagChoke byte = 0x80 // The sender cannot take any more; on a CONNECT ACK, a refusal.
	netromFlagNAK   byte = 0x40 // Please resend from the acknowledged sequence number.
	netromFlagMore  byte = 0x20 // More of this message follows in the next INFO frame.

	netromFlagsMask byte = 0xF0
)

var errNetromShort = errors.New("NET/ROM frame too short")

// netromFrame is a layer 3 frame: the network header, the transport header,
// and whatever the opcode carries.
//
// The four circuit bytes are kept as they appear on the air, because what they
// mean depends on the opcode.  index and id always name a circuit: on a
// CONNECT REQUEST the sender's own, on everything else the recipient's.  On
// INFO and INFO ACK, txSeq and rxSeq are the send and receive sequence
// numbers; on a CONNECT ACK they are the answering end's circuit index and ID.
type netromFrame struct {
	origin      string
	destination string
	ttl         byte

	index  byte
	id     byte
	txSeq  byte
	rxSeq  byte
	opcode netromOpcode
	flags  byte

	// CONNECT REQUEST and CONNECT ACK: the proposed, or accepted, window.
	window byte

	// CONNECT REQUEST: the user on whose behalf the circuit is wanted and the
	// node they are using, plus the originator's transport timeout in seconds
	// when it sends one (a BPQ extension).
	user       string
	originNode string
	timeout    maybe.Maybe[int]

	// CONNECT ACK: the answering node's TTL, when it sends one (a BPQ
	// extension).
	ackTTL maybe.Maybe[byte]

	// INFO: the data.  A protocol extension frame keeps everything after the
	// transport header here, undecoded.
	info []byte
}

// peerIndex and peerID are the answering end's circuit on a CONNECT ACK.
func (f *netromFrame) peerIndex() byte { return f.txSeq }
func (f *netromFrame) peerID() byte    { return f.rxSeq }

// has reports whether flag is set.
func (f *netromFrame) has(flag byte) bool { return f.flags&flag != 0 }

// decodeNetromFrame decodes a layer 3 frame from the information field of an
// AX.25 frame with PID 0xCF.
func decodeNetromFrame(b []byte) (*netromFrame, error) {
	if len(b) < netromHeaderLen {
		return nil, fmt.Errorf("%w: %d bytes, a header needs %d", errNetromShort, len(b), netromHeaderLen)
	}

	var f = new(netromFrame)

	var err error

	f.origin, err = decodeNetromCall(b[0:7])
	if err != nil {
		return nil, fmt.Errorf("origin: %w", err)
	}

	f.destination, err = decodeNetromCall(b[7:14])
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}

	f.ttl = b[14]
	f.index = b[15]
	f.id = b[16]
	f.txSeq = b[17]
	f.rxSeq = b[18]
	f.opcode = netromOpcode(b[19] & netromOpcodeMask)
	f.flags = b[19] & netromFlagsMask

	var rest = b[netromHeaderLen:]

	switch f.opcode {
	case netromOpConnReq:
		if len(b) < netromConnReqLen {
			return nil, fmt.Errorf("%w: CONNECT REQUEST of %d bytes, needs %d", errNetromShort, len(b), netromConnReqLen)
		}

		f.window = rest[0]

		f.user, err = decodeNetromCall(rest[1:8])
		if err != nil {
			return nil, fmt.Errorf("user: %w", err)
		}

		f.originNode, err = decodeNetromCall(rest[8:15])
		if err != nil {
			return nil, fmt.Errorf("originating node: %w", err)
		}

		if len(b) >= netromConnReqBPQLen {
			f.timeout = maybe.Just(int(rest[15]) | int(rest[16])<<8)
		}

	case netromOpConnAck:
		// A refusal need not carry a window, but an acceptance must.
		if len(rest) == 0 {
			if !f.has(netromFlagChoke) {
				return nil, fmt.Errorf("%w: CONNECT ACK with no window", errNetromShort)
			}

			break
		}

		f.window = rest[0]

		if len(b) >= netromConnAckBPQLen {
			f.ackTTL = maybe.Just(rest[1])
		}

	case netromOpInfo, netromOpProtoExt:
		f.info = append([]byte(nil), rest...)

	case netromOpDiscReq, netromOpDiscAck, netromOpInfoAck:
		// Nothing more.

	default:
		return nil, fmt.Errorf("unknown NET/ROM opcode %d", f.opcode)
	}

	return f, nil
}

// encode renders the frame as the information field of an AX.25 frame.
func (f *netromFrame) encode() ([]byte, error) {
	var b = make([]byte, 0, netromHeaderLen+netromCallLen*2+2+len(f.info))

	var err error

	b, err = appendNetromCall(b, f.origin)
	if err != nil {
		return nil, fmt.Errorf("origin: %w", err)
	}

	b, err = appendNetromCall(b, f.destination)
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}

	b = append(b, f.ttl, f.index, f.id, f.txSeq, f.rxSeq, byte(f.opcode)&netromOpcodeMask|f.flags&netromFlagsMask)

	switch f.opcode {
	case netromOpConnReq:
		b = append(b, f.window)

		b, err = appendNetromCall(b, f.user)
		if err != nil {
			return nil, fmt.Errorf("user: %w", err)
		}

		b, err = appendNetromCall(b, f.originNode)
		if err != nil {
			return nil, fmt.Errorf("originating node: %w", err)
		}

		if timeout, ok := f.timeout.Get(); ok {
			if timeout < 0 || timeout > 0xFFFF {
				return nil, fmt.Errorf("transport timeout %d does not fit in two bytes", timeout)
			}

			b = append(b, byte(timeout&0xFF), byte(timeout>>8))
		}

	case netromOpConnAck:
		b = append(b, f.window)

		if ttl, ok := f.ackTTL.Get(); ok {
			b = append(b, ttl)
		}

	case netromOpInfo, netromOpProtoExt:
		b = append(b, f.info...)

	case netromOpDiscReq, netromOpDiscAck, netromOpInfoAck:
		// Nothing more.

	default:
		return nil, fmt.Errorf("unknown NET/ROM opcode %d", f.opcode)
	}

	return b, nil
}

// netromNodesEntry is one destination advertised in a NODES broadcast.
type netromNodesEntry struct {
	callsign  string
	alias     string
	neighbour string // The best neighbour the sender reaches it by.
	quality   byte
}

// netromNodes is a decoded NODES broadcast.
type netromNodes struct {
	alias   string // The sending node's; its callsign is the AX.25 source.
	entries []netromNodesEntry
}

// decodeNetromNodes decodes the information field of a NODES broadcast.  As
// netromd does, trailing bytes too few for a whole entry are ignored; unlike
// it, an entry with an invalid callsign or alias is skipped rather than
// abandoning the rest of the broadcast.
func decodeNetromNodes(b []byte) (*netromNodes, error) {
	if len(b) < netromNodesHeaderLen {
		return nil, fmt.Errorf("%w: NODES broadcast of %d bytes", errNetromShort, len(b))
	}

	if b[0] != netromNodesSignature {
		return nil, fmt.Errorf("NODES broadcast signature is 0x%02x, not 0x%02x", b[0], netromNodesSignature)
	}

	var n = new(netromNodes)

	var err error

	n.alias, err = decodeNetromAlias(b[1:7])
	if err != nil {
		return nil, fmt.Errorf("sender alias: %w", err)
	}

	for rest := b[netromNodesHeaderLen:]; len(rest) >= netromNodesEntryLen; rest = rest[netromNodesEntryLen:] {
		var e netromNodesEntry

		var callErr, aliasErr, neighbourErr error

		e.callsign, callErr = decodeNetromCall(rest[0:7])
		e.alias, aliasErr = decodeNetromAlias(rest[7:13])
		e.neighbour, neighbourErr = decodeNetromCall(rest[13:20])
		e.quality = rest[20]

		if callErr != nil || aliasErr != nil || neighbourErr != nil {
			continue
		}

		n.entries = append(n.entries, e)
	}

	return n, nil
}

// encodeNetromNodes builds the information fields of a NODES broadcast from
// alias and entries, at most netromNodesMaxEntries to a frame.  There is
// always at least one, even with no entries: the header alone is how a node
// tells its neighbours it is there.
func encodeNetromNodes(alias string, entries []netromNodesEntry) ([][]byte, error) {
	var header = []byte{netromNodesSignature}

	var err error

	header, err = appendNetromAlias(header, alias)
	if err != nil {
		return nil, fmt.Errorf("sender alias: %w", err)
	}

	var frames [][]byte

	for {
		var frame = append([]byte(nil), header...)

		var n = min(len(entries), netromNodesMaxEntries)

		for _, e := range entries[:n] {
			frame, err = appendNetromCall(frame, e.callsign)
			if err != nil {
				return nil, fmt.Errorf("entry callsign: %w", err)
			}

			frame, err = appendNetromAlias(frame, e.alias)
			if err != nil {
				return nil, fmt.Errorf("entry alias: %w", err)
			}

			frame, err = appendNetromCall(frame, e.neighbour)
			if err != nil {
				return nil, fmt.Errorf("entry neighbour: %w", err)
			}

			frame = append(frame, e.quality)
		}

		frames = append(frames, frame)
		entries = entries[n:]

		if len(entries) == 0 {
			return frames, nil
		}
	}
}

// netromCallsign parses "CALL" or "CALL-SSID" into its base and SSID,
// uppercasing it.  The base is one to six letters and digits and the SSID
// 0 to 15, as an AX.25 address field can hold.
func netromCallsign(s string) (string, int, error) {
	var base, ssidText, hasSSID = strings.Cut(strings.ToUpper(strings.TrimSpace(s)), "-")

	if base == "" || len(base) > netromMaxCallsignBase {
		return "", 0, fmt.Errorf("callsign %q must be 1 to %d characters before any SSID", s, netromMaxCallsignBase)
	}

	for _, c := range base {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "", 0, fmt.Errorf("callsign %q may only contain letters and digits", s)
		}
	}

	var ssid = 0

	if hasSSID {
		var err error

		ssid, err = strconv.Atoi(ssidText)
		if err != nil || ssid < 0 || ssid > netromMaxSSID {
			return "", 0, fmt.Errorf("callsign %q has an SSID outside 0-%d", s, netromMaxSSID)
		}
	}

	return base, ssid, nil
}

// normaliseNetromCallsign returns s as decodeNetromCall would render it:
// uppercase, and with no SSID suffix when the SSID is 0.
func normaliseNetromCallsign(s string) (string, error) {
	var base, ssid, err = netromCallsign(s)
	if err != nil {
		return "", err
	}

	if ssid == 0 {
		return base, nil
	}

	return base + "-" + strconv.Itoa(ssid), nil
}

// appendNetromCall appends callsign as a seven-byte AX.25 address: six
// characters shifted left one bit and space padded, then the SSID byte, with
// neither the C nor the end-of-address bit set.
func appendNetromCall(b []byte, callsign string) ([]byte, error) {
	var base, ssid, err = netromCallsign(callsign)
	if err != nil {
		return b, err
	}

	for i := range netromMaxCallsignBase {
		var c = byte(' ')
		if i < len(base) {
			c = base[i]
		}

		b = append(b, c<<1)
	}

	return append(b, netromSSIDSpareBits|byte(ssid)<<1), nil
}

// decodeNetromCall decodes a seven-byte AX.25 address, accepting what
// netromd accepts: uppercase letters and digits, then nothing but spaces.
func decodeNetromCall(b []byte) (string, error) {
	if len(b) < netromCallLen {
		return "", fmt.Errorf("%w: address of %d bytes", errNetromShort, len(b))
	}

	var sb strings.Builder

	var ended = false

	for _, raw := range b[:netromMaxCallsignBase] {
		var c = (raw >> 1) & 0x7F

		switch {
		case c == ' ':
			ended = true
		case !ended && ((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')):
			sb.WriteByte(c)
		default:
			return "", fmt.Errorf("invalid character 0x%02x in address", c)
		}
	}

	if sb.Len() == 0 {
		return "", errors.New("empty address")
	}

	var ssid = int(b[6]>>1) & 0x0F
	if ssid != 0 {
		sb.WriteString("-" + strconv.Itoa(ssid))
	}

	return sb.String(), nil
}

// netromAliasChars is what netromd accepts in an alias (it calls them
// mnemonics) when not insisting on the strictly compliant uppercase set.
const netromAliasChars = "#_&-/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// validNetromAlias reports whether alias is at most six characters that
// netromd would accept.  An empty alias is allowed; some nodes have none.
func validNetromAlias(alias string) bool {
	if len(alias) > netromAliasLen {
		return false
	}

	for _, c := range alias {
		if !strings.ContainsRune(netromAliasChars, c) {
			return false
		}
	}

	return true
}

// appendNetromAlias appends alias space padded to six bytes.
func appendNetromAlias(b []byte, alias string) ([]byte, error) {
	if !validNetromAlias(alias) {
		return b, fmt.Errorf("alias %q must be at most %d of %q", alias, netromAliasLen, netromAliasChars)
	}

	b = append(b, alias...)

	for range netromAliasLen - len(alias) {
		b = append(b, ' ')
	}

	return b, nil
}

// decodeNetromAlias decodes a six-byte alias, which ends at the first space
// (as netromd reads it) or NUL.
func decodeNetromAlias(b []byte) (string, error) {
	if len(b) < netromAliasLen {
		return "", fmt.Errorf("%w: alias of %d bytes", errNetromShort, len(b))
	}

	var alias = string(b[:netromAliasLen])
	if i := strings.IndexAny(alias, " \x00"); i >= 0 {
		alias = alias[:i]
	}

	if !validNetromAlias(alias) {
		return "", fmt.Errorf("invalid alias %q", alias)
	}

	return alias, nil
}
