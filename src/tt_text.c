//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2013, 2015  John Langner, WB2OSZ
//
//    This program is free software: you can redistribute it and/or modify
//    it under the terms of the GNU General Public License as published by
//    the Free Software Foundation, either version 2 of the License, or
//    (at your option) any later version.
//
//    This program is distributed in the hope that it will be useful,
//    but WITHOUT ANY WARRANTY; without even the implied warranty of
//    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//    GNU General Public License for more details.
//
//    You should have received a copy of the GNU General Public License
//    along with this program.  If not, see <http://www.gnu.org/licenses/>.
//

/*------------------------------------------------------------------
 *
 * Module:      tt_text.c
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

static const char translate[10][4] = {
		/*	 A	 B	 C	 D  */
		/*	---	---	---	--- */
	/* 0 */	{	' ',	 0,	 0,	 0  },
	/* 1 */	{	 0,	 0,	 0,	 0  },
	/* 2 */	{	'A',	'B',	'C',	 0  },
	/* 3 */	{	'D',	'E',	'F',	 0  },
	/* 4 */	{	'G',	'H',	'I',	 0  },
	/* 5 */	{	'J',	'K',	'L',	 0  },
	/* 6 */	{	'M',	'N',	'O',	 0  },
	/* 7 */	{	'P',	'Q',	'R',	'S' },
	/* 8 */	{	'T',	'U',	'V',	 0  },
	/* 9 */	{	'W',	'X',	'Y',	'Z' } };


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

static const char call10encoding[10][4] = {
		/*	 0	 1	 2	 3  */
		/*	---	---	---	--- */
	/* 0 */	{	'0',	' ',	 0,	 0   },
	/* 1 */	{	'1',	'Q',	'Z',	 0   },
	/* 2 */	{	'2',	'A',	'B',	'C'  },
	/* 3 */	{	'3',	'D',	'E',	'F'  },
	/* 4 */	{	'4',	'G',	'H',	'I'  },
	/* 5 */	{	'5',	'J',	'K',	'L'  },
	/* 6 */	{	'6',	'M',	'N',	'O'  },
	/* 7 */	{	'7',	'P',	'R',	'S'  },
	/* 8 */	{	'8',	'T',	'U',	'V'  },
	/* 9 */	{	'9',	'W',	'X',	'Y'  } };


/*
 * Special satellite 4 digit gridsquares to cover "99.99% of the world's population."
 */

static const char grid[10][10][3] =      
     {  { "AP", "BP", "AO", "BO", "CO", "DO", "EO", "FO", "GO", "OJ" },		// 0 - Canada
        { "CN", "DN", "EN", "FN", "GN", "CM", "DM", "EM", "FM", "OI" },		// 1 - USA
        { "DL", "EL", "FL", "DK", "EK", "FK", "EJ", "FJ", "GJ", "PI" },		// 2 - C. America
        { "FI", "GI", "HI", "FH", "GH", "HH", "FG", "GG", "FF", "GF" },		// 3 - S. America
        { "JP", "IO", "JO", "KO", "IN", "JN", "KN", "IM", "JM", "KM" },		// 4 - Europe
        { "LO", "MO", "NO", "OO", "PO", "QO", "RO", "LN", "MN", "NN" },		// 5 - Russia
        { "ON", "PN", "QN", "OM", "PM", "QM", "OL", "PL", "OK", "PK" },		// 6 - Japan, China
        { "LM", "MM", "NM", "LL", "ML", "NL", "LK", "MK", "NK", "LJ" },		// 7 - India
        { "PH", "QH", "OG", "PG", "QG", "OF", "PF", "QF", "RF", "RE" },		// 8 - Aus / NZ
        { "IL", "IK", "IJ", "JJ", "JI", "JH", "JG", "KG", "JF", "KF" }  };	// 9 - Africa

#include "direwolf.h"

#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <ctype.h>
#include <assert.h>
#include <stdarg.h>

#include "textcolor.h"
#include "tt_text.h"


#if defined(ENC_MAIN) || defined(DEC_MAIN)

void text_color_set (dw_color_t c) { return; }

