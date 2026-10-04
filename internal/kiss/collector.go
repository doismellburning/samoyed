// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package kiss

import "errors"

// MaxFrameLen is the most a Collector holds of one frame, counting its FENDs
// and escapes.  The spec calls for at least 1024.
const MaxFrameLen = 2048

// MaxNoiseLen is the most a Collector keeps of the bytes between frames.
const MaxNoiseLen = 100

// ErrFrameTooLong says a frame outgrew MaxFrameLen.  What was collected of it
// is only the start of something longer, so it is thrown away at its closing
// FEND rather than handed on.
var ErrFrameTooLong = errors.New("KISS frame exceeded maximum length")

// Collector separates a byte stream into KISS frames, fed one byte at a time
// with Add.  Its zero value is ready to use.
type Collector struct {
	collecting bool

	// The frame so far: its opening FEND, and the contents still escaped.
	frame    [MaxFrameLen]byte
	frameLen int

	// overflowed is set once the frame has outgrown the buffer, so that the
	// rest of it is swallowed and the error reported only the once.
	overflowed bool

	noise    [MaxNoiseLen]byte
	noiseLen int
}

// Chunk is what one byte completed, if anything - at most one of its fields
// is set.  Its slices share the Collector's buffers, so they are only good
// until the next Add.
type Chunk struct {
	// Frame is a whole frame as it arrived: FENDs and escapes still in place,
	// ready for Unwrap.
	Frame []byte

	// Noise is a run of bytes that arrived outside any frame - from something
	// that isn't speaking KISS, or not yet.  It is ended by the FEND opening
	// the next frame or, when EndOfLine is set, by a carriage return, which
	// is included if there was room.
	Noise     []byte
	EndOfLine bool

	// Err is ErrFrameTooLong, reported on the first byte of a frame that
	// doesn't fit.
	Err error
}

// Add takes the next byte of the stream, and returns what it completed.
func (c *Collector) Add(b byte) (chunk Chunk) {
	if !c.collecting {
		return c.addNoise(b)
	}

	if b != FEND {
		if c.frameLen < MaxFrameLen {
			c.frame[c.frameLen] = b
			c.frameLen++

			return chunk
		}

		return c.overflow()
	}

	/* End of frame. */

	if c.frameLen == 1 {
		/* Empty frame.  Just go on collecting. */
		return chunk
	}

	if c.frameLen >= MaxFrameLen {
		// The closing FEND has nowhere to go either, so what was collected
		// is only the first MaxFrameLen bytes of something longer.
		chunk = c.overflow()

		c.collecting = false
		c.overflowed = false

		return chunk
	}

	c.frame[c.frameLen] = b
	c.frameLen++
	c.collecting = false
	chunk.Frame = c.frame[:c.frameLen]

	return chunk
}

func (c *Collector) addNoise(b byte) (chunk Chunk) {
	if b == FEND {
		/* Start of frame.  But first hand over any noise collected. */
		var noise = c.noise[:c.noiseLen]

		c.noiseLen = 0
		c.frame[0] = b
		c.frameLen = 1
		c.collecting = true

		if len(noise) > 0 {
			chunk.Noise = noise
		}

		return chunk
	}

	if c.noiseLen < MaxNoiseLen {
		c.noise[c.noiseLen] = b
		c.noiseLen++
	}

	if b == '\r' {
		var noise = c.noise[:c.noiseLen]

		c.noiseLen = 0
		chunk.Noise = noise
		chunk.EndOfLine = true
	}

	return chunk
}

// overflow reports a frame that doesn't fit, the first time it doesn't.
func (c *Collector) overflow() (chunk Chunk) {
	if c.overflowed {
		return chunk
	}

	c.overflowed = true
	chunk.Err = ErrFrameTooLong

	return chunk
}
