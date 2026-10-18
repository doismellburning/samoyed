// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package dtmf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// sampleRecorder is a SampleSink that keeps what it is given, and counts
// flushes.
type sampleRecorder struct {
	samples []int
	flushes int
}

func (r *sampleRecorder) PutSample(sam int) {
	r.samples = append(r.samples, sam)
}

func (r *sampleRecorder) Flush() {
	r.flushes++
}

// What Send puts on the air, a decoder on the same channel reads back.
func TestSendDecodesBack(t *testing.T) {
	t.Parallel()

	const sampleRate = 8000

	var out = new(sampleRecorder)

	Send(out, sampleRate, 50, "159D*#", 10, 300, 250)

	assert.Equal(t, 1, out.flushes, "the tones should be flushed out once, at the end")

	var decoder = NewDecoder(0, sampleRate, nil)

	var heard strings.Builder

	for _, sam := range out.samples {
		var x = decoder.Sample(float64(sam) / 16384.)
		if x != ' ' && x != '.' {
			heard.WriteRune(x)
		}
	}

	assert.Equal(t, "159D*#", heard.String())
}

// The PTT is held for the delays either side and half a tone period of tone
// and of quiet per button.
func TestDuration(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 300+400+250, Duration("1234", 10, 300, 250))
}

// Send and Duration both take the text a byte at a time, as Dire Wolf does,
// with silence for anything that isn't a button.  Send used to take it a rune
// at a time while Duration counted bytes, so the two disagreed (issue #1019).
func TestSendNonASCII(t *testing.T) {
	t.Parallel()

	const sampleRate = 8000

	var out = new(sampleRecorder)

	Send(out, sampleRate, 50, "1À2", 10, 0, 0)

	var ms = Duration("1À2", 10, 0, 0)
	assert.InDelta(t, ms*sampleRate/1000, len(out.samples), float64(sampleRate)/100)
}
