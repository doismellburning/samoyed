package direwolf

/*------------------------------------------------------------------
 *
 * Module:      aprs_tt.c
 *
 * Purpose:   	First half of APRStt gateway.
 *
 * Description: This file contains functions to parse the tone sequences
 *		and extract meaning from them.
 *
 *		tt_user.c maintains information about users and
 *		generates the APRS Object Reports.
 *
 *
 * References:	This is based upon APRStt (TM) documents with some
 *		artistic freedom.
 *
 *		http://www.aprs.org/aprstt.html
 *
 *---------------------------------------------------------------*/

// TODO:  clean up terminology.
// "Message" has a specific meaning in APRS and this is not it.
// Touch Tone sequence should be appropriate.
// What do we call the parts separated by * key?  Field.

// #include "direwolf.h"
// #include <stdlib.h>
// #include <math.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <errno.h>
// #include <ctype.h>
// #include <assert.h>
// #include "version.h"
// #include "ax25_pad.h"
// #include "hdlc_rec2.h"		/* for process_rec_frame */
// #include "textcolor.h"
// #include "aprs_tt.h"
// #include "tt_text.h"
// #include "tt_user.h"
// #include "symbols.h"
// #include "latlong.h"
// #include "dlq.h"
// #include "demod.h"          /* for alevel_t & demod_get_audio_level() */
// #include "tq.h"
// // geotranz
// #include "utm.h"
// #include "mgrs.h"
// #include "usng.h"
// #include "error_string.h"
// extern struct tt_config_s *aprs_tt_config;
// struct ttloc_s *ttloc_ptr_get(int idx);
// double ttloc_ptr_get_point_lat(int idx);
// double ttloc_ptr_get_point_lon(int idx);
// double ttloc_ptr_get_vector_lat(int idx);
// double ttloc_ptr_get_vector_lon(int idx);
// double ttloc_ptr_get_vector_scale(int idx);
// double ttloc_ptr_get_grid_lat0(int idx);
// double ttloc_ptr_get_grid_lat9(int idx);
// double ttloc_ptr_get_grid_lon0(int idx);
// double ttloc_ptr_get_grid_lon9(int idx);
// double ttloc_ptr_get_utm_scale(int idx);
// double ttloc_ptr_get_utm_x_offset(int idx);
// double ttloc_ptr_get_utm_y_offset(int idx);
// long ttloc_ptr_get_utm_lzone(int idx);
// char ttloc_ptr_get_utm_latband(int idx);
// char ttloc_ptr_get_utm_hemi(int idx);
// char *ttloc_ptr_get_mgrs_zone(int idx);
// char *ttloc_ptr_get_mhead_prefix(int idx);
// char *ttloc_ptr_get_macro_definition(int idx);
// extern struct ttloc_s aprs_tt_test_config;
import "C"

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

/*
 * Touch Tone sequences are accumulated here until # terminator found.
 * Kept separate for each audio channel so the gateway CAN be listening
 * on multiple channels at the same time.
 */

const MAX_MSG_LEN = 100

var msg_str [MAX_RADIO_CHANS]string

var tt_debug = 0

// Replacement for the TT_MAIN define, to work better with Go, and try to reduce some complexity
var running_TT_MAIN_tests = false

/*------------------------------------------------------------------
 *
 * Name:        aprs_tt_init
 *
 * Purpose:     Initialize the APRStt gateway at system startup time.
 *
 * Inputs:      P	- Pointer to configuration options gathered by config.c.
 *		debug	- Debug printing control.
 *
 * Global out:	Make our own local copy of the structure here.
 *
 * Returns:     None
 *
 * Description:	The main program needs to call this at application
 *		start up time after reading the configuration file.
 *
 *----------------------------------------------------------------*/

var tt_config *C.struct_tt_config_s

func aprs_tt_init(p *C.struct_tt_config_s, debug int) {
	tt_debug = debug

	if p == nil {
		/* For unit testing. */
		var NUM_TEST_CONFIG C.int = 10 // TODO KG Hardcoded because cross-language sizing is fiddly
		var config C.struct_tt_config_s
		config.ttloc_size = NUM_TEST_CONFIG
		config.ttloc_ptr = &C.aprs_tt_test_config
		config.ttloc_len = NUM_TEST_CONFIG
		/* Don't care about xmit timing or corral here. */

		tt_config = &config
		C.aprs_tt_config = &config
	} else {
		tt_config = p
		C.aprs_tt_config = p // For ttloc_ptr_get to work around C variable length array
	}
}

/*------------------------------------------------------------------
 *
 * Name:        aprs_tt_button
 *
 * Purpose:     Process one received button press.
 *
 * Inputs:      chan		- Audio channel it came from.
 *
 *		button		0123456789ABCD*#	- Received button press.
 *				$			- No activity timeout.
 *				space			- Quiet time filler.
 *
 * Returns:     None
 *
 * Description:	Individual key presses are accumulated here until
 *		the # message terminator is found.
 *		The complete message is then processed.
 *		The touch tone decoder sends $ if no activity
 *		for some amount of time, perhaps 5 seconds.
 *		A partially accumulated message is discarded if
 *		there is a long gap.
 *
 *		'.' means no activity during processing period.
 *		space, between blocks, shouldn't get here.
 *
 *----------------------------------------------------------------*/

var poll_period = 0

func aprs_tt_button(channel int, button rune) {
	Assert(channel >= 0 && channel < MAX_RADIO_CHANS)

	// if (button != '.') {
	//   dw_printf ("aprs_tt_button (%d, '%c')\n", channel, button);
	// }

	// TODO:  Might make more sense to put timeout here rather in the dtmf decoder.

	if button == '$' {
		/* Timeout reset. */
		msg_str[channel] = ""
	} else if button != '.' && button != ' ' {
		if len(msg_str[channel]) < MAX_MSG_LEN {
			msg_str[channel] += string(button)
		}
		if button == '#' {
			/*
			 * Put into the receive queue like any other packet.
			 * This way they are all processed by the common receive thread
			 * rather than the thread associated with the particular audio device.
			 */
			raw_tt_data_to_app(channel, msg_str[channel])

			msg_str[channel] = ""
		}
	} else { //nolint:gocritic
		/*
		 * Idle time. Poll occasionally for processing.
		 * Timing would be off we we are listening to more than
		 * one channel so do this only for the one specified
		 * in the TTOBJ command.
		 */

		if C.int(channel) == tt_config.obj_recv_chan {
			poll_period++
			if poll_period >= 39 {
				poll_period = 0
				tt_user_background()
			}
		}
	}
} /* end aprs_tt_button */

