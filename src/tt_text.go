package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Translate between text and touch tone representation.
 *
 * Description: Letters can be represented by different touch tone
 *		keypad sequences.
 *
 * References:	This is based upon APRStt (TM) documents but not 100%
 *		compliant due to ambiguities and inconsistencies in
 *		the specifications.
 *
 *		http://www.aprs.org/aprstt.html
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <string.h>
// #include <ctype.h>
// #include <stdarg.h>
// #include "textcolor.h"
// #include "tt_text.h"
// typedef const char const_char;
import "C"

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

/*
 * There are two different encodings called:
 *
 *   * Two-key
 *
 *		Digits are represented by a single key press.
 *		Letters (or space) are represented by the corresponding
 *		key followed by A, B, C, or D depending on the position
 *		of the letter.
 *
 *   * Multi-press
 *
 *		Letters are represented by one or more key presses
 *		depending on their position.
 *		e.g. on 5/JKL key, J = 1 press, K = 2, etc.
 *		The digit is the number of letters plus 1.
 *		In this case, press 5 key four times to get digit 5.
 *		When two characters in a row use the same key,
 *		use the "A" key as a separator.
 *
 * Examples:
 *
 *	Character	Multipress	Two Key		Comments
 *	---------	----------	-------		--------
 *	0		00		0		Space is handled like a letter.
 *	1		1		1		No letters on 1 button.
 *	2		2222		2		3 letters -> 4 key presses
 *	9		99999		9
 *	W		9		9A
 *	X		99		9B
 *	Y		999		9C
 *	Z		9999		9D
 *	space		0		0A		0A was used in an APRStt comment example.
 *
 *
 * Note that letters can occur in callsigns and comments.
 * Everywhere else they are simply digits.
 *
 *
 *   * New fixed length callsign format
 *
 *
 * 	The "QIKcom-2" project adds a new format where callsigns are represented by
 * 	a fixed length string of only digits.  The first 6 digits are the buttons corresponding
 * 	to the letters.  The last 4 take a little calculation.  Example:
 *
 *		W B 4 A P R	original.
 *		9 2 4 2 7 7	corresponding button.
 *		1 2 0 1 1 2	character position on key.  0 for the digit.
 *
 * 	Treat the last line as a base 4 number.
 * 	Convert it to base 10 and we get 1558 for the last four digits.
 */

/*
 * Everything is based on this table.
 * Changing it will change everything.
 * In other words, don't mess with it.
 * The world will come crumbling down.
 */

var translate = [10][4]rune{
	/*	 A	 B	 C	 D  */
	/*	---	---	---	--- */
	/* 0 */ {' ', 0, 0, 0},
	/* 1 */ {0, 0, 0, 0},
	/* 2 */ {'A', 'B', 'C', 0},
	/* 3 */ {'D', 'E', 'F', 0},
	/* 4 */ {'G', 'H', 'I', 0},
	/* 5 */ {'J', 'K', 'L', 0},
	/* 6 */ {'M', 'N', 'O', 0},
	/* 7 */ {'P', 'Q', 'R', 'S'},
	/* 8 */ {'T', 'U', 'V', 0},
	/* 9 */ {'W', 'X', 'Y', 'Z'}}

/*
 * This is for the new 10 character fixed length callsigns for APRStt 3.
 * Notice that it uses an old keypad layout with Q & Z on the 1 button.
 * The TH-D72A and all telephones that I could find all have
 * four letters each on the 7 and 9 buttons.
 * This inconsistency is sure to cause confusion but the 6+4 scheme won't
 * be possible with more than 4 characters assigned to one button.
 * 4**6-1 = 4096 which fits in 4 decimal digits.
 * 5**6-1 = 15624 would not fit.
 *
 * The column is a two bit code packed into the last 4 digits.
 */

var call10encoding = [10][4]rune{
	/*	 0	 1	 2	 3  */
	/*	---	---	---	--- */
	/* 0 */ {'0', ' ', 0, 0},
	/* 1 */ {'1', 'Q', 'Z', 0},
	/* 2 */ {'2', 'A', 'B', 'C'},
	/* 3 */ {'3', 'D', 'E', 'F'},
	/* 4 */ {'4', 'G', 'H', 'I'},
	/* 5 */ {'5', 'J', 'K', 'L'},
	/* 6 */ {'6', 'M', 'N', 'O'},
	/* 7 */ {'7', 'P', 'R', 'S'},
	/* 8 */ {'8', 'T', 'U', 'V'},
	/* 9 */ {'9', 'W', 'X', 'Y'}}

