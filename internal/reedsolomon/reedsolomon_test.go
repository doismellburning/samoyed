// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package reedsolomon

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The codec parameters FX.25 (fcr 1) and IL2P (fcr 0) use.
func testCodecs(tb testing.TB) map[string]*Codec {
	tb.Helper()

	var codecs = make(map[string]*Codec)

	for _, fcr := range []uint{0, 1} {
		for _, nroots := range []uint{2, 4, 6, 8, 16, 32, 64} {
			var c, err = New(8, 0x11d, fcr, 1, nroots)
			require.NoError(tb, err)

			codecs[fmt.Sprintf("fcr%d_nroots%d", fcr, nroots)] = c
		}
	}

	return codecs
}

func testBlock(c *Codec, rng *rand.Rand) []byte {
	var data = make([]byte, c.N()-c.NRoots())
	rng.Read(data)

	return append(data, c.Encode(data)...)
}

func TestNew_Invalid(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][5]uint{
		"symsize too big":    {9, 0x11d, 1, 1, 16},
		"fcr out of range":   {8, 0x11d, 256, 1, 16},
		"prim zero":          {8, 0x11d, 1, 0, 16},
		"prim out of range":  {8, 0x11d, 1, 256, 16},
		"too many roots":     {8, 0x11d, 1, 1, 256},
		"poly not primitive": {8, 0x100, 1, 1, 16},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var c, err = New(args[0], args[1], args[2], args[3], args[4])
			require.Error(t, err)
			assert.Nil(t, c)
		})
	}
}

func TestDecode_NoErrors(t *testing.T) {
	t.Parallel()

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A source of its own, so the subtests can run in parallel.
			var rng = rand.New(rand.NewSource(1))

			var block = testBlock(c, rng)
			var original = slices.Clone(block)

			var locs, err = c.Decode(block, nil)
			require.NoError(t, err)
			assert.Empty(t, locs)
			assert.Equal(t, original, block)
		})
	}
}

// NRoots()/2 errors in unknown places is as many as the code can repair.
func TestDecode_CorrectsErrors(t *testing.T) {
	t.Parallel()

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A source of its own, so the subtests can run in parallel.
			var rng = rand.New(rand.NewSource(2))

			var block = testBlock(c, rng)
			var original = slices.Clone(block)

			var positions = rng.Perm(c.N())[:c.NRoots()/2]
			for _, pos := range positions {
				block[pos] ^= safecast.RequireConvert[byte](t, 1+rng.Intn(255))
			}

			var locs, err = c.Decode(block, nil)
			require.NoError(t, err)
			assert.ElementsMatch(t, positions, locs)
			assert.Equal(t, original, block)
		})
	}
}

// With the positions known up front, NRoots() erasures can be repaired - twice
// as many as unknown errors.
func TestDecode_CorrectsErasures(t *testing.T) {
	t.Parallel()

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A source of its own, so the subtests can run in parallel.
			var rng = rand.New(rand.NewSource(3))

			var block = testBlock(c, rng)
			var original = slices.Clone(block)

			var positions = rng.Perm(c.N())[:c.NRoots()]
			for _, pos := range positions {
				block[pos] ^= safecast.RequireConvert[byte](t, 1+rng.Intn(255))
			}

			var _, err = c.Decode(block, positions)
			require.NoError(t, err)
			assert.Equal(t, original, block)
		})
	}
}

func TestDecode_Uncorrectable(t *testing.T) {
	t.Parallel()

	var rng = rand.New(rand.NewSource(4))

	var c, err = New(8, 0x11d, 1, 1, 16)
	require.NoError(t, err)

	var failures = 0

	for range 100 {
		var block = testBlock(c, rng)
		var original = slices.Clone(block)

		for _, pos := range rng.Perm(c.N())[:c.NRoots()] {
			block[pos] ^= safecast.RequireConvert[byte](t, 1+rng.Intn(255))
		}

		var _, decodeErr = c.Decode(block, nil)
		if decodeErr != nil {
			require.ErrorIs(t, decodeErr, ErrUncorrectable)

			failures++
		} else {
			// A miscorrection to some other codeword is possible, but never back to the original.
			assert.NotEqual(t, original, block)
		}
	}

	// Twice as many errors as can be corrected is almost always detected.
	assert.Greater(t, failures, 90)
}