/*------------------------------------------------------------------
 *
 * Name:        aprs_tt_sequence
 *
 * Purpose:     Process complete received touch tone sequence
 *		terminated by #.
 *
 * Inputs:      channel		- Audio channel it came from.
 *
 *		msg		- String of DTMF buttons.
 *				  # should be the final character.
 *
 * Returns:     None
 *
 * Description:	Process a complete tone sequence.
 *		It should have one or more fields separated by *
 *		and terminated by a final # like these:
 *
 *		callsign #
 *		entry1 * callsign #
 *		entry1 * entry * callsign #
 *
 * Limitation:	Has one set of static data for communication among
 *		group of functions.  This shouldn't be a problem
 *		when receiving on multiple channels at once
 *		because they get serialized thru the receive packet queue.
 *
 *----------------------------------------------------------------*/

var m_callsign string /* really object name */

/*
 * Standard APRStt has symbol code 'A' (box) with overlay of 0-9, A-Z.
 *
 * Dire Wolf extension allows:
 *	Symbol table '/' (primary), any symbol code.
 *	Symbol table '\' (alternate), any symbol code.
 *	Alternate table symbol code, overlay of 0-9, A-Z.
 */

var m_symtab_or_overlay rune
var m_symbol_code rune // Default 'A'
var m_loc_text string
var m_longitude float64 // Set to G_UNKNOWN if not defined.
var m_latitude float64  // Set to G_UNKNOWN if not defined.
var m_ambiguity int
var m_comment string
var m_freq string
var m_ctcss string
var m_mic_e rune
var m_dao [5]byte
var m_ssid int // Default 12 for APRStt user.

func aprs_tt_sequence(channel int, msg string) {
	/* TODO KG
	   #if DEBUG
	   	text_color_set(DW_COLOR_DEBUG);
	   	dw_printf ("\n\"%s\"\n", msg);
	   #endif
	*/

	/*
	 * Discard empty message.
	 * In case # is there as optional start.
	 */

	if msg[0] == '#' {
		return
	}

	/*
	 * The parse functions will fill these in.
	 */
	m_callsign = ""
	m_symtab_or_overlay = C.APRSTT_DEFAULT_SYMTAB
	m_symbol_code = C.APRSTT_DEFAULT_SYMBOL
	m_loc_text = ""
	m_longitude = G_UNKNOWN
	m_latitude = G_UNKNOWN
	m_ambiguity = 0
	m_comment = ""
	m_freq = ""
	m_ctcss = ""
	m_mic_e = ' '
	m_dao = [5]byte{'!', 'T', ' ', ' ', '!'} /* start out unknown */
	m_ssid = 12

	/*
	 * Parse the touch tone sequence.
	 */
	var err = parse_fields(msg)

	/* TODO KG
	#if defined(DEBUG)
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("callsign=\"%s\", ssid=%d, symbol=\"%c%c\", freq=\"%s\", ctcss=\"%s\", comment=\"%s\", lat=%.4f, lon=%.4f, dao=\"%s\"\n",
			m_callsign, m_ssid, m_symtab_or_overlay, m_symbol_code, m_freq, m_ctcss, m_comment, m_latitude, m_longitude, m_dao);
	#endif
	*/

	if running_TT_MAIN_tests {
		return
	}

	/*
	 * If digested successfully.  Add to our list of users and schedule transmissions.
	 */

	if err == 0 {
		err = tt_user_heard(m_callsign, m_ssid, m_symtab_or_overlay, m_symbol_code,
			m_loc_text, m_latitude, m_longitude, m_ambiguity,
			m_freq, m_ctcss, m_comment, m_mic_e, string(m_dao[:]))
	}

	/*
	 * If a command / script was supplied, run it now.
	 * This can do additional processing and provide a custom audible response.
	 * This is done only for the success case.
	 * It might be useful to run it for error cases as well but we currently
	 * don't pass in the success / failure code to know the difference.
	 */
	var script_response string
	if err == 0 && C.strlen(&tt_config.ttcmd[0]) > 0 {
		var _script_response, _ = dw_run_cmd(C.GoString(&tt_config.ttcmd[0]), 1)
		script_response = string(_script_response)
	}

	/*
	 * Send response to user by constructing packet with SPEECH or MORSE as destination.
	 * Source shouldn't matter because it doesn't get transmitted as AX.25 frame.
	 * Use high priority queue for consistent timing.
	 *
	 * Anything from script, above, will override other predefined responses.
	 */

	var response = C.GoString(&tt_config.response[err].mtext[0])
	if len(script_response) > 0 {
		response = script_response
	}

	var audible_response = fmt.Sprintf("APRSTT>%s:%s", C.GoString(&tt_config.response[err].method[0]), response)

	var pp = C.ax25_from_text(C.CString(audible_response), 0)

	if pp == nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error. Couldn't make frame from \"%s\"\n", audible_response)
		return
	}

	C.tq_append(C.int(channel), TQ_PRIO_0_HI, pp)
} /* end aprs_tt_sequence */

/*------------------------------------------------------------------
 *
 * Name:        parse_fields
 *
 * Purpose:     Separate the complete string of touch tone characters
 *		into fields, delimited by *, and process each.
 *
 * Inputs:      msg		- String of DTMF buttons.
 *
 * Returns:     None
 *
 * Description:	It should have one or more fields separated by *.
 *
 *		callsign #
 *		entry1 * callsign #
 *		entry1 * entry * callsign #
 *
 *		Note that this will be used recursively when macros
 *		are expanded.
 *
 *		"To iterate is human, to recurse divine."
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 *----------------------------------------------------------------*/

