package direwolf

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
	"strings"
	"unicode"

	"github.com/sirupsen/logrus"
)

const DTMF_TIMEOUT_SEC = 5 /* for normal operation. */

const NUM_TONES = 8

// dtmfKeys is the keypad, a row at a time: the button at row r and column c
// is dtmfKeys[r*4+c].
const dtmfKeys = "123A456B789C*0#D"

// dtmfTones are the frequencies, in Hz, of the four keypad rows and then the
// four columns.
func dtmfTones() [NUM_TONES]int {
	return [NUM_TONES]int{697, 770, 852, 941, 1209, 1336, 1477, 1633}
}

// A DTMFDecoder is the current state of the DTMF decoding for one radio
// channel.
//
// Only the receive goroutine for the channel's audio device drives it, so it
// needs no lock.
type DTMFDecoder struct {
	channel int

	sampleRate int /* Samples per sec.  Typ. 44100, 8000, etc. */
	blockSize  int /* Number of samples to process in one block. */
	coef       [NUM_TONES]float64

	n             int /* Samples processed in this block. */
	q1            [NUM_TONES]float64
	q2            [NUM_TONES]float64
	prevDec       rune
	debounced     rune
	prevDebounced rune
	timeout       int
}

/*------------------------------------------------------------------
 *
 * Name:        NewDTMFDecoder
 *
 * Purpose:     Initialize the DTMF decoder for one radio channel.
 *
 * Inputs:      channel		- Radio channel number, for the DCD indicator.
 *
 *		sampleRate	- Audio sample frequency, typically
 *				  44100, 22050, 8000, etc.
 *
 *				  This is associated with the soundcard.
 *				  In version 1.2, we can have multiple soundcards
 *				  with potentially different sample rates.
 *
 *----------------------------------------------------------------*/

func NewDTMFDecoder(channel int, sampleRate int) *DTMFDecoder {
	logrus.WithField("channel", channel).Debug("NewDTMFDecoder")

	var d = new(DTMFDecoder)

	d.channel = channel
	d.sampleRate = sampleRate
	d.prevDec = ' '
	d.debounced = ' '
	d.prevDebounced = ' '

	/*
	 * Pick a suitable processing block size.
	 * Larger = narrower bandwidth, slower response.
	 */
	d.blockSize = (205 * sampleRate) / 8000

	for j, tone := range dtmfTones() {
		// Why do some insist on rounding k to the nearest integer?
		// That would move the filter center frequency away from ideal.
		// What is to be gained?
		// More consistent results for all the tones when k is not rounded off.
		var k = float64(d.blockSize) * float64(tone) / float64(sampleRate)

		d.coef[j] = 2.0 * math.Cos(2.0*math.Pi*k/float64(d.blockSize))

		Assert(d.coef[j] > 0.0 && d.coef[j] < 2.0)
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
 * Returns:     0123456789ABCD*# for a button push.
 *		. for nothing happening during sample interval.
 *		$ after several seconds of inactivity.
 *		space between sample intervals.
 *
 *
 *----------------------------------------------------------------*/

func (d *DTMFDecoder) Sample(input float64) rune {
	for i := range NUM_TONES {
		var q0 = input + d.q1[i]*d.coef[i] - d.q2[i]
		d.q2[i] = d.q1[i]
		d.q1[i] = q0
	}

	/*
	 * Is it time to process the block?
	 */
	d.n++
	if d.n != d.blockSize {
		return ' '
	}

	var output [NUM_TONES]float64

	for i := range NUM_TONES {
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
		logrus.WithField("output", output).Trace("DTMFDecoder.Sample tone outputs")
	}

	var decoded = ' '

	if row >= 0 && col >= 0 {
		decoded = rune(dtmfKeys[row*4+col])
	}

	// Consider valid only if we get same twice in a row.

	if decoded == d.prevDec {
		d.debounced = decoded

		// Update Data Carrier Detect Indicator.
		var _tmpIntBool = 0
		if decoded != ' ' {
			_tmpIntBool = 1
		}

		hdlcReceiver.DCDChange(d.channel, MAX_SUBCHANS, 0, _tmpIntBool)

		/* Reset timeout timer. */
		if decoded != ' ' {
			d.timeout = ((DTMF_TIMEOUT_SEC) * d.sampleRate) / d.blockSize
		}
	}

	d.prevDec = decoded

	// Return only new button pushes.
	// Also report timeout after period of inactivity.

	var ret = '.'

	if d.debounced != d.prevDebounced {
		if d.debounced != ' ' {
			ret = d.debounced
		}
	}

	if ret == '.' {
		if d.timeout > 0 {
			d.timeout--
			if d.timeout == 0 {
				ret = '$'
			}
		}
	}

	d.prevDebounced = d.debounced

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		logrus.WithFields(logrus.Fields{
			"dec":     string(decoded),
			"deb":     string(d.debounced),
			"ret":     string(ret),
			"timeout": d.timeout,
		}).Trace("DTMFDecoder.Sample")
	}

	return ret
}

/*-------------------------------------------------------------------
 *
 * Name:        dtmf_send
 *
 * Purpose:    	Generate DTMF tones from text string.
 *
 * Inputs:	channel	- Radio channel number.
 *		str	- Character string to send.  0-9, A-D, *, #
 *		speed	- Number of tones per second.  Range 1 to 10.
 *		txdelay	- Delay (ms) from PTT to start.
 *		txtail	- Delay (ms) from end to PTT off.
 *
 * Returns:	Total number of milliseconds to activate PTT.
 *		This includes delays before the first tone
 *		and after the last to avoid chopping off part of it.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send DTMF.
 *
 *--------------------------------------------------------------------*/

func dtmf_send(channel int, str string, speed int, txdelay int, txtail int) int {
	if toneGenerators[channel] == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for tone generation.\n", channel)
	} else {
		toneGenerators[channel].SendDTMF(str, speed, txdelay, txtail)
	}

	return (txdelay +
		int(1000.0*float64(len(str))/float64(speed)+0.5) +
		txtail)
} /* end dtmf_send */

