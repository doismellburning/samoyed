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

// #include <stdlib.h>
// #include <stdio.h>
// #include <math.h>
// #include <assert.h>
// #include <string.h>
import "C"

import (
	"math"
	"os"
)

const DTMF_TIMEOUT_SEC = 5 /* for normal operation. */

const NUM_TONES = 8

var DTMF_TONES = [NUM_TONES]int{697, 770, 852, 941, 1209, 1336, 1477, 1633}

/*
 * Current state of the DTMF decoding.
 */

type dd_s struct { /* Separate for each audio channel. */

	sample_rate int /* Samples per sec.  Typ. 44100, 8000, etc. */
	block_size  int /* Number of samples to process in one block. */
	coef        [NUM_TONES]float64

	n              int /* Samples processed in this block. */
	Q1             [NUM_TONES]float64
	Q2             [NUM_TONES]float64
	prev_dec       rune
	debounced      rune
	prev_debounced rune
	timeout        int
}

var dd [MAX_RADIO_CHANS]dd_s

var s_amplitude int = 100 // range of 0 .. 100

/*------------------------------------------------------------------
 *
 * Name:        dtmf_init
 *
 * Purpose:     Initialize the DTMF decoder.
 *		This should be called once at application start up time.
 *
 * Inputs:      p_audio_config - Configuration for audio interfaces.
 *
 *			All we care about is:
 *
 *				samples_per_sec - Audio sample frequency, typically
 *				  		44100, 22050, 8000, etc.
 *
 *			This is a associated with the soundcard.
 *			In version 1.2, we can have multiple soundcards
 *			with potentially different sample rates.
 *
 *		amp		- Signal amplitude, for transmit, on scale of 0 .. 100.
 *
 *				  100 will produce maximum amplitude of +-32k samples.
 *
 * Returns:     None.
 *
 *----------------------------------------------------------------*/

func dtmf_init(p_audio_config *audio_s, amp int) {

	s_amplitude = amp

	/*
	 * Pick a suitable processing block size.
	 * Larger = narrower bandwidth, slower response.
	 */

	for c := int(0); c < MAX_RADIO_CHANS; c++ {
		var D = &(dd[c])
		var a = ACHAN2ADEV(c)

		D.sample_rate = p_audio_config.adev[a].samples_per_sec

		if p_audio_config.achan[c].dtmf_decode != DTMF_DECODE_OFF {

			/* TODO KG
			#if DEBUG
				    text_color_set(DW_COLOR_DEBUG);
				    dw_printf ("channel %d:\n", c);
			#endif
			*/
			D.block_size = (205 * D.sample_rate) / 8000

			/* TODO KG
			#if DEBUG
				    dw_printf ("    freq      k     coef    \n");
			#endif
			*/
			for j := 0; j < NUM_TONES; j++ {

				// Why do some insist on rounding k to the nearest integer?
				// That would move the filter center frequency away from ideal.
				// What is to be gained?
				// More consistent results for all the tones when k is not rounded off.

				var k = float64(D.block_size) * float64(DTMF_TONES[j]) / float64(D.sample_rate)

				D.coef[j] = float64(2.0 * math.Cos(2.0*math.Pi*float64(k)/float64(D.block_size)))

				Assert(D.coef[j] > 0.0 && D.coef[j] < 2.0)
				/* TODO KG
				#if DEBUG
					      dw_printf ("%8d   %5.1f   %8.5f  \n", DTMF_TONES[j], k, D.coef[j]);
				#endif
				*/
			}
		}
	}

	for c := 0; c < MAX_RADIO_CHANS; c++ {
		var D = &(dd[c])
		D.n = 0
		for j := 0; j < NUM_TONES; j++ {
			D.Q1[j] = 0
			D.Q2[j] = 0
		}
		D.prev_dec = ' '
		D.debounced = ' '
		D.prev_debounced = ' '
		D.timeout = 0
	}

}

/*------------------------------------------------------------------
 *
 * Name:        dtmf_sample
 *
 * Purpose:     Process one audio sample from the sound input source.
 *
 * Inputs:	c	- Audio channel number.
 *			  This can process multiple channels in parallel.
 *		input	- Audio sample.
 *
 * Returns:     0123456789ABCD*# for a button push.
 *		. for nothing happening during sample interval.
 *		$ after several seconds of inactivity.
 *		space between sample intervals.
 *
 *
 *----------------------------------------------------------------*/