int dw_printf (const char *fmt, ...) 
{
	va_list args;
	int len;
	
	va_start (args, fmt);
	len = vprintf (fmt, args);
	va_end (args);
	return (len);
}

#endif


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

int tt_text_to_multipress (const char *text, int quiet, char *buttons)
{
	const char *t = text;
	char *b = buttons;
	char c;
	int row, col;
	int errors = 0;
	int found;
	int n;

	*b = '\0';
	
	while ((c = *t++) != '\0') {

	  if (isdigit(c)) {
	
/* Count number of other characters assigned to this button. */
/* Press that number plus one more. */

	    n = 1;
	    row = c - '0';
	    for (col=0; col<4; col++) {
	      if (translate[row][col] != 0) {
	        n++;
	      }
	    }
	    if (buttons[0] != '\0' && *(b-1) == row + '0') {
	      *b++ = 'A';
	    }
	    while (n--) {
	      *b++ = row + '0';
	      *b = '\0';
	    }
	  }
	  else {
	    if (isupper(c)) {
	      ;
	    }	  
	    else if (islower(c)) {
	      c = toupper(c);
	    }
	    else if (c != ' ') {
	      errors++;
	      if (! quiet) {
	        text_color_set (DW_COLOR_ERROR);
		dw_printf ("Text to multi-press: Only letters, digits, and space allowed.\n");
	      }
	      c = ' ';
	    }

/* Search for everything else in the translation table. */
/* Press number of times depending on column where found. */

	    found = 0;

	    for (row=0; row<10 && ! found; row++) {
	      for (col=0; col<4 && ! found; col++) {
	        if (c == translate[row][col]) {

/* Stick in 'A' if previous character used same button. */

	          if (buttons[0] != '\0' && *(b-1) == row + '0') {
	            *b++ = 'A';
	          }
	          n = col + 1;
	          while (n--) {
	            *b++ = row + '0';
	            *b = '\0';
	            found = 1;
	          }
	        }
	      }
	    }
	    if (! found) {
	      errors++;
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Text to multi-press: INTERNAL ERROR.  Should not be here.\n");
	    }
	  }
	}
	return (errors);          

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

int tt_text_to_two_key (const char *text, int quiet, char *buttons)
{
	const char *t = text;
	char *b = buttons;
	char c;
	int row, col;
	int errors = 0;
	int found;


	*b = '\0';
	
	while ((c = *t++) != '\0') {

	  if (isdigit(c)) {
	
/* Digit is single key press. */
	  
	    *b++ = c;
	    *b = '\0';
	  }
	  else {
	    if (isupper(c)) {
	      ;
	    }	  
	    else if (islower(c)) {
	      c = toupper(c);
	    }
	    else if (c != ' ') {
	      errors++;
	      if (! quiet) {
	        text_color_set (DW_COLOR_ERROR);
		dw_printf ("Text to two key: Only letters, digits, and space allowed.\n");
	      }
	      c = ' ';
	    }

/* Search for everything else in the translation table. */

	    found = 0;

	    for (row=0; row<10 && ! found; row++) {
	      for (col=0; col<4 && ! found; col++) {
	        if (c == translate[row][col]) {
		  *b++ = '0' + row;
	          *b++ = 'A' + col;
	          *b = '\0';
	          found = 1;
	        }
	      }
	    }
	    if (! found) {
	      errors++;
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Text to two-key: INTERNAL ERROR.  Should not be here.\n");
	    }
	  }
	}
	return (errors);          

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

int tt_letter_to_two_digits (char c, int quiet, char buttons[3])
{
	int row, col;
	int errors = 0;
	int found;

	strlcpy(buttons, "", 3);
  
	if (islower(c)) {
	  c = toupper(c);
	}

	if ( ! isupper(c)) {
	  errors++;
	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Letter to two digits: \"%c\" found where a letter is required.\n", c);
	  }
	  strlcpy (buttons, "00", 3);
	  return (errors);
	}

/* Search in the translation table. */

	found = 0;

	for (row=0; row<10 && ! found; row++) {
	  for (col=0; col<4 && ! found; col++) {
	    if (c == translate[row][col]) {
	      buttons[0] = '0' + row;
	      buttons[1] = '1' + col;
	      buttons[2] = '\0';
	      found = 1;
	    }
	  }
	 }
	 if (! found) {
	  errors++;
	  text_color_set (DW_COLOR_ERROR);
	  dw_printf ("Letter to two digits: INTERNAL ERROR.  Should not be here.\n");
	  strlcpy (buttons, "00", 3);
	}

	return (errors);          

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

int tt_text_to_call10 (const char *text, int quiet, char *buttons)
{
	const char *t;
	char *b;
	char c;
	int packed;		/* two bits per character */
	int row, col;
	int errors = 0;
	int found;
	char padded[8];
	char stemp[11];

	// FIXME: Add parameter for sizeof buttons and use strlcpy
	strcpy (buttons, "");

/* Quick validity check. */
	
	if (strlen(text) < 1 || strlen(text) > 6) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Text to callsign 6+4: Callsign \"%s\" not between 1 and 6 characters.\n", text);
	  }
	  errors++;
	  return (errors);
   	}

	for (t = text; *t != '\0'; t++) {

	  if (! isalnum(*t)) {
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Text to callsign 6+4: Callsign \"%s\" can contain only letters and digits.\n", text);
	    }
	    errors++;
	    return (errors);
	  }
   	}