/*
 * Special satellite 4 digit gridsquares to cover "99.99% of the world's population."
 */

var grid = [10][10]string{
	{"AP", "BP", "AO", "BO", "CO", "DO", "EO", "FO", "GO", "OJ"}, // 0 - Canada
	{"CN", "DN", "EN", "FN", "GN", "CM", "DM", "EM", "FM", "OI"}, // 1 - USA
	{"DL", "EL", "FL", "DK", "EK", "FK", "EJ", "FJ", "GJ", "PI"}, // 2 - C. America
	{"FI", "GI", "HI", "FH", "GH", "HH", "FG", "GG", "FF", "GF"}, // 3 - S. America
	{"JP", "IO", "JO", "KO", "IN", "JN", "KN", "IM", "JM", "KM"}, // 4 - Europe
	{"LO", "MO", "NO", "OO", "PO", "QO", "RO", "LN", "MN", "NN"}, // 5 - Russia
	{"ON", "PN", "QN", "OM", "PM", "QM", "OL", "PL", "OK", "PK"}, // 6 - Japan, China
	{"LM", "MM", "NM", "LL", "ML", "NL", "LK", "MK", "NK", "LJ"}, // 7 - India
	{"PH", "QH", "OG", "PG", "QG", "OF", "PF", "QF", "RF", "RE"}, // 8 - Aus / NZ
	{"IL", "IK", "IJ", "JJ", "JI", "JH", "JG", "KG", "JF", "KF"}} // 9 - Africa

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_multipress
 *
 * Purpose:     Convert text to the multi-press representation.
 *
 * Inputs:      text	- Input string.
 *			  Should contain only digits, letters, or space.
 *			  All other punctuation is treated as space.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of buttons to press.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_multipress
func tt_text_to_multipress(_text *C.const_char, _quiet C.int, _buttons *C.char) C.int {

	var text = C.GoString(_text)
	var quiet = _quiet != 0
	*_buttons = 0

	var buttons = ""
	var errors C.int = 0

	for _, c := range text {

		if unicode.IsDigit(c) {

			/* Count number of other characters assigned to this button. */
			/* Press that number plus one more. */

			var n = 1
			var row = c - '0'
			for col := 0; col < 4; col++ {
				if translate[row][col] != 0 {
					n++
				}
			}
			if len(buttons) > 0 && rune(buttons[len(buttons)-1]) == row+'0' {
				buttons += "A"
			}
			for ; n > 0; n-- {
				buttons += string(row + '0')
			}
		} else {
			if unicode.IsUpper(c) {

			} else if unicode.IsLower(c) {
				c = unicode.ToUpper(c)
			} else if c != ' ' {
				errors++
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Text to multi-press: Only letters, digits, and space allowed.\n")
				}
				c = ' '
			}

			/* Search for everything else in the translation table. */
			/* Press number of times depending on column where found. */

			var found = false

			for row := 0; row < 10 && !found; row++ {
				for col := 0; col < 4 && !found; col++ {
					if c == translate[row][col] {

						/* Stick in 'A' if previous character used same button. */
						if len(buttons) > 0 && rune(buttons[len(buttons)-1]) == rune(row+'0') {
							buttons += "A"
						}
						for n := col + 1; n > 0; n-- {
							buttons += string(rune(row + '0'))
							found = true
						}
					}
				}
			}
			if !found {
				errors++
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Text to multi-press: INTERNAL ERROR.  Should not be here.\n")
			}
		}
	}

	C.strcpy(_buttons, C.CString(buttons))

	return errors

} /* end tt_text_to_multipress */

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_two_key
 *
 * Purpose:     Convert text to the two-key representation.
 *
 * Inputs:      text	- Input string.
 *			  Should contain only digits, letters, or space.
 *			  All other punctuation is treated as space.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of buttons to press.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_two_key
