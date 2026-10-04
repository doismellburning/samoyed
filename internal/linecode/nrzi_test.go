// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package linecode

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// referenceNRZI encodes data the way a sender does: a 1 leaves the line as
// it was, a 0 inverts it.  The line starts low.
func referenceNRZI(data []int) []int {
	var line = make([]int, len(data))
	var level = 0

	for i, d := range data {
		if d == 0 {
			level = 1 - level
		}

		line[i] = level
	}

	return line
}

func decodeAll(d *Decoder, line []int, scrambled bool) []int {
	var data = make([]int, len(line))

	for i, b := range line {
		if d.Decode(b != 0, scrambled) {
			data[i] = 1
		}
	}

	return data
}

func TestDecoderUndoesNRZI(t *testing.T) {
	// No change is a 1; a change either way is a 0.
	var line = []int{0, 1, 1, 0, 0, 0, 1}

	assert.Equal(t, []int{1, 0, 1, 0, 1, 1, 0}, decodeAll(new(Decoder), line, false))
}

func TestDecoderRoundTrip(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, data, decodeAll(new(Decoder), referenceNRZI(data), false))
}

// At 9600 baud the sender NRZI encodes and then scrambles, so the receiver
// descrambles and then undoes NRZI.
func TestDecoderRoundTripScrambled(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, data, decodeAll(new(Decoder), referenceScramble(referenceNRZI(data)), true))
}

// A frame is decoded again, with bits flipped, from the state the decoder was
// in at its start.  Restoring that state must give what carrying on would.
func TestDecoderRestoreCarriesOn(t *testing.T) {
	for _, scrambled := range []bool{false, true} {
		var line = referenceNRZI(randomBits(200))
		if scrambled {
			line = referenceScramble(line)
		}

		var whole = decodeAll(new(Decoder), line, scrambled)

		var first Decoder
		var firstHalf = decodeAll(&first, line[:100], scrambled)

		var descramState, prevDescram = first.State()

		var second Decoder
		second.Restore(descramState, prevDescram, first.PrevRaw())

		var secondHalf = decodeAll(&second, line[100:], scrambled)

		assert.Equal(t, whole, append(firstHalf, secondHalf...), "scrambled = %v", scrambled)
	}
}

func TestDecoderPrevRawIsTheLastBit(t *testing.T) {
	var d Decoder

	d.Decode(true, false)
	assert.True(t, d.PrevRaw())

	d.Decode(false, true)
	assert.False(t, d.PrevRaw())
}

// encodeAll NRZI encodes data with an Encoder, returning the line levels.
func encodeAll(data []int) []int {
	var line []int

	var e = NewEncoder(func(level int) { line = append(line, level) })
	for _, d := range data {
		e.WriteNRZI(d != 0)
	}

	return line
}

func TestEncoderMatchesNRZI(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, referenceNRZI(data), encodeAll(data))
}

func TestEncoderDecoderRoundTrip(t *testing.T) {
	var data = randomBits(1000)

	assert.Equal(t, data, decodeAll(new(Decoder), encodeAll(data), false))
}

func TestEncoderDecoderRoundTripScrambled(t *testing.T) {
	var data = randomBits(1000)

	var s Scrambler

	var line = encodeAll(data)
	for i, b := range line {
		line[i] = s.Scramble(b)
	}

	assert.Equal(t, data, decodeAll(new(Decoder), line, true))
}

// IL2P sends bits as they are, inverted if asked, and must not disturb the
// NRZI level an HDLC frame after it carries on from.
func TestEncoderWriteLeavesTheNRZILevelAlone(t *testing.T) {
	var line []int

	var e = NewEncoder(func(level int) { line = append(line, level) })

	e.WriteNRZI(false)
	e.Write(true, false)
	e.Write(false, false)
	e.Write(true, true)
	e.Write(false, true)

	assert.Equal(t, 1, e.Level())

	e.WriteNRZI(true)

	assert.Equal(t, []int{1, 1, 0, 0, 1, 1}, line)
}
