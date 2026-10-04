// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package linecode

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

// referenceScramble is G3RUH scrambling written out from its definition,
// y[n] = x[n] ^ y[n-12] ^ y[n-17], with nothing sent before the first bit.
func referenceScramble(in []int) []int {
	var out = make([]int, len(in))

	for n, x := range in {
		var y = x

		if n >= 12 {
			y ^= out[n-12]
		}

		if n >= 17 {
			y ^= out[n-17]
		}

		out[n] = y
	}

	return out
}

// randomBits is a repeatable stream of bits for comparing against.
func randomBits(n int) []int {
	var rng = rand.New(rand.NewPCG(1, 2))

	var bits = make([]int, n)
	for i := range bits {
		bits[i] = rng.IntN(2)
	}

	return bits
}

func descrambleAll(d *Descrambler, in []int) []int {
	var out = make([]int, len(in))
	for i, b := range in {
		out[i] = d.Descramble(b)
	}

	return out
}

// Descrambling is x[n] = y[n] ^ y[n-12] ^ y[n-17], so a lone 1 comes out at
// those three places and nowhere else.
func TestDescramblerImpulseResponse(t *testing.T) {
	var in = make([]int, 40)
	in[0] = 1

	var expected = make([]int, 40)
	expected[0] = 1
	expected[12] = 1
	expected[17] = 1

	assert.Equal(t, expected, descrambleAll(new(Descrambler), in))
}

func TestDescramblerUndoesScrambling(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, data, descrambleAll(new(Descrambler), referenceScramble(data)))
}

// A receiver joining part way through a transmission starts in the wrong
// state, but has the right one once 17 bits have gone through.
func TestDescramblerSynchronisesWithin17Bits(t *testing.T) {
	var data = randomBits(200)

	var d = NewDescrambler(0x5a5a5)
	var got = descrambleAll(&d, referenceScramble(data))

	assert.Equal(t, data[17:], got[17:])
}

func TestDescramblerStateCarriesOn(t *testing.T) {
	var line = referenceScramble(randomBits(200))

	var whole = descrambleAll(new(Descrambler), line)

	var first Descrambler
	var firstHalf = descrambleAll(&first, line[:100])

	var second = NewDescrambler(first.State())
	var secondHalf = descrambleAll(&second, line[100:])

	assert.Equal(t, whole, append(firstHalf, secondHalf...))
}

func scrambleAll(s *Scrambler, in []int) []int {
	var out = make([]int, len(in))
	for i, b := range in {
		out[i] = s.Scramble(b)
	}

	return out
}

// Scrambling feeds its own output back, y[n] = x[n] ^ y[n-12] ^ y[n-17], so a
// lone 1 keeps echoing.  Worked by hand for the first 41 bits.
func TestScramblerImpulseResponse(t *testing.T) {
	var in = make([]int, 41)
	in[0] = 1

	var expected = make([]int, 41)
	for _, n := range []int{0, 12, 17, 24, 34, 36} {
		expected[n] = 1
	}

	assert.Equal(t, expected, scrambleAll(new(Scrambler), in))
}

func TestScramblerMatchesItsDefinition(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, referenceScramble(data), scrambleAll(new(Scrambler), data))
}

func TestScramblerRoundTrip(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, data, descrambleAll(new(Descrambler), scrambleAll(new(Scrambler), data)))
}