func tt_text_to_two_key(_text *C.const_char, _quiet C.int, _buttons *C.char) C.int {

	var text = C.GoString(_text)
	var quiet = _quiet != 0
	*_buttons = 0

	var buttons = ""
	var errors C.int = 0

	for _, c := range text {
		if unicode.IsDigit(c) {

			/* Digit is single key press. */

			buttons += string(c)
		} else {
			if unicode.IsUpper(c) {

			} else if unicode.IsLower(c) {
				c = unicode.ToUpper(c)
			} else if c != ' ' {
				errors++
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Text to two key: Only letters, digits, and space allowed.\n")
				}
				c = ' '
			}

			/* Search for everything else in the translation table. */

			var found = false

			for row := 0; row < 10 && !found; row++ {
				for col := 0; col < 4 && !found; col++ {
					if c == translate[row][col] {
						buttons += string(rune(row + '0'))
						buttons += string(rune(col + 'A'))
						found = true
					}
				}
			}
			if !found {
				errors++
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Text to two-key: INTERNAL ERROR.  Should not be here.\n")
			}
		}
	}

	C.strcpy(_buttons, C.CString(buttons))

	return (errors)

} /* end tt_text_to_two_key */

/*------------------------------------------------------------------
 *
 * Name:        tt_letter_to_two_digits
 *
 * Purpose:     Convert one letter to 2 digit representation.
 *
 * Inputs:      c	- One letter.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of two buttons to press.
 *			  "00" for error because this is probably
 *			  being used to build up a fixed length
 *			  string where positions are significant.
 *			  Must be at least 3 bytes.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

// TODO:  need to test this.

func tt_letter_to_two_digits(c rune, quiet bool) (string, C.int) {

	var errors C.int = 0

	var buttons string

	if unicode.IsLower(c) {
		c = unicode.ToUpper(c)
	}

	if !unicode.IsUpper(c) {
		errors++
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Letter to two digits: \"%c\" found where a letter is required.\n", c)
		}
		return "00", errors
	}

	/* Search in the translation table. */

	var found = false

	for row := 0; row < 10 && !found; row++ {
		for col := 0; col < 4 && !found; col++ {
			if c == translate[row][col] {
				buttons = string(rune('0'+row)) + string(rune('1'+col))
				found = true
			}
		}
	}
	if !found {
		errors++
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Letter to two digits: INTERNAL ERROR.  Should not be here.\n")
		return "00", errors
	}

	return buttons, errors

} /* end tt_letter_to_two_digits */

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_call10
 *
 * Purpose:     Convert text to the 10 character callsign format.
 *
 * Inputs:      text	- Input string.
 *			  Should contain from 1 to 6 letters and digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of buttons to press.
 *			  Should be exactly 10 unless error.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_call10
func tt_text_to_call10(_text *C.const_char, _quiet C.int, _buttons *C.char) C.int {

	var text = C.GoString(_text)
	var quiet = _quiet != 0

	var errors C.int = 0

	// FIXME: Add parameter for sizeof buttons and use strlcpy
	C.strcpy(_buttons, C.CString(""))

	/* Quick validity check. */

	if len(text) < 1 || len(text) > 6 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Text to callsign 6+4: Callsign \"%s\" not between 1 and 6 characters.\n", text)
		}
		errors++
		return (errors)
	}

	for _, t := range text {
		if !unicode.IsLetter(t) && !unicode.IsDigit(t) {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Text to callsign 6+4: Callsign \"%s\" can contain only letters and digits.\n", text)
			}
			errors++
			return (errors)
		}
	}

	/* Append spaces if less than 6 characters. */

	var padded = text
	for len(padded) < 6 {
		padded += " "
	}

	var packed = 0 // two bits per character
	var buttons string

	for _, c := range padded {
		if unicode.IsLower(c) {
			c = unicode.ToUpper(c)
		}

		/* Search in the translation table. */

		var found = false

		for row := 0; row < 10 && !found; row++ {
			for col := 0; col < 4 && !found; col++ {
				if c == call10encoding[row][col] {
					buttons += string(rune('0' + row))
					packed = packed*4 + col /* base 4 to binary */
					found = true
				}
			}
		}

		if !found {
			/* Earlier check should have caught any character not in translation table. */
			errors++
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Text to callsign 6+4: INTERNAL ERROR 0x%02x.  Should not be here.\n", c)
		}
	}

	/* Binary to decimal for the columns. */

	buttons += fmt.Sprintf("%04d", packed)
	C.strcpy(_buttons, C.CString(buttons))

	return (errors)

} /* end tt_text_to_call10 */

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_satsq
 *
 * Purpose:     Convert Special Satellite Gridsquare to 4 digit DTMF representation.
 *
 * Inputs:      text	- Input string.
 *			  Should be two letters (A thru R) and two digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of buttons to press.
 *			  Should be 4 digits unless error.
 *
 * Returns:     Number of errors detected.
 *
 * Example:	"FM19" is converted to "1819."
 *		"AA00" is converted to empty string and error return code.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_satsq
