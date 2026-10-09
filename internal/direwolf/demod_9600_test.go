// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// generate9600 has the tone generator send each of frames as G3RUH (scrambled
// baseband) at baud, sampled at sampleRate, and returns the 16 bit samples it
// makes.
func generate9600(t *testing.T, audioConfig *RadioConfig, channel int, frames []*ax25.Packet) []int {
	t.Helper()

	var sink = new(byteSink)

	var sender = NewLayer2Sender(channel, audioConfig, NewToneGenerator(channel, audioConfig, 50, sink), 0, 0)

	sender.SendPreamblePostamble(32, false)

	for _, pp := range frames {
		sender.SendFrame(pp, false)
		sender.SendPreamblePostamble(4, false)
	}

	sender.SendPreamblePostamble(16, true)

	require.NotEmpty(t, sink.data)
	require.Zero(t, len(sink.data)%2, "16 bit samples come in pairs of bytes")

	var samples = make([]int, 0, len(sink.data)/2)
	for i := 0; i < len(sink.data); i += 2 {
		samples = append(samples, int(int16(binary.LittleEndian.Uint16(sink.data[i:])))) //nolint:gosec // G115: unchecked narrowing conversion, see #294
	}

	return samples
}

// demodulate9600 runs samples through the demodulators set up for
// audioConfig, and returns the frames they hand on.
func demodulate9600(t *testing.T, audioConfig *RadioConfig, channel int, samples []int) []*ax25.Packet {
	t.Helper()

	var origDemodulators = demodulators

	t.Cleanup(func() {
		demodulators = origDemodulators
		multiModems = newMultiModems()
	})

	var sink = new(recordingReceiveSink)

	multi_modem_init(audioConfig, 0, 0, sink)

	for _, sam := range samples {
		multi_modem_process_sample(channel, sam)
	}

	// Enough silence afterwards for the last frame to be picked from the
	// candidates, however many slicers heard it.
	for range 2 * multiModems[channel].processAge {
		multi_modem_process_sample(channel, 0)
	}

	return sink.frames
}

func new9600TestRadioConfig(sampleRate int, profiles string, upsample int) *RadioConfig {
	var audioConfig = newTestRadioConfig(0, MODEM_SCRAMBLE, 9600, 0, 0, sampleRate)
	audioConfig.adev[0].defined = 1
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[0].profiles = profiles
	audioConfig.achan[0].upsample = upsample
	audioConfig.achan[0].fix_bits = RETRY_NONE

	return audioConfig
}

// What the tone generator sends as G3RUH at 9600 baud, the 9600 demodulator
// decodes back, however the samples are upsampled, and with one slicer or
// several.
func TestDemod9600RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sampleRate int
		profiles   string
		upsample   int
	}{
		{"single slicer, default upsample", 48000, "-", 0},
		{"multiple slicers, default upsample", 48000, "+", 0},
		{"upsample 1", 96000, "-", 1},
		{"upsample 2", 96000, "-", 2},
		{"upsample 4", 44100, "-", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var texts = []string{
				"Q1TEST>Q2TEST:Hello at 9600 baud",
				"Q1TEST>Q2TEST,WIDE1-1:>A second frame, somewhat longer than the first",
			}

			var frames = make([]*ax25.Packet, 0, len(texts))

			for _, text := range texts {
				var pp = ax25.FromText(text, true)
				require.NotNil(t, pp)

				frames = append(frames, pp)
			}

			var samples = generate9600(t, new9600TestRadioConfig(tc.sampleRate, tc.profiles, tc.upsample), 0, frames)

			var audioConfig = new9600TestRadioConfig(tc.sampleRate, tc.profiles, tc.upsample)
			var got = demodulate9600(t, audioConfig, 0, samples)

			require.Len(t, got, len(texts))

			for i, pp := range got {
				assert.Equal(t, texts[i], pp.FormatAddrs()+string(pp.Info()))
			}

			var d = demodulators[0]
			require.NotNil(t, d)

			if tc.profiles == "+" {
				assert.Equal(t, MAX_SLICERS, d.states[0].num_slicers)
			} else {
				assert.Equal(t, 1, d.states[0].num_slicers)
			}

			// The signal level seen along the way was captured for reporting.
			assert.Positive(t, d.states[0].alevel_mark_peak)
			assert.Negative(t, d.states[0].alevel_space_peak)
		})
	}
}

// Silence decodes to nothing.
func TestDemod9600Silence(t *testing.T) {
	var audioConfig = new9600TestRadioConfig(48000, "-", 0)

	var got = demodulate9600(t, audioConfig, 0, make([]int, 48000/10))

	assert.Empty(t, got)
}

// The upsampling factor is kept to what there are polyphase filters for.
func TestDemod9600InitClampsUpsample(t *testing.T) {
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

		demod_9600_init(MODEM_SCRAMBLE, 48000, tc.upsample, 9600, D)

		assert.Equal(t, MODEM_SCRAMBLE, D.modem_type)
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
	for j := range MAX_SUBCHANS {
		assert.InDelta(t, -slice_point[MAX_SUBCHANS-1-j], slice_point[j], 1e-12)
	}

	for j := 1; j < MAX_SUBCHANS; j++ {
		assert.InDelta(t, 0.02, slice_point[j]-slice_point[j-1], 1e-12)
	}
}

func TestPushSample(t *testing.T) {
	var buff = []float64{1, 2, 3, 4, 99}

	push_sample(0, buff, 4)

	assert.Equal(t, []float64{0, 1, 2, 3, 99}, buff, "only the first size elements shift, the oldest dropping off")
}

// The AGC settles a steady square wave into the range -0.5 .. +0.5, whatever
// its level and DC offset.
func TestAGCNormalises(t *testing.T) {
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
	var peak, valley, out = agc(0, 0.08, 0.00012, 0, 0)

	assert.Zero(t, peak)
	assert.Zero(t, valley)
	assert.Zero(t, out)
}
