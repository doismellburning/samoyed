// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package morse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// sampleRecorder is a SampleSink that keeps the samples it is given, and
// counts quiet milliseconds and flushes.
type sampleRecorder struct {
	samples []int
	quietMs []int
	flushes int
}

func (r *sampleRecorder) PutSample(sam int) {
	r.samples = append(r.samples, sam)
}

func (r *sampleRecorder) PutQuietMs(ms int) {
	r.quietMs = append(r.quietMs, ms)
}

func (r *sampleRecorder) Flush() {
	r.flushes++
}

// The txdelay and txtail go to the sink as quiet periods, so that a tone
// generator can tidy up after them, the characters between them as samples,
// and the lot is flushed out once, at the end.
func TestSendToASink(t *testing.T) {
	const sampleRate = 8000

	var out = new(sampleRecorder)

	Send(out, sampleRate, 50, "SOS", 10, 300, 250)

	assert.Equal(t, []int{300, 250}, out.quietMs)
	assert.Equal(t, 1, out.flushes, "the tones should be flushed out once, at the end")

	// Allow a little for rounding each element.
	var ms = Duration("SOS", 10, 0, 0)
	assert.InDelta(t, ms*sampleRate/1000, len(out.samples), float64(sampleRate)/100)

	var loudest = 0
	for _, sam := range out.samples {
		loudest = max(loudest, sam)
	}

	// The tone's samples needn't land on its peak, but amplitude 50 is half
	// the 16 bit range.
	assert.LessOrEqual(t, loudest, 32767/2)
	assert.Greater(t, loudest, 32767*4/10)
}

// At a sample rate that isn't a multiple of 1000, the sample count for a tone
// must not be truncated by integer division (issue #762).  PutQuietMs, which
// gives txdelay and txtail, is checked beside the tone generator in
// internal/direwolf.
func TestSampleCountsAt44100(t *testing.T) {
	const sampleRate = 44100

	var out = new(sampleRecorder)
	var sineTable = newSineTable(50)

	// One unit at 10 WPM is 120 ms, which is 5292 samples.
	tone(out, sampleRate, &sineTable, 1, 10)
	assert.Len(t, out.samples, 5292, "dot")
}