// paddedBlock zero-pads tail at the front out to a whole block, as
// il2p_decode_rs does with a short IL2P block.
func paddedBlock(tb testing.TB, c *Codec, tail string) []byte {
	tb.Helper()

	var b, err = hex.DecodeString(tail)
	require.NoError(tb, err)

	return append(make([]byte, c.N()-len(b)), b...)
}

func isCodeword(c *Codec, block []byte) bool {
	var k = c.N() - c.NRoots()

	return slices.Equal(c.Encode(block[:k]), block[k:])
}

// Each of these blocks is too far from any codeword to correct, but the
// decoder once reported it repaired (issue #790).
func TestDecode_UncorrectableRegressions(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		fcr, nroots uint
		tail        string
	}{
		// The syndromes are S0 != 0 and S1 = 0, which reduces the locator
		// to 1: no errors to find, and none found.
		"no locator for a non-zero syndrome": {0, 2, "1223403f0edeae13e49219c0f83240"},
		// One "correction", leaving a block that still isn't a codeword.
		"fcr0 corrected to a non-codeword": {0, 4, "5b613fde3c0d880cecef4101aad8b8b1c60a92ac"},
		"fcr1 corrected to a non-codeword": {1, 4, "13308ddf2af52ca390487eb20c716b76d78ad451"},
		// More corrections than NRoots()/2, the most the code can make.
		"3 corrections with 4 parity symbols": {0, 4, "1589d0a711f8e74be32bc60f43d693de77f21d5f"},
		"4 corrections with 6 parity symbols": {0, 6, "42302779d03b019d2aa7db2b7aa8fb5b29a53fbc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var c, err = New(8, 0x11d, tc.fcr, 1, tc.nroots)
			require.NoError(t, err)

			var block = paddedBlock(t, c, tc.tail)
			require.False(t, isCodeword(c, block))

			var _, decodeErr = c.Decode(block, nil)
			require.ErrorIs(t, decodeErr, ErrUncorrectable)
		})
	}
}

func TestDecode_BadArguments(t *testing.T) {
	t.Parallel()

	var c, err = New(8, 0x11d, 1, 1, 4)
	require.NoError(t, err)

	var _, decodeErr = c.Decode(make([]byte, 254), nil)
	require.Error(t, decodeErr)

	_, decodeErr = c.Decode(make([]byte, 255), []int{1, 2, 3, 4, 5})
	require.Error(t, decodeErr)

	_, decodeErr = c.Decode(make([]byte, 255), []int{255})
	require.Error(t, decodeErr)

	_, decodeErr = c.Decode(make([]byte, 255), []int{-1})
	require.Error(t, decodeErr)
}

func TestEncode_WrongLengthPanics(t *testing.T) {
	t.Parallel()

	var c, err = New(8, 0x11d, 1, 1, 16)
	require.NoError(t, err)

	assert.Panics(t, func() { c.Encode(make([]byte, 240)) })
	assert.Panics(t, func() { c.Encode(make([]byte, 238)) })
}

// FuzzDecode feeds the decoder arbitrary blocks, which is what it gets from
// anyone on frequency.  Whatever it accepts it must leave as a codeword,
// having corrected no more symbols than the code can repair.
func FuzzDecode(f *testing.F) {
	var codecs = testCodecs(f)

	var names = make([]string, 0, len(codecs))
	for name := range codecs {
		names = append(names, name)
	}

	slices.Sort(names)

	f.Add(make([]byte, 255), uint8(0))
	f.Add(slices.Repeat([]byte{0xff}, 255), uint8(3))
	f.Add(testBlock(codecs["fcr1_nroots16"], rand.New(rand.NewSource(5))), uint8(0))

	f.Fuzz(func(t *testing.T, block []byte, which uint8) {
		var c = codecs[names[int(which)%len(names)]]

		block = append(block, make([]byte, max(0, c.N()-len(block)))...)[:c.N()]

		var locs, err = c.Decode(block, nil)
		if err != nil {
			return
		}

		for _, pos := range locs {
			assert.True(t, pos >= 0 && pos < c.N(), "Corrected position %d is outside the block", pos)
		}

		assert.LessOrEqual(t, len(locs), c.NRoots()/2)
		assert.True(t, isCodeword(c, block), "Decode accepted a block it did not leave as a codeword")
	})
}
