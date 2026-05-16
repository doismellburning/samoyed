// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"errors"
	"fmt"
	"strings"
)

// NET/ROM protocol constants.
const (
	// NETROM_BROADCAST_CALLSIGN is the AX.25 destination for NODES broadcasts.
	NETROM_BROADCAST_CALLSIGN = "NODES"

	// NETROM_TTL_DEFAULT is the default time-to-live for NET/ROM transport frames.
	NETROM_TTL_DEFAULT byte = 7

	// NETROM_WINDOW_DEFAULT is the default transport window size.
	NETROM_WINDOW_DEFAULT byte = 4

	// NETROM_MAX_INFO is the maximum NET/ROM transport payload size in bytes.
	NETROM_MAX_INFO = 236

	netromOpcodeConnect    byte = 0x01
	netromOpcodeConnAck    byte = 0x02
	netromOpcodeDisconnect byte = 0x03
	netromOpcodeDiscAck    byte = 0x04
	netromOpcodeInfo       byte = 0x05
	netromOpcodeInfoAck    byte = 0x06

	netromFlagChoke  byte = 0x80
	netromFlagNAK    byte = 0x40
	netromFlagMore   byte = 0x20
	netromOpcodeMask byte = 0x0f

	netromAddrLen       = 7  // AX.25-encoded callsign length.
	netromAliasLen      = 6  // Plain-ASCII alias length.
	netromNetHdrLen     = 15 // dst(7) + src(7) + ttl(1).
	netromTransHdrLen   = 5  // cktIdx + cktID + txSeq + rxSeq + opcode.
	netromConnReqExtra  = 28 // origIdx(1) + origID(1) + origCall(7) + origAlias(6) + dstCall(7) + dstAlias(6); window is optional.
	netromConnAckExtra  = 3  // acceptIdx + acceptID + window.
	netromNodesEntryLen = 21 // dstCall(7) + dstAlias(6) + neighbor(7) + quality(1).
)

// netromNetHeader is the NET/ROM network-layer header present in all transport frames.
type netromNetHeader struct {
	dst string
	src string
	ttl byte
}

// netromTransportFrame holds a decoded NET/ROM transport frame.
type netromTransportFrame struct {
	net    netromNetHeader
	cktIdx byte
	cktID  byte
	txSeq  byte
	rxSeq  byte
	opcode byte
	flags  byte

	// CONNECT REQUEST fields.
	origIdx      byte
	origID       byte
	origCallsign string
	origAlias    string
	dstCallsign  string
	dstAlias     string
	windowSize   byte

	// CONNECT ACK fields.
	acceptIdx byte
	acceptID  byte

	// INFORMATION payload.
	info []byte
}

// netromNodesEntry is a single entry in a NODES routing broadcast.
type netromNodesEntry struct {
	dstCallsign string
	dstAlias    [netromAliasLen]byte
	neighbor    string
	quality     byte
}

// netromRoutingBroadcast holds a decoded NODES broadcast.
type netromRoutingBroadcast struct {
	srcAlias [netromAliasLen]byte
	entries  []netromNodesEntry
}

// netromEncodeAddr encodes a callsign string into the 7-byte AX.25-format address
// used inside NET/ROM network headers.
func netromEncodeAddr(callsign string) [netromAddrLen]byte {
	var result [netromAddrLen]byte

	var ssid = 0
	var call = callsign
	if before, after, ok := strings.Cut(callsign, "-"); ok {
		fmt.Sscanf(after, "%d", &ssid)
		call = before
	}

	for i := range netromAliasLen {
		var ch byte = ' '
		if i < len(call) {
			ch = call[i]
		}
		result[i] = ch << 1
	}
	// SSID byte: RR bits 0x60 always set; SSID in bits 1–4.
	result[6] = 0x60 | byte((ssid&0x0f)<<1)

	return result
}

// netromDecodeAddr decodes a 7-byte AX.25-format address into a callsign string.
func netromDecodeAddr(b [netromAddrLen]byte) string {
	var callBytes [netromAliasLen]byte
	for i := range netromAliasLen {
		callBytes[i] = (b[i] >> 1) & 0x7f
	}
	var call = strings.TrimRight(string(callBytes[:]), " ")
	var ssid = int((b[6] & 0x1e) >> 1)
	if ssid != 0 {
		return fmt.Sprintf("%s-%d", call, ssid)
	}
	return call
}

