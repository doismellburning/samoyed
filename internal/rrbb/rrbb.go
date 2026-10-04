// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

// Package rrbb holds the Raw Received Bit Buffer: the bits of one frame as
// they come out of a demodulator, before HDLC decoding turns them into bytes.
// Keeping the raw bits, along with the 9600 baud descrambler state at the
// start of the frame, lets the decoder retry with bits flipped when the frame
// check sequence fails.
//
// From Dire Wolf's rrbb.c:
//
//	Version 1.2: Save initial state of 9600 baud descrambler so we can
//			attempt bit fix up on G3RUH/K9NG scrambled data.
//
//	Version 1.3:	Store as bytes rather than packing 8 bits per byte.
package rrbb

import (
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
)

// maxFrameLen is the longest frame the buffer holds, in bytes: the longest
// packet plus its 2 byte frame check sequence.
const maxFrameLen = ax25.MaxPacketLen + 2

// MaxNumBits is the maximum number of bits in an AX.25 frame excluding the
// flags. It is adequate for the extreme case of bit stuffing after every 5
// bits, which could never happen.
const MaxNumBits = maxFrameLen * 8 * 6 / 5

// Buffer holds the raw bits of one received frame.
type Buffer struct {
	channel    int /* Radio channel from which it was received. */
	subchannel int /* Which modem when more than one per channel. */
	slice      int /* Which slicer. */

	alevel     ax25.ALevel /* Received audio level at time of frame capture. */
	speedError float64     /* Received data speed error as percentage. */
	length     int         /* Current number of samples in array. */

	isScrambled  bool /* Is data scrambled G3RUH / K9NG style? */
	descramState int  /* Descrambler state before first data bit of frame. */
	prevDescram  int  /* Previous descrambled bit. */

	fdata [MaxNumBits]byte
}

// New allocates a bit buffer for frames heard on the given radio
// channel, demodulator (subchannel) and slicer, and clears it as Clear does.
func New(channel int, subchannel int, slice int, isScrambled bool, descramState int, prevDescram int) *Buffer {
	var result = new(Buffer)

	result.channel = channel
	result.subchannel = subchannel
	result.slice = slice

	result.Clear(isScrambled, descramState, prevDescram)

	return result
}

// Clear empties the buffer and records the state of the data descrambler, and
// the previous descrambled bit (0 or 1), before the first data bit of the frame.
// isScrambled says whether the data is scrambled G3RUH / K9NG style.
func (b *Buffer) Clear(isScrambled bool, descramState int, prevDescram int) {
	dwutil.Assert(prevDescram == 0 || prevDescram == 1)

	b.alevel.Rec = 9999 // TODO: was there some reason for this instead of 0 or -1?
	b.alevel.Mark = 9999
	b.alevel.Space = 9999

	b.length = 0

	b.isScrambled = isScrambled
	b.descramState = descramState
	b.prevDescram = prevDescram
}

// AppendBit appends another bit to the end, silently discarding it if the
// buffer is full.
func (b *Buffer) AppendBit(val byte) {
	if b.length >= MaxNumBits {
		return /* Silently discard if full. */
	}

	b.fdata[b.length] = val
	b.length++
}

// Chop8 removes 8 bits from the end, to back up after appending the flag
// sequence.
func (b *Buffer) Chop8() {
	if b.length >= 8 {
		b.length -= 8
	}
}

// Len returns the number of bits in the buffer.
func (b *Buffer) Len() int {
	return b.length
}

// Bit returns the value of the bit at index ind.
func (b *Buffer) Bit(ind int) byte {
	return b.fdata[ind]
}

// Channel returns the radio channel the bits were received on.
func (b *Buffer) Channel() int {
	return b.channel
}

// Subchannel returns the demodulator the bits were received by.
func (b *Buffer) Subchannel() int {
	return b.subchannel
}

// Slice returns the slicer the bits were received by.
func (b *Buffer) Slice() int {
	return b.slice
}

// SetAudioLevel sets the audio level at the time the frame was received.
func (b *Buffer) SetAudioLevel(alevel ax25.ALevel) {
	b.alevel = alevel
}

// AudioLevel returns the audio level at the time the frame was received.
func (b *Buffer) AudioLevel() ax25.ALevel {
	return b.alevel
}

// SetSpeedError sets the speed error of the received frame, as a percentage.
func (b *Buffer) SetSpeedError(speedError float64) {
	b.speedError = speedError
}

// SpeedError returns the speed error of the received frame, as a percentage.
func (b *Buffer) SpeedError() float64 {
	return b.speedError
}

// IsScrambled reports whether the data is scrambled: true for 9600 baud,
// false for slower AFSK.
func (b *Buffer) IsScrambled() bool {
	return b.isScrambled
}

// DescramState returns the data descrambler state before the first data bit
// of the frame.
func (b *Buffer) DescramState() int {
	return b.descramState
}

// PrevDescram returns the previous descrambled bit before the first data bit
// of the frame.
func (b *Buffer) PrevDescram() int {
	return b.prevDescram
}
