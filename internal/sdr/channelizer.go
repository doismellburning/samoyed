// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package sdr

import (
	"fmt"
	"math"
	"math/cmplx"

	"github.com/doismellburning/samoyed/internal/dsp"
)

// passband is how far either side of a channel's frequency to keep, in Hz:
// enough for 9600 bps with 3 kHz deviation, which is the widest packet signal
// on FM.
const passband = 8500

// fullScaleDeviation is the deviation, in Hz, that comes out at half of full
// scale: louder than packet signals deviate, so they don't clip.
const fullScaleDeviation = 5000

// dcBlockCorner is the corner frequency, in Hz, of the high pass filter that
// takes out the offset a tuning error leaves in the demodulated audio.
const dcBlockCorner = 10

// renormaliseEvery is how many samples an oscillator turns through before its
// magnitude is put back to 1, against rounding errors building up.
const renormaliseEvery = 4096

// Channelizer picks each of several frequencies out of a stream of IQ samples
// and FM demodulates it, giving audio for each.
type Channelizer struct {
	channels []*channel

	// iq turns an unsigned 8 bit sample into the range -1 to 1.
	iq [256]float64

	// odd is an I sample whose Q hadn't arrived yet, when hasOdd is set.
	odd    byte
	hasOdd bool

	frame []int16
}

// channel is one frequency's path from IQ samples to audio.
type channel struct {
	// The oscillator that moves the channel's frequency down to 0 Hz.
	phasor complex128
	step   complex128
	turns  int

	first  *decimator
	second *decimator

	// The previous sample, for the FM discriminator.
	previous complex128

	// The DC blocking filter's previous input and output, and its pole.
	dcIn, dcOut, dcPole float64

	// scale turns radians per sample into audio sample values.
	scale float64
}

// NewChannelizer makes a Channelizer that takes IQ samples at sampleRate and
// gives audio at audioRate for each of offsets, in Hz from the centre.
// sampleRate has to be a multiple of audioRate.
func NewChannelizer(sampleRate int, audioRate int, offsets []float64) (*Channelizer, error) {
	if len(offsets) == 0 {
		return nil, ErrNoFrequencies
	}

	if audioRate <= 0 || sampleRate <= 0 || sampleRate%audioRate != 0 {
		return nil, fmt.Errorf("an IQ sample rate of %d doesn't decimate to an audio sample rate of %d", sampleRate, audioRate)
	}

	var c = new(Channelizer)

	for i := range c.iq {
		c.iq[i] = (float64(i) - 127.5) / 127.5
	}

	var firstFactor, secondFactor = decimationStages(sampleRate / audioRate)
	var intermediateRate = sampleRate / firstFactor
	var keep = math.Min(passband, 0.4*float64(audioRate))

	for _, offset := range offsets {
		if math.Abs(offset) >= float64(sampleRate)/2 {
			return nil, fmt.Errorf("a frequency %g Hz from the centre is outside the %d Hz sampled", offset, sampleRate)
		}

		var ch = new(channel)
		ch.phasor = 1
		ch.step = cmplx.Exp(complex(0, -2*math.Pi*offset/float64(sampleRate)))

		// The first stage only has to stop what would fold into the
		// passband when it decimates; the second sets the channel's width.
		ch.first = newDecimator(firstFactor, float64(sampleRate), float64(intermediateRate)/2, float64(intermediateRate)-2*keep)

		var stop = math.Min(float64(audioRate)-keep, 2.5*keep)
		ch.second = newDecimator(secondFactor, float64(intermediateRate), (keep+stop)/2, stop-keep)

		ch.dcPole = 1 - 2*math.Pi*dcBlockCorner/float64(audioRate)
		ch.scale = float64(audioRate) / (2 * math.Pi) * 16384 / fullScaleDeviation

		c.channels = append(c.channels, ch)
	}

	c.frame = make([]int16, len(offsets))

	return c, nil
}