func parse_fields(msg string) int {
	var fields = strings.FieldsFunc(msg, func(r rune) bool {
		return strings.ContainsRune("*#", r)
	})

	var err int

	for _, e := range fields {
		// text_color_set(DW_COLOR_DEBUG);
		// dw_printf ("parse_fields () field = %s\n", e);

		switch e[0] {
		case 'A':
			switch e[1] {
			case 'A': /* AA object-name */
				err = parse_object_name(e)
				if err != 0 {
					return (err)
				}
			case 'B': /* AB symbol */
				err = parse_symbol(e)
				if err != 0 {
					return (err)
				}
			case 'C': /* AC new-style-callsign */
				err = parse_aprstt3_call(e)
				if err != 0 {
					return (err)
				}
			default: /* Traditional style call or suffix */
				err = parse_callsign(e)
				if err != 0 {
					return (err)
				}
			}
		case 'B':
			err = parse_location(e)
			if err != 0 {
				return (err)
			}
		case 'C':
			err = parse_comment(e)
			if err != 0 {
				return (err)
			}
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			err = expand_macro(e)
			if err != 0 {
				return (err)
			}
		default:
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Field does not start with A, B, C, or digit: \"%s\"\n", e)
			return (C.TT_ERROR_D_MSG)
		}
	}

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("parse_fields () normal return\n");

	return (0)
} /* end parse_fields */

/*------------------------------------------------------------------
 *
 * Name:        expand_macro
 *
 * Purpose:     Expand compact form "macro" to full format then process.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should contain only digits.
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	Separate out the fields, perform substitution,
 *		call parse_fields for processing.
 *
 *
 * Future:	Generalize this to allow any lower case letter for substitution?
 *
 *----------------------------------------------------------------*/

func expand_macro(e string) int {
	text_color_set(DW_COLOR_DEBUG)
	dw_printf("Macro tone sequence: '%s'\n", e)

	var xstr, ystr, zstr, _, _, _ipat = find_ttloc_match(e)
	var ipat = C.int(_ipat)

	if ipat >= 0 {
		// Why did we print b & d here?
		// Documentation says only x, y, z can be used with macros.
		// Only those 3 are processed below.

		// dw_printf ("Matched pattern %3d: '%s', x=%s, y=%s, z=%s, b=%s, d=%s\n", ipat, C.ttloc_ptr_get(ipat).pattern, xstr, ystr, zstr, bstr, dstr);
		dw_printf("Matched pattern %3d: '%s', x=%s, y=%s, z=%s\n", ipat, C.GoString(&C.ttloc_ptr_get(ipat).pattern[0]), xstr, ystr, zstr)

		dw_printf("Replace with:        '%s'\n", C.GoString(C.ttloc_ptr_get_macro_definition(ipat)))

		if C.ttloc_ptr_get(ipat)._type != C.TTLOC_MACRO {
			/* Found match to a different type.  Really shouldn't be here. */
			/* Print internal error message... */
			dw_printf("expand_macro: type != TTLOC_MACRO\n")
			return (C.TT_ERROR_INTERNAL)
		}

		/*
		 * We found a match for the length and any fixed digits.
		 * Substitute values in to the definition.
		 */

		var stemp string

		var definition = C.GoString(C.ttloc_ptr_get_macro_definition(ipat))
		for i, d := range definition {
			if i != len(definition)-1 {
				if (d == 'x' || d == 'y' || d == 'z') && d == rune(definition[i+1]) {
					// Collapse adjacent matching substitution characters.
					continue
				}
			}

			switch d {
			case 'x':
				stemp += xstr
			case 'y':
				stemp += ystr
			case 'z':
				stemp += zstr
			default:
				stemp += fmt.Sprintf("%c", d)
			}
		}
		/*
		 * Process as if we heard this over the air.
		 */

		dw_printf("After substitution:  '%s'\n", stemp)
		return (parse_fields(stemp))
	} else {
		/* Send reject sound. */
		/* Does not match any macro definitions. */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Tone sequence did not match any pattern\n")
		return (C.TT_ERROR_MACRO_NOMATCH)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        parse_callsign
 *
 * Purpose:     Extract traditional format callsign or object name from touch tone sequence.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "A" then a digit.
 *
 * Outputs:	m_callsign
 *
 *		m_symtab_or_overlay - Set to 0-9 or A-Z if specified.
 *
 *		m_symbol_code	- Always set to 'A' (Box, DTMF or RFID)
 *					If you want a different symbol, use the new
 *					object name format and separate symbol specification.
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	We recognize 3 different formats:
 *
 *		Annn		- 3 digits are a tactical callsign.  No overlay.
 *
 *		Annnvk		- Abbreviation with 3 digits, numeric overlay, checksum.
 *		Annnvvk		- Abbreviation with 3 digits, letter overlay, checksum.
 *
 *		Att...ttvk	- Full callsign in two key method, numeric overlay, checksum.
 *		Att...ttvvk	- Full callsign in two key method, letter overlay, checksum.
 *
 *
 *----------------------------------------------------------------*/

func checksum_not_ok(str string, length int, found rune) int {
	var sum = 0

	for _, c := range str {
		if unicode.IsDigit(c) {
			sum += int(c - '0')
		} else if c >= 'A' && c <= 'D' {
			sum += int(c-'A') + 10
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("aprs_tt: checksum: bad character \"%c\" in checksum calculation!\n", c)
		}
	}

	var expected = rune('0' + (sum % 10))

	if expected != found {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Bad checksum for \"%.*s\".  Expected %c but received %c.\n", length, str, expected, found)
		return (C.TT_ERROR_BAD_CHECKSUM)
	}

	return (0)
}

func parse_callsign(e string) int {
	if tt_debug > 0 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("APRStt parse callsign (starts with A then digit): \"%s\"\n", e)
	}

	Assert(e[0] == 'A')

	var length = len(e)

	/*
	 * special case: 3 digit tactical call.
	 */

	if length == 4 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) {
		m_callsign = e[1:]
		if tt_debug > 0 {
			text_color_set(DW_COLOR_DEBUG)
			dw_printf("Special case, 3 digit tactical call: \"%s\"\n", m_callsign)
		}
		return (0)
	}

	/*
	 * 3 digit abbreviation:  We only do the parsing here.
	 * Another part of application will try to find corresponding full call.
	 */

	if (length == 6 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) && unicode.IsDigit(rune(e[4])) && unicode.IsDigit(rune(e[5]))) ||
		(length == 7 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) && unicode.IsDigit(rune(e[4])) && unicode.IsUpper(rune(e[5])) && unicode.IsDigit(rune(e[6]))) {
		var cs_err = checksum_not_ok(e[1:length-1], length-2, rune(e[length-1]))

		if cs_err != 0 {
			return (cs_err)
		}

		m_callsign = e[1:4]

		if length == 7 {
			var tttemp = string(e[length-3]) + string(e[length-2])
			var stemp [30]C.char

			C.tt_two_key_to_text(C.CString(tttemp), 0, &stemp[0])

			m_symbol_code = C.APRSTT_DEFAULT_SYMBOL
			m_symtab_or_overlay = rune(stemp[0])
			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Three digit abbreviation1: callsign \"%s\", symbol code '%c (Box DTMF)', overlay '%c', checksum %c\n",
					m_callsign, m_symbol_code, m_symtab_or_overlay, e[length-1])
			}
		} else {
			m_symbol_code = C.APRSTT_DEFAULT_SYMBOL
			m_symtab_or_overlay = rune(e[length-2])
			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Three digit abbreviation2: callsign \"%s\", symbol code '%c' (Box DTMF), overlay '%c', checksum %c\n",
					m_callsign, m_symbol_code, m_symtab_or_overlay, e[length-1])
			}
		}
		return (0)
	}

	/*
	 * Callsign in two key format.
	 */

	if length >= 7 && length <= 24 {
		var cs_err = checksum_not_ok(e[1:length-1], length-2, rune(e[length-1]))

		if cs_err != 0 {
			return (cs_err)
		}

		if unicode.IsUpper(rune(e[length-2])) {
			var tttemp = e[1 : length-3]
			var _m_callsign [30]C.char
			C.tt_two_key_to_text(C.CString(tttemp), 0, &_m_callsign[0])
			m_callsign = C.GoString(&_m_callsign[0])

			tttemp = string(e[length-3]) + string(e[length-2])
			var stemp [30]C.char
			C.tt_two_key_to_text(C.CString(tttemp), 0, &stemp[0])

			m_symbol_code = C.APRSTT_DEFAULT_SYMBOL
			m_symtab_or_overlay = rune(stemp[0])

			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Callsign in two key format1: callsign \"%s\", symbol code '%c' (Box DTMF), overlay '%c', checksum %c\n",
					m_callsign, m_symbol_code, m_symtab_or_overlay, e[length-1])
			}
		} else {
			var tttemp = e[1 : length-2]
			var _m_callsign [30]C.char
			C.tt_two_key_to_text(C.CString(tttemp), 0, &_m_callsign[0])
			m_callsign = C.GoString(&_m_callsign[0])

			m_symbol_code = C.APRSTT_DEFAULT_SYMBOL
			m_symtab_or_overlay = rune(e[length-2])

			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Callsign in two key format2: callsign \"%s\", symbol code '%c' (Box DTMF), overlay '%c', checksum %c\n",
					m_callsign, m_symbol_code, m_symtab_or_overlay, e[length-1])
			}
		}
		return (0)
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Touch tone callsign not valid: \"%s\"\n", e)
	return (C.TT_ERROR_INVALID_CALL)
}