// SendDTMF generates the tones for str, as dtmf_send describes, on the
// generator's channel.
func (tg *ToneGenerator) SendDTMF(str string, speed int, txdelay int, txtail int) {
	// Length of tone or gap between.
	var len_ms = int((500.0 / float64(speed)) + 0.5)

	tg.pushButton(' ', txdelay)

	for _, p := range str {
		tg.pushButton(p, len_ms)
		tg.pushButton(' ', len_ms)
	}

	tg.pushButton(' ', txtail)

	tg.Flush()
}

/*------------------------------------------------------------------
 *
 * Name:        pushButton
 *
 * Purpose:     Generate DTMF tone for a button push.
 *
 * Inputs:	button	- One of 0-9, A-D, *, #.  Others result in silence.
 *
 *		ms	- Duration in milliseconds.
 *			  Use 50 ms for tone and 50 ms of silence for max rate of 10 per second.
 *
 * Outputs:	Audio is sent to radio.
 *
 *----------------------------------------------------------------*/

func (tg *ToneGenerator) pushButton(button rune, ms int) {
	var sampleRate = tg.audioConfig.adev[tg.adevIndex].samples_per_sec

	for dtmf := range dtmfButtonSamples(button, ms, sampleRate) {
		// 'dtmf' can be in range of +-2.0 because it is sum of two sine waves.
		// Amplitude of 100 would use full +-32k range.
		tg.PutSample(int(dtmf * 16383.0 * float64(tg.amplitude) / 100.0))
	}
}

// dtmfButtonSamples is ms milliseconds of audio at sampleRate for button: the
// sum of its two sine waves, so in the range +-2.0, or silence for anything
// that isn't a button.
func dtmfButtonSamples(button rune, ms int, sampleRate int) iter.Seq[float64] {
	return func(yield func(float64) bool) {
		var fa, fb int

		var i = strings.IndexRune(dtmfKeys, unicode.ToUpper(button))
		if i >= 0 {
			var tones = dtmfTones()

			fa = tones[i/4]
			fb = tones[4+i%4]
		}

		var phasea, phaseb float64

		for range (ms * sampleRate) / 1000 {
			// This could be more efficient with a precomputed sine wave table
			// but I'm not that worried about it.
			// With a Raspberry Pi, model 2, default 1200 receiving takes about 14% of one CPU core.
			// When transmitting tones, it briefly shoots up to about 33%.
			var dtmf float64 // Audio.  Sum of two sine waves.

			if fa > 0 && fb > 0 {
				dtmf = math.Sin(phasea) + math.Sin(phaseb)
				phasea += 2.0 * math.Pi * float64(fa) / float64(sampleRate)
				phaseb += 2.0 * math.Pi * float64(fb) / float64(sampleRate)
			}

			if !yield(dtmf) {
				return
			}
		}
	}
}
