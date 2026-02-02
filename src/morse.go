package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Generate audio for morse code.
 *
 *---------------------------------------------------------------*/

// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
import "C"

import (
	"math"
	"unicode"
)

/*
 * Might get ambitious and make this adjustable some day.
 * Good enough for now.
 */

const MORSE_TONE = 800

func TIME_UNITS_TO_MS(tu C.int, wpm C.int) float64 {
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

/* Constants after initialization. */

const TICKS_PER_CYCLE = (256.0 * 256.0 * 256.0 * 256.0)

var SineTable [256]int

/*------------------------------------------------------------------
 *
 * Name:        morse_init
 *
 * Purpose:     Initialize for tone generation.
 *
 * Inputs:      audio_config_p		- Pointer to audio configuration structure.
 *
 *				The fields we care about are:
 *
 *					samples_per_sec
 *
 *		amp		- Signal amplitude on scale of 0 .. 100.
 *
 *				  100 will produce maximum amplitude of +-32k samples.
 *
 * Returns:     0 for success.
 *              -1 for failure.
 *
 * Description:	 Precompute a sine wave table.
 *
 *----------------------------------------------------------------*/

func morse_init(audio_config_p *audio_s, amp C.int) {
	/*
	 * Save away modem parameters for later use.
	 */

	save_audio_config_p = audio_config_p

	for j := 0; j < len(SineTable); j++ {
		var a = (float64(j) / 256.0) * (2 * math.Pi)
		var s = int(math.Sin(a) * 32767.0 * float64(amp) / 100.0)

		/* 16 bit sound sample is in range of -32768 .. +32767. */
		Assert(s >= -32768 && s <= 32767)
		SineTable[j] = s
	}
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

func morse_send(channel C.int, str string, wpm C.int, txdelay C.int, txtail C.int) C.int {

	var time_units C.int = 0

	morse_quiet_ms(channel, txdelay)

	for strIdx, p := range str {
		var i = morse_lookup(p)
		if i >= 0 {
			var enc = MORSE[i].enc
			for encIdx, e := range enc {
				if e == '.' {
					morse_tone(channel, 1, wpm)
					time_units++
				} else {
					morse_tone(channel, 3, wpm)
					time_units += 3
				}
				if encIdx != len(enc)-1 { // Intersperse quiet
					morse_quiet(channel, 1, wpm)
					time_units++
				}
			}
		} else {
			morse_quiet(channel, 1, wpm)
			time_units++
		}

		if strIdx != len(str)-1 { // Intersperse quiet
			morse_quiet(channel, 3, wpm)
			time_units += 3
		}
	}

	morse_quiet_ms(channel, txtail)

	if time_units != morse_units_str(str) {
		dw_printf("morse: Internal error.  Inconsistent length, %d vs. %d calculated.\n", time_units, morse_units_str(str))
	}

	audio_flush(C.int(ACHAN2ADEV(int(channel))))

	return (txdelay + C.int(TIME_UNITS_TO_MS(time_units, wpm)+0.5) + txtail)

} /* end morse_send */

/*-------------------------------------------------------------------
 *
 * Name:        morse_tone
 *
 * Purpose:    	Generate tone for specified number of time units.
 *
 * Inputs:	channel	- Radio channel.
 *		tu	- Number of time units.  Should be 1 or 3.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func morse_tone(channel C.int, tu C.int, wpm C.int) {

	/* TODO KG
	#if MTEST1
		int n;
		for (n=0; n<tu; n++) {
		  dw_printf ("#");
		}
	#else
	*/

	var a = C.int(ACHAN2ADEV(int(channel))) /* device for channel. */

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for sending Morse Code.\n", channel)
		return
	}

	// Phase accumulator for tone generation.
	// Upper bits are used as index into sine table.
	var tone_phase C.int = 0

	// How much to advance phase for each audio sample.
	var f1_change_per_sample = (C.int)(((MORSE_TONE * TICKS_PER_CYCLE) / float64(save_audio_config_p.adev[a].samples_per_sec)) + 0.5)

	var nsamples = (int)((TIME_UNITS_TO_MS(tu, wpm) * float64(save_audio_config_p.adev[a].samples_per_sec/1000.)) + 0.5)

	for j := 0; j < nsamples; j++ {

		tone_phase += f1_change_per_sample
		var sam = C.int(SineTable[(tone_phase>>24)&0xff])
		gen_tone_put_sample(channel, a, sam)
	}

	// TODO KG #endif

} /* end morse_tone */

/*-------------------------------------------------------------------
 *
 * Name:        morse_quiet
 *
 * Purpose:    	Generate silence for specified number of time units.
 *
 * Inputs:	channel	- Radio channel.
 *		tu	- Number of time units.
 *		wpm	- Speed in WPM.
 *
 *--------------------------------------------------------------------*/

func morse_quiet(channel C.int, tu C.int, wpm C.int) {

	/* TODO KG
	#if MTEST1
		int n;
		for (n=0; n<tu; n++) {
		  dw_printf (".");
		}
	#else
	*/
	var a = C.int(ACHAN2ADEV(int(channel))) /* device for channel. */
	var sam C.int = 0

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for sending Morse Code.\n", channel)
		return
	}

	var nsamples = int((TIME_UNITS_TO_MS(tu, wpm) * float64(save_audio_config_p.adev[a].samples_per_sec) / 1000.) + 0.5)

	for j := 0; j < nsamples; j++ {

		gen_tone_put_sample(channel, a, sam)

	}
	// TODO KG #endif

} /* end morse_quiet */

/*-------------------------------------------------------------------
 *
 * Name:        morse_quiet_ms
 *
 * Purpose:    	Generate silence for specified number of milliseconds.
 *		This is used for the txdelay and txtail times.
 *
 * Inputs:	channel	- Radio channel.
 *		tu	- Number of time units.
 *
 *--------------------------------------------------------------------*/

func morse_quiet_ms(channel C.int, ms C.int) {

	/* TODO KG
	#if MTEST1
	#else
	*/
	var a = C.int(ACHAN2ADEV(int(channel))) /* device for channel. */
	var sam C.int = 0

	if save_audio_config_p.chan_medium[channel] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid channel %d for sending Morse Code.\n", channel)
		return
	}

	var nsamples = int(float64(ms*C.int(save_audio_config_p.adev[a].samples_per_sec)/1000.) + 0.5)

	for j := 0; j < nsamples; j++ {
		gen_tone_put_sample(channel, a, sam)
	}

	// TODO KG #endif

} /* end morse_quiet_ms */

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

func morse_units_ch(ch rune) C.int {

	var i = morse_lookup(ch)

	if i < 0 {
		return (1) /* space or any invalid character */
	}

	var enc = MORSE[i].enc
	var length = C.int(len(enc))
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

func morse_units_str(str string) C.int {

	var units = C.int(len(str)-1) * 3

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