/*------------------------------------------------------------------
 *
 * Name:        parse_object_name
 *
 * Purpose:     Extract object name from touch tone sequence.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AA".
 *
 * Outputs:	m_callsign
 *
 *		m_ssid		- Cleared to remove the default of 12.
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	Data format
 *
 *		AAtt...tt	- Symbol name, two key method, up to 9 characters.
 *
 *----------------------------------------------------------------*/

func parse_object_name(e string) int {
	if tt_debug > 0 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("APRStt parse object name (starts with AA): \"%s\"\n", e)
	}

	Assert(e[0] == 'A')
	Assert(e[1] == 'A')

	var length = len(e)

	/*
	 * Object name in two key format.
	 */

	if length >= 2+1 && length <= 30 {
		var _m_callsign [30]C.char
		if C.tt_two_key_to_text(C.CString(e[2:]), 0, &_m_callsign[0]) == 0 {
			m_callsign = C.GoString(&_m_callsign[0])
			if len(m_callsign) > 9 {
				m_callsign = m_callsign[:9]
			}
			m_ssid = 0 /* No ssid for object name */

			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("Object name in two key format: \"%s\"\n", m_callsign)
			}

			return (0)
		}
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Touch tone object name not valid: \"%s\"\n", e)

	return (C.TT_ERROR_INVALID_OBJNAME)
} /* end parse_oject_name */

/*------------------------------------------------------------------
 *
 * Name:        parse_symbol
 *
 * Purpose:     Extract symbol from touch tone sequence.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AB".
 *
 * Outputs:	m_symtab_or_overlay
 *
 * 		m_symbol_code
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	Data format
 *
 *		AB1nn		- Symbol from primary symbol table.
 *				  Two digits nn are the same as in the GPSCnn
 *				  generic address used as a destination.
 *
 *		AB2nn		- Symbol from alternate symbol table.
 *				  Two digits nn are the same as in the GPSEnn
 *				  generic address used as a destination.
 *
 *		AB0nnvv		- Symbol from alternate symbol table.
 *				  Two digits nn are the same as in the GPSEnn
 *				  generic address used as a destination.
 *	 			  vv is an overlay digit or letter in two key method.
 *
 *----------------------------------------------------------------*/