func tt_text_to_satsq(_text *C.const_char, _quiet C.int, _buttons *C.char, buttonsize C.size_t) C.int {

	var text = C.GoString(_text)
	var quiet = _quiet != 0

	C.strcpy(_buttons, C.CString(""))

	var errors C.int = 0

	/* Quick validity check. */

	if len(text) != 4 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Satellite Gridsquare to DTMF: Gridsquare \"%s\" must be 4 characters.\n", text)
		}
		errors++
		return (errors)
	}

	/* Changing to upper case makes things easier later. */

	var uc = strings.ToUpper(text[0:2])

	if uc[0] < 'A' || uc[0] > 'R' || uc[1] < 'A' || uc[1] > 'R' {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Satellite Gridsquare to DTMF: First two characters \"%s\" must be letters in range of A to R.\n", text)
		}
		errors++
		return (errors)
	}

	if !unicode.IsDigit(rune(text[2])) || !unicode.IsDigit(rune(text[3])) {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Satellite Gridsquare to DTMF: Last two characters \"%s\" must be digits.\n", text)
		}
		errors++
		return (errors)
	}

	/* Search in the translation table. */

	var found = false

	for row := 0; row < 10 && !found; row++ {
		for col := 0; col < 10 && !found; col++ {
			if uc == grid[row][col] {

				var btemp [8]C.char

				btemp[0] = C.char(row + '0')
				btemp[1] = C.char(col + '0')
				btemp[2] = C.char(text[2])
				btemp[3] = C.char(text[3])
				btemp[4] = 0

				C.strlcpy(_buttons, &btemp[0], buttonsize)
				found = true
			}
		}
	}

	if !found {
		/* Sorry, Greenland, and half of Africa, and ... */
		errors++
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Satellite Gridsquare to DTMF: Sorry, your location can't be converted to DTMF.\n")
		}
	}

	return (errors)

} /* end tt_text_to_satsq */

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_ascii2d
 *
 * Purpose:     Convert text to the two digit per ascii character representation.
 *
 * Inputs:      text	- Input string.
 *			  Any printable ASCII characters.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	buttons	- Sequence of buttons to press.
 *
 * Returns:     Number of errors detected.
 *
 * Description:	The standard comment format uses the multipress
 *		encoding which allows only single case letters, digits,
 *		and the space character.
 *		This is a more flexible format that can handle all
 *		printable ASCII characters.  We take the character code,
 *		subtract 32 and convert to two decimal digits.  i.e.
 *			space	= 00
 *			!	= 01
 *			"	= 02
 *			...
 *			~	= 94
 *
 *		This is mostly for internal use, so macros can generate
 *		comments with all characters.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_ascii2d
func tt_text_to_ascii2d(_text *C.const_char, _quiet C.int, _buttons *C.char) C.int {

	var text = C.GoString(_text)

	var errors C.int = 0
	var buttons = ""

	for _, c := range text {

		/* "isprint()" might depend on locale so use brute force. */

		if c < ' ' || c > '~' {
			c = '?'
		}

		var n = c - 32

		buttons += string((n / 10) + '0')
		buttons += string((n % 10) + '0')
	}

	C.strcpy(_buttons, C.CString(buttons))

	return (errors)

} /* end tt_text_to_ascii2d */

