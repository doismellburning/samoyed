//
//    This file is part of Dire Wolf, an amateur radio packet TNC.
//
//    Copyright (C) 2013, 2014, 2017, 2023  John Langner, WB2OSZ
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
 * Module:      kiss_frame.c
 *
 * Purpose:   	Common code used by Serial port and network versions of KISS protocol.
 *		
 * Description: The KISS TNC protocol is described in http://www.ka9q.net/papers/kiss.html
 *
 *		( An extended form, to handle multiple TNCs on a single serial port.
 *		  Not applicable for our situation.  http://he.fi/pub/oh7lzb/bpq/multi-kiss.pdf )
 *
 * 		Briefly, a frame is composed of 
 *
 *			* FEND (0xC0)
 *			* Contents - with special escape sequences so a 0xc0
 *				byte in the data is not taken as end of frame.
 *				as part of the data.
 *			* FEND
 *
 *		The first byte of the frame contains:
 *	
 *			* radio channel in upper nybble.
 *				(KISS doc uses "port" but I don't like that because it has too many meanings.)
 *			* command in lower nybble.
 *
 *	
 *		Commands from application tp TNC:
 *
 *			_0	Data Frame	AX.25 frame in raw format.
 *
 *			_1	TXDELAY		See explanation in xmit.c.
 *
 *			_2	Persistence	"	"
 *
 *			_3 	SlotTime	"	"
 *
 *			_4	TXtail		"	"
 *						Spec says it is obsolete but Xastir
 *						sends it and we respect it.
 *
 *			_5	FullDuplex	Full Duplex.  Transmit immediately without
 *						waiting for channel to be clear.
 *		
 *			_6	SetHardware	TNC specific.
 *
 *			_C	XKISS extension - not supported.
 *			_E	XKISS extension - not supported.
 *			
 *			FF	Return		Exit KISS mode.  Ignored.
 *
 *
 *		Messages sent to client application:
 *
 *			_0	Data Frame	Received AX.25 frame in raw format.
 *
 *			_6	SetHardware	TNC specific.
 *						Usually a response to a query.
 *
 *---------------------------------------------------------------*/

#include "direwolf.h"

#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>
#include <ctype.h>
#include <assert.h>
#include <string.h>

#include "ax25_pad.h"
#include "textcolor.h"
#include "kiss_frame.h"
#include "tq.h"
#include "xmit.h"
#include "version.h"
#include "kissnet.h"

int KISSUTIL = 0; // Dynamic replacement for the old #define

/* In server.c.  Should probably move to some misc. function file. */
void hex_dump (unsigned char *p, int len);


/*-------------------------------------------------------------------
 *
 * Name:        kiss_frame_init 
 *
 * Purpose:     Save information about valid channels for later error checking.
 *
 * Inputs:      pa		- Address of structure of type audio_s.
 *
 *-----------------------------------------------------------------*/

static struct audio_s *save_audio_config_p;

void kiss_frame_init (struct audio_s *pa)
{
	save_audio_config_p = pa;
}


/*-------------------------------------------------------------------
 *
 * Name:        kiss_encapsulate 
 *
 * Purpose:     Ecapsulate a frame into KISS format.
 *
 * Inputs:	in	- Address of input block.
 *			  First byte is the "type indicator" with type and 
 *			  channel but we don't care about that here.
 *			  If it happens to be FEND or FESC, it is escaped, like any other byte.
 *
 *			  This seems cumbersome and confusing to have this
 *			  one byte offset when encapsulating an AX.25 frame.
 *			  Maybe the type/channel byte should be passed in 
 *			  as a separate argument.
 *
 *			  Note that this is "binary" data and can contain
 *			  nul (0x00) values.   Don't treat it like a text string!
 *
 *		ilen	- Number of bytes in input block.
 *
 * Outputs:	out	- Address where to place the KISS encoded representation.
 *			  The sequence is:
 *				FEND		- Magic frame separator.
 *				data		- with certain byte values replaced so
 *						  FEND will never occur here.
 *				FEND		- Magic frame separator.
 *
 * Returns:	Number of bytes in the output.
 *		Absolute max length (extremely unlikely) will be twice input plus 2.
 *
 *-----------------------------------------------------------------*/

int kiss_encapsulate (unsigned char *in, int ilen, unsigned char *out)
{
	int olen;
	int j;

	olen = 0;
	out[olen++] = FEND;
	for (j=0; j<ilen; j++) {

	  if (in[j] == FEND) {
	    out[olen++] = FESC;
	    out[olen++] = TFEND;
	  }
	  else if (in[j] == FESC) {
	    out[olen++] = FESC;
	    out[olen++] = TFESC;
	  }
	  else {
	    out[olen++] = in[j];
	  }
	}
	out[olen++] = FEND;
	
	return (olen);

}  /* end kiss_encapsulate */


/*-------------------------------------------------------------------
 *
 * Name:        kiss_unwrap 
 *
 * Purpose:     Extract original data from a KISS frame.
 *
 * Inputs:	in	- Address of the received the KISS encoded representation.
 *			  The sequence is:
 *				FEND		- Magic frame separator, optional.
 *				data		- with certain byte values replaced so
 *						  FEND will never occur here.
 *				FEND		- Magic frame separator.
 *		ilen	- Number of bytes in input block.
 *
 * Inputs:	out	- Where to put the resulting frame without
 *			  the escapes or FEND.
 *			  First byte is the "type indicator" with type and 
 *			  channel but we don't care about that here.
 *			  We treat it like any other byte with special handling
 *			  if it happens to be FESC.
 *			  Note that this is "binary" data and can contain
 *			  nul (0x00) values.   Don't treat it like a text string!
 *
 * Returns:	Number of bytes in the output.
 *
 *-----------------------------------------------------------------*/

int kiss_unwrap (unsigned char *in, int ilen, unsigned char *out)
{
	int olen;
	int j;
	int escaped_mode;

	olen = 0;
	escaped_mode = 0;

	if (ilen < 2) {
	  /* Need at least the "type indicator" byte and FEND. */
	  /* Probably more. */
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("KISS message less than minimum length.\n");
	  return (0);
	}

	if (in[ilen-1] == FEND) {
	  ilen--;	/* Don't try to process below. */
	}
	else {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("KISS frame should end with FEND.\n");
	}

	if (in[0] == FEND) {
	  j = 1;	/* skip over optional leading FEND. */
	}
	else {
	  j = 0;
	}

	for ( ; j<ilen; j++) {

	  if (in[j] == FEND) {
	    text_color_set(DW_COLOR_ERROR);
	    dw_printf ("KISS frame should not have FEND in the middle.\n");
	  }

	  if (escaped_mode) {

	    if (in[j] == TFESC) {
	      out[olen++] = FESC;
	    }
	    else if (in[j] == TFEND) {
	      out[olen++] = FEND;
	    }
	    else {
	      text_color_set(DW_COLOR_ERROR);
	      dw_printf ("KISS protocol error.  Found 0x%02x after FESC.\n", in[j]);
	    }
	    escaped_mode = 0;
	  }
	  else if (in[j] == FESC) {
	    escaped_mode = 1;
	  }
	  else {
	    out[olen++] = in[j];
	  }
	}
	
	return (olen);

}  /* end kiss_unwrap */


/*-------------------------------------------------------------------
 *
 * Name:        kiss_debug_print 
 *
 * Purpose:     Print message to/from client for debugging.
 *
 * Inputs:	fromto		- Direction of message.
 *		special		- Comment if not a KISS frame.
 *		pmsg		- Address of the message block.
 *		msg_len		- Length of the message.
 *
 *--------------------------------------------------------------------*/


void kiss_debug_print (fromto_t fromto, char *special, unsigned char *pmsg, int msg_len)
{
	const char *direction [2] = { "from", "to" };
	const char *prefix [2] = { "<<<", ">>>" };
	const char *function[16] = { 
		"Data frame",	"TXDELAY",	"P",		"SlotTime",
		"TXtail",	"FullDuplex",	"SetHardware",	"Invalid 7",
		"Invalid 8", 	"Invalid 9",	"Invalid 10",	"Invalid 11",
		"Invalid 12", 	"Invalid 13",	"Invalid 14",	"Return" };

	text_color_set(DW_COLOR_DEBUG);

	if (KISSUTIL) {
	  dw_printf ("From KISS TNC:\n");
	} else {
	  dw_printf ("\n");
	  if (special == NULL) {
	    unsigned char *p;	/* to skip over FEND if present. */

	    p = pmsg;
	    if (*p == FEND) p++;

	    dw_printf ("%s %s %s KISS client application, channel %d, total length = %d\n",
			prefix[(int)fromto], function[p[0] & 0xf], direction[(int)fromto], 
			(p[0] >> 4) & 0xf, msg_len);
	  }
	  else {
	    dw_printf ("%s %s %s KISS client application, total length = %d\n",
			prefix[(int)fromto], special, direction[(int)fromto], 
			msg_len);
	  }
	}

	hex_dump (pmsg, msg_len);

} /* end kiss_debug_print */

/* end kiss_frame.c */