func dtmf_sample(c int, input float64) rune {

	// Only applies to radio channels.  Should not be here.
	if c >= MAX_RADIO_CHANS {
		return ('$')
	}

	var D = &(dd[c])
	var Q0 float64

	for i := 0; i < NUM_TONES; i++ {
		Q0 = input + D.Q1[i]*D.coef[i] - D.Q2[i]
		D.Q2[i] = D.Q1[i]
		D.Q1[i] = Q0
	}

	/*
	 * Is it time to process the block?
	 */
	D.n++
	if D.n == D.block_size {
		var output [NUM_TONES]float64
		var decoded rune
		var row, col int

		for i := 0; i < NUM_TONES; i++ {
			output[i] = float64(math.Sqrt(float64(D.Q1[i]*D.Q1[i] + D.Q2[i]*D.Q2[i] - D.Q1[i]*D.Q2[i]*D.coef[i])))
			D.Q1[i] = 0
			D.Q2[i] = 0
		}
		D.n = 0

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

		/* TODO KG
		for i := 0; i < NUM_TONES; i++ {
			#if DEBUG
				    dw_printf ("%5.0f ", output[i]);
			#endif
		}
		*/

		var rc2char = []rune{'1', '2', '3', 'A',
			'4', '5', '6', 'B',
			'7', '8', '9', 'C',
			'*', '0', '#', 'D'}

		if row >= 0 && col >= 0 {
			decoded = rc2char[row*4+col]
		} else {
			decoded = ' '
		}

		// Consider valid only if we get same twice in a row.

		if decoded == D.prev_dec {
			D.debounced = decoded

			// Update Data Carrier Detect Indicator.
			var _tmpIntBool = 0
			if decoded != ' ' {
				_tmpIntBool = 1
			}
			dcd_change(c, MAX_SUBCHANS, 0, _tmpIntBool)

			/* Reset timeout timer. */
			if decoded != ' ' {
				D.timeout = ((DTMF_TIMEOUT_SEC) * D.sample_rate) / D.block_size
			}
		}
		D.prev_dec = decoded

		// Return only new button pushes.
		// Also report timeout after period of inactivity.

		var ret = '.'
		if D.debounced != D.prev_debounced {
			if D.debounced != ' ' {
				ret = D.debounced
			}
		}
		if ret == '.' {
			if D.timeout > 0 {
				D.timeout--
				if D.timeout == 0 {
					ret = '$'
				}
			}
		}
		D.prev_debounced = D.debounced

		/* TODO KG
		#if DEBUG
			  dw_printf ("     dec=%c, deb=%c, ret=%c, to=%d \n",
					decoded, D.debounced, ret, D.timeout);
		#endif
		*/
		return (ret)
	}

	return (' ')
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

	// Length of tone or gap between.
	var len_ms = int((500.0 / float64(speed)) + 0.5)

	push_button(channel, ' ', txdelay)

	for _, p := range str {
		push_button(channel, p, len_ms)
		push_button(channel, ' ', len_ms)
	}

	push_button(channel, ' ', txtail)

	audio_flush(ACHAN2ADEV(channel))

	return (txdelay +
		int(1000.0*float64(len(str))/float64(speed)+0.5) +
		txtail)

} /* end dtmf_send */

/*------------------------------------------------------------------
 *
 * Name:        push_button
 *
 * Purpose:     Generate DTMF tone for a button push.
 *
 * Inputs:	channel	- Radio channel number.
 *
 *		button	- One of 0-9, A-D, *, #.  Others result in silence.
 *			  '?' is a special case used only for unit testing.
 *
 *		ms	- Duration in milliseconds.
 *			  Use 50 ms for tone and 50 ms of silence for max rate of 10 per second.
 *
 * Outputs:	Audio is sent to radio.
 *
 *----------------------------------------------------------------*/

// test_mode
var push_button_result string

