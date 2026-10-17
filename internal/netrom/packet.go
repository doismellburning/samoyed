// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"errors"
	"fmt"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// L3HeaderLen is the network header: origin node, destination node, TTL.
const L3HeaderLen = 2*CallLen + 1

// L4HeaderLen is the transport header: two circuit bytes, two sequence bytes
// and the opcode with its flags.
const L4HeaderLen = 5

// MaxInfoLen is the most user data one information packet carries, so that the
// whole NET/ROM packet fits a 256-byte AX.25 information field.
const MaxInfoLen = 256 - L3HeaderLen - L4HeaderLen

// Opcode is the transport-layer operation a packet carries, the low four bits
// of the header's last byte.
type Opcode byte

const (
	OpProtocolExtension  Opcode = 0
	OpConnectRequest     Opcode = 1
	OpConnectAck         Opcode = 2
	OpDisconnectRequest  Opcode = 3
	OpDisconnectAck      Opcode = 4
	OpInfo               Opcode = 5
	OpInfoAck            Opcode = 6
	OpReset              Opcode = 7 // An extension some nodes understand.
	opcodeMask                  = 0x0f
	flagsMask                   = 0xf0
	connectRequestMinLen        = 1 + 2*CallLen
)

func (o Opcode) String() string {
	switch o {
	case OpProtocolExtension:
		return "PROTO-EXT"
	case OpConnectRequest:
		return "CONN-REQ"
	case OpConnectAck:
		return "CONN-ACK"
	case OpDisconnectRequest:
		return "DISC-REQ"
	case OpDisconnectAck:
		return "DISC-ACK"
	case OpInfo:
		return "INFO"
	case OpInfoAck:
		return "INFO-ACK"
	case OpReset:
		return "RESET"
	default:
		return fmt.Sprintf("OP-%d", byte(o))
	}
}

// The flags in the high four bits of the opcode byte.
const (
	FlagChoke byte = 0x80 // The sender cannot take any more just now; on a connect ack, a refusal.
	FlagNAK   byte = 0x40 // Resend the packet the receive sequence number names.
	FlagMore  byte = 0x20 // This information packet continues in the next.
)

// transport returns a transport packet with the header fields given, for
// Router.send to address.
func transport(index, id, txSeq, rxSeq byte, op Opcode, flags byte, payload []byte) Packet {
	return Packet{
		Origin:      "",
		Destination: "",
		TTL:         0,
		Index:       index,
		ID:          id,
		TxSeq:       txSeq,
		RxSeq:       rxSeq,
		Opcode:      op,
		Flags:       flags,
		Payload:     payload,
	}
}

// Packet is a NET/ROM network-layer packet and the transport header it
// carries.  What the four circuit and sequence bytes mean depends on the
// opcode, so they are named here by position, as they are on the wire.
type Packet struct {
	Origin      string // The node the packet started from.
	Destination string // The node it is going to.
	TTL         int    // Hops left before it is dropped.

	Index byte // The receiver's circuit index; for a connect request, the sender's.
	ID    byte // The receiver's circuit ID; for a connect request, the sender's.
	TxSeq byte // The send sequence number; for a connect ack, the sender's circuit index.
	RxSeq byte // The receive sequence number; for a connect ack, the sender's circuit ID.

	Opcode  Opcode
	Flags   byte // FlagChoke, FlagNAK, FlagMore.
	Payload []byte
}

var errShortPacket = errors.New("netrom: packet too short")

// DecodePacket decodes a NET/ROM packet, as carried in an AX.25 information
// frame with the NET/ROM PID.  Anything that is not an ordinary packet - an
// INP3 routing frame, which starts with a signature byte rather than a
// callsign, say - is an error.
func DecodePacket(b []byte) (Packet, error) {
	var p Packet

	if len(b) < L3HeaderLen+L4HeaderLen {
		return p, errShortPacket
	}

	var origin, err = decodeCall(b)
	if err != nil {
		return p, fmt.Errorf("netrom: origin: %w", err)
	}

	var destination, derr = decodeCall(b[CallLen:])
	if derr != nil {
		return p, fmt.Errorf("netrom: destination: %w", derr)
	}

	p.Origin = origin
	p.Destination = destination
	p.TTL = int(b[2*CallLen])

	var l4 = b[L3HeaderLen:]
	p.Index = l4[0]
	p.ID = l4[1]
	p.TxSeq = l4[2]
	p.RxSeq = l4[3]
	p.Opcode = Opcode(l4[4] & opcodeMask)
	p.Flags = l4[4] & flagsMask
	p.Payload = append([]byte(nil), l4[L4HeaderLen:]...)

	return p, nil
}

// Encode encodes p for an AX.25 information frame.
func (p Packet) Encode() ([]byte, error) {
	if p.TTL < 0 || p.TTL > 255 {
		return nil, fmt.Errorf("netrom: TTL %d out of range", p.TTL)
	}

	var b = make([]byte, 0, L3HeaderLen+L4HeaderLen+len(p.Payload))

	var err error

	b, err = encodeCall(b, p.Origin)
	if err != nil {
		return nil, err
	}

	b, err = encodeCall(b, p.Destination)
	if err != nil {
		return nil, err
	}

	b = append(b, byte(p.TTL&0xff), p.Index, p.ID, p.TxSeq, p.RxSeq, (byte(p.Opcode)&opcodeMask)|(p.Flags&flagsMask))

	return append(b, p.Payload...), nil
}

// ConnectRequest is what a connect request's payload carries.
type ConnectRequest struct {
	Window int    // The window the requester proposes.
	User   string // Who the circuit is for.
	Node   string // The node the user is on.

	// Timeout is the requester's transport timeout, in seconds, which BPQ and
	// Linux nodes send so the two ends can agree on the shorter.
	Timeout maybe.Maybe[int]
}

// DecodeConnectRequest decodes the payload of a connect request.
func DecodeConnectRequest(payload []byte) (ConnectRequest, error) {
	var cr ConnectRequest

	if len(payload) < connectRequestMinLen {
		return cr, errShortPacket
	}

	var user, err = decodeCall(payload[1:])
	if err != nil {
		return cr, fmt.Errorf("netrom: connect request user: %w", err)
	}

	var node, nerr = decodeCall(payload[1+CallLen:])
	if nerr != nil {
		return cr, fmt.Errorf("netrom: connect request node: %w", nerr)
	}

	cr.Window = int(payload[0])
	cr.User = user
	cr.Node = node

	if len(payload) >= connectRequestMinLen+2 {
		cr.Timeout = maybe.Just(int(payload[connectRequestMinLen]) | int(payload[connectRequestMinLen+1])<<8)
	}

	return cr, nil
}

// Encode encodes cr as a connect request's payload.
func (cr ConnectRequest) Encode() ([]byte, error) {
	if cr.Window < 1 || cr.Window > 127 {
		return nil, fmt.Errorf("netrom: window %d out of range", cr.Window)
	}

	var b = []byte{byte(cr.Window & 0x7f)}

	var err error

	b, err = encodeCall(b, cr.User)
	if err != nil {
		return nil, err
	}

	b, err = encodeCall(b, cr.Node)
	if err != nil {
		return nil, err
	}

	if timeout, ok := cr.Timeout.Get(); ok {
		if timeout < 0 || timeout > 0xffff {
			return nil, fmt.Errorf("netrom: timeout %d out of range", timeout)
		}

		b = append(b, byte(timeout&0xff), byte((timeout>>8)&0xff))
	}

	return b, nil
}