/* Append spaces if less than 6 characters. */

	strcpy (padded, text);
	while (strlen(padded) < 6) {
	  strcat (padded, " ");
	}

	b = buttons;
	packed = 0;

	for (t = padded; *t != '\0'; t++) {
	
	  c = *t;
	  if (islower(c)) {
	      c = toupper(c);
	  }

/* Search in the translation table. */

	  found = 0;

	  for (row=0; row<10 && ! found; row++) {
	    for (col=0; col<4 && ! found; col++) {
	      if (c == call10encoding[row][col]) {
	        *b++ = '0' + row;
	        *b = '\0';
	        packed = packed * 4 + col;  /* base 4 to binary */
	        found = 1;
	      }
	    }
	  }

	  if (! found) {
	    /* Earlier check should have caught any character not in translation table. */
	    errors++;
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Text to callsign 6+4: INTERNAL ERROR 0x%02x.  Should not be here.\n", c);
	  }
	}

/* Binary to decimal for the columns. */

	snprintf (stemp, sizeof(stemp), "%04d", packed);
	// FIXME: add parameter for sizeof buttons and use strlcat
	strcat (buttons, stemp);

	return (errors);          

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

int tt_text_to_satsq (const char *text, int quiet, char *buttons, size_t buttonsize)
{

	int row, col;
	int errors = 0;
	int found;
	char uc[3];


	strlcpy (buttons, "", buttonsize);

/* Quick validity check. */
	
	if (strlen(text) < 1 || strlen(text) > 4) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Satellite Gridsquare to DTMF: Gridsquare \"%s\" must be 4 characters.\n", text);
	  }
	  errors++;
	  return (errors);
   	}

/* Changing to upper case makes things easier later. */

	uc[0] = islower(text[0]) ? toupper(text[0]) : text[0];
	uc[1] = islower(text[1]) ? toupper(text[1]) : text[1];
	uc[2] = '\0';

	if (uc[0] < 'A' || uc[0] > 'R' || uc[1] < 'A' || uc[1] > 'R') {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Satellite Gridsquare to DTMF: First two characters \"%s\" must be letters in range of A to R.\n", text);
	  }
	  errors++;
	  return (errors);
	}

	if (! isdigit(text[2]) || ! isdigit(text[3])) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Satellite Gridsquare to DTMF: Last two characters \"%s\" must digits.\n", text);
	  }
	  errors++;
	  return (errors);
   	}


