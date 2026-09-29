// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package rrbb

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
)

func TestNewRecordsWhereTheBitsCameFrom(t *testing.T) {
	var b = New(1, 2, 3, true, 0x1234, 1)

	assert.Equal(t, 1, b.Channel())
	assert.Equal(t, 2, b.Subchannel())
	assert.Equal(t, 3, b.Slice())
	assert.True(t, b.IsScrambled())
	assert.Equal(t, 0x1234, b.DescramState())
	assert.Equal(t, 1, b.PrevDescram())
	assert.Equal(t, 0, b.Len())
	assert.Equal(t, ax25.ALevel{Rec: 9999, Mark: 9999, Space: 9999}, b.AudioLevel()) //nolint:exhaustruct
}

func TestClearEmptiesButKeepsWhereTheBitsCameFrom(t *testing.T) {
	var b = New(1, 2, 3, false, 0, 0)

	b.AppendBit(1)
	b.SetAudioLevel(ax25.ALevel{Rec: 50, Mark: 40, Space: 30}) //nolint:exhaustruct
	b.Clear(true, 0x55, 1)

	assert.Equal(t, 0, b.Len())
	assert.Equal(t, 1, b.Channel())
	assert.Equal(t, 2, b.Subchannel())
	assert.Equal(t, 3, b.Slice())
	assert.True(t, b.IsScrambled())
	assert.Equal(t, 0x55, b.DescramState())
	assert.Equal(t, 1, b.PrevDescram())
	assert.Equal(t, 9999, b.AudioLevel().Rec)
}

func TestClearRejectsANonBitPrevDescram(t *testing.T) {
	var b = New(0, 0, 0, false, 0, 0)

	assert.Panics(t, func() { b.Clear(false, 0, 2) })
}

func TestAppendBitThenBit(t *testing.T) {
	var b = New(0, 0, 0, false, 0, 0)
	var bits = []byte{1, 0, 0, 1, 1}

	for _, bit := range bits {
		b.AppendBit(bit)
	}

	assert.Equal(t, len(bits), b.Len())

	for i, bit := range bits {
		assert.Equal(t, bit, b.Bit(i), "bit %d", i)
	}
}

func TestAppendBitDiscardsOnceFull(t *testing.T) {
	var b = New(0, 0, 0, false, 0, 0)

	for range MaxNumBits {
		b.AppendBit(0)
	}

	b.AppendBit(1)

	assert.Equal(t, MaxNumBits, b.Len())
	assert.Equal(t, byte(0), b.Bit(MaxNumBits-1))
}

func TestChop8(t *testing.T) {
	var b = New(0, 0, 0, false, 0, 0)

	for range 10 {
		b.AppendBit(1)
	}

	b.Chop8()
	assert.Equal(t, 2, b.Len())

	// Fewer than 8 bits are left alone rather than going negative.
	b.Chop8()
	assert.Equal(t, 2, b.Len())
}

func TestSpeedError(t *testing.T) {
	var b = New(0, 0, 0, false, 0, 0)

	b.SetSpeedError(-1.5)

	assert.InDelta(t, -1.5, b.SpeedError(), 0)
}
