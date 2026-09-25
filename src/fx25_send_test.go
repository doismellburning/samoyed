package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func Test_bitStuff(t *testing.T) {
	const FLAG byte = 0x7e

	rapid.Check(t, func(t *rapid.T) {
		var in = rapid.SliceOf(rapid.Byte()).Draw(t, "in")

		var out, _ = bitStuff(in, 0) // 0 means no padding

		assert.GreaterOrEqualf(t, len(out), 2, "There should always be at least two bytes of output - the start and end flags! Got %v", out)
		assert.Equal(t, FLAG, out[0], "Missing start flag")
		assert.GreaterOrEqual(t, len(out)-2, len(in), "Somehow bits were lost in stuffing!") // Subtract 2 for start and end flags

		// TODO Check *nicely* for sequential 1s

		// Until then, check crudely! This isn't as complete as doing a proper bitstream check (because things can cross bytes), but it's a useful fast test!
		// Drop last 2 bytes to definitely avoid picking up flag
		var outWithNoEndFlag = out[:len(out)-2]

		assert.NotContains(t, outWithNoEndFlag, byte(0x3f))
		assert.NotContains(t, outWithNoEndFlag, byte(0x7f))
		assert.NotContains(t, outWithNoEndFlag, byte(0xff))
		assert.NotContains(t, outWithNoEndFlag, byte(0xfe))
		assert.NotContains(t, outWithNoEndFlag, byte(0xfc))
	})
}

// An FX.25 frame goes out as its correlation tag, then the data and check
// bytes of the codeblock, NRZI encoded and least significant bit first.
func TestFX25FrameIsSentAsTagDataAndCheck(t *testing.T) {
	FX25Init(0)

	var fbuf = []byte{'Q', '1', 'T', 'E', 'S', 'T'}

	var ctagNum, data, check = fx25_encode_frame(hdlcSendTestChannel, append([]byte{}, fbuf...), 16)
	require.GreaterOrEqual(t, ctagNum, CTAG_MIN)

	var sent int

	var bits = captureBits(t, nil, func(s *HDLCSender) {
		sent = s.sendFX25Frame(fbuf, 16)
	})

	assert.Equal(t, len(bits), sent, "the count returned should be the bits actually sent")

	var ctagValue = fx25_get_ctag_value(ctagNum)

	var expected []byte
	for k := range 8 {
		expected = append(expected, byte(ctagValue>>(k*8)))
	}

	expected = append(expected, data...)
	expected = append(expected, check...)

	assert.Equal(t, expected, packLSBFirst(t, nrziDecode(bits)))
}

// A frame too large for FX.25 is rejected without sending anything, so the
// caller can fall back to AX.25.
func TestFX25FrameTooLargeSendsNothing(t *testing.T) {
	FX25Init(0)

	var bits = captureBits(t, nil, func(s *HDLCSender) {
		assert.Equal(t, -1, s.sendFX25Frame(make([]byte, FX25_MAX_DATA), 16))
	})

	assert.Empty(t, bits)
}

// FX.25 and AX.25 go out on the same line, so a codeblock has to start from
// the level whatever went before it left the line at, or its first bit is
// received inverted.
func TestFX25FrameCarriesOnFromTheLineLevelBeforeIt(t *testing.T) {
	FX25Init(0)

	// A frame and its FCS always hold an even number of zeros, so it takes
	// a stuffed zero to leave the line at 1, where starting the codeblock
	// afresh from 0 would show.
	var before = []byte{hdlcSixtyOne, 'Q', '1', 'T', 'E', 'S', 'T'}
	var fbuf = []byte{'Q', '2', 'T', 'E', 'S', 'T'}

	var ctagNum, data, check = fx25_encode_frame(hdlcSendTestChannel, append([]byte{}, fbuf...), 16)
	require.GreaterOrEqual(t, ctagNum, CTAG_MIN)

	var beforeLen int

	var bits = captureBits(t, nil, func(s *HDLCSender) {
		beforeLen = s.sendAX25Frame(before, false)
		s.sendFX25Frame(fbuf, 16)
	})

	require.Equal(t, 1, bits[beforeLen-1], "the frame before should leave the line at 1")

	var ctagValue = fx25_get_ctag_value(ctagNum)

	var expected []byte
	for k := range 8 {
		expected = append(expected, byte(ctagValue>>(k*8)))
	}

	expected = append(expected, data...)
	expected = append(expected, check...)

	assert.Equal(t, expected, packLSBFirst(t, nrziDecode(bits)[beforeLen:]))
}
