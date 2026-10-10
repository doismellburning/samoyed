// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"errors"
	"fmt"
)

// NodesDestination is the destination callsign NODES broadcasts are sent to,
// as UI frames with the NET/ROM PID.
const NodesDestination = "NODES"

// nodesSignature starts the information field of every NODES broadcast.
const nodesSignature = 0xff

// nodesEntryLen is one destination in a NODES broadcast: its callsign, its
// alias, the sender's best neighbour towards it and the quality of that route.
const nodesEntryLen = CallLen + AliasLen + CallLen + 1

// nodesMaxLen is the most a NODES broadcast's information field holds, so that
// it fits a 256-byte AX.25 information field: the header and eleven entries.
const nodesMaxLen = 256

// NodesEntry is one destination a NODES broadcast advertises.
type NodesEntry struct {
	Call          string // The destination node.
	Alias         string // Its alias, or "" for none.
	BestNeighbour string // The sender's best neighbour towards it.
	Quality       int    // The quality of the sender's best route, 0 to 255.
}

// NodesBroadcast is a decoded NODES broadcast.
type NodesBroadcast struct {
	Alias   string // The sending node's own alias.
	Entries []NodesEntry
}

var errNotNodes = errors.New("netrom: not a NODES broadcast")

// DecodeNodes decodes the information field of a NODES broadcast.  Entries that
// do not decode - a callsign or alias NET/ROM cannot carry - are skipped rather
// than taken as reason to throw the rest away, as is anything after the last
// whole entry: INP3 nodes append extensions there.
func DecodeNodes(info []byte) (NodesBroadcast, error) {
	var nb NodesBroadcast

	if len(info) < 1+AliasLen || info[0] != nodesSignature {
		return nb, errNotNodes
	}

	var alias, ok = decodeAlias(info[1:])
	if !ok {
		return nb, fmt.Errorf("%w: invalid alias", errNotNodes)
	}

	nb.Alias = alias

	for rest := info[1+AliasLen:]; len(rest) >= nodesEntryLen; rest = rest[nodesEntryLen:] {
		var e, err = decodeNodesEntry(rest[:nodesEntryLen])
		if err == nil {
			nb.Entries = append(nb.Entries, e)
		}
	}

	return nb, nil
}

func decodeNodesEntry(b []byte) (NodesEntry, error) {
	var e NodesEntry

	var call, err = decodeCall(b)
	if err != nil {
		return e, err
	}

	var alias, ok = decodeAlias(b[CallLen:])
	if !ok {
		return e, fmt.Errorf("netrom: invalid alias for %s", call)
	}

	var neighbour, nerr = decodeCall(b[CallLen+AliasLen:])
	if nerr != nil {
		return e, nerr
	}

	e.Call = call
	e.Alias = alias
	e.BestNeighbour = neighbour
	e.Quality = int(b[CallLen+AliasLen+CallLen])

	return e, nil
}

// EncodeNodes encodes a NODES broadcast from a node with alias, advertising
// entries, as as many information fields as it takes.  A node with nothing to
// advertise still sends one, so its neighbours learn of the node itself.
func EncodeNodes(alias string, entries []NodesEntry) ([][]byte, error) {
	if !validAlias(alias) {
		return nil, fmt.Errorf("netrom: invalid alias %q", alias)
	}

	var header = encodeAlias([]byte{nodesSignature}, alias)

	var frames [][]byte

	var frame = append([]byte(nil), header...)

	for _, e := range entries {
		if e.Quality < 0 || e.Quality > 255 || !validAlias(e.Alias) {
			return nil, fmt.Errorf("netrom: invalid NODES entry for %s", e.Call)
		}

		if len(frame)+nodesEntryLen > nodesMaxLen {
			frames = append(frames, frame)
			frame = append([]byte(nil), header...)
		}

		var err error

		frame, err = encodeCall(frame, e.Call)
		if err != nil {
			return nil, err
		}

		frame = encodeAlias(frame, e.Alias)

		frame, err = encodeCall(frame, e.BestNeighbour)
		if err != nil {
			return nil, err
		}

		frame = append(frame, byte(e.Quality&0xff))
	}

	return append(frames, frame), nil
}
