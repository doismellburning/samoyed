// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package kiss

import (
	"bytes"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
)

// FuzzUnwrap covers the KISS framing, which any client on the KISS TCP
// port or the serial KISS device can feed.
func FuzzUnwrap(f *testing.F) {
	testutils.DiscardLogrus(f)

	f.Add([]byte{0x00, 'h', 'e', 'l', 'l', 'o', FEND})
	f.Add([]byte{0x00, FESC, TFEND, FESC, TFESC, FEND})
	f.Add([]byte{FEND})

	f.Fuzz(func(t *testing.T, in []byte) {
		Unwrap(in)
	})
}

// FuzzCollector covers splitting a stream into frames, which anything on the
// far end of a KISS connection controls.  Whatever came before, a FEND and
// then a frame that fits gets that frame through intact.
func FuzzCollector(f *testing.F) {
	f.Add([]byte("junk\r"), []byte{CmdDataFrame, 'h', 'i'})
	f.Add([]byte{FEND, 'x', FESC}, []byte{CmdDataFrame, FEND, FESC})
	f.Add(bytes.Repeat([]byte{'x'}, MaxFrameLen+1), []byte{CmdTxDelay, 30})

	f.Fuzz(func(t *testing.T, before []byte, payload []byte) {
		var frame = Encapsulate(payload)
		if len(payload) == 0 || len(frame) > MaxFrameLen {
			t.Skip()
		}

		var c Collector

		for _, b := range before {
			c.Add(b)
		}

		c.Add(FEND)

		var last []byte

		for _, b := range frame {
			if chunk := c.Add(b); chunk.Frame != nil {
				last = bytes.Clone(chunk.Frame)
			}
		}

		if !bytes.Equal(frame, last) {
			t.Fatalf("sent %x, got %x", frame, last)
		}
	})
}