func parse_symbol(e string) int {
	if tt_debug > 0 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("APRStt parse symbol (starts with AB): \"%s\"\n", e)
	}

	Assert(e[0] == 'A')
	Assert(e[1] == 'B')

	var length = len(e)

	if length >= 4 && length <= 10 {
		var nstr = string(e[3]) + string(e[4])

		var nn, _ = strconv.Atoi(nstr)

		if nn < 1 {
			nn = 1
		} else if nn > 94 {
			nn = 94
		}

		switch e[2] {
		case '1':
			m_symtab_or_overlay = '/'
			m_symbol_code = rune(32 + nn)
			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("symbol code '%c', primary symbol table '%c'\n",
					m_symbol_code, m_symtab_or_overlay)
			}
			return (0)

		case '2':
			m_symtab_or_overlay = '\\'
			m_symbol_code = rune(32 + nn)
			if tt_debug > 0 {
				text_color_set(DW_COLOR_DEBUG)
				dw_printf("symbol code '%c', alternate symbol table '%c'\n",
					m_symbol_code, m_symtab_or_overlay)
			}
			return (0)

		case '0':
			if length >= 6 {
				var stemp [30]C.char
				if C.tt_two_key_to_text(C.CString(e[5:]), 0, &stemp[0]) == 0 {
					m_symbol_code = rune(32 + nn)
					m_symtab_or_overlay = rune(stemp[0])
					if tt_debug > 0 {
						text_color_set(DW_COLOR_DEBUG)
						dw_printf("symbol code '%c', alternate symbol table with overlay '%c'\n",
							m_symbol_code, m_symtab_or_overlay)
					}
					return (0)
				}
			}
		}
	}

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Touch tone symbol not valid: \"%s\"\n", e)

	return (C.TT_ERROR_INVALID_SYMBOL)
} /* end parse_oject_name */

/*------------------------------------------------------------------
 *
 * Name:        parse_aprstt3_call
 *
 * Purpose:     Extract QIKcom-2 / APRStt 3 ten digit call or five digit suffix.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AC".
 *
 * Outputs:	m_callsign
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	We recognize 3 different formats:
 *
 *		ACxxxxxxxxxx	- 10 digit full callsign.
 *
 *		ACxxxxx		- 5 digit suffix.   If we can find a corresponding full
 *				  callsign, that will be substituted.
 *				  Error condition is returned if we can't find one.
 *
 *----------------------------------------------------------------*/

func parse_aprstt3_call(e string) int {
	Assert(e[0] == 'A')
	Assert(e[1] == 'C')

	if tt_debug > 0 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("APRStt parse QIKcom-2 / APRStt 3 ten digit call or five digit suffix (starts with AC): \"%s\"\n", e)
	}

	if len(e) == 2+10 {
		var call [12]C.char

		if C.tt_call10_to_text(C.CString(e[2:]), 1, &call[0]) == 0 {
			m_callsign = C.GoString(&call[0])
		} else {
			return (C.TT_ERROR_INVALID_CALL) /* Could not convert to text */
		}
	} else if len(e) == 2+5 {
		var suffix [8]C.char
		if C.tt_call5_suffix_to_text(C.CString(e[2:]), 1, &suffix[0]) == 0 {
			if running_TT_MAIN_tests {
				/* For unit test, use suffix rather than trying lookup. */
				m_callsign = C.GoString(&suffix[0])
			} else {
				var _suffix = C.GoString(&suffix[0])
				var _call, _idx = tt_3char_suffix_search(_suffix)

				/* In normal operation, try to find full callsign for the suffix received. */

				if _idx >= 0 {
					text_color_set(DW_COLOR_INFO)
					dw_printf("Suffix \"%s\" was converted to full callsign \"%s\"\n", _suffix, _call)

					m_callsign = _call
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Couldn't find full callsign for suffix \"%s\"\n", _suffix)
					return (C.TT_ERROR_SUFFIX_NO_CALL) /* Don't know this user. */
				}
			}
		} else {
			return (C.TT_ERROR_INVALID_CALL) /* Could not convert to text */
		}
	} else {
		return (C.TT_ERROR_INVALID_CALL) /* Invalid length, not 2+ (10 ir 5) */
	}

	return (0)
} /* end parse_aprstt3_call */

/*------------------------------------------------------------------
 *
 * Name:        parse_location
 *
 * Purpose:     Extract location from touch tone sequence.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "B".
 *
 * Outputs:	m_latitude
 *		m_longitude
 *
 *		m_dao		It should previously be "!T  !" to mean unknown or none.
 *				We generally take the first two tones of the field.
 *				For example, "!TB5!" for the standard bearing & range.
 *				The point type is an exception where we use "!Tn !" for
 *				one of ten positions or "!Tnn" for one of a hundred.
 *				If this ever changes, be sure to update corresponding
 *				section in process_comment() in decode_aprs.c
 *
 *		m_ambiguity
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	There are many different formats recognizable
 *		by total number of digits and sometimes the first digit.
 *
 *		We handle most of them in a general way, processing
 *		them in 5 groups:
 *
 *		* points
 *		* vector
 *		* grid
 *		* utm
 *		* usng / mgrs
 *
 *		Position ambiguity is also handled here.
 *			Latitude, Longitude, and DAO should not be touched in this case.
 *		 	We only record a position ambiguity value.
 *
 *----------------------------------------------------------------*/

/* Average radius of earth in meters. */
const R = 6371000.0