/* Search in the translation table. */

	found = 0;

	for (row=0; row<10 && ! found; row++) {
	  for (col=0; col<10 && ! found; col++) {
	    if (strcmp(uc,grid[row][col]) == 0) {

	      char btemp[8];

	      btemp[0] = row + '0';
	      btemp[1] = col + '0';
	      btemp[2] = text[2];
	      btemp[3] = text[3];
	      btemp[4] = '\0';

	      strlcpy (buttons, btemp, buttonsize);
	      found = 1;
	    }
	  }
	}

	if (! found) {
	  /* Sorry, Greenland, and half of Africa, and ... */
	  errors++;
	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Satellite Gridsquare to DTMF: Sorry, your location can't be converted to DTMF.\n");
	  }
	}

	return (errors);          

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

int tt_text_to_ascii2d (const char *text, int quiet, char *buttons)
{
	const char *t = text;
	char *b = buttons;
	char c;
	int errors = 0;


	*b = '\0';

	while ((c = *t++) != '\0') {

	  int n;

	  /* "isprint()" might depend on locale so use brute force. */

	  if (c < ' ' || c > '~') c = '?';

	  n = c - 32;

	  *b++ = (n / 10) + '0';
	  *b++ = (n % 10) + '0';
	  *b = '\0';
	}
	return (errors);

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

int tt_multipress_to_text (const char *buttons, int quiet, char *text)
{
	const char *b = buttons;
	char *t = text;
	char c;
	int row, col;
	int errors = 0;
	int maxspan;
	int n;

	*t = '\0';
	
	while ((c = *b++) != '\0') {

	  if (isdigit(c)) {
	
/* Determine max that can occur in a row. */
/* = number of other characters assigned to this button + 1. */

	    maxspan = 1;
	    row = c - '0';
	    for (col=0; col<4; col++) {
	      if (translate[row][col] != 0) {
	        maxspan++;
	      }
	    }

/* Count number of consecutive same digits. */

	    n = 1;
	    while (c == *b) {
	      b++;
	      n++;
	    }

	    if (n < maxspan) {
	      *t++ = translate[row][n-1];
	      *t = '\0';
	    }
	    else if (n == maxspan) {
	      *t++ = c;
	      *t = '\0';
	    }
	    else {
	      errors++;
	      if (! quiet) {
	        text_color_set (DW_COLOR_ERROR);
	        dw_printf ("Multi-press to text: Maximum of %d \"%c\" can occur in a row.\n", maxspan, c);
	      }
	      /* Treat like the maximum length. */
	      *t++ = c;
	      *t = '\0';
	    }
	  }
	  else if (c == 'A' || c == 'a') {

/* Separator should occur only if digit before and after are the same. */
	     
	    if (b == buttons + 1 || *b == '\0' || *(b-2) != *b) {
	      errors++;
	      if (! quiet) {
	        text_color_set (DW_COLOR_ERROR);
	        dw_printf ("Multi-press to text: \"A\" can occur only between two same digits.\n");
  	      }
	    }
	  }
	  else {

/* Completely unexpected character. */

	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Multi-press to text: \"%c\" not allowed.\n", c);
  	    }
	  }
	}
	return (errors);          

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

int tt_two_key_to_text (const char *buttons, int quiet, char *text)
{
	const char *b = buttons;
	char *t = text;
	char c;
	int row, col;
	int errors = 0;

	*t = '\0';
	
	while ((c = *b++) != '\0') {

	  if (isdigit(c)) {
	
/* Letter (or space) if followed by ABCD. */
	    
	    row = c - '0';
	    col = -1;

	    if (*b >= 'A' && *b <= 'D') {
	      col = *b++ - 'A';
	    }
	    else if (*b >= 'a' && *b <= 'd') {
	      col = *b++ - 'a';
	    }

	    if (col >= 0) {
	      if (translate[row][col] != 0) {
	        *t++ = translate[row][col];
	        *t = '\0';
	      }
	      else {
		errors++;
	        if (! quiet) {
	          text_color_set (DW_COLOR_ERROR);
	          dw_printf ("Two key to text: Invalid combination \"%c%c\".\n", c, col+'A');
		}
	      }
	    }
	    else {
	      *t++ = c;
	      *t = '\0';
	    }
	  }
	  else if ((c >= 'A' && c <= 'D') || (c >= 'a' && c <= 'd')) {

/* ABCD not expected here. */
	     
	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Two-key to text: A, B, C, or D in unexpected location.\n");
	    }
	  }
	  else {

/* Completely unexpected character. */

	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Two-key to text: Invalid character \"%c\".\n", c);
  	    }
	  }
	}
	return (errors);          

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

