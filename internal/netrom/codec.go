// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

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
// docs/source/glossary/netrom.rst has the references.

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	callLen            = 7  // An AX.25 address as it appears in an address field.
	AliasLen           = 6  // Space padded.
	networkHeaderLen   = 15 // Origin, destination, TTL.
	transportHeaderLen = 5
	headerLen          = networkHeaderLen + transportHeaderLen
	ttlOffset          = 14 // Of the time to live, in the network header.

	nodesSignature  = 0xFF
	nodesHeaderLen  = 1 + AliasLen
	nodesEntryLen   = callLen + AliasLen + callLen + 1
	nodesMaxEntries = 11 // What fits in a 256-byte information field.
	NodesCallsign   = "NODES"

	// The BPQ extensions: a CONNECT REQUEST may carry the originator's
	// transport timeout, and a CONNECT ACKNOWLEDGE the answering node's TTL.
	connReqLen      = headerLen + 1 + callLen + callLen
	connReqBPQLen   = connReqLen + 2
	connAckLen      = headerLen + 1
	connAckBPQLen   = connAckLen + 1
	ssidSpareBits   = 0x60 // The two reserved bits of an SSID byte, set as AX.25 sets them.
	maxSSID         = 15
	maxCallsignBase = 6
)

// Opcode is the low nibble of the fifth transport header byte.
type Opcode byte

const (
	OpProtoExt Opcode = 0 // Protocol extension, e.g. IP over NET/ROM; not handled.
	OpConnReq  Opcode = 1
	OpConnAck  Opcode = 2
	OpDiscReq  Opcode = 3
	OpDiscAck  Opcode = 4
	OpInfo     Opcode = 5
	OpInfoAck  Opcode = 6

	opcodeMask = 0x0F
)

func (op Opcode) String() string {
	switch op {
	case OpProtoExt:
		return "PROTOEXT"
	case OpConnReq:
		return "CONN REQ"
	case OpConnAck:
		return "CONN ACK"
	case OpDiscReq:
		return "DISC REQ"
	case OpDiscAck:
		return "DISC ACK"
	case OpInfo:
		return "INFO"
	case OpInfoAck:
		return "INFO ACK"
	}

	return "opcode " + strconv.Itoa(int(op))
}

// The flags in the high nibble of the fifth transport header byte.
const (
	FlagChoke byte = 0x80 // The sender cannot take any more; on a CONNECT ACK, a refusal.
	FlagNAK   byte = 0x40 // Please resend from the acknowledged sequence number.
	FlagMore  byte = 0x20 // More of this message follows in the next INFO frame.

	flagsMask byte = 0xF0
)

var errShort = errors.New("NET/ROM frame too short")

// Frame is a layer 3 frame: the network header, the transport header,
// and whatever the opcode carries.
//
// The four circuit bytes are kept as they appear on the air, because what they
// mean depends on the opcode.  index and id always name a circuit: on a
// CONNECT REQUEST the sender's own, on everything else the recipient's.  On
// INFO and INFO ACK, txSeq and rxSeq are the send and receive sequence
// numbers; on a CONNECT ACK they are the answering end's circuit index and ID.
type Frame struct {
	origin      string
	destination string
	ttl         byte

	index  byte
	id     byte
	txSeq  byte
	rxSeq  byte
	opcode Opcode
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
func (f *Frame) peerIndex() byte { return f.txSeq }
func (f *Frame) peerID() byte    { return f.rxSeq }

// has reports whether flag is set.
func (f *Frame) has(flag byte) bool { return f.flags&flag != 0 }

// DecodeFrame decodes a layer 3 frame from the information field of an
// AX.25 frame with PID 0xCF.
func DecodeFrame(b []byte) (*Frame, error) {
	if len(b) < headerLen {
		return nil, fmt.Errorf("%w: %d bytes, a header needs %d", errShort, len(b), headerLen)
	}

	var f = new(Frame)

	var err error

	f.origin, err = decodeCall(b[0:7])
	if err != nil {
		return nil, fmt.Errorf("origin: %w", err)
	}

	f.destination, err = decodeCall(b[7:14])
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}

	f.ttl = b[14]
	f.index = b[15]
	f.id = b[16]
	f.txSeq = b[17]
	f.rxSeq = b[18]
	f.opcode = Opcode(b[19] & opcodeMask)
	f.flags = b[19] & flagsMask

	var rest = b[headerLen:]

	switch f.opcode {
	case OpConnReq:
		if len(b) < connReqLen {
			return nil, fmt.Errorf("%w: CONNECT REQUEST of %d bytes, needs %d", errShort, len(b), connReqLen)
		}

		f.window = rest[0]

		f.user, err = decodeCall(rest[1:8])
		if err != nil {
			return nil, fmt.Errorf("user: %w", err)
		}

		f.originNode, err = decodeCall(rest[8:15])
		if err != nil {
			return nil, fmt.Errorf("originating node: %w", err)
		}

		if len(b) >= connReqBPQLen {
			f.timeout = maybe.Just(int(rest[15]) | int(rest[16])<<8)
		}

	case OpConnAck:
		// A refusal need not carry a window, but an acceptance must.
		if len(rest) == 0 {
			if !f.has(FlagChoke) {
				return nil, fmt.Errorf("%w: CONNECT ACK with no window", errShort)
			}

			break
		}

		f.window = rest[0]

		if len(b) >= connAckBPQLen {
			f.ackTTL = maybe.Just(rest[1])
		}

	case OpInfo, OpProtoExt:
		f.info = append([]byte(nil), rest...)

	case OpDiscReq, OpDiscAck, OpInfoAck:
		// Nothing more.

	default:
		return nil, fmt.Errorf("unknown NET/ROM opcode %d", f.opcode)
	}

	return f, nil
}