func parse_location(e string) int {
	if tt_debug > 0 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("APRStt parse location (starts with B): \"%s\"\n", e)
		// TODO: more detail later...
	}

	Assert(e[0] == 'B')

	var xstr, ystr, _, bstr, dstr, _ipat = find_ttloc_match(e)
	var ipat = C.int(_ipat)

	if ipat >= 0 {
		// dw_printf ("ipat=%d, x=%s, y=%s, b=%s, d=%s\n", ipat, xstr, ystr, bstr, dstr);

		var ttloc_type = C.ttloc_ptr_get(ipat)._type
		switch ttloc_type {
		case C.TTLOC_POINT:

			m_latitude = float64(C.ttloc_ptr_get_point_lat(ipat))
			m_longitude = float64(C.ttloc_ptr_get_point_lon(ipat))

			/* Is it one of ten or a hundred positions? */
			/* It's not hardwired to always be B0n or B9nn.  */
			/* This is a pretty good approximation. */

			m_dao[2] = e[0]
			m_dao[3] = e[1]

			if len(e) == 3 { /* probably B0n -->  !Tn ! */
				m_dao[2] = e[2]
				m_dao[3] = ' '
			}
			if len(e) == 4 { /* probably B9nn -->  !Tnn! */
				m_dao[2] = e[2]
				m_dao[3] = e[3]
			}

		case C.TTLOC_VECTOR:
			if len(bstr) != 3 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Bearing \"%s\" should be 3 digits.\n", bstr)
				// return error code?
			}
			if len(dstr) < 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Distance \"%s\" should 1 or more digits.\n", dstr)
				// return error code?
			}

			var lat0 = D2R(float64(C.ttloc_ptr_get_vector_lat(ipat)))
			var lon0 = D2R(float64(C.ttloc_ptr_get_vector_lon(ipat)))
			var d, _ = strconv.ParseFloat(dstr, 64)
			var dist = d * float64(C.ttloc_ptr_get_vector_scale(ipat))
			var b, _ = strconv.ParseFloat(bstr, 64)
			var bearing = D2R(b)

			/* Equations and caluculators found here: */
			/* http://movable-type.co.uk/scripts/latlong.html */
			/* This should probably be a function in latlong.c in case we have another use for it someday. */

			m_latitude = R2D(math.Asin(math.Sin(lat0)*math.Cos(dist/R) + math.Cos(lat0)*math.Sin(dist/R)*math.Cos(bearing)))

			m_longitude = R2D(lon0 + math.Atan2(math.Sin(bearing)*math.Sin(dist/R)*math.Cos(lat0),
				math.Cos(dist/R)-math.Sin(lat0)*math.Sin(D2R(m_latitude))))

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_GRID:
			if len(xstr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Missing X coordinate.\n")
				xstr = "0"
			}
			if len(ystr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Missing Y coordinate.\n")
				ystr = "0"
			}

			var lat0 = float64(C.ttloc_ptr_get_grid_lat0(ipat))
			var lat9 = float64(C.ttloc_ptr_get_grid_lat9(ipat))
			var yrange = lat9 - lat0
			var y, _ = strconv.ParseFloat(ystr, 64)
			var user_y_max = math.Round(math.Pow(10., float64(len(ystr))) - 1.) // e.g. 999 for 3 digits
			m_latitude = lat0 + yrange*y/user_y_max

			/* TODO KG
			#if 0
				      dw_printf ("TTLOC_GRID LAT min=%f, max=%f, range=%f\n", lat0, lat9, yrange);
				      dw_printf ("TTLOC_GRID LAT user_y=%f, user_y_max=%f\n", y, user_y_max);
				      dw_printf ("TTLOC_GRID LAT min + yrange * user_y / user_y_range = %f\n", m_latitude);
			#endif
			*/

			var lon0 = float64(C.ttloc_ptr_get_grid_lon0(ipat))
			var lon9 = float64(C.ttloc_ptr_get_grid_lon9(ipat))
			var xrange = lon9 - lon0
			var x, _ = strconv.ParseFloat(xstr, 64)
			var user_x_max = math.Round(math.Pow(10., float64(len(xstr))) - 1.)
			m_longitude = lon0 + xrange*x/user_x_max

			/* TODO KG
			#if 0
				      dw_printf ("TTLOC_GRID LON min=%f, max=%f, range=%f\n", lon0, lon9, xrange);
				      dw_printf ("TTLOC_GRID LON user_x=%f, user_x_max=%f\n", x, user_x_max);
				      dw_printf ("TTLOC_GRID LON min + xrange * user_x / user_x_range = %f\n", m_longitude);
			#endif
			*/

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_UTM:
			if len(xstr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Missing X coordinate.\n")
				/* Avoid divide by zero later.  Put in middle of range. */
				xstr = "5"
			}
			if len(ystr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Missing Y coordinate.\n")
				/* Avoid divide by zero later.  Put in middle of range. */
				ystr = "5"
			}

			var x, _ = strconv.ParseFloat(xstr, 64)
			var easting = x*float64(C.ttloc_ptr_get_utm_scale(ipat)) + float64(C.ttloc_ptr_get_utm_x_offset(ipat))

			var y, _ = strconv.ParseFloat(ystr, 64)
			var northing = y*float64(C.ttloc_ptr_get_utm_scale(ipat)) + float64(C.ttloc_ptr_get_utm_y_offset(ipat))

			if unicode.IsLetter(rune(C.ttloc_ptr_get_utm_latband(ipat))) {
				m_loc_text = fmt.Sprintf("%d%c %.0f %.0f", int(C.ttloc_ptr_get_utm_lzone(ipat)), C.ttloc_ptr_get_utm_latband(ipat), easting, northing)
			} else if C.ttloc_ptr_get_utm_latband(ipat) == '-' {
				m_loc_text = fmt.Sprintf("%d %.0f %.0f", int(-C.ttloc_ptr_get_utm_lzone(ipat)), easting, northing)
			} else {
				m_loc_text = fmt.Sprintf("%d %.0f %.0f", int(C.ttloc_ptr_get_utm_lzone(ipat)), easting, northing)
			}

			var lat0, lon0 C.double
			var lerr = C.Convert_UTM_To_Geodetic(C.ttloc_ptr_get_utm_lzone(ipat),
				C.ttloc_ptr_get_utm_hemi(ipat), C.double(easting), C.double(northing), &lat0, &lon0)

			if lerr == 0 {
				m_latitude = R2D(float64(lat0))
				m_longitude = R2D(float64(lon0))

				// dw_printf ("DEBUG: from UTM, latitude = %.6f, longitude = %.6f\n", m_latitude, m_longitude);
			} else {
				var message [300]C.char

				text_color_set(DW_COLOR_ERROR)
				C.utm_error_string(lerr, &message[0])
				dw_printf("Conversion from UTM failed:\n%s\n\n", C.GoString(&message[0]))
			}

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_MGRS, C.TTLOC_USNG:
			if len(xstr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("MGRS/USNG: Missing X (easting) coordinate.\n")
				/* Should not be possible to get here. Fake it and carry on. */
				xstr = "5"
			}
			if len(ystr) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("MGRS/USNG: Missing Y (northing) coordinate.\n")
				/* Should not be possible to get here. Fake it and carry on. */
				ystr = "5"
			}

			var _loc = C.ttloc_ptr_get_mgrs_zone(ipat)
			var loc = C.GoString(_loc)
			loc += xstr
			loc += ystr

			// text_color_set(DW_COLOR_DEBUG);
			// dw_printf ("MGRS/USNG location debug:  %s\n", loc);

			m_loc_text = loc

			var lerr C.long
			var lat0, lon0 C.double
			if C.ttloc_ptr_get(ipat)._type == C.TTLOC_MGRS {
				lerr = C.Convert_MGRS_To_Geodetic(C.CString(loc), &lat0, &lon0)
			} else {
				lerr = C.Convert_USNG_To_Geodetic(C.CString(loc), &lat0, &lon0)
			}

			if lerr == 0 {
				m_latitude = R2D(float64(lat0))
				m_longitude = R2D(float64(lon0))

				// dw_printf ("DEBUG: from MGRS/USNG, latitude = %.6f, longitude = %.6f\n", m_latitude, m_longitude);
			} else {
				var message [300]C.char

				text_color_set(DW_COLOR_ERROR)
				C.mgrs_error_string(lerr, &message[0])
				dw_printf("Conversion from MGRS/USNG failed:\n%s\n\n", C.GoString(&message[0]))
			}

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_MHEAD:

			/* Combine prefix from configuration and digits from user. */

			var stemp = C.GoString(C.ttloc_ptr_get_mhead_prefix(ipat))
			stemp += xstr

			if len(stemp) != 4 && len(stemp) != 6 && len(stemp) != 10 && len(stemp) != 12 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Expected total of 4, 6, 10, or 12 digits for the Maidenhead Locator \"%s\" + \"%s\"\n",
					C.GoString(C.ttloc_ptr_get_mhead_prefix(ipat)), xstr)
				return (C.TT_ERROR_INVALID_MHEAD)
			}

			// text_color_set(DW_COLOR_DEBUG);
			// dw_printf ("Case MHEAD: Convert to text \"%s\".\n", stemp);

			var mh [20]C.char
			if C.tt_mhead_to_text(C.CString(stemp), 0, &mh[0], C.ulong(len(mh))) == 0 {
				// text_color_set(DW_COLOR_DEBUG);
				// dw_printf ("Case MHEAD: Resulting text \"%s\".\n", mh);

				m_loc_text = C.GoString(&mh[0])

				var _m_latitude, _m_longitude C.double
				C.ll_from_grid_square(&mh[0], &_m_latitude, &_m_longitude)
				m_latitude = float64(_m_latitude)
				m_longitude = float64(_m_longitude)
			}

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_SATSQ:

			if len(xstr) != 4 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Expected 4 digits for the Satellite Square.\n")
				return (C.TT_ERROR_INVALID_SATSQ)
			}

			/* Convert 4 digits to usual AA99 form, then to location. */

			var mh [20]C.char
			if C.tt_satsq_to_text(C.CString(xstr), 0, &mh[0]) == 0 {
				m_loc_text = C.GoString(&mh[0])

				var _m_latitude, _m_longitude C.double
				C.ll_from_grid_square(&mh[0], &_m_latitude, &_m_longitude)
				m_latitude = float64(_m_latitude)
				m_longitude = float64(_m_longitude)
			}

			m_dao[2] = e[0]
			m_dao[3] = e[1]

		case C.TTLOC_AMBIG:

			if len(xstr) != 1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Expected 1 digits for the position ambiguity.\n")
				return (C.TT_ERROR_INVALID_LOC)
			}

			m_ambiguity, _ = strconv.Atoi(xstr)

		default:
			panic(fmt.Sprintf("Unknown ttloc_ptr type: %d", ttloc_type))
		}
		return (0)
	}

	/* Does not match any location specification. */

	text_color_set(DW_COLOR_ERROR)
	dw_printf("Received location \"%s\" does not match any definitions.\n", e)

	/* Send reject sound. */

	return (C.TT_ERROR_INVALID_LOC)
} /* end parse_location */

