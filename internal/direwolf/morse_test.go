// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Morse goes through the channel's tone generator and its own audio config,
// so it needs nothing another test left behind (issue #761).
func TestMorseSendWithoutSharedRadioConfig(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var audioConfig = new(RadioConfig)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var sink = new(byteSink)

	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	var ms = morse_send(tg, channel, "SOS", MORSE_DEFAULT_WPM, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")

	// Two bytes per sample; allow a little for rounding each element.
	var samples = len(sink.data) / 2
	assert.InDelta(t, ms*sampleRate/1000, samples, float64(sampleRate)/100)
}

// A channel with no tone generator still says how long the transmission would
// have been, as xmit_thread holds the PTT for that long.
func TestMorseSendWithoutToneGenerator(t *testing.T) {
	var units = morse_units_str("E")

	assert.Equal(t, 300+int(TIME_UNITS_TO_MS(units, 10)+0.5)+250, morse_send(nil, 0, "E", 10, 300, 250))
}

// morseRecorder is a morseSampleSink that keeps the samples it is given, and
// counts quiet milliseconds and flushes.
type morseRecorder struct {
	samples []int
	quietMs []int
	flushes int
}

func (r *morseRecorder) PutSample(sam int) {
	r.samples = append(r.samples, sam)
}

func (r *morseRecorder) PutQuietMs(ms int) {
	r.quietMs = append(r.quietMs, ms)
}

func (r *morseRecorder) Flush() {
	r.flushes++
}

// The txdelay and txtail go to the sink as quiet periods, so that a tone
// generator can tidy up after them, the characters between them as samples,
// and the lot is flushed out once, at the end.
func TestMorseSendToASink(t *testing.T) {
	const sampleRate = 8000

	var out = new(morseRecorder)

	morseSend(out, sampleRate, 50, "SOS", MORSE_DEFAULT_WPM, 300, 250)

	assert.Equal(t, []int{300, 250}, out.quietMs)
	assert.Equal(t, 1, out.flushes, "the tones should be flushed out once, at the end")

	// Allow a little for rounding each element.
	var ms = morseDuration("SOS", MORSE_DEFAULT_WPM, 0, 0)
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
// gives txdelay and txtail, is checked beside the tone generator.
func TestMorseSampleCountsAt44100(t *testing.T) {
	const sampleRate = 44100

	var out = new(morseRecorder)
	var sineTable = morseSineTable(50)

	// One unit at 10 WPM is 120 ms, which is 5292 samples.
	morseTone(out, sampleRate, &sineTable, 1, 10)
	assert.Len(t, out.samples, 5292, "dot")
}
