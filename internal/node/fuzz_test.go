// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
)

// Anyone on frequency can connect to the node and send it whatever they like,
// so what the shell makes of its input is fuzzed like the packet decoders.
// Seeds added here run as ordinary unit tests under "go test".

// FuzzNode hands a connected session two frames of data, split anywhere, and
// checks that every reply is headed as the node's should be, and that the
// node hangs up on the user at most once.
func FuzzNode(f *testing.F) {
	f.Add([]byte("BYE\r"), []byte(""))
	f.Add([]byte("XYZZY\r\nBY"), []byte("E\r"))
	f.Add([]byte("\r\n\r\n"), []byte("\x00\xff\r"))
	f.Add([]byte(strings.Repeat("X", maxLineLen+1)), []byte("\rBYE\r"))

	// BYE in each frame, which once asked for a second disconnect.
	f.Add([]byte("B\r"), []byte("B\r"))

	f.Fuzz(func(t *testing.T, first []byte, second []byte) {
		var n, link = connected(t)

		n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, first)
		n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, second)

		for _, s := range link.sent {
			if !strings.HasPrefix(s.data, n.prompt()) {
				t.Errorf("reply %q does not start with %q", s.data, n.prompt())
			}
		}

		var hangUps = 0

		for _, call := range link.disconnects {
			if call == userCall {
				hangUps++
			}
		}

		if hangUps > 1 {
			t.Errorf("hung up on the user %d times", hangUps)
		}
	})
}