/*------------------------------------------------------------------
 *
 * Name:        tt_multipress_to_text
 *
 * Purpose:     Convert the multi-press representation to text.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain only 0123456789A.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to letters, digits, space.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_multipress_to_text
func tt_multipress_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var text string
	var errors C.int = 0

	for i := 0; i < len(buttons); i++ {
		var c = rune(buttons[i])
		if unicode.IsDigit(c) {

			/* Determine max that can occur in a row. */
			/* = number of other characters assigned to this button + 1. */

			var maxspan = 1
			var row = c - '0'
			for col := 0; col < 4; col++ {
				if translate[row][col] != 0 {
					maxspan++
				}
			}

			/* Count number of consecutive same digits. */

			var n = 1
			for j := i + 1; j < len(buttons); j++ {
				if rune(buttons[j]) != c {
					break
				}
				n++
				i = j // Update the main index
			}

			if n < maxspan {
				text += string(translate[row][n-1])
			} else if n == maxspan {
				text += string(c)
			} else {
				errors++
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Multi-press to text: Maximum of %d \"%c\" can occur in a row.\n", maxspan, c)
				}
				/* Treat like the maximum length. */
				text += string(c)
			}
		} else if c == 'A' || c == 'a' {

			/* Separator should occur only if digit before and after are the same. */

			if i == 0 || i == len(buttons)-1 || buttons[i-1] != buttons[i+1] {
				errors++
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Multi-press to text: \"A\" can occur only between two same digits.\n")
				}
			}
		} else {

			/* Completely unexpected character. */

			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Multi-press to text: \"%c\" not allowed.\n", c)
			}
		}
	}

	C.strcpy(_text, C.CString(text))
	return errors
} /* end tt_multipress_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_two_key_to_text
 *
 * Purpose:     Convert the two key representation to text.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain only 0123456789ABCD.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to letters, digits, space.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_two_key_to_text
func tt_two_key_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var errors C.int = 0
	var text string

	for i := 0; i < len(buttons); i++ {
		var c = rune(buttons[i])
		if unicode.IsDigit(c) {

			/* Letter (or space) if followed by ABCD. */

			var row = c - '0'
			var col = -1

			if i+1 < len(buttons) {
				var b = buttons[i+1]
				if b >= 'A' && b <= 'D' {
					col = int(b - 'A')
				} else if b >= 'a' && b <= 'd' {
					col = int(b - 'a')
				}
			}

			if col >= 0 {
				if translate[row][col] != 0 {
					text += string(translate[row][col])
				} else {
					errors++
					if !quiet {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Two key to text: Invalid combination \"%c%c\".\n", c, col+'A')
					}
				}
				i++ // Skip the next character since we consumed it
			} else {
				text += string(c)
			}
		} else if (c >= 'A' && c <= 'D') || (c >= 'a' && c <= 'd') {

			/* ABCD not expected here. */

			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Two-key to text: A, B, C, or D in unexpected location.\n")
			}
		} else {

			/* Completely unexpected character. */

			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Two-key to text: Invalid character \"%c\".\n", c)
			}
		}
	}

	C.strcpy(_text, C.CString(text))

	return (errors)

} /* end tt_two_key_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_two_digits_to_letter
 *
 * Purpose:     Convert the two digit representation to one letter.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain exactly two digits.
 *
 *		quiet	- True to suppress error messages.
 *
 *		textsiz	- Size of result storage.  Typically 2.
 *
 * Outputs:	text	- Converted to string which should contain one upper case letter.
 *			  Empty string on error.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_two_digits_to_letter
func tt_two_digits_to_letter(_buttons *C.const_char, _quiet C.int, _text *C.char, textsiz C.size_t) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var text string
	var errors C.int = 0

	var c1 = buttons[0]
	var c2 = buttons[1]

	if c1 >= '2' && c1 <= '9' {

		if c2 >= '1' && c2 <= '4' {

			var row = c1 - '0'
			var col = c2 - '1'

			if translate[row][col] != 0 {

				text += string(translate[row][col])
			} else {
				errors++
				text = ""
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Two digits to letter: Invalid combination \"%c%c\".\n", c1, c2)
				}
			}
		} else {
			errors++
			text = ""
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Two digits to letter: Second character \"%c\" must be in range of 1 through 4.\n", c2)
			}
		}
	} else {
		errors++
		text = ""
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Two digits to letter: First character \"%c\" must be in range of 2 through 9.\n", c1)
		}
	}

	C.strlcpy(_text, C.CString(text), textsiz)

	return (errors)

} /* end tt_two_digits_to_letter */

