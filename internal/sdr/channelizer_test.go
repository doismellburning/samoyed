// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"math"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testSampleRate = 1_200_000
	testAudioRate  = 48000
)

// tone is seconds of a sine wave at hz, full scale, at rate samples a second.
func tone(hz float64, rate int, seconds float64) []float64 {
	var audio = make([]float64, int(seconds*float64(rate)))
	for n := range audio {
		audio[n] = math.Sin(2 * math.Pi * hz * float64(n) / float64(rate))
	}

	return audio
}

// power is how much of hz there is in channel ch of the interleaved stereo
// audio at testAudioRate, by the Goertzel algorithm, leaving out the first
// 20 ms while the filters settle.
func power(audio []int16, ch int, hz float64) float64 {
	const channels = 2
	const skip = testAudioRate / 50

	var coeff = 2 * math.Cos(2*math.Pi*hz/float64(testAudioRate))
	var s1, s2 float64
	var n = 0

	for i := (skip * channels) + ch; i < len(audio); i += channels {
		var s = float64(audio[i]) + coeff*s1 - s2
		s2 = s1
		s1 = s
		n++
	}

	return (s1*s1 + s2*s2 - coeff*s1*s2) / float64(n*n)
}

func Test_Channelizer_demodulatesEachChannel(t *testing.T) {
	t.Parallel()

	var iq = testutils.FMModulate(testSampleRate,
		testutils.FMSignal{Offset: -200_000, Deviation: 3000, Audio: tone(1200, testAudioRate, 0.2), AudioRate: testAudioRate},
		testutils.FMSignal{Offset: 150_000, Deviation: 3000, Audio: tone(2200, testAudioRate, 0.2), AudioRate: testAudioRate},
	)

	var c, err = NewChannelizer(testSampleRate, testAudioRate, []float64{-200_000, 150_000})
	require.NoError(t, err)
	assert.Equal(t, 2, c.Channels())

	// In uneven pieces, as TCP would hand them over.
	var audio []int16
	for len(iq) > 0 {
		var n = min(len(iq), 4097)
		audio = c.Process(iq[:n], audio)
		iq = iq[n:]
	}

	assert.Len(t, audio, 2*int(0.2*testAudioRate))

	var ch0at1200 = power(audio, 0, 1200)
	var ch0at2200 = power(audio, 0, 2200)
	var ch1at1200 = power(audio, 1, 1200)
	var ch1at2200 = power(audio, 1, 2200)

	assert.Greater(t, ch0at1200, 100*ch0at2200, "channel 0 should hear only its own tone")
	assert.Greater(t, ch1at2200, 100*ch1at1200, "channel 1 should hear only its own tone")

	// 3 kHz deviation is 3/5 of half scale: a sine of amplitude ~9800, whose
	// Goertzel power is a quarter of that squared.
	var amplitude = 2 * math.Sqrt(ch0at1200)
	assert.InDelta(t, 16384*3.0/5, amplitude, 1000)
}

func Test_Channelizer_refuses(t *testing.T) {
	t.Parallel()

	var _, err = NewChannelizer(1_200_000, 44100, []float64{0})
	require.Error(t, err, "not a multiple")

	_, err = NewChannelizer(1_200_000, 48000, nil)
	require.ErrorIs(t, err, ErrNoFrequencies)

	_, err = NewChannelizer(1_200_000, 48000, []float64{700_000})
	require.Error(t, err, "outside what is sampled")
}

func Benchmark_Channelizer(b *testing.B) {
	var c, err = NewChannelizer(1_200_000, 48000, []float64{-200_000, 150_000})
	require.NoError(b, err)

	// A tenth of a second.
	var iq = make([]byte, 2*120_000)
	for i := range iq {
		iq[i] = byte(i & 0xff)
	}

	var out = make([]int16, 0, 2*4800)

	b.SetBytes(int64(len(iq)))

	for b.Loop() {
		out = c.Process(iq, out[:0])
	}
}
