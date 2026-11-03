// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package demod

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
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
	t.Parallel()

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
	t.Parallel()

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

// The upsampling factor is kept to what there are polyphase filters for.
func TestDemod9600InitClampsUpsample(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		upsample int
		want     int
	}{
		{-1, 1},
		{0, 1},
		{1, 1},
		{4, 4},
		{9, 4},
	} {
		var D = new(demodulator_state_s)

		demod_9600_init(phy.Scramble, 48000, tc.upsample, 9600, D)

		assert.Equal(t, phy.Scramble, D.modem_type)
		assert.Equal(t, 1, D.num_slicers)
		assert.Equal(t, 5, D.lp_filter_taps, "one symbol's worth of taps at 48000/9600")

		var wantStep = int32(math.Round(TICKS_PER_PLL_CYCLE * 9600 / float64(48000*tc.want)))
		assert.Equal(t, wantStep, D.pll_step_per_sample, "upsample %d", tc.upsample)

		// Each polyphase filter in use has taken its share of the prototype,
		// and those beyond the upsampling factor are left empty.
		var polyphases = [][]float64{
			D.u.bb.lp_polyphase_1[:], D.u.bb.lp_polyphase_2[:],
			D.u.bb.lp_polyphase_3[:], D.u.bb.lp_polyphase_4[:],
		}

		for p, poly := range polyphases {
			for i := range D.lp_filter_taps {
				assert.InDelta(t, D.u.bb.lp_filter[i*tc.want+p]*boolToFloat(p < tc.want), poly[i], 0, "upsample %d, phase %d, tap %d", tc.upsample, p, i)
			}
		}
	}
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}

	return 0
}

// The slicing levels are spread evenly and symmetrically about zero.
func TestSlicePoints(t *testing.T) {
	t.Parallel()

	for j := range phy.MaxSubchans {
		assert.InDelta(t, -slice_point[phy.MaxSubchans-1-j], slice_point[j], 1e-12)
	}

	for j := 1; j < phy.MaxSubchans; j++ {
		assert.InDelta(t, 0.02, slice_point[j]-slice_point[j-1], 1e-12)
	}
}

func TestPushSample(t *testing.T) {
	t.Parallel()

	var buff = []float64{1, 2, 3, 4, 99}

	push_sample(0, buff, 4)

	assert.Equal(t, []float64{0, 1, 2, 3, 99}, buff, "only the first size elements shift, the oldest dropping off")
}

// The AGC settles a steady square wave into the range -0.5 .. +0.5, whatever
// its level and DC offset.
func TestAGCNormalises(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		amplitude, offset float64
	}{
		{1, 0},
		{0.1, 0},
		{3, 2},
	} {
		var peak, valley, out float64

		for i := range 20000 {
			var in = tc.offset + tc.amplitude*float64(1-2*(i/5%2))

			peak, valley, out = agc(in, 0.08, 0.00012, peak, valley)

			_ = out
		}

		assert.InDelta(t, tc.offset+tc.amplitude, peak, 0.05*tc.amplitude)
		assert.InDelta(t, tc.offset-tc.amplitude, valley, 0.05*tc.amplitude)

		_, _, out = agc(tc.offset+tc.amplitude, 0.08, 0.00012, peak, valley)
		assert.InDelta(t, 0.5, out, 0.05)

		_, _, out = agc(tc.offset-tc.amplitude, 0.08, 0.00012, peak, valley)
		assert.InDelta(t, -0.5, out, 0.05)
	}
}

// With no spread between peak and valley yet, the AGC has nothing to scale
// by and says nothing.
func TestAGCWithNoSpread(t *testing.T) {
	t.Parallel()

	var peak, valley, out = agc(0, 0.08, 0.00012, 0, 0)

	assert.Zero(t, peak)
	assert.Zero(t, valley)
	assert.Zero(t, out)
}