// encode renders the frame as the information field of an AX.25 frame.
func (f *Frame) encode() ([]byte, error) {
	var b = make([]byte, 0, headerLen+callLen*2+2+len(f.info))

	var err error

	b, err = appendCall(b, f.origin)
	if err != nil {
		return nil, fmt.Errorf("origin: %w", err)
	}

	b, err = appendCall(b, f.destination)
	if err != nil {
		return nil, fmt.Errorf("destination: %w", err)
	}

	b = append(b, f.ttl, f.index, f.id, f.txSeq, f.rxSeq, byte(f.opcode)&opcodeMask|f.flags&flagsMask)

	switch f.opcode {
	case OpConnReq:
		b = append(b, f.window)

		b, err = appendCall(b, f.user)
		if err != nil {
			return nil, fmt.Errorf("user: %w", err)
		}

		b, err = appendCall(b, f.originNode)
		if err != nil {
			return nil, fmt.Errorf("originating node: %w", err)
		}

		if timeout, ok := f.timeout.Get(); ok {
			if timeout < 0 || timeout > 0xFFFF {
				return nil, fmt.Errorf("transport timeout %d does not fit in two bytes", timeout)
			}

			b = append(b, byte(timeout&0xFF), byte((timeout>>8)&0xFF))
		}

	case OpConnAck:
		b = append(b, f.window)

		if ttl, ok := f.ackTTL.Get(); ok {
			b = append(b, ttl)
		}

	case OpInfo, OpProtoExt:
		b = append(b, f.info...)

	case OpDiscReq, OpDiscAck, OpInfoAck:
		// Nothing more.

	default:
		return nil, fmt.Errorf("unknown NET/ROM opcode %d", f.opcode)
	}

	return b, nil
}

// NodesEntry is one destination advertised in a NODES broadcast.
type NodesEntry struct {
	callsign  string
	alias     string
	neighbour string // The best neighbour the sender reaches it by.
	quality   byte
}

// Nodes is a decoded NODES broadcast.
type Nodes struct {
	alias   string // The sending node's; its callsign is the AX.25 source.
	entries []NodesEntry
}

// DecodeNodes decodes the information field of a NODES broadcast.  Trailing
// bytes too few for a whole entry are ignored, and an entry with an invalid
// callsign or alias is skipped, where netromd abandons the rest of the
// broadcast.
func DecodeNodes(b []byte) (*Nodes, error) {
	if len(b) < nodesHeaderLen {
		return nil, fmt.Errorf("%w: NODES broadcast of %d bytes", errShort, len(b))
	}

	if b[0] != nodesSignature {
		return nil, fmt.Errorf("NODES broadcast signature is 0x%02x, not 0x%02x", b[0], nodesSignature)
	}

	var n = new(Nodes)

	var err error

	n.alias, err = decodeAlias(b[1:7])
	if err != nil {
		return nil, fmt.Errorf("sender alias: %w", err)
	}

	for rest := b[nodesHeaderLen:]; len(rest) >= nodesEntryLen; rest = rest[nodesEntryLen:] {
		var e NodesEntry

		var callErr, aliasErr, neighbourErr error

		e.callsign, callErr = decodeCall(rest[0:7])
		e.alias, aliasErr = decodeAlias(rest[7:13])
		e.neighbour, neighbourErr = decodeCall(rest[13:20])
		e.quality = rest[20]

		if callErr != nil || aliasErr != nil || neighbourErr != nil {
			continue
		}

		n.entries = append(n.entries, e)
	}

	return n, nil
}

