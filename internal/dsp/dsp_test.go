// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dsp

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func sum(xs []float64) float64 {
	var total float64
	for _, x := range xs {
		total += x
	}

	return total
}

func assertSymmetric(t *testing.T, filter []float64) {
	t.Helper()

	for j := range len(filter) / 2 {
		assert.InDelta(t, filter[j], filter[len(filter)-1-j], 1e-12, "tap %d", j)
	}
}

// gainAt is the magnitude of the filter's frequency response at f, as a
// fraction of the sampling frequency.
func gainAt(filter []float64, f float64) float64 {
	var re, im float64
	for j, x := range filter {
		re += x * math.Cos(2*math.Pi*f*float64(j))
		im -= x * math.Sin(2*math.Pi*f*float64(j))
	}

	return math.Hypot(re, im)
}

func TestWindowShape(t *testing.T) {
	const size = 101

	for _, w := range []Window{WindowTruncated, WindowCosine, WindowHamming, WindowBlackman} {
		assert.InDelta(t, 1.0, w.shape(size, size/2), 0.01, "window %d peaks at 1 in the middle", w)
	}

	// The flat top coefficients aren't normalised, so it peaks at their sum.
	// Every filter is normalised after windowing, so this doesn't matter.
	assert.InDelta(t, 1+1.93+1.29+0.388+0.028, WindowFlattop.shape(size, size/2), 1e-12)

	assert.InDelta(t, 1.0, WindowTruncated.shape(size, 0), 1e-12)
	assert.InDelta(t, 0.53836-0.46164, WindowHamming.shape(size, 0), 1e-12)
	assert.InDelta(t, 0.007, WindowBlackman.shape(size, 0), 0.001)
	assert.InDelta(t, 1.0, Window(99).shape(size, 0), 1e-12, "an unknown window is truncated")
}

func TestLowpass(t *testing.T) {
	for _, w := range []Window{WindowTruncated, WindowCosine, WindowHamming, WindowBlackman, WindowFlattop} {
		var filter = make([]float64, 63)
		Lowpass(0.1, filter, w)

		assert.InDelta(t, 1.0, sum(filter), 1e-9, "unity gain at DC, window %d", w)
		assertSymmetric(t, filter)
	}

	var filter = make([]float64, 101)
	Lowpass(0.1, filter, WindowHamming)
	assert.Less(t, gainAt(filter, 0.3), 0.01, "stopband")
}

func TestBandpass(t *testing.T) {
	var filter = make([]float64, 101)
	Bandpass(0.1, 0.2, filter, WindowHamming)

	assertSymmetric(t, filter)
	// Dire Wolf's normalisation doubles the gain it measures, so the filter
	// comes out at half gain mid-passband rather than the unity it intends.
	// Left as it is in Dire Wolf.
	assert.InDelta(t, 0.5, gainAt(filter, 0.15), 0.01, "gain mid-passband")
	assert.Less(t, gainAt(filter, 0), 0.01, "DC is stopped")
	assert.Less(t, gainAt(filter, 0.35), 0.01, "high frequencies are stopped")
}

func TestFiltersRejectTooFewTaps(t *testing.T) {
	assert.Panics(t, func() { Lowpass(0.1, make([]float64, 2), WindowTruncated) })
	assert.Panics(t, func() { Bandpass(0.1, 0.2, make([]float64, 2), WindowTruncated) })
}

func TestRRC(t *testing.T) {
	assert.InDelta(t, 1.0, rrc(0, 0.5), 1e-12)

	for _, tt := range []float64{-3, -2, -1, 1, 2, 3} {
		assert.InDelta(t, 0.0, rrc(tt, 0.5), 1e-12, "t=%v", tt)
	}

	// The singularity where |a*t| is a half is special-cased.
	assert.False(t, math.IsInf(rrc(1, 0.5), 0))
	assert.InDelta(t, math.Pi/4*math.Sin(math.Pi*1.001)/(math.Pi*1.001), rrc(1.001, 0.5), 1e-12)
}

func TestRRCLowpass(t *testing.T) {
	var filter = make([]float64, 81)
	RRCLowpass(filter, 0.2, 10)

	assert.InDelta(t, 1.0, sum(filter), 1e-9)
	assertSymmetric(t, filter)

	for j, x := range filter {
		assert.LessOrEqual(t, x, filter[len(filter)/2], "tap %d exceeds the centre", j)
	}
}
