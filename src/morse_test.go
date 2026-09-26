// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Morse goes through the channel's tone generator and its own audio config,
// so it needs nothing from the shared save_audio_config_p (issue #761).
func TestMorseSendWithoutSharedAudioConfig(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var origAudio, origGenerators = save_audio_config_p, toneGenerators

	t.Cleanup(func() { save_audio_config_p, toneGenerators = origAudio, origGenerators })

	save_audio_config_p = nil

	var audioConfig = new(audio_s)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var sink = new(byteSink)

	toneGenerators = [MAX_RADIO_CHANS]*ToneGenerator{}
	toneGenerators[channel] = NewToneGenerator(channel, audioConfig, 50, sink)

	var ms = morse_send(channel, "SOS", MORSE_DEFAULT_WPM, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")

	// Two bytes per sample; allow a little for rounding each element.
	var samples = len(sink.data) / 2
	assert.InDelta(t, ms*sampleRate/1000, samples, float64(sampleRate)/100)
}

// A channel with no tone generator still says how long the transmission would
// have been, as xmit_thread holds the PTT for that long.
func TestMorseSendWithoutToneGenerator(t *testing.T) {
	var origGenerators = toneGenerators

	t.Cleanup(func() { toneGenerators = origGenerators })

	toneGenerators = [MAX_RADIO_CHANS]*ToneGenerator{}

	var units = morse_units_str("E")

	assert.Equal(t, 300+int(TIME_UNITS_TO_MS(units, 10)+0.5)+250, morse_send(0, "E", 10, 300, 250))
}

// At a sample rate that isn't a multiple of 1000, the sample counts for tones
// and for txdelay/txtail must not be truncated by integer division (issue #762).
func TestMorseSampleCountsAt44100(t *testing.T) {
	const channel = 0
	const sampleRate = 44100

	var audioConfig = new(audio_s)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate
	audioConfig.chan_medium[channel] = MEDIUM_RADIO

	var sink = new(byteSink)
	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	// One unit at 10 WPM is 120 ms, which is 5292 samples.
	tg.morseTone(1, 10)
	assert.Equal(t, 5292, len(sink.data)/2, "dot")

	sink.data = nil

	// 5 ms is 220.5 samples, which rounds to 221.
	tg.morseQuietMs(5)
	assert.Equal(t, 221, len(sink.data)/2, "quiet ms")
}
