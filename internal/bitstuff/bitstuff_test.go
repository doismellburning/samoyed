// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bitstuff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestStuffProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var in = rapid.SliceOf(rapid.Byte()).Draw(t, "in")

		var out, _ = Stuff(in, 0) // 0 means no padding

		assert.GreaterOrEqualf(t, len(out), 2, "There should always be at least two bytes of output - the start and end flags! Got %v", out)
		assert.Equal(t, flag, out[0], "Missing start flag")
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

// Worked by hand, least significant bit first: the start flag 01111110, then
// 0xff's eight 1s with a 0 stuffed after the fifth, then the end flag, 25
// bits in all:
//
//	01111110 11111011 10111111 0
//
// which packs into 7e df fd 00.
func TestStuffKnownAnswer(t *testing.T) {
	var out, meaningful = Stuff([]byte{0xff}, 0)

	assert.Equal(t, []byte{0x7e, 0xdf, 0xfd, 0x00}, out)
	assert.Equal(t, 4, meaningful)
}

// FX.25 fills the rest of its codeblock with flags, as if the end flag
// carried on repeating.
func TestStuffPadsWithFlags(t *testing.T) {
	var unpadded, meaningful = Stuff([]byte{0xff}, 0)
	var padded, paddedMeaningful = Stuff([]byte{0xff}, 8)

	assert.Len(t, padded, 8)
	assert.Equal(t, meaningful, paddedMeaningful, "padding is not part of the frame")
	assert.Equal(t, unpadded[:3], padded[:3])

	// The last bit of the end flag, then the flag pattern over again from
	// its first bit: 0 01111110 01111110 ...
	assert.Equal(t, []byte{0xfc, 0xfc, 0xfc, 0xfc, 0xfc}, padded[3:])
}

func TestUnstuffRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// An empty frame is only flags, which Unstuff skips over looking
		// for the start of the frame.
		var in = rapid.SliceOfN(rapid.Byte(), 1, 300).Draw(t, "in")

		var stuffed, _ = Stuff(in, 0)

		var out, err = Unstuff(stuffed)
		require.NoError(t, err)
		assert.Equal(t, in, out)

		var padded, _ = Stuff(in, 2*len(in)+4)

		out, err = Unstuff(padded)
		require.NoError(t, err)
		assert.Equal(t, in, out)
	})
}

func TestUnstuffSkipsExtraLeadingFlags(t *testing.T) {
	var stuffed, _ = Stuff([]byte("Q1TEST"), 0)

	var out, err = Unstuff(append([]byte{flag, flag}, stuffed...))
	require.NoError(t, err)
	assert.Equal(t, []byte("Q1TEST"), out)
}

func TestUnstuffErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrNoStartFlag},
		{"no start flag", []byte{0x00, flag}, ErrNoStartFlag},
		{"seven ones", []byte{flag, 0xff}, ErrSevenOnes},
		// After the start flag, three 0 bits and then a flag: 00001111 110...
		{"not whole bytes", []byte{flag, 0xf0, 0x03}, ErrNotWholeBytes},
		{"no end flag", []byte{flag, 0x00}, ErrNoEndFlag},
		{"only flags", []byte{flag, flag}, ErrNoEndFlag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, err = Unstuff(tc.in)

			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, out)
		})
	}
}
