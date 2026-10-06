// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package fx25

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nrziDecode recovers the data bits from an NRZI stream: a one leaves the
// signal as it was, a zero inverts it.  The line starts at the level
// captureFX25Bits starts it at.
func nrziDecode(bits []int) []bool {
	var data []bool
	var previous = 0

	for _, bit := range bits {
		data = append(data, bit == previous)
		previous = bit
	}

	return data
}

// packLSBFirst reassembles bytes from bits in the order HDLC sends them.
func packLSBFirst(t *testing.T, bits []bool) []byte {
	t.Helper()

	require.Zero(t, len(bits)%8, "a whole number of bytes should have been sent")

	var out = make([]byte, 0, len(bits)/8)

	for i := 0; i < len(bits); i += 8 {
		var b byte

		for j := range 8 {
			if bits[i+j] {
				b |= 1 << j
			}
		}

		out = append(out, b)
	}

	return out
}

// captureFX25Bits collects the line levels a new Sender sends while fn
// runs.  The line starts low.
func captureFX25Bits(fn func(s *Sender)) []int {
	return testutils.LineLevels(func(line *linecode.Encoder) { fn(NewSender(line, 0, 0)) })
}

// An FX.25 frame goes out as its correlation tag, then the data and check
// bytes of the codeblock, NRZI encoded and least significant bit first.
func TestFX25FrameIsSentAsTagDataAndCheck(t *testing.T) {
	var fbuf = []byte{'Q', '1', 'T', 'E', 'S', 'T'}

	var ctagNum, data, check = fx25_encode_frame(0, append([]byte{}, fbuf...), 16, 0)
	require.GreaterOrEqual(t, ctagNum, CTAG_MIN)

	var sent int

	var bits = captureFX25Bits(func(s *Sender) {
		sent = s.SendFrame(fbuf, 16)
	})

	assert.Equal(t, len(bits), sent, "the count returned should be the bits actually sent")

	var ctagValue = fx25_get_ctag_value(ctagNum)

	var expected []byte
	for k := range 8 {
		expected = append(expected, byte(ctagValue>>(k*8))) //nolint:gosec // G115: unchecked narrowing conversion, see #294
	}

	expected = append(expected, data...)
	expected = append(expected, check...)

	assert.Equal(t, expected, packLSBFirst(t, nrziDecode(bits)))
}

// A frame too large for FX.25 is rejected without sending anything, so the
// caller can fall back to AX.25.
func TestFX25FrameTooLargeSendsNothing(t *testing.T) {
	var bits = captureFX25Bits(func(s *Sender) {
		assert.Equal(t, -1, s.SendFrame(make([]byte, MaxData), 16))
	})

	assert.Empty(t, bits)
}