/*------------------------------------------------------------------
 *
 * Name:        tt_call10_to_text
 *
 * Purpose:     Convert the 10 digit callsign representation to text.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain only ten digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to callsign with upper case letters and digits.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_call10_to_text
func tt_call10_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0 /* result */

	var text string
	var errors C.int = 0

	/* Validity check. */

	if len(buttons) != 10 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Callsign 6+4 to text: Encoded Callsign \"%s\" must be exactly 10 digits.\n", buttons)
		}
		errors++
		return (errors)
	}

	for _, b := range buttons {
		if !unicode.IsDigit(b) {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Callsign 6+4 to text: Encoded Callsign \"%s\" can contain only digits.\n", buttons)
			}
			errors++
			return (errors)
		}
	}

	var packed, _ = strconv.Atoi(buttons[6:])

	for k := 0; k < 6; k++ {
		var c = buttons[k]

		var row = c - '0'
		var col = (packed >> ((5 - k) * 2)) & 3

		if row < 0 || row > 9 || col < 0 || col > 3 { //nolint:staticcheck
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Callsign 6+4 to text: INTERNAL ERROR %d %d.  Should not be here.\n", row, col)
			errors++
			row = 0
			col = 1
		}

		if call10encoding[row][col] != 0 {
			text += string(call10encoding[row][col])
		} else {
			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Callsign 6+4 to text: Invalid combination: button %d, position %d.\n", row, col)
			}
		}
	}

	/* Trim any trailing spaces. */

	text = strings.TrimSpace(text)

	C.strcpy(_text, C.CString(text))

	return (errors)

} /* end tt_call10_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_call5_suffix_to_text
 *
 * Purpose:     Convert the 5 digit APRStt 3 style callsign suffix
 *		representation to text.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain exactly 5 digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to 3 upper case letters and/or digits.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_call5_suffix_to_text
func tt_call5_suffix_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var text string
	var errors C.int = 0

	/* Validity check. */

	if len(buttons) != 5 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Callsign 3+2 suffix to text: Encoded Callsign \"%s\" must be exactly 5 digits.\n", buttons)
		}
		errors++
		return (errors)
	}

	for _, b := range buttons {
		if !unicode.IsDigit(b) {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Callsign 3+2 suffix to text: Encoded Callsign \"%s\" can contain only digits.\n", buttons)
			}
			errors++
			return (errors)
		}
	}

	var packed, _ = strconv.Atoi(buttons[3:])

	for k := 0; k < 3; k++ {
		var c = buttons[k]

		var row = c - '0'
		var col = (packed >> ((2 - k) * 2)) & 3

		if row < 0 || row > 9 || col < 0 || col > 3 { //nolint:staticcheck
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Callsign 3+2 suffix to text: INTERNAL ERROR %d %d.  Should not be here.\n", row, col)
			errors++
			row = 0
			col = 1
		}

		if call10encoding[row][col] != 0 {
			text += string(call10encoding[row][col])
		} else {
			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Callsign 3+2 suffix to text: Invalid combination: button %d, position %d.\n", row, col)
			}
		}
	}

	if errors > 0 {
		*_text = 0
		return (errors)
	}

	C.strcpy(_text, C.CString(text))

	return (errors)

} /* end tt_call5_suffix_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_mhead_to_text
 *
 * Purpose:     Convert the DTMF representation of
 *		Maidenhead Grid Square Locator to normal text representation.
 *
 * Inputs:      buttons	- Input string.
 *			  Must contain 4, 6, 10, or 12, 16, or 18 digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to gridsquare with upper case letters and digits.
 *			  Length should be 2, 4, 6, or 8 with alternating letter or digit pairs.
 *			  Zero length if any error.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

const MAXMHPAIRS = 6

type mhpairType struct {
	position string
	min_ch   rune
	max_ch   rune
}

var mhpair = [MAXMHPAIRS]mhpairType{
	{"first", 'A', 'R'},
	{"second", '0', '9'},
	{"third", 'A', 'X'},
	{"fourth", '0', '9'},
	{"fifth", 'A', 'X'},
	{"sixth", '0', '9'},
}

//export tt_mhead_to_text
func tt_mhead_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char, textsiz C.size_t) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var text string
	var errors C.int = 0

	/* Validity check. */

	if len(buttons) != 4 && len(buttons) != 6 &&
		len(buttons) != 10 && len(buttons) != 12 &&
		len(buttons) != 16 && len(buttons) != 18 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("DTMF to Maidenhead Gridsquare Locator: Input \"%s\" must be exactly 4, 6, 10, or 12 digits.\n", buttons)
		}
		errors++
		return (errors)
	}

	for _, b := range buttons {
		if !unicode.IsDigit(b) {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("DTMF to Maidenhead Gridsquare Locator: Input \"%s\" can contain only digits.\n", buttons)
			}
			errors++
			return (errors)
		}
	}

	/* Convert DTMF to normal representation. */

	// Maidenhead locators are alternating pairs of letters and digits.
	// DTMF encodes letters as two digits, digits unchanged, so chomp 4 digits then 2 digits alternately
	// Rely on earlier length check to ensure we don't run out of buttons
	for n := 0; len(buttons) > 0; n++ {
		if n%2 == 0 {
			var t2 [2]C.char

			errors += tt_two_digits_to_letter(C.CString(buttons), _quiet, &t2[0], C.size_t(len(t2)))
			text += C.GoString(&t2[0])
			buttons = buttons[2:]

			errors += tt_two_digits_to_letter(C.CString(buttons), _quiet, &t2[0], C.size_t(len(t2)))
			text += C.GoString(&t2[0])
			buttons = buttons[2:]
		} else {
			text += buttons[0:2]
			buttons = buttons[2:]
		}
	}

	// No need to handle case where there's anything remaining - we checked earlier

	if errors != 0 {
		text = ""
	}

	C.strcpy(_text, C.CString(text))

	return (errors)

} /* end tt_mhead_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_text_to_mhead
 *
 * Purpose:     Convert normal text Maidenhead Grid Square Locator to DTMF representation.
 *
 * Inputs:	text	- Maidenhead Grid Square locator in usual format.
 *			  Length should be 1 to 6 pairs with alternating letter or digit pairs.
 *
 *		quiet	- True to suppress error messages.
 *
 *		buttonsize - space available for 'buttons' result.
 *
 * Outputs:	buttons	- Result with 4, 6, 10, 12, 16, 18 digits.
 *			  Each letter is replaced by two digits.
 *			  Digits are simply copied.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_text_to_mhead
func tt_text_to_mhead(_text *C.const_char, _quiet C.int, _buttons *C.char, buttonsize C.size_t) C.int {

	var text = C.GoString(_text)
	var quiet = _quiet != 0
	*_buttons = 0

	var errors C.int = 0
	var buttons string

	var np = len(text) / 2

	if (len(text) % 2) != 0 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Maidenhead Gridsquare Locator to DTMF: Input \"%s\" must be even number of characters.\n", text)
		}
		errors++
		return (errors)
	}

	if np < 1 || np > MAXMHPAIRS {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Maidenhead Gridsquare Locator to DTMF: Input \"%s\" must be 1 to %d pairs of characters.\n", text, np)
		}
		errors++
		return (errors)
	}

	for i := 0; i < np; i++ {

		var t0 = rune(text[i*2])
		var t1 = rune(text[i*2+1])

		if unicode.ToUpper(t0) < mhpair[i].min_ch || unicode.ToUpper(t0) > mhpair[i].max_ch ||
			unicode.ToUpper(t1) < mhpair[i].min_ch || unicode.ToUpper(t1) > mhpair[i].max_ch {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("The %s pair of characters in Maidenhead locator \"%s\" must be in range of %c thru %c.\n",
					mhpair[i].position, text, mhpair[i].min_ch, mhpair[i].max_ch)
			}
			*_buttons = 0
			errors++
			return (errors)
		}

		if mhpair[i].min_ch == 'A' { /* Should be letters */

			var b3, _errors = tt_letter_to_two_digits(t0, quiet)
			errors += _errors
			buttons += b3

			b3, _errors = tt_letter_to_two_digits(t1, quiet)
			errors += _errors
			buttons += b3
		} else { /* Should be digits */

			buttons += string(t0) + string(t1)
		}
	}

	if errors != 0 {
		buttons = ""
	}

	C.strcpy(_buttons, C.CString(buttons))

	return (errors)

} /* tt_text_to_mhead */

