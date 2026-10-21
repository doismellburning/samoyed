// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// byteSink is an AudioSink that keeps what it is given, and counts flushes.
type byteSink struct {
	data    []byte
	flushes int
}

func (s *byteSink) Put(_ int, c uint8) int {
	s.data = append(s.data, c)

	return 0
}

func (s *byteSink) Flush(int) int {
	s.flushes++

	return 0
}

// The configuration samoyed-gen_tone plays its tones with sets up a
// transmitting device, and a tone generator for every channel on it, mono
// and stereo alike.
func TestGenToneTestConfigGeneratesEveryChannel(t *testing.T) {
	t.Parallel()

	for _, numChannels := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d channels", numChannels), func(t *testing.T) {
			t.Parallel()

			var config = NewGenToneTestConfig(numChannels)

			// AudioOpen only sets up an output buffer for a defined device.
			assert.NotZero(t, config.adev[0].defined)

			var sink = new(byteSink)

			var toneGenerators = NewToneGenerators(config, 100, sink)

			var bytesPerFrame = config.adev[0].num_channels * config.adev[0].bits_per_sample / 8

			for channel := range numChannels {
				require.NotNil(t, toneGenerators[channel], "channel %d has no tone generator", channel)
				assert.Same(t, config, toneGenerators[channel].audioConfig)

				sink.data = nil

				// One second of bits is one second of audio.
				for range config.achan[channel].baud {
					toneGenerators[channel].PutBit(1)
				}

				assert.InDelta(t, config.adev[0].samples_per_sec*bytesPerFrame, len(sink.data), float64(bytesPerFrame))
			}
		})
	}
}

// putCountingSink counts the bytes put to it.
type putCountingSink struct {
	puts int
}

func (s *putCountingSink) Put(int, uint8) int {
	s.puts++

	return 0
}

func (s *putCountingSink) Flush(int) int { return 0 }

// A channel the tone generator has no way to modulate for is an internal
// error, which the transmitter is told of once and otherwise carries on past,
// sending nothing.  It runs mid-transmission, with the PTT keyed, so ending
// the process there would leave the transmitter keyed.
//
//nolint:paralleltest // Serial: captures os.Stdout and changes logrus's global logger.
func TestPutBitWithUnknownModemSendsNothing(t *testing.T) {
	var sink = new(putCountingSink)
	var audioConfig = newTestRadioConfig(0, MODEM_OFF, 1200, 1200, 2200, 44100)
	var tg = NewToneGenerator(0, audioConfig, 100, sink)

	var output = testutils.CaptureOutput(t, func() {
		var oldLogOutput = logrus.StandardLogger().Out
		logrus.SetOutput(os.Stdout)

		defer logrus.SetOutput(oldLogOutput)

		tg.PutBit(1)
		tg.PutBit(0)
	})

	assert.Equal(t, 0, sink.puts, "nothing should have been sent")
	assert.Equal(t, 1, strings.Count(output, "Internal error: tone generator can't modulate"), "the error should be reported once: %s", output)
}

// At a sample rate that isn't a multiple of 1000, the sample count for a
// quiet period, such as Morse's txdelay and txtail, must not be truncated by
// integer division (issue #762).
func TestPutQuietMsRoundsAt44100(t *testing.T) {
	t.Parallel()

	var sink = new(byteSink)
	var audioConfig = newTestRadioConfig(0, MODEM_AFSK, 1200, 1200, 2200, 44100)
	var tg = NewToneGenerator(0, audioConfig, 50, sink)

	// 5 ms is 220.5 samples, which rounds to 221.
	tg.PutQuietMs(5)
	assert.Equal(t, 221, len(sink.data)/2)
}

// A BPSK phase reversal is shaped rather than jumped, so the waveform never
// steps further between samples than the carrier's own slew plus the
// envelope's, which sweeps half a cosine cycle over half a symbol.  An
// instant 180 degree jump would step by up to twice the amplitude.
func TestBPSKReversalsHaveNoDiscontinuity(t *testing.T) {
	t.Parallel()

	for _, baud := range []int{300, 1200} {
		t.Run(strconv.Itoa(baud), func(t *testing.T) {
			t.Parallel()

			var samplesPerSec = 48000
			var tg, sink, cfg = newCapturingToneGenerator(MODEM_BPSK, baud, samplesPerSec)

			// A mix of bits, so reversals follow steady symbols as well as
			// each other.
			var rng = rand.New(rand.NewPCG(3, 4))
			for range 64 {
				tg.PutBit(rng.IntN(2))
			}

			var samples = decodeMono16(sink.data)
			require.NotEmpty(t, samples)

			var carrier = float64(cfg.achan[0].mark_freq)
			var slew = 2*math.Pi*carrier/float64(samplesPerSec) + 2*math.Pi*float64(baud)/float64(samplesPerSec)
			var limit = 1.2 * 32767 * slew

			for i := 1; i < len(samples); i++ {
				var step = math.Abs(float64(samples[i]) - float64(samples[i-1]))
				require.LessOrEqual(t, step, limit, "step at sample %d", i)
			}
		})
	}
}