int tt_two_digits_to_letter (const char *buttons, int quiet, char *text, size_t textsiz)
{
	char c1 = buttons[0];
	char c2 = buttons[1];
	int row, col;
	int errors = 0;
	char stemp2[2];

	strlcpy (text, "", textsiz);
	
	if (c1 >= '2' && c1 <= '9') {

	  if (c2 >= '1' && c2 <= '4') {

	    row = c1 - '0';
	    col = c2 - '1';

	    if (translate[row][col] != 0) {

	      stemp2[0] = translate[row][col];
	      stemp2[1] = '\0';
	      strlcpy (text, stemp2, textsiz);
	    }
	    else {
	      errors++;
	      strlcpy (text, "", textsiz);
	      if (! quiet) {
	        text_color_set (DW_COLOR_ERROR);
	        dw_printf ("Two digits to letter: Invalid combination \"%c%c\".\n", c1, c2);
	      }
	    }
	  }
	  else {
	    errors++;
	    strlcpy (text, "", textsiz);
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Two digits to letter: Second character \"%c\" must be in range of 1 through 4.\n", c2);
	    }
	  }
	}
	else {
	  errors++;
	  strlcpy (text, "", textsiz);
	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Two digits to letter: First character \"%c\" must be in range of 2 through 9.\n", c1);
	  }
	}

	return (errors);     

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

int tt_call10_to_text (const char *buttons, int quiet, char *text)
{
	const char *b;
	char *t;
	char c;
	int packed;		/* from last 4 digits */
	int row, col;
	int errors = 0;
	int k;

	t = text;
	*t = '\0';	/* result */

/* Validity check. */

	if (strlen(buttons) != 10) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Callsign 6+4 to text: Encoded Callsign \"%s\" must be exactly 10 digits.\n", buttons);
	  }
	  errors++;
	  return (errors);
   	}

	for (b = buttons; *b != '\0'; b++) {

	  if (! isdigit(*b)) {
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Callsign 6+4 to text: Encoded Callsign \"%s\" can contain only digits.\n", buttons);
	    }
	    errors++;
	    return (errors);
	  }
   	}

	packed = atoi(buttons+6);

	for (k = 0; k < 6; k++) {
	  c = buttons[k];

	  row = c - '0';
	  col = (packed >> ((5 - k) *2)) & 3;

	  if (row < 0 || row > 9 || col < 0 || col > 3) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Callsign 6+4 to text: INTERNAL ERROR %d %d.  Should not be here.\n", row, col);
	    errors++;
	    row = 0;
	    col = 1;
	  }

	  if (call10encoding[row][col] != 0) {
	    *t++ = call10encoding[row][col];
	    *t = '\0';
	  }
	  else {
	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Callsign 6+4 to text: Invalid combination: button %d, position %d.\n", row, col);
	    }
	  }
	}

/* Trim any trailing spaces. */

	k = strlen(text) - 1;		/* should be 6 - 1 = 5 */

	while (k >= 0 && text[k] == ' ') {
	  text[k] = '\0';
	  k--;
	}

	return (errors);          

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

int tt_call5_suffix_to_text (const char *buttons, int quiet, char *text)
{
	const char *b;
	char *t;
	char c;
	int packed;		/* from last 4 digits */
	int row, col;
	int errors = 0;
	int k;

	t = text;
	*t = '\0';	/* result */

/* Validity check. */

	if (strlen(buttons) != 5) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Callsign 3+2 suffix to text: Encoded Callsign \"%s\" must be exactly 5 digits.\n", buttons);
	  }
	  errors++;
	  return (errors);
	}

	for (b = buttons; *b != '\0'; b++) {

	  if (! isdigit(*b)) {
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Callsign 3+2 suffix to text: Encoded Callsign \"%s\" can contain only digits.\n", buttons);
	    }
	    errors++;
	    return (errors);
	  }
	}

	packed = atoi(buttons+3);

	for (k = 0; k < 3; k++) {
	  c = buttons[k];

	  row = c - '0';
	  col = (packed >> ((2 - k) * 2)) & 3;

	  if (row < 0 || row > 9 || col < 0 || col > 3) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Callsign 3+2 suffix to text: INTERNAL ERROR %d %d.  Should not be here.\n", row, col);
	    errors++;
	    row = 0;
	    col = 1;
	  }

	  if (call10encoding[row][col] != 0) {
	    *t++ = call10encoding[row][col];
	    *t = '\0';
	  }
	  else {
	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("Callsign 3+2 suffix to text: Invalid combination: button %d, position %d.\n", row, col);
	    }
	  }
	}

	if (errors > 0) {
	  strcpy (text, "");
	  return (errors);
	}

	return (errors);

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

