// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package kiss

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collected is everything a Collector made of a stream, copied out of its
// buffers.
type collected struct {
	frames [][]byte
	noise  [][]byte
	lines  []bool
	errs   []error
}

func feed(c *Collector, data []byte) collected {
	var got collected

	for _, b := range data {
		var chunk = c.Add(b)

		if chunk.Frame != nil {
			got.frames = append(got.frames, bytes.Clone(chunk.Frame))
		}

		if chunk.Noise != nil {
			got.noise = append(got.noise, bytes.Clone(chunk.Noise))
			got.lines = append(got.lines, chunk.EndOfLine)
		}

		if chunk.Err != nil {
			got.errs = append(got.errs, chunk.Err)
		}
	}

	return got
}

func TestCollectorWholeFrame(t *testing.T) {
	var frame = Encapsulate([]byte{CmdDataFrame, FEND, 'h', 'i', FESC})

	var got = feed(new(Collector), frame)

	require.Len(t, got.frames, 1)
	assert.Equal(t, frame, got.frames[0], "the frame should arrive as it was sent")
	assert.Empty(t, got.noise)
	assert.Empty(t, got.errs)
}

func TestCollectorFramesBackToBack(t *testing.T) {
	var first = Encapsulate([]byte{CmdDataFrame, 'a'})
	var second = Encapsulate([]byte{CmdDataFrame, 'b'})

	var got = feed(new(Collector), append(bytes.Clone(first), second...))

	assert.Equal(t, [][]byte{first, second}, got.frames)
	assert.Empty(t, got.noise)
}

// FENDs with nothing between them are how some clients and TNCs idle, and
// are not frames.
func TestCollectorEmptyFrames(t *testing.T) {
	var got = feed(new(Collector), []byte{FEND, FEND, FEND, FEND})

	assert.Empty(t, got.frames)
	assert.Empty(t, got.noise)
}

// Whatever precedes a frame is handed over as noise when the frame starts,
// and the frame is unaffected by it.
func TestCollectorNoiseBeforeAFrame(t *testing.T) {
	var frame = Encapsulate([]byte{CmdDataFrame, 'h', 'i'})

	var got = feed(new(Collector), append([]byte("junk"), frame...))

	assert.Equal(t, [][]byte{[]byte("junk")}, got.noise)
	assert.Equal(t, []bool{false}, got.lines)
	assert.Equal(t, [][]byte{frame}, got.frames)
}

// Something that thinks it is talking to a command-mode TNC sends lines of
// text, each of which is handed over as it ends.
func TestCollectorNoiseLines(t *testing.T) {
	var got = feed(new(Collector), []byte("XFLOW OFF\rKISS ON\r"))

	assert.Equal(t, [][]byte{[]byte("XFLOW OFF\r"), []byte("KISS ON\r")}, got.noise)
	assert.Equal(t, []bool{true, true}, got.lines)
	assert.Empty(t, got.frames)
}

// Noise is kept only so far: a stream of it must not make the Collector grow
// without limit.
func TestCollectorNoiseIsBounded(t *testing.T) {
	var got = feed(new(Collector), append(bytes.Repeat([]byte{'x'}, MaxNoiseLen*2), '\r'))

	require.Len(t, got.noise, 1)
	assert.Len(t, got.noise[0], MaxNoiseLen)
	assert.True(t, got.lines[0], "the line still ends, carriage return kept or not")
}

// A frame that never ends would otherwise fill the buffer without limit.  It
// is reported once, however much more of it there is.
func TestCollectorOverlongFrameIsReportedOnce(t *testing.T) {
	var got = feed(new(Collector), append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MaxFrameLen*2)...))

	assert.Equal(t, []error{ErrFrameTooLong}, got.errs)
	assert.Empty(t, got.frames)
}

// When the frame does end it is thrown away, not handed over as a fragment,
// and the next frame still arrives.
func TestCollectorOverlongFrameIsDiscarded(t *testing.T) {
	var next = Encapsulate([]byte{CmdDataFrame, 'o', 'k'})

	var stream = append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MaxFrameLen+10)...)
	stream = append(stream, FEND)
	stream = append(stream, next...)

	var got = feed(new(Collector), stream)

	assert.Equal(t, []error{ErrFrameTooLong}, got.errs)
	assert.Equal(t, [][]byte{next}, got.frames)
	assert.Empty(t, got.noise, "the rest of the overlong frame is not noise")
}

// A frame that fills the buffer exactly leaves no room for its closing FEND,
// so it is overlong too.
func TestCollectorExactlyFullFrameIsDiscarded(t *testing.T) {
	var stream = append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MaxFrameLen-1)...)
	stream = append(stream, FEND)

	var got = feed(new(Collector), stream)

	assert.Equal(t, []error{ErrFrameTooLong}, got.errs)
	assert.Empty(t, got.frames)
}

// And the largest frame that does fit gets through.
func TestCollectorLargestFrame(t *testing.T) {
	var stream = append([]byte{FEND}, bytes.Repeat([]byte{'x'}, MaxFrameLen-2)...)
	stream = append(stream, FEND)

	var got = feed(new(Collector), stream)

	assert.Empty(t, got.errs)
	assert.Equal(t, [][]byte{stream}, got.frames)
}
