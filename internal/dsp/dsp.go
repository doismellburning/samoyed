// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

// Package dsp generates the FIR filter kernels the demodulators use: windowed
// sinc low pass and band pass filters, and root raised cosine low pass filters.
//
// Each generator fills the whole of the slice it is given, so its length is the
// number of filter taps.
package dsp

import (
	"math"

	"github.com/doismellburning/samoyed/internal/dwutil"
)

// Window is the shape of the window applied to a filter kernel.
type Window int

const (
	WindowTruncated Window = iota
	WindowCosine
	WindowHamming
	WindowBlackman
	WindowFlattop
)

// shape returns the window's multiplier for tap j, in the range 0 to size-1, of
// a filter with size taps.
func (w Window) shape(size int, j int) float64 {
	var n = float64(size)
	var x = float64(j)

	var center = 0.5 * (n - 1)

	switch w {
	case WindowCosine:
		return math.Cos((x - center) / n * math.Pi)

	case WindowHamming:
		return 0.53836 - 0.46164*math.Cos((x*2*math.Pi)/(n-1))

	case WindowBlackman:
		return 0.42659 - 0.49656*math.Cos((x*2*math.Pi)/(n-1)) +
			0.076849*math.Cos((x*4*math.Pi)/(n-1))

	case WindowFlattop:
		return 1.0 - 1.93*math.Cos((x*2*math.Pi)/(n-1)) +
			1.29*math.Cos((x*4*math.Pi)/(n-1)) -
			0.388*math.Cos((x*6*math.Pi)/(n-1)) +
			0.028*math.Cos((x*8*math.Pi)/(n-1))

	case WindowTruncated:
		fallthrough
	default:
		return 1.0
	}
}

// Lowpass fills filter with a low pass filter kernel, normalised for unity gain
// at DC.  fc is the cutoff frequency as a fraction of the sampling frequency.
func Lowpass(fc float64, filter []float64, w Window) {
	var size = len(filter)

	dwutil.Assert(size >= 3)

	var center = 0.5 * float64(size-1)

	for j := range size {
		var sinc float64

		if float64(j)-center == 0 {
			sinc = 2 * fc
		} else {
			sinc = math.Sin(2*math.Pi*(fc*(float64(j)-center))) / (math.Pi * (float64(j) - center))
		}

		filter[j] = sinc * w.shape(size, j)
	}

	var G float64 = 0
	for j := range size {
		G += filter[j]
	}

	for j := range size {
		filter[j] /= G
	}
}

// Bandpass fills filter with a band pass filter kernel for a prefilter,
// normalised for unity gain in the middle of the passband.  f1 and f2 are the
// lower and upper cutoff frequencies as fractions of the sampling frequency.
//
// Reference: http://www.labbookpages.co.uk/audio/firWindowing.html
func Bandpass(f1 float64, f2 float64, filter []float64, w Window) {
	var size = len(filter)

	dwutil.Assert(size >= 3)

	var center = 0.5 * float64(size-1)

	for j := range size {
		var sinc float64

		if float64(j)-center == 0 {
			sinc = 2 * (f2 - f1)
		} else {
			sinc = math.Sin(2*math.Pi*f2*(float64(j)-center))/(math.Pi*(float64(j)-center)) -
				math.Sin(2*math.Pi*f1*(float64(j)-center))/(math.Pi*(float64(j)-center))
		}

		filter[j] = sinc * w.shape(size, j)
	}

	// Normalising for unity gain at DC, as for the low pass filter, won't do
	// here, so compute the gain in the middle of the passband instead.
	// See http://dsp.stackexchange.com/questions/4693/fir-filter-gain
	var omega = 2 * math.Pi * (f1 + f2) / 2

	var G float64 = 0
	for j := range size {
		G += 2 * filter[j] * math.Cos((float64(j)-center)*omega) // is this correct?
	}

	for j := range size {
		filter[j] /= G
	}
}

// rrc is the Root Raised Cosine function: mostly the sinc function, with cosine
// windowing to taper off the edges faster.  t is time in symbol durations, so
// the centres of two adjacent symbols differ by 1, and a is the roll off factor,
// between 0 and 1.  The result is 1 for t = 0 and 0 at every other integer t.
func rrc(t float64, a float64) float64 {
	var sinc, window float64

	if t > -0.001 && t < 0.001 {
		sinc = 1
	} else {
		sinc = math.Sin(math.Pi*t) / (math.Pi * t)
	}

	if math.Abs(a*t) > 0.499 && math.Abs(a*t) < 0.501 {
		window = math.Pi / 4
	} else {
		// This goes negative when a > 0.5 / (filter width in symbol times).
		var x = 2 * a * t
		window = math.Cos(math.Pi*a*t) / (1 - x*x)
	}

	return sinc * window
}

// RRCLowpass fills filter with a Root Raised Cosine low pass filter kernel,
// which is supposed to minimise intersymbol interference, normalised for unity
// gain.
func RRCLowpass(filter []float64, rolloff float64, samplesPerSymbol float64) {
	var size = len(filter)

	for k := range size {
		var t = (float64(k) - ((float64(size) - 1.0) / 2.0)) / samplesPerSymbol
		filter[k] = rrc(t, rolloff)
	}

	var G float64 = 0
	for k := range size {
		G += filter[k]
	}

	for k := range size {
		filter[k] /= G
	}
}