/*------------------------------------------------------------------
 *
 * Name:        find_ttloc_match
 *
 * Purpose:     Try to match the received position report to a pattern
 *		defined in the configuration file.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "B".
 *
 *		valstrsize	- size of the outputs so we can check for buffer overflow.
 *
 * Outputs:	xstr		- All digits matching x positions in configuration.
 *		ystr		-                     y
 *		zstr		-                     z
 *		bstr		-                     b
 * 		dstr		-                     d
 *
 * Returns:     >= 0 for index into table if found.
 *		-1 if not found.
 *
 * Description:
 *
 *----------------------------------------------------------------*/

func find_ttloc_match(e string) (string, string, string, string, string, int) {
	// debug dw_printf ("find_ttloc_match: e=%s\n", e);
	var xstr, ystr, zstr, bstr, dstr string

	for ipat := C.int(0); ipat < tt_config.ttloc_len; ipat++ {
		var _pattern = C.ttloc_ptr_get(ipat).pattern
		var pattern = C.GoString(&_pattern[0])
		var length = len(pattern) /* Length of pattern we are trying to match. */

		if len(e) == length {
			var match = true
			xstr = ""
			ystr = ""
			zstr = ""
			bstr = ""
			dstr = ""

			for k := range length {
				var mc = pattern[k]

				switch mc {
				case 'B', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'A', 'C', 'D': /* Allow A,C,D after the B? */
					if e[k] != mc {
						match = false
					}
				case 'x':
					if unicode.IsDigit(rune(e[k])) {
						xstr += string(e[k])
					} else {
						match = false
					}
				case 'y':
					if unicode.IsDigit(rune(e[k])) {
						ystr += string(e[k])
					} else {
						match = false
					}
				case 'z':
					if unicode.IsDigit(rune(e[k])) {
						zstr += string(e[k])
					} else {
						match = false
					}
				case 'b':
					if unicode.IsDigit(rune(e[k])) {
						bstr += string(e[k])
					} else {
						match = false
					}
				case 'd':
					if unicode.IsDigit(rune(e[k])) {
						dstr += string(e[k])
					} else {
						match = false
					}
				default:
					dw_printf("find_ttloc_match: shouldn't be here.\n")
					/* Shouldn't be here. */
					match = false
				} /* switch */
			} /* for k */

			if match {
				return xstr, ystr, zstr, bstr, dstr, int(ipat)
			}
		} /* if strlen */
	}
	return xstr, ystr, zstr, bstr, dstr, -1
} /* end find_ttloc_match */

