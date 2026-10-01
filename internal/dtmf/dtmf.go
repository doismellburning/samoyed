// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dtmf decodes and synthesises DTMF, the dual tones a telephone
// keypad sends and APRStt carries its messages in.
package dtmf

/*------------------------------------------------------------------
 *
 * Purpose:   	Decoder for DTMF, commonly known as "touch tones."
 *
 * Description: This uses the Goertzel Algorithm for tone detection.
 *
 * References:	http://eetimes.com/design/embedded/4024443/The-Goertzel-Algorithm
 * 		http://www.ti.com/ww/cn/uprogram/share/ppt/c5000/17dtmf_v13.ppt
 *
 * Revisions:	1.4 - Added transmit capability.
 *
 *---------------------------------------------------------------*/

import (
	"iter"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/sirupsen/logrus"
)

// TimeoutSec is how long a Decoder waits, after the last button, before it
// reports inactivity.
const TimeoutSec = 5 /* for normal operation. */

const numTones = 8

// An Event is what a Decoder makes of one sample.
type Event int

const (
	// NoEvent is a sample part way through a block: nothing to say yet.
	NoEvent Event = iota

	// Idle is a block that ended with no new button.
	Idle

	// Pressed is a block that ended with a new button press.
	Pressed

	// TimedOut is TimeoutSec of Idle after the last button.
	TimedOut
)

func (e Event) String() string {
	switch e {
	case NoEvent:
		return "NoEvent"
	case Idle:
		return "Idle"
	case Pressed:
		return "Pressed"
	case TimedOut:
		return "TimedOut"
	}

	return "Event(" + strconv.Itoa(int(e)) + ")"
}

// keys is the keypad, a row at a time: the button at row r and column c
// is keys[r*4+c].
const keys = "123A456B789C*0#D"

// tones are the frequencies, in Hz, of the four keypad rows and then the
// four columns.
func tones() [numTones]int {
	return [numTones]int{697, 770, 852, 941, 1209, 1336, 1477, 1633}
}

// A Decoder is the current state of the DTMF decoding for one audio
// channel.
//
// It has no lock, so only one goroutine may drive it.
type Decoder struct {
	dcd func(on bool)

	sampleRate int /* Samples per sec.  Typ. 44100, 8000, etc. */
	blockSize  int /* Number of samples to process in one block. */
	coef       [numTones]float64

	n             int /* Samples processed in this block. */
	q1            [numTones]float64
	q2            [numTones]float64
	prevDec       rune
	debounced     rune
	prevDebounced rune
	timeout       int
}

/*------------------------------------------------------------------
 *
 * Name:        NewDecoder
 *
 * Purpose:     Initialize the DTMF decoder for one audio channel.
 *
 * Inputs:	sampleRate	- Audio sample frequency, typically
 *				  44100, 22050, 8000, etc.
 *
 *				  This is associated with the soundcard.
 *				  In version 1.2, we can have multiple soundcards
 *				  with potentially different sample rates.
 *
 *		dcd		- Told, after each block of samples that
 *				  agrees with the one before, whether a
 *				  button is being held: the Data Carrier
 *				  Detect indicator.  May be nil.
 *
 *----------------------------------------------------------------*/

func NewDecoder(sampleRate int, dcd func(on bool)) *Decoder {
	logrus.WithField("sampleRate", sampleRate).Debug("dtmf.NewDecoder")

	var d = new(Decoder)

	d.dcd = dcd
	d.sampleRate = sampleRate
	d.prevDec = ' '
	d.debounced = ' '
	d.prevDebounced = ' '

	/*
	 * Pick a suitable processing block size.
	 * Larger = narrower bandwidth, slower response.
	 */
	d.blockSize = (205 * sampleRate) / 8000

	for j, tone := range tones() {
		// Why do some insist on rounding k to the nearest integer?
		// That would move the filter center frequency away from ideal.
		// What is to be gained?
		// More consistent results for all the tones when k is not rounded off.
		var k = float64(d.blockSize) * float64(tone) / float64(sampleRate)

		d.coef[j] = 2.0 * math.Cos(2.0*math.Pi*k/float64(d.blockSize))

		dwutil.Assert(d.coef[j] > 0.0 && d.coef[j] < 2.0)
		logrus.WithFields(logrus.Fields{
			"freq": tone,
			"k":    k,
			"coef": d.coef[j],
		}).Debug("DTMF tone filter")
	}

	return d
}