// Shaped BPSK keeps its power near the carrier: little of it should land
// more than two baud away, where unshaped symbols put several percent.
func TestBPSKSpectrumIsContained(t *testing.T) {
	t.Parallel()

	var samplesPerSec = 8000
	var baud = 300
	var tg, sink, cfg = newCapturingToneGenerator(MODEM_BPSK, baud, samplesPerSec)

	var rng = rand.New(rand.NewPCG(1, 2))
	for range baud {
		tg.PutBit(rng.IntN(2))
	}

	var samples = decodeMono16(sink.data)
	var n = len(samples)

	// Hann window, so the abrupt start and end of the burst don't smear
	// power across the spectrum themselves.
	var x = make([]float64, n)
	for i, s := range samples {
		x[i] = float64(s) * 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}

	var cosTable = make([]float64, n)
	var sinTable = make([]float64, n)

	for i := range n {
		cosTable[i] = math.Cos(2 * math.Pi * float64(i) / float64(n))
		sinTable[i] = math.Sin(2 * math.Pi * float64(i) / float64(n))
	}

	var carrier = float64(cfg.achan[0].mark_freq)
	var total, outside float64

	for k := range n / 2 {
		var re, im float64

		for i := range n {
			var j = (k * i) % n
			re += x[i] * cosTable[j]
			im -= x[i] * sinTable[j]
		}

		var power = re*re + im*im
		var freq = float64(k) * float64(samplesPerSec) / float64(n)

		total += power

		if math.Abs(freq-carrier) > 2*float64(baud) {
			outside += power
		}
	}

	var ratioDB = 10 * math.Log10(outside/total)
	t.Logf("power more than %d Hz from the carrier: %.1f dB", 2*baud, ratioDB)
	assert.Less(t, ratioDB, -30.0)
}

// goertzelPower is the power in samples at freq, for a signal sampled at
// samplesPerSec.
func goertzelPower(samples []int16, freq float64, samplesPerSec int) float64 {
	var coeff = 2 * math.Cos(2*math.Pi*freq/float64(samplesPerSec))
	var s1, s2 float64

	for _, x := range samples {
		var s0 = float64(x) + coeff*s1 - s2
		s2 = s1
		s1 = s0
	}

	return s1*s1 + s2*s2 - coeff*s1*s2
}

// BPSK's carrier is the 1500 Hz the NinoTNC's PSK modes use, at every bit
// rate.
func TestBPSKCarrierIsNinoTNCs(t *testing.T) {
	t.Parallel()

	for _, baud := range []int{300, 1200} {
		t.Run(strconv.Itoa(baud), func(t *testing.T) {
			t.Parallel()

			var samplesPerSec = 48000
			var tg, sink, cfg = newCapturingToneGenerator(MODEM_BPSK, baud, samplesPerSec)
			cfg.achan[0].layer2_xmit = LAYER2_IL2P // Bits as they are, not scrambled.

			// No reversals: a steady carrier.
			for range baud / 10 {
				tg.PutBit(1)
			}

			var samples = decodeMono16(sink.data)

			assert.Greater(t, goertzelPower(samples, 1500, samplesPerSec), 100*goertzelPower(samples, 1800, samplesPerSec))
		})
	}
}

// BPSK sends IL2P as the NinoTNC does: the phase reverses for a 0 bit, and
// holds for a 1.
func TestBPSKIL2PReversesOnZero(t *testing.T) {
	t.Parallel()

	var tg, _, cfg = newCapturingToneGenerator(MODEM_BPSK, 300, 48000)
	cfg.achan[0].layer2_xmit = LAYER2_IL2P

	for _, bit := range []int{0, 1, 1, 0, 0, 1, 0} {
		var before = tg.bpskSign

		tg.PutBit(bit)

		assert.Equal(t, bit == 0, tg.bpskSign != before, "bit %d", bit)
	}
}
