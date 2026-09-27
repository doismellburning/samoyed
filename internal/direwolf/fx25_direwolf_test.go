package direwolf

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from the "fxsend" and "fxrec" standalone test programs in Dire Wolf,
// which encoded a frame with each of the correlation tags into fx01.dat ...
// fx0b.dat, then read those files back in and counted the successes.
// Here the codeblocks stay in memory.

var fxTestFrame = []byte{ //nolint:gochecknoglobals
	'Q' << 1, '1' << 1, 'T' << 1, 'E' << 1, 'S' << 1, 'T' << 1, 0x60,
	'Q' << 1, '2' << 1, 'T' << 1, 'E' << 1, 'S' << 1, 'T' << 1, 0x63,
	0x03, 0xf0,
	'F', 'o', 'o', '?', 'B', 'a', 'r', '?', //  '?' causes bit stuffing
}

// How many data bytes we clobber before handing the block to the receiver.
// The weakest format, RS(48,32), can repair 8, so this is as much damage as
// every correlation tag is expected to survive.
const fxTestCorruptBytes = 8

func Test_FX25_round_trip(t *testing.T) {
	FX25Init(1)

	for ctag := CTAG_MIN; ctag <= CTAG_MAX; ctag++ {
		t.Run(fmt.Sprintf("ctag_%02x", ctag), func(t *testing.T) {
			var ctag_num, data, check = fx25_encode_frame(0, slices.Clone(fxTestFrame), 100+ctag)
			require.Equal(t, ctag, ctag_num, "Wrong correlation tag chosen")

			// Give the FEC something to do.
			for j := 8; j < 8+fxTestCorruptBytes; j++ {
				data[j] ^= 0xff
			}

			var frames, derrors = fxTestReceive(fxTestBlock(ctag_num, data, check))

			require.Len(t, frames, 1, "Expected exactly one frame out of the receiver")
			assert.Equal(t, fxTestFrame, frames[0], "Frame did not survive the round trip")
			assert.Equal(t, fxTestCorruptBytes, derrors[0], "Unexpected number of bytes corrected")
		})
	}
}

// fxTestBlock assembles what would go out over the air: some leading flags,
// the correlation tag, then the data and check bytes.
func fxTestBlock(ctag_num int, data []byte, check []byte) []byte {
	var flags = slices.Repeat([]byte{0x7e}, 16)

	var block = slices.Clone(flags)

	var ctag_value = fx25_get_ctag_value(ctag_num)
	for k := range 8 {
		block = append(block, byte(ctag_value>>(k*8))&0xff) // Should be portable to big endian too.
	}

	block = append(block, data...)
	block = append(block, check...)

	return append(block, flags...)
}

// fxTestReceive feeds a block to the receiver a bit at a time, LSB first, and
// returns any frames extracted along with the number of bytes the FEC had to
// correct for each.
func fxTestReceive(block []byte) ([][]byte, []int) {
	var frames [][]byte
	var derrors []int

	var collect = func(channel int, subchannel int, slice int, frame []byte, d int) {
		frames = append(frames, frame)
		derrors = append(derrors, d)
	}

	var rx = newFX25Receiver(0, 0, 0, collect)

	for _, b := range block {
		for imask := byte(0x01); imask != 0; imask <<= 1 {
			rx.recBit(int(b & imask))
		}
	}

	return frames, derrors
}
