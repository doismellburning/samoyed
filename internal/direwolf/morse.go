//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Generate audio for morse code.
 *
 *---------------------------------------------------------------*/

import (
	"math"
	"unicode"

	"github.com/sirupsen/logrus"
)

/*
 * Might get ambitious and make this adjustable some day.
 * Good enough for now.
 */

const MORSE_TONE = 800

func TIME_UNITS_TO_MS(tu int, wpm int) float64 {
	return (float64((tu)*1200.0) / float64(wpm))
}

type morse_s struct {
	ch  rune
	enc string
}

var MORSE []morse_s = []morse_s{
	{'A', ".-"},
	{'B', "-..."},
	{'C', "-.-."},
	{'D', "-.."},
	{'E', "."},
	{'F', "..-."},
	{'G', "--."},
	{'H', "...."},
	{'I', ".."},
	{'J', ".---"},
	{'K', "-.-"},
	{'L', ".-.."},
	{'M', "--"},
	{'N', "-."},
	{'O', "---"},
	{'P', ".--."},
	{'Q', "--.-"},
	{'R', ".-."},
	{'S', "..."},
	{'T', "-"},
	{'U', "..-"},
	{'V', "...-"},
	{'W', ".--"},
	{'X', "-..-"},
	{'Y', "-.--"},
	{'Z', "--.."},
	{'1', ".----"},
	{'2', "..---"},
	{'3', "...--"},
	{'4', "....-"},
	{'5', "....."},
	{'6', "-...."},
	{'7', "--..."},
	{'8', "---.."},
	{'9', "----."},
	{'0', "-----"},
	{'.', ".-.-.-"},
	{',', "--..--"},
	{'?', "..--.."},
	{'/', "-..-."},

	{'=', "-...-"}, /* from ARRL */
	{'-', "-....-"},
	{')', "-.--.-"}, /* does not distinguish open/close */
	{':', "---..."},
	{';', "-.-.-."},
	{'"', ".-..-."},
	{'\'', ".----."},
	{'$', "...-..-"},

	{'!', "-.-.--"}, /* more from wikipedia */
	{'(', "-.--."},
	{'&', ".-..."},
	{'+', ".-.-."},
	{'_', "..--.-"},
	{'@', ".--.-."},
}

const TICKS_PER_CYCLE = (256.0 * 256.0 * 256.0 * 256.0)

// morseSampleSink is where morseSend puts the audio it generates.
type morseSampleSink interface {
	// PutSample ships out one audio sample, in the range of a signed 16 bit
	// integer.
	PutSample(sam int)

	// PutQuietMs ships out ms milliseconds of silence.  A tone generator
	// also takes the chance to reset its own phase, so that what it
	// generates next starts cleanly.
	PutQuietMs(ms int)

	// Flush pushes out whatever audio is still buffered.
	Flush()
}

/*-------------------------------------------------------------------
 *
 * Name:        morseSend
 *
 * Purpose:    	Given a string, generate appropriate lengths of
 *		tone and silence.
 *
 * Inputs:	out	- Where the audio goes.
 *
 *		sampleRate - Samples per second of out.
 *		amplitude - Signal amplitude on scale of 0 .. 100.
 *		str	- Character string to send.
 *		wpm	- Speed in words per minute.
 *		txdelay	- Delay (ms) from PTT to first character.
 *		txtail	- Delay (ms) from last character to PTT off.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send morse
 *		code.  morseDuration says how long it takes, PTT included.
 *
 *--------------------------------------------------------------------*/

func morseSend(out morseSampleSink, sampleRate int, amplitude int, str string, wpm int, txdelay int, txtail int) {
	var sineTable = morseSineTable(amplitude)

	var time_units = 0

	morseQuietMs(out, txdelay)

	for strIdx, p := range str {
		var i = morse_lookup(p)
		if i >= 0 {
			var enc = MORSE[i].enc
			for encIdx, e := range enc {
				if e == '.' {
					morseTone(out, sampleRate, &sineTable, 1, wpm)

					time_units++
				} else {
					morseTone(out, sampleRate, &sineTable, 3, wpm)

					time_units += 3
				}

				if encIdx != len(enc)-1 { // Intersperse quiet
					morseQuiet(out, sampleRate, 1, wpm)

					time_units++
				}
			}
		} else {
			morseQuiet(out, sampleRate, 1, wpm)

			time_units++
		}

		if strIdx != len(str)-1 { // Intersperse quiet
			morseQuiet(out, sampleRate, 3, wpm)

			time_units += 3
		}
	}

	morseQuietMs(out, txtail)

	if time_units != morse_units_str(str) {
		logrus.WithFields(logrus.Fields{
			"sent":       time_units,
			"calculated": morse_units_str(str),
		}).Error("morse: Internal error.  Inconsistent length")
	}

	out.Flush()
}

// morseDuration returns the total number of milliseconds to activate PTT to
// morseSend str at wpm.  This includes delays before the first character and
// after the last to avoid chopping off part of it.
func morseDuration(str string, wpm int, txdelay int, txtail int) int {
	return (txdelay + int(TIME_UNITS_TO_MS(morse_units_str(str), wpm)+0.5) + txtail)
}