/*------------------------------------------------------------------
 *
 * Name:        Sample
 *
 * Purpose:     Process one audio sample from the sound input source.
 *
 * Inputs:	input	- Audio sample.
 *
 * Returns:     The Event, and for Pressed the button: one of
 *		0123456789ABCD*#.
 *
 *----------------------------------------------------------------*/

func (d *Decoder) Sample(input float64) (Event, rune) {
	for i := range numTones {
		var q0 = input + d.q1[i]*d.coef[i] - d.q2[i]
		d.q2[i] = d.q1[i]
		d.q1[i] = q0
	}

	/*
	 * Is it time to process the block?
	 */
	d.n++
	if d.n != d.blockSize {
		return NoEvent, 0
	}

	var output [numTones]float64

	for i := range numTones {
		output[i] = math.Sqrt(d.q1[i]*d.q1[i] + d.q2[i]*d.q2[i] - d.q1[i]*d.q2[i]*d.coef[i])
		d.q1[i] = 0
		d.q2[i] = 0
	}

	d.n = 0

	/*
	 * The input signal can vary over a couple orders of
	 * magnitude so we can't set some absolute threshold.
	 *
	 * See if one tone is stronger than the sum of the
	 * others in the same group multiplied by some factor.
	 *
	 * For perfect synthetic signals this needs to be in
	 * the range of about 1.33 (very sensitive) to 2.15 (very fussy).
	 *
	 * Too low will cause false triggers on random noise.
	 * Too high will won't decode less than perfect signals.
	 *
	 * Use the mid point 1.74 as our initial guess.
	 * It might need some fine tuning for imperfect real world signals.
	 */

	const THRESHOLD = 1.74

	var row, col int

	if output[0] > THRESHOLD*(output[1]+output[2]+output[3]) {
		row = 0
	} else if output[1] > THRESHOLD*(output[0]+output[2]+output[3]) {
		row = 1
	} else if output[2] > THRESHOLD*(output[0]+output[1]+output[3]) {
		row = 2
	} else if output[3] > THRESHOLD*(output[0]+output[1]+output[2]) {
		row = 3
	} else {
		row = -1
	}

	if output[4] > THRESHOLD*(output[5]+output[6]+output[7]) {
		col = 0
	} else if output[5] > THRESHOLD*(output[4]+output[6]+output[7]) {
		col = 1
	} else if output[6] > THRESHOLD*(output[4]+output[5]+output[7]) {
		col = 2
	} else if output[7] > THRESHOLD*(output[4]+output[5]+output[6]) {
		col = 3
	} else {
		col = -1
	}

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithField("output", output).Trace("dtmf.Decoder.Sample tone outputs")
	}

	var decoded = ' '

	if row >= 0 && col >= 0 {
		decoded = rune(keys[row*4+col])
	}

	// Consider valid only if we get same twice in a row.

	if decoded == d.prevDec {
		d.debounced = decoded

		// Update Data Carrier Detect Indicator.
		if d.dcd != nil {
			d.dcd(decoded != ' ')
		}

		/* Reset timeout timer. */
		if decoded != ' ' {
			d.timeout = ((TimeoutSec) * d.sampleRate) / d.blockSize
		}
	}

	d.prevDec = decoded

	// Return only new button pushes.
	// Also report timeout after period of inactivity.

	var event = Idle

	var button rune

	if d.debounced != d.prevDebounced {
		if d.debounced != ' ' {
			event = Pressed
			button = d.debounced
		}
	}

	if event == Idle {
		if d.timeout > 0 {
			d.timeout--
			if d.timeout == 0 {
				event = TimedOut
			}
		}
	}

	d.prevDebounced = d.debounced

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"dec":     string(decoded),
			"deb":     string(d.debounced),
			"event":   event,
			"button":  string(button),
			"timeout": d.timeout,
		}).Trace("dtmf.Decoder.Sample")
	}

	return event, button
}