// netromDecodeAddrAt decodes a 7-byte AX.25-format address from data at offset.
func netromDecodeAddrAt(data []byte, offset int) string {
	var b [netromAddrLen]byte
	copy(b[:], data[offset:offset+netromAddrLen])
	return netromDecodeAddr(b)
}

// netromEncodeAddrAt encodes a callsign into data[offset : offset+7].
func netromEncodeAddrAt(data []byte, offset int, callsign string) {
	var b = netromEncodeAddr(callsign)
	copy(data[offset:], b[:])
}

// netromPadAlias pads or truncates an alias string to exactly netromAliasLen bytes.
func netromPadAlias(alias string) [netromAliasLen]byte {
	var result [netromAliasLen]byte
	for i := range netromAliasLen {
		result[i] = ' '
	}
	for i, ch := range alias {
		if i >= netromAliasLen {
			break
		}
		result[i] = byte(ch)
	}
	return result
}

// netromParseRoutingBroadcast decodes a NODES broadcast from the AX.25 info field.
// The data starts with the 6-byte source alias (plain ASCII, not AX.25-encoded).
func netromParseRoutingBroadcast(data []byte) (*netromRoutingBroadcast, error) {
	if len(data) < netromAliasLen {
		return nil, errors.New("NET/ROM NODES broadcast too short for alias")
	}

	var bc = new(netromRoutingBroadcast)
	copy(bc.srcAlias[:], data[:netromAliasLen])

	var pos = netromAliasLen
	for pos+netromNodesEntryLen <= len(data) {
		var entry netromNodesEntry
		entry.dstCallsign = netromDecodeAddrAt(data, pos)
		copy(entry.dstAlias[:], data[pos+netromAddrLen:pos+netromAddrLen+netromAliasLen])
		entry.neighbor = netromDecodeAddrAt(data, pos+netromAddrLen+netromAliasLen)
		entry.quality = data[pos+netromAddrLen+netromAliasLen+netromAddrLen]
		bc.entries = append(bc.entries, entry)
		pos += netromNodesEntryLen
	}

	return bc, nil
}

// netromBuildRoutingBroadcast encodes a NODES broadcast payload for the AX.25 info field.
func netromBuildRoutingBroadcast(srcAlias [netromAliasLen]byte, entries []netromNodesEntry) []byte {
	var buf = make([]byte, netromAliasLen+len(entries)*netromNodesEntryLen)
	copy(buf[:netromAliasLen], srcAlias[:])
	var pos = netromAliasLen
	for _, e := range entries {
		netromEncodeAddrAt(buf, pos, e.dstCallsign)
		copy(buf[pos+netromAddrLen:], e.dstAlias[:])
		netromEncodeAddrAt(buf, pos+netromAddrLen+netromAliasLen, e.neighbor)
		buf[pos+netromAddrLen+netromAliasLen+netromAddrLen] = e.quality
		pos += netromNodesEntryLen
	}
	return buf
}

// netromBuildNetHeader encodes the 15-byte NET/ROM network header.
func netromBuildNetHeader(dst, src string, ttl byte) []byte {
	var buf = make([]byte, netromNetHdrLen)
	netromEncodeAddrAt(buf, 0, dst)
	netromEncodeAddrAt(buf, netromAddrLen, src)
	buf[netromAddrLen*2] = ttl
	return buf
}