// morseSineTable makes one cycle of a sine wave, amplitude percent of the
// full 16 bit sample range, clipping anything that would not fit - worked
// out just as the tone generator works out its own, which the Morse tone
// used to be read from.
func morseSineTable(amplitude int) [256]int16 {
	var table [256]int16

	for j := range 256 {
		var a = (float64(j) / 256.0) * (2.0 * math.Pi)
		var s = int(math.Sin(a) * 32767 * float64(amplitude) / 100.0)

		/* 16 bit sound sample must fit in range of -32768 .. +32767. */

		if s < -32768 {
			s = -32768
		} else if s > 32767 {
			s = 32767
		}

		table[j] = int16(s)
	}

	return table
}

/*-------------------------------------------------------------------
 *
 * Name:        morseTone
 *
 * Purpose:    	Generate tone for specified number of time units.
 *
 * Inputs:	out	- Where the audio goes.
 *		sampleRate - Samples per second of out.
 *		sineTable - One cycle of the tone, at the amplitude wanted.
 *		tu	- Number of time units.  Should be 1 or 3.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func morseTone(out morseSampleSink, sampleRate int, sineTable *[256]int16, tu int, wpm int) {
	// Phase accumulator for tone generation.
	// Upper bits are used as index into sine table.
	var tone_phase = 0

	// How much to advance phase for each audio sample.
	var f1_change_per_sample = (int)(((MORSE_TONE * TICKS_PER_CYCLE) / float64(sampleRate)) + 0.5)

	var nsamples = (int)((TIME_UNITS_TO_MS(tu, wpm) * float64(sampleRate) / 1000.) + 0.5)

	for range nsamples {
		tone_phase += f1_change_per_sample
		out.PutSample(int(sineTable[(tone_phase>>24)&0xff]))
	}
} /* end morseTone */

/*-------------------------------------------------------------------
 *
 * Name:        morseQuiet
 *
 * Purpose:    	Generate silence for specified number of time units.
 *
 * Inputs:	out	- Where the audio goes.
 *		sampleRate - Samples per second of out.
 *		tu	- Number of time units.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func morseQuiet(out morseSampleSink, sampleRate int, tu int, wpm int) {
	var nsamples = int((TIME_UNITS_TO_MS(tu, wpm) * float64(sampleRate) / 1000.) + 0.5)

	for range nsamples {
		out.PutSample(0)
	}
} /* end morseQuiet */

/*-------------------------------------------------------------------
 *
 * Name:        morseQuietMs
 *
 * Purpose:    	Generate silence for specified number of milliseconds.
 *		This is used for the txdelay and txtail times.
 *
 * Inputs:	out	- Where the audio goes.
 *		ms	- Number of milliseconds.
 *
 *--------------------------------------------------------------------*/

func morseQuietMs(out morseSampleSink, ms int) {
	out.PutQuietMs(ms)
} /* end morseQuietMs */

/*-------------------------------------------------------------------
 *
 * Name:        morse_lookup
 *
 * Purpose:    	Given a character, find index in table above.
 *
 * Inputs:	ch
 *
 * Returns:	Index into table above or -1 if not found.
 *		Notice that space is not in the table.
 *		Any unusual character, that is not in the table,
 *		ends up being treated like space.
 *
 *--------------------------------------------------------------------*/

func morse_lookup(ch rune) int {
	if unicode.IsLower(ch) {
		ch = unicode.ToUpper(ch)
	}

	for i, m := range MORSE {
		if ch == m.ch {
			return i
		}
	}

	return -1
}

/*-------------------------------------------------------------------
 *
 * Name:        morse_units_ch
 *
 * Purpose:    	Find number of time units for a character.
 *
 * Inputs:	ch
 *
 * Returns:	1 for E (.)
 *		3 for T (-)
 *		3 for I.= (..)
 *		etc.
 *
 *		The one unexpected result is 1 for space.  Why not 7?
 *		When a space appears between two other characters,
 *		we already have 3 before and after so only 1 more is needed.
 *
 *--------------------------------------------------------------------*/

func morse_units_ch(ch rune) int {
	var i = morse_lookup(ch)

	if i < 0 {
		return (1) /* space or any invalid character */
	}

	var enc = MORSE[i].enc
	var length = len(enc)
	var units = length - 1

	for _, k := range enc {
		switch k {
		case '.':
			units++
		case '-':
			units += 3
		default:
			logrus.WithField("element", string(k)).Error("morse_units_ch: should not be here")
		}
	}

	return (units)
}

/*-------------------------------------------------------------------
 *
 * Name:        morse_units_str
 *
 * Purpose:    	Find number of time units for a string of characters.
 *
 * Inputs:	str
 *
 * Returns:	1 for E
 *		5 for EE	(1 + 3 + 1)
 *		9 for E E	(1 + 7 + 1)
 *		etc.
 *
 *--------------------------------------------------------------------*/

func morse_units_str(str string) int {
	var units = (len(str) - 1) * 3

	for _, k := range str {
		units += morse_units_ch(k)
	}

	return (units)
}

/* TODO KG
#if MTEST1

int main (int argc, char *argv[]) {

	dw_printf ("CQ DX\n");
	morse_send (0, "CQ DX", 10, 10, 10);
	dw_printf ("\n\n");

	dw_printf ("wb2osz/9\n");
	morse_send (0, "wb2osz/9", 10, 10, 10);
	dw_printf ("\n\n");

}

#endif
*/

/* end morse.c */