func push_button_raw(channel int, button rune, ms int, test_mode bool) {

	var fa, fb int
	switch button {
	case '1':
		fa = DTMF_TONES[0]
		fb = DTMF_TONES[4]
	case '2':
		fa = DTMF_TONES[0]
		fb = DTMF_TONES[5]
	case '3':
		fa = DTMF_TONES[0]
		fb = DTMF_TONES[6]
	case 'a', 'A':
		fa = DTMF_TONES[0]
		fb = DTMF_TONES[7]
	case '4':
		fa = DTMF_TONES[1]
		fb = DTMF_TONES[4]
	case '5':
		fa = DTMF_TONES[1]
		fb = DTMF_TONES[5]
	case '6':
		fa = DTMF_TONES[1]
		fb = DTMF_TONES[6]
	case 'b', 'B':
		fa = DTMF_TONES[1]
		fb = DTMF_TONES[7]
	case '7':
		fa = DTMF_TONES[2]
		fb = DTMF_TONES[4]
	case '8':
		fa = DTMF_TONES[2]
		fb = DTMF_TONES[5]
	case '9':
		fa = DTMF_TONES[2]
		fb = DTMF_TONES[6]
	case 'c', 'C':
		fa = DTMF_TONES[2]
		fb = DTMF_TONES[7]
	case '*':
		fa = DTMF_TONES[3]
		fb = DTMF_TONES[4]
	case '0':
		fa = DTMF_TONES[3]
		fb = DTMF_TONES[5]
	case '#':
		fa = DTMF_TONES[3]
		fb = DTMF_TONES[6]
	case 'd', 'D':
		fa = DTMF_TONES[3]
		fb = DTMF_TONES[7]

	case '?': /* check result */
		Assert(test_mode)
		if push_button_result == "123A456B789C*0#D123$789$" { //nolint:staticcheck
			text_color_set(DW_COLOR_REC)
			dw_printf("\nSuccess!\n")
		} else if push_button_result == "123A456B789C*0#D123789" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\n * Time-out failed, otherwise OK *\n")
			dw_printf("\"%s\"\n", push_button_result)
			os.Exit(1)
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("\n *** TEST FAILED ***\n")
			dw_printf("\"%s\"\n", push_button_result)
			os.Exit(1)
		}
	}

	//dw_printf ("push_button (%d, '%c', %d), fa=%.0f, fb=%.0f. %d samples\n", channel, button, ms, fa, fb, (ms*dd[channel].sample_rate)/1000);

	var dtmf float64 // Audio.  Sum of two sine waves.
	var phasea, phaseb float64

	for i := int(0); i < (ms*dd[channel].sample_rate)/1000; i++ {

		// This could be more efficient with a precomputed sine wave table
		// but I'm not that worried about it.
		// With a Raspberry Pi, model 2, default 1200 receiving takes about 14% of one CPU core.
		// When transmitting tones, it briefly shoots up to about 33%.

		if fa > 0 && fb > 0 {
			dtmf = float64(math.Sin(float64(phasea)) + math.Sin(float64(phaseb)))
			phasea += 2.0 * float64(math.Pi) * float64(fa) / float64(dd[channel].sample_rate)
			phaseb += 2.0 * float64(math.Pi) * float64(fb) / float64(dd[channel].sample_rate)
		} else {
			dtmf = 0
		}

		if test_mode {
			/* Make sure it is insensitive to signal amplitude. */
			/* (Uncomment each of below when testing.) */

			var x = dtmf_sample(0, dtmf)
			//x = dtmf_sample (0, dtmf * 1000);
			//x = dtmf_sample (0, dtmf * 0.001);

			if x != ' ' && x != '.' {
				push_button_result += string([]rune{x})
			}
		} else {
			// 'dtmf' can be in range of +-2.0 because it is sum of two sine waves.
			// Amplitude of 100 would use full +-32k range.

			var sam = C.int(dtmf * 16383.0 * float64(s_amplitude) / 100.0)
			gen_tone_put_sample(C.int(channel), C.int(ACHAN2ADEV(channel)), sam)
		}
	}
}

func push_button(channel int, button rune, ms int) {
	push_button_raw(channel, button, ms, false)
}

func push_button_test(channel int, button rune, ms int) {
	push_button_raw(channel, button, ms, true)
}
