// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
)

// naiveConvolve is the straightforward single-accumulator FIR kernel that
// convolve is expected to agree with, up to floating-point reordering.
func naiveConvolve(data, filter []float64, filter_size int) float64 {
	var sum = 0.0

	for j := range filter_size {
		sum += filter[j] * data[j]
	}

	return sum
}

func TestConvolveMatchesNaive(t *testing.T) {
	var rng = rand.New(rand.NewPCG(1, 2))

	// Cover every remainder modulo the unroll width, plus the empty filter.
	for size := range 20 {
		var data = make([]float64, size)
		var filter = make([]float64, size)

		for i := range size {
			data[i] = rng.Float64()*2 - 1
			filter[i] = rng.Float64()*2 - 1
		}

		var want = naiveConvolve(data, filter, size)
		var got = convolve(data, filter, size)

		assert.InDelta(t, want, got, 1e-12, "size %d", size)
	}
}

func TestConvolveIgnoresTrailingElements(t *testing.T) {
	// Only the first filter_size taps count, even when the slices are longer.
	var data = []float64{1, 2, 3, 4, 5, 6, 7, 1000}
	var filter = []float64{1, 1, 1, 1, 1, 1, 1, 1000}

	assert.InDelta(t, 28.0, convolve(data, filter, 7), 0)
}

func BenchmarkConvolve(b *testing.B) {
	const size = 480

	var data = make([]float64, size)
	var filter = make([]float64, size)

	for i := range size {
		data[i] = math.Sin(float64(i))
		filter[i] = math.Cos(float64(i))
	}

	var sink float64

	for b.Loop() {
		sink += convolve(data, filter, size)
	}

	_ = sink
}
