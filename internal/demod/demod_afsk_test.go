// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package demod

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/stretchr/testify/assert"
)

// fcos256 and fsin256 take a phase whose top byte is the fraction of a turn.
func TestFcos256Table(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.0, fcos256(0), 1e-12)
	assert.InDelta(t, 0.0, fsin256(0), 1e-12)
	assert.InDelta(t, 0.0, fcos256(64<<24), 1e-12)
	assert.InDelta(t, 1.0, fsin256(64<<24), 1e-12)
	assert.InDelta(t, -1.0, fcos256(128<<24), 1e-12)
}

// The space gains run geometrically from MIN_G to MAX_G, one per slicer.
func TestAFSKSpaceGain(t *testing.T) {
	t.Parallel()

	assert.Len(t, afskSpaceGain, phy.MaxSlicers)
	assert.InDelta(t, MIN_G, afskSpaceGain[0], 1e-12)
	assert.InDelta(t, MAX_G, afskSpaceGain[phy.MaxSlicers-1], 1e-9)

	var ratio = afskSpaceGain[1] / afskSpaceGain[0]
	for j := 2; j < phy.MaxSlicers; j++ {
		assert.InDelta(t, ratio, afskSpaceGain[j]/afskSpaceGain[j-1], 1e-12)
	}
}

// Setting up a demodulator only touches the demodulator, so two can be set
// up at once - as tests running in parallel would - without racing over a
// shared table.
func TestDemodulatorInitsDoNotShareState(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup

	for range 2 {
		wg.Go(func() {
			demod_afsk_init(44100, 1200, 1200, 2200, 'A', new(demodulator_state_s))
		})
		wg.Go(func() {
			demod_9600_init(phy.Scramble, 48000, 1, 9600, new(demodulator_state_s))
		})
	}

	wg.Wait()
}

// Every filter the AFSK demodulator builds is symmetric, tap for tap - which
// convolveSymmetric depends on, reading only the first half of each - for each
// profile, at the usual tones and rates, decimated or not, and for EAS.
func TestAFSKFiltersAreSymmetric(t *testing.T) {
	t.Parallel()

	for _, profile := range []byte{'A', 'B'} {
		for _, tc := range []struct {
			baud, mark, space int
			sampleRates       []int
		}{
			{1200, 1200, 2200, []int{44100, 48000, 22050, 16000, 14700, 8000}},
			{300, 1600, 1800, []int{44100, 48000, 16000, 14700}},
			{521, 2083, 1563, []int{44100, 48000}},
		} {
			for _, sampleRate := range tc.sampleRates {
				var D = new(demodulator_state_s)
				demod_afsk_init(sampleRate, tc.baud, tc.mark, tc.space, profile, D)

				var what = fmt.Sprintf("profile %c, %d baud, %d:%d Hz, %d Hz", profile, tc.baud, tc.mark, tc.space, sampleRate)

				assert.True(t, isSymmetric(D.pre_filter[:D.pre_filter_taps]), "pre filter, %s", what)
				assert.True(t, isSymmetric(D.lp_filter[:D.lp_filter_taps]), "low pass filter, %s", what)
			}
		}
	}
}

func TestConvolveSymmetricMatchesNaive(t *testing.T) {
	t.Parallel()

	var rng = rand.New(rand.NewPCG(3, 4))

	// Every remainder modulo the unroll width, odd and even, the empty filter,
	// and some sizes the AFSK demodulator uses: profiles A and B at 44.1kHz,
	// and A at 48kHz.
	var sizes = []int{73, 103, 113, 299, 383, 417}
	for size := range 41 {
		sizes = append(sizes, size)
	}

	for _, size := range sizes {
		var data = make([]float64, size)
		var filter = make([]float64, size)

		for i := range size {
			data[i] = rng.Float64()*2 - 1
		}

		for i := range (size + 1) / 2 {
			filter[i] = rng.Float64()*2 - 1
			filter[size-1-i] = filter[i]
		}

		var want = naiveConvolve(data, filter, size)
		var got = convolveSymmetric(data, filter, size)

		assert.InDelta(t, want, got, 1e-12, "size %d", size)
	}
}

func TestIsSymmetric(t *testing.T) {
	t.Parallel()

	assert.True(t, isSymmetric(nil))
	assert.True(t, isSymmetric([]float64{1}))
	assert.True(t, isSymmetric([]float64{1, 2, 1}))
	assert.True(t, isSymmetric([]float64{1, 2, 2, 1}))
	assert.False(t, isSymmetric([]float64{1, 2, 3}))
	assert.False(t, isSymmetric([]float64{1, 2, 2, 1 + 1e-16*4}))
}

func TestConvolveSymmetricIgnoresTrailingElements(t *testing.T) {
	t.Parallel()

	var data = []float64{1, 2, 3, 4, 5, 6, 7, 1000}
	var filter = []float64{1, 2, 3, 4, 3, 2, 1, 1000}

	assert.InDelta(t, naiveConvolve(data, filter, 7), convolveSymmetric(data, filter, 7), 0)
}

func BenchmarkConvolveSymmetric(b *testing.B) {
	const size = 480

	var data = make([]float64, size)
	var filter = make([]float64, size)

	for i := range size {
		data[i] = math.Sin(float64(i))
		filter[i] = math.Cos(float64(i - size/2))
	}

	var sink float64

	for b.Loop() {
		sink += convolveSymmetric(data, filter, size)
	}

	_ = sink
}
