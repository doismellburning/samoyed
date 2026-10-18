// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzDecode covers the NET/ROM decoders: a NODES broadcast is a UI
// frame anyone can send, and a layer 3 frame arrives from any neighbour that
// connects.  What decodes must also encode again to the same thing, since a
// node forwards what it receives.
func FuzzDecode(f *testing.F) {
	var connReq = new(Frame)
	connReq.origin = "Q1TEST-7"
	connReq.destination = "Q2TEST-7"
	connReq.ttl = 16
	connReq.index = 1
	connReq.id = 2
	connReq.opcode = OpConnReq
	connReq.window = 4
	connReq.user = "Q1TEST"
	connReq.originNode = "Q1TEST-7"

	var seed, err = connReq.encode()
	require.NoError(f, err)
	f.Add(seed)

	var nodes, nodesErr = EncodeNodes("QNODEA", []NodesEntry{
		{callsign: "Q2TEST-7", alias: "QNODEB", neighbour: "Q2TEST-7", quality: 200},
	})
	require.NoError(f, nodesErr)
	f.Add(nodes[0])

	f.Fuzz(func(t *testing.T, data []byte) {
		var frame, frameErr = DecodeFrame(data)
		if frameErr == nil {
			var encoded, encodeErr = frame.encode()
			require.NoError(t, encodeErr)

			var again, againErr = DecodeFrame(encoded)
			require.NoError(t, againErr)
			require.Equal(t, frame, again)
		}

		var n, nodesErr = DecodeNodes(data)
		if nodesErr == nil {
			var frames, encodeErr = EncodeNodes(n.alias, n.entries)
			require.NoError(t, encodeErr)

			var entries []NodesEntry

			for _, frame := range frames {
				var again, againErr = DecodeNodes(frame)
				require.NoError(t, againErr)
				require.Equal(t, n.alias, again.alias)

				entries = append(entries, again.entries...)
			}

			require.Equal(t, n.entries, entries)
		}
	})
}
