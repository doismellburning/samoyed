package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureFX25Bits collects the line levels a new FX25Sender sends while fn
// runs.  The line starts low.
func captureFX25Bits(fn func(s *FX25Sender)) []int {
	var bits []int

	fn(NewFX25Sender(linecode.NewEncoder(func(level int) {
		bits = append(bits, level)
	}), 0, 0))

	return bits
}

// An FX.25 frame goes out as its correlation tag, then the data and check
// bytes of the codeblock, NRZI encoded and least significant bit first.
func TestFX25FrameIsSentAsTagDataAndCheck(t *testing.T) {
	var fbuf = []byte{'Q', '1', 'T', 'E', 'S', 'T'}

	var ctagNum, data, check = fx25_encode_frame(0, append([]byte{}, fbuf...), 16, 0)
	require.GreaterOrEqual(t, ctagNum, CTAG_MIN)

	var sent int

	var bits = captureFX25Bits(func(s *FX25Sender) {
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
	var bits = captureFX25Bits(func(s *FX25Sender) {
		assert.Equal(t, -1, s.SendFrame(make([]byte, FX25_MAX_DATA), 16))
	})

	assert.Empty(t, bits)
}