#define MAXMHPAIRS 6

static const struct {
	char *position;
	char  min_ch;
	char  max_ch;
} mhpair[MAXMHPAIRS] = {
	{ "first",  'A', 'R' },
	{ "second", '0', '9' },
	{ "third",  'A', 'X' },
	{ "fourth", '0', '9' },
	{ "fifth",  'A', 'X' },
	{ "sixth",  '0', '9' }
};


int tt_mhead_to_text (const char *buttons, int quiet, char *text, size_t textsiz)
{
	const char *b;
	int errors = 0;

	strlcpy (text, "", textsiz);

/* Validity check. */

	if (strlen(buttons) != 4 && strlen(buttons) != 6 &&
	    strlen(buttons) != 10 && strlen(buttons) != 12 &&
	    strlen(buttons) != 16 && strlen(buttons) != 18) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("DTMF to Maidenhead Gridsquare Locator: Input \"%s\" must be exactly 4, 6, 10, or 12 digits.\n", buttons);
	  }
	  errors++;
	  strlcpy (text, "", textsiz);
	  return (errors);
   	}

	for (b = buttons; *b != '\0'; b++) {

	  if (! isdigit(*b)) {
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("DTMF to Maidenhead Gridsquare Locator: Input \"%s\" can contain only digits.\n", buttons);
	    }
	    errors++;
	    strlcpy (text, "", textsiz);
	    return (errors);
	  }
   	}


/* Convert DTMF to normal representation. */

	b = buttons;

	int n;

	for (n = 0; n < 6 && b < buttons+strlen(buttons); n++) {
	  if ((n % 2) == 0) {

	    /* Convert pairs of digits to letter. */

	    char t2[2];

	    errors += tt_two_digits_to_letter (b, quiet, t2, sizeof(t2));
	    strlcat (text, t2, textsiz);
	    b += 2;

	    errors += tt_two_digits_to_letter (b, quiet, t2, sizeof(t2));
	    strlcat (text, t2, textsiz);
	    b += 2;
	  }
	  else {

	    /* Copy the digits. */

	    char d3[3];
	    d3[0] = *b++;
	    d3[1] = *b++;
	    d3[2] = '\0';
	    strlcat (text, d3, textsiz);
	  }
	}

	if (errors != 0) {
	  strlcpy (text, "", textsiz);
	}
	return (errors);          

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