// netromParseTransportFrame decodes a NET/ROM transport frame from an AX.25 info field.
func netromParseTransportFrame(data []byte) (*netromTransportFrame, error) {
	if len(data) < netromNetHdrLen+netromTransHdrLen {
		return nil, errors.New("NET/ROM transport frame too short for headers")
	}

	var f = new(netromTransportFrame)
	f.net.dst = netromDecodeAddrAt(data, 0)
	f.net.src = netromDecodeAddrAt(data, netromAddrLen)
	f.net.ttl = data[netromAddrLen*2]

	var pos = netromNetHdrLen
	f.cktIdx = data[pos]
	f.cktID = data[pos+1]
	f.txSeq = data[pos+2]
	f.rxSeq = data[pos+3]
	var opcodeByte = data[pos+4]
	f.flags = opcodeByte & ^netromOpcodeMask
	f.opcode = opcodeByte & netromOpcodeMask
	pos += netromTransHdrLen

	switch f.opcode {
	case netromOpcodeConnect:
		if len(data) < pos+netromConnReqExtra {
			return nil, errors.New("NET/ROM CONNECT REQUEST frame too short for extra fields")
		}
		f.origIdx = data[pos]
		f.origID = data[pos+1]
		f.origCallsign = netromDecodeAddrAt(data, pos+2)
		var origAliasBytes [netromAliasLen]byte
		copy(origAliasBytes[:], data[pos+2+netromAddrLen:])
		f.origAlias = strings.TrimRight(string(origAliasBytes[:]), " ")
		f.dstCallsign = netromDecodeAddrAt(data, pos+2+netromAddrLen+netromAliasLen)
		var dstAliasBytes [netromAliasLen]byte
		copy(dstAliasBytes[:], data[pos+2+netromAddrLen+netromAliasLen+netromAddrLen:])
		f.dstAlias = strings.TrimRight(string(dstAliasBytes[:]), " ")
		if len(data) > pos+netromConnReqExtra {
			f.windowSize = data[pos+netromConnReqExtra]
		}

	case netromOpcodeConnAck:
		if len(data) < pos+netromConnAckExtra {
			return nil, errors.New("NET/ROM CONNECT ACK frame too short for extra fields")
		}
		f.acceptIdx = data[pos]
		f.acceptID = data[pos+1]
		f.windowSize = data[pos+2]

	case netromOpcodeInfo:
		f.info = make([]byte, len(data)-pos)
		copy(f.info, data[pos:])

	case netromOpcodeDisconnect, netromOpcodeDiscAck, netromOpcodeInfoAck:
		// Transport header only; no additional payload.

	default:
		return nil, fmt.Errorf("NET/ROM unknown opcode 0x%02x", f.opcode)
	}

	return f, nil
}

// netromBuildConnect builds the payload for a CONNECT REQUEST frame.
func netromBuildConnect(dst, src string, ttl, cktIdx, cktID, origIdx, origID byte, origCall, origAlias, dstCall, dstAlias string, window byte) []byte {
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, 0, 0, netromOpcodeConnect)
	buf = append(buf, origIdx, origID)
	var origAddr = netromEncodeAddr(origCall)
	buf = append(buf, origAddr[:]...)
	var oAlias = netromPadAlias(origAlias)
	buf = append(buf, oAlias[:]...)
	var dstAddr = netromEncodeAddr(dstCall)
	buf = append(buf, dstAddr[:]...)
	var dAlias = netromPadAlias(dstAlias)
	buf = append(buf, dAlias[:]...)
	buf = append(buf, window)
	return buf
}

// netromBuildConnAck builds the payload for a CONNECT ACK frame.
func netromBuildConnAck(dst, src string, ttl, cktIdx, cktID, acceptIdx, acceptID, window byte, choke bool) []byte {
	var flags byte
	if choke {
		flags = netromFlagChoke
	}
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, 0, 0, flags|netromOpcodeConnAck)
	buf = append(buf, acceptIdx, acceptID, window)
	return buf
}

// netromBuildDisconnect builds the payload for a DISCONNECT REQUEST frame.
func netromBuildDisconnect(dst, src string, ttl, cktIdx, cktID, rxSeq byte) []byte {
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, 0, rxSeq, netromOpcodeDisconnect)
	return buf
}

// netromBuildDiscAck builds the payload for a DISCONNECT ACK frame.
func netromBuildDiscAck(dst, src string, ttl, cktIdx, cktID byte) []byte {
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, 0, 0, netromOpcodeDiscAck)
	return buf
}

// netromBuildInfo builds the payload for an INFORMATION frame.
func netromBuildInfo(dst, src string, ttl, cktIdx, cktID, txSeq, rxSeq byte, choke, nak, more bool, info []byte) []byte {
	var flags byte
	if choke {
		flags |= netromFlagChoke
	}
	if nak {
		flags |= netromFlagNAK
	}
	if more {
		flags |= netromFlagMore
	}
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, txSeq, rxSeq, flags|netromOpcodeInfo)
	buf = append(buf, info...)
	return buf
}

// netromBuildInfoAck builds the payload for an INFORMATION ACK frame.
func netromBuildInfoAck(dst, src string, ttl, cktIdx, cktID, rxSeq byte, choke, nak bool) []byte {
	var flags byte
	if choke {
		flags |= netromFlagChoke
	}
	if nak {
		flags |= netromFlagNAK
	}
	var buf = netromBuildNetHeader(dst, src, ttl)
	buf = append(buf, cktIdx, cktID, 0, rxSeq, flags|netromOpcodeInfoAck)
	return buf
}