// buttonSamples is ms milliseconds of audio at sampleRate for button: the
// mean of its two sine waves, so in the range +-1.0, or silence for anything
// that isn't a button.
func buttonSamples(button rune, ms int, sampleRate int) iter.Seq[float64] {
	return func(yield func(float64) bool) {
		var fa, fb int

		var i = strings.IndexRune(keys, unicode.ToUpper(button))
		if i >= 0 {
			var tones = tones()

			fa = tones[i/4]
			fb = tones[4+i%4]
		}

		var phasea, phaseb float64

		for range (ms * sampleRate) / 1000 {
			// This could be more efficient with a precomputed sine wave table
			// but I'm not that worried about it.
			// With a Raspberry Pi, model 2, default 1200 receiving takes about 14% of one CPU core.
			// When transmitting tones, it briefly shoots up to about 33%.
			var dtmf float64 // Audio.  Mean of two sine waves.

			if fa > 0 && fb > 0 {
				dtmf = (math.Sin(phasea) + math.Sin(phaseb)) / 2
				phasea += 2.0 * math.Pi * float64(fa) / float64(sampleRate)
				phaseb += 2.0 * math.Pi * float64(fb) / float64(sampleRate)
			}

			if !yield(dtmf) {
				return
			}
		}
	}
}

// An Output is where a Sender puts its tones: in samoyed-direwolf, the
// channel's tone generator.
type Output interface {
	// SampleRate is how many audio samples a second the output plays.
	SampleRate() int

	// PutLevel puts out one sample, in the range +-1.0, scaled to the
	// output's own amplitude.
	PutLevel(level float64)

	// Flush sends on whatever the output has buffered.
	Flush()
}

// A Sender turns text into DTMF tones on an Output.
type Sender struct {
	output Output
}

// NewSender returns a Sender that puts its tones on output.
func NewSender(output Output) *Sender {
	var s = new(Sender)

	s.output = output

	return s
}

/*-------------------------------------------------------------------
 *
 * Name:        Send
 *
 * Purpose:    	Generate DTMF tones from text string.
 *
 * Inputs:	str	- Character string to send.  0-9, A-D, *, #
 *		speed	- Number of tones per second.  Range 1 to 10.
 *		txdelay	- Delay (ms) from PTT to start.
 *		txtail	- Delay (ms) from end to PTT off.
 *
 * Outputs:	txdelay of silence, each button followed by a gap as
 *		long as it, then txtail of silence, flushed out once
 *		at the end.  Duration says how long that lasts.
 *
 *--------------------------------------------------------------------*/

func (s *Sender) Send(str string, speed int, txdelay int, txtail int) {
	for level := range samples(str, speed, txdelay, txtail, s.output.SampleRate()) {
		s.output.PutLevel(level)
	}

	s.output.Flush()
}

// samples is the whole transmission Send makes, as buttonSamples describes.
func samples(str string, speed int, txdelay int, txtail int, sampleRate int) iter.Seq[float64] {
	return func(yield func(float64) bool) {
		// Length of tone or gap between.
		var lenMs = int((500.0 / float64(speed)) + 0.5)

		var push = func(button rune, ms int) bool {
			for sample := range buttonSamples(button, ms, sampleRate) {
				if !yield(sample) {
					return false
				}
			}

			return true
		}

		if !push(' ', txdelay) {
			return
		}

		for _, p := range str {
			if !push(p, lenMs) || !push(' ', lenMs) {
				return
			}
		}

		push(' ', txtail)
	}
}

// Duration is how long, in milliseconds, the transmission Send makes of the
// same arguments lasts: how long to hold the PTT for it.
func Duration(str string, speed int, txdelay int, txtail int) int {
	return (txdelay +
		int(1000.0*float64(utf8.RuneCountInString(str))/float64(speed)+0.5) +
		txtail)
}
