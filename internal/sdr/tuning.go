// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// nominalSampleRate is roughly how many IQ samples a second to ask the dongle
// for: enough to span a band's packet frequencies, few enough to filter on a
// Raspberry Pi.  The rate used is the nearest multiple of the audio rate
// above it that decimates in two stages.
const nominalSampleRate = 1_200_000

// maxSampleRate is the most an RTL-SDR does without dropping samples.
const maxSampleRate = 2_400_000

// usableFraction is how much of the sampled bandwidth, either side of the
// centre, a frequency can be in: the dongle's own filters roll off towards
// the edges.
const usableFraction = 0.4

// dcClearance is how far from the centre a frequency should be.  An RTL-SDR
// has a spike at its centre frequency, and the frequency correction error
// smears it.
const dcClearance = 50_000

// dcShift is how far off the middle of the frequencies to tune when one of
// them is too close to the centre.
const dcShift = 150_000

// ErrNoFrequencies is what NewTuning reports when it is given none.
var ErrNoFrequencies = errors.New("no frequencies to receive")

// Tuning is where to tune the dongle, and how fast to sample, to receive a set
// of frequencies.
type Tuning struct {
	// Centre is the frequency to tune the dongle to, in Hz.
	Centre int

	// SampleRate is the IQ samples a second to ask the dongle for.
	SampleRate int

	// Offsets are how far each frequency is from Centre, in Hz.
	Offsets []float64
}

// NewTuning works out a Tuning that receives every one of frequencies, in Hz,
// and decimates to audioRate.
func NewTuning(frequencies []float64, audioRate int) (Tuning, error) {
	var tuning Tuning

	if len(frequencies) == 0 {
		return tuning, ErrNoFrequencies
	}

	if audioRate <= 0 {
		return tuning, fmt.Errorf("audio sample rate %d is not positive", audioRate)
	}

	for _, f := range frequencies {
		if !(f > 0) || f > math.MaxInt32 {
			return tuning, fmt.Errorf("frequency %g Hz is out of range", f)
		}
	}

	var rate, ok = sampleRateFor(audioRate)
	if !ok {
		return tuning, fmt.Errorf("no IQ sample rate up to %d decimates to an audio sample rate of %d", maxSampleRate, audioRate)
	}

	var lowest = slices.Min(frequencies)
	var highest = slices.Max(frequencies)
	var middle = (lowest + highest) / 2
	var limit = usableFraction * float64(rate)

	if highest-lowest > 2*limit {
		return tuning, fmt.Errorf("frequencies %.4f to %.4f MHz are more than %.4f MHz apart, which is as wide as one receiver goes",
			lowest/1e6, highest/1e6, 2*limit/1e6)
	}

	var centre = middle
	// Shifting down past 0 Hz would ask the dongle for a negative frequency.
	if (highest-lowest)/2 < dcClearance && highest-(middle-dcShift) <= limit && middle-dcShift > 0 {
		centre = middle - dcShift
	}

	tuning.Centre = int(math.Round(centre))
	tuning.SampleRate = rate

	for _, f := range frequencies {
		tuning.Offsets = append(tuning.Offsets, f-float64(tuning.Centre))
	}

	return tuning, nil
}

// sampleRateFor gives the IQ sample rate to decimate to audioRate: the first
// multiple of it, from about nominalSampleRate up, whose decimation factor
// splits into two stages - a large prime factor would need one long filter.
func sampleRateFor(audioRate int) (int, bool) {
	var factor = max(1, (nominalSampleRate+audioRate-1)/audioRate)

	for ; factor*audioRate <= maxSampleRate; factor++ {
		var _, second = decimationStages(factor)
		if second > 1 || factor < 4 {
			return factor * audioRate, true
		}
	}

	return 0, false
}

// decimationStages splits decimation by factor into two stages, the first
// taking the larger share: the closest to equal that divides exactly.  A prime
// factor gives itself and 1.
func decimationStages(factor int) (int, int) {
	var second = 1

	for d := 2; d*d <= factor; d++ {
		if factor%d == 0 {
			second = d
		}
	}

	return factor / second, second
}
