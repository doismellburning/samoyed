// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package reedsolomon

import (
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
	for name, args := range map[string][5]uint{
		"symsize too big":    {9, 0x11d, 1, 1, 16},
		"fcr out of range":   {8, 0x11d, 256, 1, 16},
		"prim zero":          {8, 0x11d, 1, 0, 16},
		"prim out of range":  {8, 0x11d, 1, 256, 16},
		"too many roots":     {8, 0x11d, 1, 1, 256},
		"poly not primitive": {8, 0x100, 1, 1, 16},
	} {
		t.Run(name, func(t *testing.T) {
			var c, err = New(args[0], args[1], args[2], args[3], args[4])
			require.Error(t, err)
			assert.Nil(t, c)
		})
	}
}

func TestDecode_NoErrors(t *testing.T) {
	var rng = rand.New(rand.NewSource(1))

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
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
	var rng = rand.New(rand.NewSource(2))

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
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
	var rng = rand.New(rand.NewSource(3))

	for name, c := range testCodecs(t) {
		t.Run(name, func(t *testing.T) {
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

func TestDecode_BadArguments(t *testing.T) {
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
	var c, err = New(8, 0x11d, 1, 1, 16)
	require.NoError(t, err)

	assert.Panics(t, func() { c.Encode(make([]byte, 240)) })
	assert.Panics(t, func() { c.Encode(make([]byte, 238)) })
}

// FuzzDecode feeds the decoder arbitrary blocks, which is what it gets from
// anyone on frequency.  The failure looked for is a panic.
//
// It does not check that a claimed success leaves a codeword behind: with
// more errors than it can repair, the decoder occasionally "corrects" a block
// into something that still is not one (see issue #790), so the FCS or CRC
// the caller checks afterwards is what catches those.
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
	})
}