/*------------------------------------------------------------------
 *
 * Name:        tt_satsq_to_text
 *
 * Purpose:     Convert the 4 digit DTMF special Satellite gridsquare to normal 2 letters and 2 digits.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain 4 digits.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to gridsquare with upper case letters and digits.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_satsq_to_text
func tt_satsq_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var errors C.int = 0

	/* Validity check. */

	if len(buttons) != 4 {

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("DTMF to Satellite Gridsquare: Input \"%s\" must be exactly 4 digits.\n", buttons)
		}
		errors++
		return (errors)
	}

	for _, b := range buttons {
		if !unicode.IsDigit(b) {
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("DTMF to Satellite Gridsquare: Input \"%s\" can contain only digits.\n", buttons)
			}
			errors++
			return (errors)
		}
	}

	var row = buttons[0] - '0'
	var col = buttons[1] - '0'

	// FIXME: Add parameter for sizeof text and use strlcpy, strlcat.
	var text = grid[row][col] + buttons[2:]

	C.strcpy(_text, C.CString(text))

	return (errors)

} /* end tt_satsq_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_ascii2d_to_text
 *
 * Purpose:     Convert the two digit ascii representation back to normal text.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain pairs of digits in range 00 to 94.
 *
 *		quiet	- True to suppress error messages.
 *
 * Outputs:	text	- Converted to any printable ascii characters.
 *
 * Returns:     Number of errors detected.
 *
 *----------------------------------------------------------------*/

