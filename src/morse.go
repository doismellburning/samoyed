//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Generate audio for morse code.
 *
 *---------------------------------------------------------------*/

import (
	"unicode"
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

/*------------------------------------------------------------------
 *
 * Name:        morse_init
 *
 * Purpose:     Save the audio configuration.
 *
 * Inputs:      audio_config_p		- Pointer to audio configuration structure.
 *
 *		amp		- Ignored: Morse goes through the channel's tone
 *				  generator, which has its own sine table.
 *
 *----------------------------------------------------------------*/

func morse_init(audio_config_p *audio_s, _ int) {
	save_audio_config_p = audio_config_p
} /* end morse_init */

/*-------------------------------------------------------------------
 *
 * Name:        morse_send
 *
 * Purpose:    	Given a string, generate appropriate lengths of
 *		tone and silence.
 *
 * Inputs:	chan	- Radio channel number.
 *		str	- Character string to send.
 *		wpm	- Speed in words per minute.
 *		txdelay	- Delay (ms) from PTT to first character.
 *		txtail	- Delay (ms) from last character to PTT off.
 *
 *
 * Returns:	Total number of milliseconds to activate PTT.
 *		This includes delays before the first character
 *		and after the last to avoid chopping off part of it.
 *
 * Description:	xmit_thread calls this instead of the usual hdlc_send
 *		when we have a special packet that means send morse
 *		code.
 *
 *--------------------------------------------------------------------*/

func morse_send(channel int, str string, wpm int, txdelay int, txtail int) int {
	if toneGenerators[channel] == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for sending Morse Code.\n", channel)
	} else {
		toneGenerators[channel].SendMorse(str, wpm, txdelay, txtail)
	}

	return (txdelay + int(TIME_UNITS_TO_MS(morse_units_str(str), wpm)+0.5) + txtail)
} /* end morse_send */

// SendMorse generates the tones and silences for str, as morse_send
// describes, on the generator's channel.
func (tg *ToneGenerator) SendMorse(str string, wpm int, txdelay int, txtail int) {
	var time_units = 0

	tg.morseQuietMs(txdelay)

	for strIdx, p := range str {
		var i = morse_lookup(p)
		if i >= 0 {
			var enc = MORSE[i].enc
			for encIdx, e := range enc {
				if e == '.' {
					tg.morseTone(1, wpm)

					time_units++
				} else {
					tg.morseTone(3, wpm)

					time_units += 3
				}

				if encIdx != len(enc)-1 { // Intersperse quiet
					tg.morseQuiet(1, wpm)

					time_units++
				}
			}
		} else {
			tg.morseQuiet(1, wpm)

			time_units++
		}

		if strIdx != len(str)-1 { // Intersperse quiet
			tg.morseQuiet(3, wpm)

			time_units += 3
		}
	}

	tg.morseQuietMs(txtail)

	if time_units != morse_units_str(str) {
		dw_printf("morse: Internal error.  Inconsistent length, %d vs. %d calculated.\n", time_units, morse_units_str(str))
	}

	tg.Flush()
}

/*-------------------------------------------------------------------
 *
 * Name:        morseTone
 *
 * Purpose:    	Generate tone for specified number of time units.
 *
 * Inputs:	tu	- Number of time units.  Should be 1 or 3.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func (tg *ToneGenerator) morseTone(tu int, wpm int) {
	var samplesPerSec = tg.audioConfig.adev[tg.adevIndex].samples_per_sec

	// Phase accumulator for tone generation.
	// Upper bits are used as index into sine table.
	var tone_phase = 0

	// How much to advance phase for each audio sample.
	var f1_change_per_sample = (int)(((MORSE_TONE * TICKS_PER_CYCLE) / float64(samplesPerSec)) + 0.5)

	var nsamples = (int)((TIME_UNITS_TO_MS(tu, wpm) * float64(samplesPerSec/1000.)) + 0.5)

	for range nsamples {
		tone_phase += f1_change_per_sample
		tg.PutSample(int(tg.sineTable[(tone_phase>>24)&0xff]))
	}
} /* end morseTone */

/*-------------------------------------------------------------------
 *
 * Name:        morseQuiet
 *
 * Purpose:    	Generate silence for specified number of time units.
 *
 * Inputs:	tu	- Number of time units.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func (tg *ToneGenerator) morseQuiet(tu int, wpm int) {
	var nsamples = int((TIME_UNITS_TO_MS(tu, wpm) * float64(tg.audioConfig.adev[tg.adevIndex].samples_per_sec) / 1000.) + 0.5)

	for range nsamples {
		tg.PutSample(0)
	}
} /* end morseQuiet */

/*-------------------------------------------------------------------
 *
 * Name:        morseQuietMs
 *
 * Purpose:    	Generate silence for specified number of milliseconds.
 *		This is used for the txdelay and txtail times.
 *
 * Inputs:	ms	- Number of milliseconds.
 *
 *--------------------------------------------------------------------*/

func (tg *ToneGenerator) morseQuietMs(ms int) {
	var nsamples = int(float64(ms*tg.audioConfig.adev[tg.adevIndex].samples_per_sec/1000.) + 0.5)

	for range nsamples {
		tg.PutSample(0)
	}
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
			dw_printf("ERROR: morse_units_ch: should not be here.\n")
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