/*------------------------------------------------------------------
 *
 * Name:        parse_comment
 *
 * Purpose:     Extract comment / status or other special information from touch tone message.
 *
 * Inputs:      e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "C".
 *
 * Outputs:	m_comment
 *		m_mic_e
 *
 * Returns:	0 for success or one of the TT_ERROR_... codes.
 *
 * Description:	We recognize these different formats:
 *
 *		Cn		- One digit (1-9) predefined status.  0 is reserved for none.
 *				  The defaults are derived from the MIC-E position comments
 *				  which were always "/" plus exactly 10 characters.
 *				  Users can override the defaults with configuration options.
 *
 *		Cnnnnnn		- Six digit frequency reformatted as nnn.nnnMHz
 *
 *		Cnnn		- Three digit are for CTCSS tone.  Use only integer part
 *				  and leading 0 if necessary to make exactly 3 digits.
 *
 *		Cttt...tttt	- General comment in Multi-press encoding.
 *
 *		CAttt...tttt	- New enhanced comment format that can handle all ASCII characters.
 *
 *----------------------------------------------------------------*/

func parse_comment(e string) int {
	Assert(e[0] == 'C')

	var length = len(e)

	if e[1] == 'A' {
		var _m_comment [200]C.char
		C.tt_ascii2d_to_text(C.CString(e[2:]), 0, &_m_comment[0])
		m_comment = C.GoString(&_m_comment[0])

		return (0)
	}

	if length == 2 && unicode.IsDigit(rune(e[1])) {
		m_mic_e = rune(e[1])
		return (0)
	}

	if length == 7 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) && unicode.IsDigit(rune(e[4])) && unicode.IsDigit(rune(e[5])) &&
		unicode.IsDigit(rune(e[6])) {
		m_freq = e[1:4] + "." + e[4:7] + "MHz"
		return (0)
	}

	if length == 4 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) {
		m_ctcss = e[1:]
		return (0)
	}

	var _m_comment [200]C.char
	C.tt_multipress_to_text(C.CString(e[1:]), 0, &_m_comment[0])
	m_comment = C.GoString(&_m_comment[0])

	return (0)
}

/*------------------------------------------------------------------
 *
 * Name:        raw_tt_data_to_app
 *
 * Purpose:     Send raw touch tone data to application.
 *
 * Inputs:      channel		- Channel where touch tone data heard.
 *		msg		- String of button pushes.
 *				  Normally ends with #.
 *
 * Global In:	m_callsign
 *		m_symtab_or_overlay
 *		m_symbol_code
 *
 * Returns:     None
 *
 * Description:
 * 		Put raw touch tone message in a packet and send to application.
 * 		The APRS protocol does not have provision for this.
 * 		For now, use the unused "t" message type.
 * 		TODO:  Get an officially sanctioned method.
 *
 * 		Use callsign for source if available.
 * 		Encode the overlay in the destination.
 *
 *----------------------------------------------------------------*/

func raw_tt_data_to_app(channel int, msg string) {
	// Set source and dest to something valid to keep rest of processing happy.
	// For lack of a better idea, make source "DTMF" to indicate where it came from.
	// Application version might be useful in case we end up using different
	// message formats in later versions.

	var src = "DTMF"
	var dest = fmt.Sprintf("%s%d%d", C.APP_TOCALL, C.MAJOR_VERSION, C.MINOR_VERSION)
	var raw_tt_msg = fmt.Sprintf("%s>%s:t%s", src, dest, msg)

	var pp = C.ax25_from_text(C.CString(raw_tt_msg), 1)

	/*
	 * Process like a normal received frame.
	 * NOTE:  This goes directly to application rather than
	 * thru the multi modem duplicate processing.
	 *
	 * Should we use a different type so it can be easily
	 * distinguished later?
	 *
	 * We try to capture an overall audio level here.
	 * Mark and space do not apply in this case.
	 * This currently doesn't get displayed but we might want it someday.
	 */

	if pp != nil {
		var alevel = C.demod_get_audio_level(C.int(channel), 0)
		alevel.mark = -2
		alevel.space = -2

		C.dlq_rec_frame(C.int(channel), -1, 0, pp, alevel, 0, C.RETRY_NONE, C.CString("tt"))
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not convert \"%s\" into APRS packet.\n", raw_tt_msg)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        dw_run_cmd
 *
 * Purpose:     Run a command and capture the output.
 *
 * Inputs:      cmd		- The command.
 *
 *		oneline		- 0 = Keep original line separators. Caller
 *					must deal with operating system differences.
 *				  1 = Change CR, LF, TAB to space so result
 *					is one line of text.
 *				  2 = Also remove any trailing whitespace.
 *
 * Description:	This is currently used for running a user-specified
 *		script to generate a custom speech response.
 *
 * Future:	There are potential other uses so it should probably
 *		be relocated to a file of other misc. utilities.
 *
 *----------------------------------------------------------------*/

func dw_run_cmd(cmd string, oneline int) ([]byte, error) {
	if oneline > 0 {
		cmd = strings.ReplaceAll(cmd, "\r", " ")
		cmd = strings.ReplaceAll(cmd, "\n", " ")
		cmd = strings.ReplaceAll(cmd, "\t", " ")
	}

	if oneline > 1 {
		cmd = strings.TrimSpace(cmd)
	}

	return exec.Command(cmd).Output()
}

/* end aprs_tt.c */