//export tt_ascii2d_to_text
func tt_ascii2d_to_text(_buttons *C.const_char, _quiet C.int, _text *C.char) C.int {

	var buttons = C.GoString(_buttons)
	var quiet = _quiet != 0
	*_text = 0

	var text string
	var errors C.int = 0

	// TODO KG
	if len(buttons)%2 != 0 {
		return 1
	}

	for i := range len(buttons) / 2 {
		var c1 = rune(buttons[2*i])
		var c2 = rune(buttons[2*i+1])

		if unicode.IsDigit(c1) && unicode.IsDigit(c2) {
			var n = (c1-'0')*10 + (c2 - '0')

			text += string(n + 32)
		} else {
			// Unexpected character.

			errors++
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("ASCII2D to text: Invalid character pair \"%c%c\".\n", c1, c2)
			}
		}
	}
	return (errors)

} /* end tt_ascii2d_to_text */

/*------------------------------------------------------------------
 *
 * Name:        tt_guess_type
 *
 * Purpose:     Try to guess which encoding we have.
 *
 * Inputs:      buttons	- Input string.
 *			  Should contain only 0123456789ABCD.
 *
 * Returns:     TT_MULTIPRESS	- Looks like multipress.
 *		TT_TWO_KEY	- Looks like two key.
 *		TT_EITHER	- Could be either one.
 *
 *----------------------------------------------------------------*/

//export tt_guess_type
func tt_guess_type(_buttons *C.char) C.tt_enc_t {

	var buttons = C.GoString(_buttons)

	var text [256]C.char

	/* If it contains B, C, or D, it can't be multipress. */

	if strings.ContainsAny(buttons, "BCDbcd") {
		return (C.TT_TWO_KEY)
	}

	/* Try parsing quietly and see if one gets errors and the other doesn't. */

	var err_mp = tt_multipress_to_text(_buttons, 1, &text[0])
	var err_tk = tt_two_key_to_text(_buttons, 1, &text[0])

	if err_mp == 0 && err_tk > 0 {
		return (C.TT_MULTIPRESS)
	} else if err_tk == 0 && err_mp > 0 {
		return (C.TT_TWO_KEY)
	}

	/* Could be either one. */

	return (C.TT_EITHER)

} /* end tt_guess_type */

/* end tt_text.c */
