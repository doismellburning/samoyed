// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Len(t, afskSpaceGain, MAX_SLICERS)
	assert.InDelta(t, MIN_G, afskSpaceGain[0], 1e-12)
	assert.InDelta(t, MAX_G, afskSpaceGain[MAX_SLICERS-1], 1e-9)

	var ratio = afskSpaceGain[1] / afskSpaceGain[0]
	for j := 2; j < MAX_SLICERS; j++ {
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
			demod_9600_init(MODEM_SCRAMBLE, 48000, 1, 9600, new(demodulator_state_s))
		})
	}

	wg.Wait()
}

// afskTestCase is an AFSK modem set up one of the ways the round trip tests
// try.
type afskTestCase struct {
	name       string
	baud       int
	mark       int
	space      int
	sampleRate int
	profiles   string
	decimate   int // 0 lets NewDemodulator choose.
}

func (tc afskTestCase) radioConfig() *RadioConfig {
	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, tc.baud, tc.mark, tc.space, tc.sampleRate)
	audioConfig.adev[0].defined = 1
	audioConfig.achan[0].num_freq = 1
	audioConfig.achan[0].profiles = tc.profiles
	audioConfig.achan[0].decimate = tc.decimate
	audioConfig.achan[0].fix_bits = RETRY_NONE

	return audioConfig
}

// afskTestFrames is count frames, each saying which it is.
func afskTestFrames(t *testing.T, count int) ([]string, []*ax25.Packet) {
	t.Helper()

	var texts = make([]string, 0, count)
	var frames = make([]*ax25.Packet, 0, count)

	for i := range count {
		var text = fmt.Sprintf("Q1TEST>Q2TEST,WIDE1-1:>Frame %02d, long enough for the noise to have a go at it", i)

		var pp = ax25.FromText(text, true)
		require.NotNil(t, pp)

		texts = append(texts, text)
		frames = append(frames, pp)
	}

	return texts, frames
}

// What the tone generator sends as AFSK, each demodulator profile decodes
// back, with one slicer or several, at the common sample rates, and when the
// audio is decimated before demodulating - asked for, or chosen for 300 baud.
func TestDemodAFSKRoundTrip(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		afskTestCase

		wantSlicers  int
		wantDecimate int
	}{
		{afskTestCase{"A, single slicer", 1200, 1200, 2200, 44100, "A-", 0}, 1, 1},
		{afskTestCase{"A, multiple slicers", 1200, 1200, 2200, 44100, "A+", 0}, MAX_SLICERS, 1},
		{afskTestCase{"B, single slicer", 1200, 1200, 2200, 44100, "B-", 0}, 1, 3},
		{afskTestCase{"B, multiple slicers", 1200, 1200, 2200, 44100, "B+", 0}, MAX_SLICERS, 3},
		{afskTestCase{"A at 48kHz", 1200, 1200, 2200, 48000, "A-", 0}, 1, 1},
		{afskTestCase{"A at 48kHz, decimated", 1200, 1200, 2200, 48000, "A-", 3}, 1, 3},
		{afskTestCase{"A at 300 baud", 300, 1600, 1800, 48000, "A-", 0}, 1, 3},
		{afskTestCase{"B at 300 baud", 300, 1600, 1800, 48000, "B-", 0}, 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var texts, frames = afskTestFrames(t, 2)

			var samples = generateFrames(t, tc.radioConfig(), frames)

			var got, receiver = demodulateFrames(t, tc.radioConfig(), samples)

			require.Len(t, got, len(texts))

			for i, pp := range got {
				assert.Equal(t, texts[i], pp.FormatAddrs()+string(pp.Info()))
			}

			var d = receiver.demods[0]
			require.NotNil(t, d)

			assert.Equal(t, tc.wantSlicers, d.states[0].num_slicers)
			assert.Equal(t, tc.wantDecimate, d.decimate)
		})
	}
}

// Near where noise starts to win, each way of demodulating still gets at
// least as many frames through as it did when this test was written.  The
// noise is the same every run, so the counts are too: a change to the
// demodulator that should leave its output alone, such as rearranging a
// filter's arithmetic for speed, keeps them, and one that loses frames fails
// here.  One that gets more through can raise the count.
func TestDemodAFSKInNoise(t *testing.T) {
	t.Parallel()

	const sent = 20

	for _, tc := range []struct {
		afskTestCase

		noise float64 // Standard deviation, against a tone generator amplitude of 50.
		least int
	}{
		{afskTestCase{"A, single slicer", 1200, 1200, 2200, 44100, "A-", 0}, 14000, 14},
		{afskTestCase{"A, multiple slicers", 1200, 1200, 2200, 44100, "A+", 0}, 14000, 16},
		{afskTestCase{"B, single slicer", 1200, 1200, 2200, 44100, "B-", 0}, 14000, 13},
		{afskTestCase{"B, multiple slicers", 1200, 1200, 2200, 44100, "B+", 0}, 14000, 15},
		{afskTestCase{"A at 48kHz", 1200, 1200, 2200, 48000, "A-", 0}, 14000, 18},
		{afskTestCase{"A at 48kHz, decimated", 1200, 1200, 2200, 48000, "A-", 3}, 14000, 17},
		{afskTestCase{"A at 300 baud", 300, 1600, 1800, 48000, "A-", 0}, 30000, 17},
		{afskTestCase{"B at 300 baud", 300, 1600, 1800, 48000, "B-", 0}, 30000, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var texts, frames = afskTestFrames(t, sent)

			var samples = generateFrames(t, tc.radioConfig(), frames)

			var rng = rand.New(rand.NewPCG(1, 2))
			for i := range samples {
				samples[i] += int(rng.NormFloat64() * tc.noise)
			}

			var got, _ = demodulateFrames(t, tc.radioConfig(), samples)

			t.Logf("decoded %d of %d", len(got), sent)

			assert.GreaterOrEqual(t, len(got), tc.least)
			assert.Less(t, len(got), sent, "the noise is meant to cost some frames")

			// Whatever got through is what was sent: the noise never forges a frame.
			for _, pp := range got {
				assert.Contains(t, texts, pp.FormatAddrs()+string(pp.Info()))
			}
		})
	}
}
