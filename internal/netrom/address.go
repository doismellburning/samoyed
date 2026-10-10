// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package netrom implements NET/ROM: the NODES broadcasts that build a routing
// table, the network layer (L3) that carries packets between nodes, and the
// transport layer (L4) circuits that carry a user's session end to end.
//
// Nothing here touches a radio.  A Router is handed what arrives - a NODES
// broadcast, an L3 packet from a neighbour - and the time, and tells a Link
// what to send.  It is not safe for concurrent use: whoever owns one calls it
// from a single goroutine, as the AX.25 link layer is.
package netrom

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CallLen is the length of a callsign on the wire: six characters shifted
// left one bit, padded with spaces, then the SSID byte.
const CallLen = 7

// AliasLen is the length of a node alias (NET/ROM's "mnemonic") on the wire:
// plain ASCII, padded with spaces.
const AliasLen = 6

var errBadCall = errors.New("netrom: invalid callsign")

// encodeCall appends call, as "CALL" or "CALL-SSID", to b in its shifted
// on-the-wire form.  The SSID byte carries the two spare bits set and neither
// the command/response nor the end-of-address bit, as NET/ROM addresses do.
func encodeCall(b []byte, call string) ([]byte, error) {
	var base, ssid, err = splitCall(call)
	if err != nil {
		return b, err
	}

	for i := range 6 {
		var c = byte(' ')
		if i < len(base) {
			c = base[i]
		}

		b = append(b, c<<1)
	}

	return append(b, byte((0x60|(ssid<<1))&0xff)), nil
}

// decodeCall reads a shifted callsign from the first CallLen bytes of b,
// returning it as "CALL" or "CALL-SSID".
func decodeCall(b []byte) (string, error) {
	if len(b) < CallLen {
		return "", errBadCall
	}

	var sb strings.Builder

	var ended = false

	for i := range 6 {
		var c = b[i] >> 1
		if b[i]&1 != 0 {
			return "", errBadCall
		}

		if c == ' ' {
			ended = true

			continue
		}

		if ended || !isCallChar(c) {
			return "", errBadCall
		}

		sb.WriteByte(c)
	}

	if sb.Len() == 0 {
		return "", errBadCall
	}

	var ssid = int((b[6] >> 1) & 0x0f)
	if ssid != 0 {
		sb.WriteString("-")
		sb.WriteString(strconv.Itoa(ssid))
	}

	return sb.String(), nil
}

// NormaliseCall returns call upper-cased and with a "-0" SSID dropped, so that
// two spellings of the same station compare equal, or an error if it is not a
// callsign NET/ROM can carry.
func NormaliseCall(call string) (string, error) {
	var base, ssid, err = splitCall(call)
	if err != nil {
		return "", err
	}

	if ssid == 0 {
		return base, nil
	}

	return base + "-" + strconv.Itoa(ssid), nil
}

// splitCall breaks call into its upper-cased base and SSID.
func splitCall(call string) (string, int, error) {
	var base, ssidText, hasSSID = strings.Cut(strings.ToUpper(strings.TrimSpace(call)), "-")

	if len(base) == 0 || len(base) > 6 {
		return "", 0, fmt.Errorf("%w: %q", errBadCall, call)
	}

	for i := range len(base) {
		if !isCallChar(base[i]) {
			return "", 0, fmt.Errorf("%w: %q", errBadCall, call)
		}
	}

	var ssid = 0

	if hasSSID {
		var n, err = strconv.Atoi(ssidText)
		if err != nil || n < 0 || n > 15 {
			return "", 0, fmt.Errorf("%w: %q", errBadCall, call)
		}

		ssid = n
	}

	return base, ssid, nil
}

func isCallChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// validAlias says whether alias can be a node alias: up to six characters,
// from the set NET/ROM implementations accept.  The empty alias is allowed,
// for a node that has none.
func validAlias(alias string) bool {
	if len(alias) > AliasLen {
		return false
	}

	for i := range len(alias) {
		var c = alias[i]
		if !isCallChar(c) && (c < 'a' || c > 'z') && !strings.ContainsRune("#_&-/", rune(c)) {
			return false
		}
	}

	return true
}

// NormaliseAlias returns alias upper-cased, or an error if it cannot be a
// node alias.
func NormaliseAlias(alias string) (string, error) {
	var a = strings.ToUpper(strings.TrimSpace(alias))
	if !validAlias(a) {
		return "", fmt.Errorf("netrom: invalid alias %q", alias)
	}

	return a, nil
}

func encodeAlias(b []byte, alias string) []byte {
	for i := range AliasLen {
		var c = byte(' ')
		if i < len(alias) {
			c = alias[i]
		}

		b = append(b, c)
	}

	return b
}

func decodeAlias(b []byte) (string, bool) {
	var alias = strings.TrimRight(string(b[:AliasLen]), " \x00")

	return alias, validAlias(alias)
}