// EncodeNodes builds the information fields of a NODES broadcast from
// alias and entries, at most nodesMaxEntries to a frame.  There is
// always at least one, even with no entries: the header alone is how a node
// tells its neighbours it is there.
func EncodeNodes(alias string, entries []NodesEntry) ([][]byte, error) {
	var header = []byte{nodesSignature}

	var err error

	header, err = appendAlias(header, alias)
	if err != nil {
		return nil, fmt.Errorf("sender alias: %w", err)
	}

	var frames [][]byte

	for {
		var frame = append([]byte(nil), header...)

		var n = min(len(entries), nodesMaxEntries)

		for _, e := range entries[:n] {
			frame, err = appendCall(frame, e.callsign)
			if err != nil {
				return nil, fmt.Errorf("entry callsign: %w", err)
			}

			frame, err = appendAlias(frame, e.alias)
			if err != nil {
				return nil, fmt.Errorf("entry alias: %w", err)
			}

			frame, err = appendCall(frame, e.neighbour)
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

// parseCallsign parses "CALL" or "CALL-SSID" into its base and SSID,
// uppercasing it.  The base is one to six letters and digits and the SSID
// 0 to 15, as an AX.25 address field can hold.
func parseCallsign(s string) (string, int, error) {
	var base, ssidText, hasSSID = strings.Cut(strings.ToUpper(strings.TrimSpace(s)), "-")

	if base == "" || len(base) > maxCallsignBase {
		return "", 0, fmt.Errorf("callsign %q must be 1 to %d characters before any SSID", s, maxCallsignBase)
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
		if err != nil || ssid < 0 || ssid > maxSSID {
			return "", 0, fmt.Errorf("callsign %q has an SSID outside 0-%d", s, maxSSID)
		}
	}

	return base, ssid, nil
}

// NormaliseCallsign returns s as decodeCall would render it:
// uppercase, and with no SSID suffix when the SSID is 0.
func NormaliseCallsign(s string) (string, error) {
	var base, ssid, err = parseCallsign(s)
	if err != nil {
		return "", err
	}

	if ssid == 0 {
		return base, nil
	}

	return base + "-" + strconv.Itoa(ssid), nil
}

// appendCall appends callsign as a seven-byte AX.25 address: six
// characters shifted left one bit and space padded, then the SSID byte, with
// neither the C nor the end-of-address bit set.
func appendCall(b []byte, callsign string) ([]byte, error) {
	var base, ssid, err = parseCallsign(callsign)
	if err != nil {
		return b, err
	}

	for i := range maxCallsignBase {
		var c = byte(' ')
		if i < len(base) {
			c = base[i]
		}

		b = append(b, c<<1)
	}

	return append(b, ssidSpareBits|byte(ssid&0x0F)<<1), nil
}

// decodeCall decodes a seven-byte AX.25 address: uppercase letters and
// digits, then nothing but spaces.
func decodeCall(b []byte) (string, error) {
	if len(b) < callLen {
		return "", fmt.Errorf("%w: address of %d bytes", errShort, len(b))
	}

	var sb strings.Builder

	var ended = false

	for _, raw := range b[:maxCallsignBase] {
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

// aliasChars is what an alias may contain: letters, digits and a few
// punctuation marks.
const aliasChars = "#_&-/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// ValidAlias reports whether alias is at most six of aliasChars.  An empty
// alias is allowed; some nodes have none.
func ValidAlias(alias string) bool {
	if len(alias) > AliasLen {
		return false
	}

	for _, c := range alias {
		if !strings.ContainsRune(aliasChars, c) {
			return false
		}
	}

	return true
}

// appendAlias appends alias space padded to six bytes.
func appendAlias(b []byte, alias string) ([]byte, error) {
	if !ValidAlias(alias) {
		return b, fmt.Errorf("alias %q must be at most %d of %q", alias, AliasLen, aliasChars)
	}

	b = append(b, alias...)

	for range AliasLen - len(alias) {
		b = append(b, ' ')
	}

	return b, nil
}

// decodeAlias decodes a six-byte alias, which ends at the first space or
// NUL.
func decodeAlias(b []byte) (string, error) {
	if len(b) < AliasLen {
		return "", fmt.Errorf("%w: alias of %d bytes", errShort, len(b))
	}

	var alias = string(b[:AliasLen])
	if i := strings.IndexAny(alias, " \x00"); i >= 0 {
		alias = alias[:i]
	}

	if !ValidAlias(alias) {
		return "", fmt.Errorf("invalid alias %q", alias)
	}

	return alias, nil
}
