// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// easUnpack turns the line levels an EASSender sent back into bytes, least
// significant bit first: SAME has no NRZI, so each level is a bit.
func easUnpack(t *testing.T, levels []int) []byte {
	t.Helper()

	require.Zero(t, len(levels)%8, "a whole number of bytes should have been sent")

	var out = make([]byte, 0, len(levels)/8)

	for i := 0; i < len(levels); i += 8 {
		var b byte

		for j := range 8 {
			if levels[i+j] != 0 {
				b |= 1 << j
			}
		}

		out = append(out, b)
	}

	return out
}

// A message goes out as the preamble, sixteen 0xAB bytes, then the message
// itself, each byte as it is, least significant bit first.  SAME is not HDLC,
// so there is no NRZI and no stuffing, and the line's NRZI level, which the
// next HDLC frame carries on from, is left as it was.
func TestEASSenderSendsThePreambleThenTheMessage(t *testing.T) {
	var levels []int

	var line = linecode.NewEncoder(func(level int) {
		levels = append(levels, level)
	})

	// Leave the line high, where a sender that used NRZI, or reset the
	// level, would show.
	line.WriteNRZI(false)
	require.Equal(t, 1, line.Level())

	levels = nil

	var message = []byte("ZCZC-Q1TEST")

	var sent = NewEASSender(line).SendMessage(message)

	assert.Len(t, levels, sent, "the count returned should be the bits actually sent")

	var expected = append(bytes.Repeat([]byte{0xAB}, 16), message...)

	assert.Equal(t, expected, easUnpack(t, levels))
	assert.Equal(t, 1, line.Level(), "the NRZI level should be as it was")
}