// Channels is how many frequencies c gives audio for.
func (c *Channelizer) Channels() int {
	return len(c.channels)
}

// Process takes iq, unsigned 8 bit I and Q samples interleaved, and appends
// the audio they make to out: a sample for each channel in turn, a frame at a
// time.  iq can end part way through a sample; the rest comes with the next
// call.
func (c *Channelizer) Process(iq []byte, out []int16) []int16 {
	if c.hasOdd && len(iq) > 0 {
		out = c.sample(c.odd, iq[0], out)
		iq = iq[1:]
		c.hasOdd = false
	}

	for len(iq) >= 2 {
		out = c.sample(iq[0], iq[1], out)
		iq = iq[2:]
	}

	if len(iq) == 1 {
		c.odd = iq[0]
		c.hasOdd = true
	}

	return out
}

// sample takes one IQ sample, appending a frame of audio to out whenever the
// channels have decimated down to one.  Every channel decimates alike, so
// they all have a sample together.
func (c *Channelizer) sample(i byte, q byte, out []int16) []int16 {
	var x = complex(c.iq[i], c.iq[q])
	var ready = false

	for n, ch := range c.channels {
		var audio, ok = ch.sample(x)
		if ok {
			c.frame[n] = audio
			ready = true
		}
	}

	if ready {
		out = append(out, c.frame...)
	}

	return out
}

// sample takes one IQ sample, and returns an audio sample when there is one.
func (ch *channel) sample(x complex128) (int16, bool) {
	var mixed = x * ch.phasor

	ch.phasor *= ch.step

	ch.turns++
	if ch.turns == renormaliseEvery {
		ch.phasor /= complex(cmplx.Abs(ch.phasor), 0)
		ch.turns = 0
	}

	var y, ok = ch.first.push(mixed)
	if !ok {
		return 0, false
	}

	y, ok = ch.second.push(y)
	if !ok {
		return 0, false
	}

	// The FM discriminator: how far the phase turned since the last sample.
	var turned = cmplx.Phase(y * cmplx.Conj(ch.previous))
	ch.previous = y

	var dc = turned - ch.dcIn + ch.dcPole*ch.dcOut
	ch.dcIn = turned
	ch.dcOut = dc

	var audio = math.Round(dc * ch.scale)

	return int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, audio))), true
}

// decimator is a low pass filter that keeps one sample in factor.
type decimator struct {
	taps   []float64
	factor int

	// The last len(taps) samples, twice over, so that the newest len(taps)
	// are always contiguous whatever pos is.
	history []complex128
	pos     int
	phase   int
}

// newDecimator makes a decimator for samples at rate, with a cut off and the
// width of its transition band, both in Hz.
func newDecimator(factor int, rate float64, cutoff float64, transition float64) *decimator {
	// A Blackman window's transition band is about 5.5 over the number of
	// taps wide.  An odd number keeps the filter's delay a whole sample.
	var n = int(math.Ceil(5.5*rate/transition)) | 1
	n = max(n, 3)

	var d = new(decimator)
	d.factor = factor
	d.taps = make([]float64, n)
	dsp.Lowpass(cutoff/rate, d.taps, dsp.WindowBlackman)
	d.history = make([]complex128, 2*n)

	return d
}

// push adds x to the filter, and returns the filter's output when it is time
// to keep one.
func (d *decimator) push(x complex128) (complex128, bool) {
	var n = len(d.taps)

	d.history[d.pos] = x
	d.history[d.pos+n] = x

	d.pos++
	if d.pos == n {
		d.pos = 0
	}

	d.phase++
	if d.phase < d.factor {
		return 0, false
	}

	d.phase = 0

	var window = d.history[d.pos : d.pos+n]
	var re, im float64

	for k, t := range d.taps {
		re += t * real(window[k])
		im += t * imag(window[k])
	}

	return complex(re, im), true
}