int tt_text_to_mhead (const char *text, int quiet, char *buttons, size_t buttonsize)
{
	int errors = 0;
	int np, i;

	strlcpy (buttons, "", buttonsize);

	np = strlen(text) / 2;

	if ((strlen(text) % 2) != 0) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Maidenhead Gridsquare Locator to DTMF: Input \"%s\" must be even number of characters.\n", text);
	  }
	  errors++;
	  return (errors);
   	}

	if (np < 1 || np > MAXMHPAIRS) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("Maidenhead Gridsquare Locator to DTMF: Input \"%s\" must be 1 to %d pairs of characters.\n", text, np);
	  }
	  errors++;
	  return (errors);
   	}

	for (i = 0; i < np; i++) {

	  char t0 = text[i*2];
	  char t1 = text[i*2+1];

	  if (toupper(t0) < mhpair[i].min_ch || toupper(t0) > mhpair[i].max_ch ||
		toupper(t1) < mhpair[i].min_ch || toupper(t1) > mhpair[i].max_ch) {
	    if (! quiet) {
	      text_color_set(DW_COLOR_ERROR);
	      dw_printf("The %s pair of characters in Maidenhead locator \"%s\" must be in range of %c thru %c.\n", 
				mhpair[i].position, text, mhpair[i].min_ch, mhpair[i].max_ch);
	    }
	    strlcpy (buttons, "", buttonsize);
	    errors++;  
	    return(errors);
	  }

	  if (mhpair[i].min_ch == 'A') {		/* Should be letters */

	    char b3[3];

	    errors += tt_letter_to_two_digits (t0, quiet, b3);
	    strlcat (buttons, b3, buttonsize);

	    errors += tt_letter_to_two_digits (t1, quiet, b3);
	    strlcat (buttons, b3, buttonsize);
	  }
	  else {					/* Should be digits */

	    char b3[3];

	    b3[0] = t0;
	    b3[1] = t1;
	    b3[2] = '\0';
	    strlcat (buttons, b3, buttonsize);
	  }
	}

	if (errors != 0) strlcpy (buttons, "", buttonsize);

	return (errors);          

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

int tt_satsq_to_text (const char *buttons, int quiet, char *text)
{
	const char *b;
	int row, col;
	int errors = 0;

	strcpy (text, "");

/* Validity check. */

	if (strlen(buttons) != 4) {

	  if (! quiet) {
	    text_color_set (DW_COLOR_ERROR);
	    dw_printf ("DTMF to Satellite Gridsquare: Input \"%s\" must be exactly 4 digits.\n", buttons);
	  }
	  errors++;
	  return (errors);
   	}

	for (b = buttons; *b != '\0'; b++) {

	  if (! isdigit(*b)) {
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("DTMF to Satellite Gridsquare: Input \"%s\" can contain only digits.\n", buttons);
	    }
	    errors++;
	    return (errors);
	  }
   	}

	row = buttons[0] - '0';
	col = buttons[1] - '0';

	// FIXME: Add parameter for sizeof text and use strlcpy, strlcat.
	strcpy (text, grid[row][col]);
	strcat (text, buttons+2);

	return (errors);          

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

int tt_ascii2d_to_text (const char *buttons, int quiet, char *text)
{
	const char *b = buttons;
	char *t = text;
	char c1, c2;
	int errors = 0;


	*t = '\0';

	while (*b != '\0') {

	  c1 = *b++;
	  if (*b != '\0') {
	    c2 = *b++;
	  }
	  else {
	    c2 = ' ';
	  }

	  if (isdigit(c1) && isdigit(c2)) {
	    int n;

	    n = (c1 - '0') * 10 + (c2 - '0');

           *t++ = n + 32;
	   *t = '\0';
	  }
	  else {

/* Unexpected character. */

	    errors++;
	    if (! quiet) {
	      text_color_set (DW_COLOR_ERROR);
	      dw_printf ("ASCII2D to text: Invalid character pair \"%c%c\".\n", c1, c2);
	    }
	  }
	}
	return (errors);

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

tt_enc_t tt_guess_type (char *buttons) 
{
	char text[256];
	int err_mp;
	int err_tk;
	
/* If it contains B, C, or D, it can't be multipress. */

	if (strchr (buttons, 'B') != NULL || strchr (buttons, 'b') != NULL ||
	    strchr (buttons, 'C') != NULL || strchr (buttons, 'c') != NULL ||
	    strchr (buttons, 'D') != NULL || strchr (buttons, 'd') != NULL) {
	  return (TT_TWO_KEY);
	}

/* Try parsing quietly and see if one gets errors and the other doesn't. */

	err_mp = tt_multipress_to_text (buttons, 1, text);
	err_tk = tt_two_key_to_text (buttons, 1, text);

	if (err_mp == 0 && err_tk > 0) {
	  return (TT_MULTIPRESS);
 	}
	else if (err_tk == 0 && err_mp > 0) {
	  return (TT_TWO_KEY);
	}

/* Could be either one. */

	return (TT_EITHER);

} /* end tt_guess_type */

/* end tt_text.c */
