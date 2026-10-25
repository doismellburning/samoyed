// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewTuning_centresBetweenFrequencies(t *testing.T) {
	t.Parallel()

	var tuning, err = NewTuning([]float64{144_800_000, 145_000_000}, 48000)
	require.NoError(t, err)

	assert.Equal(t, 144_900_000, tuning.Centre)
	assert.Equal(t, 1_200_000, tuning.SampleRate)
	assert.Equal(t, []float64{-100_000, 100_000}, tuning.Offsets)
}

func Test_NewTuning_keepsOffTheCentreSpike(t *testing.T) {
	t.Parallel()

	var testCases = map[string][]float64{
		"one":        {144_800_000},
		"close pair": {144_800_000, 144_825_000},
	}

	for name, frequencies := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var tuning, err = NewTuning(frequencies, 48000)
			require.NoError(t, err)

			for _, offset := range tuning.Offsets {
				assert.GreaterOrEqual(t, offset, float64(dcClearance))
				assert.LessOrEqual(t, offset, usableFraction*float64(tuning.SampleRate))
			}
		})
	}
}

func Test_NewTuning_sampleRateDecimatesToAudioRate(t *testing.T) {
	t.Parallel()

	for _, audioRate := range []int{8000, 11025, 22050, 44100, 48000, 96000, 192000} {
		var tuning, err = NewTuning([]float64{144_800_000}, audioRate)
		require.NoError(t, err, "audio rate %d", audioRate)

		assert.Zero(t, tuning.SampleRate%audioRate, "audio rate %d", audioRate)
		assert.GreaterOrEqual(t, tuning.SampleRate, nominalSampleRate, "audio rate %d", audioRate)
		assert.LessOrEqual(t, tuning.SampleRate, maxSampleRate, "audio rate %d", audioRate)

		var factor = tuning.SampleRate / audioRate
		var _, second = decimationStages(factor)
		assert.True(t, second > 1 || factor < 4, "audio rate %d decimates by a prime %d", audioRate, factor)
	}
}

func Test_NewTuning_refuses(t *testing.T) {
	t.Parallel()

	var testCases = map[string]struct {
		frequencies []float64
		audioRate   int
	}{
		"nothing":       {nil, 48000},
		"too far apart": {[]float64{144_800_000, 145_800_000}, 48000},
		"negative":      {[]float64{-1}, 48000},
		"too high":      {[]float64{3e9}, 48000},
		"no audio rate": {[]float64{144_800_000}, 0},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var _, err = NewTuning(tc.frequencies, tc.audioRate)
			require.Error(t, err)
		})
	}
}

func Test_decimationStages(t *testing.T) {
	t.Parallel()

	var testCases = map[int][2]int{
		25: {5, 5},
		28: {7, 4},
		50: {10, 5},
		7:  {7, 1},
		1:  {1, 1},
	}

	for factor, want := range testCases {
		var first, second = decimationStages(factor)
		assert.Equal(t, want, [2]int{first, second}, "factor %d", factor)
	}
}

// A frequency too low to shift the centre below stays clear of 0 Hz.
func Test_NewTuning_neverTunesBelowZero(t *testing.T) {
	t.Parallel()

	var tuning, err = NewTuning([]float64{100_000}, 48000)
	require.NoError(t, err)
	assert.Positive(t, tuning.Centre)
}
