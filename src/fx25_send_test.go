package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
