// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

//#define DEBUG1 1		/* Parsing of original human readable format. */
//#define DEBUG2 1		/* Parsing of base 91 compressed format. */
//#define DEBUG3 1		/* Parsing of special messages. */
//#define DEBUG4 1		/* Resulting display form. */

/*------------------------------------------------------------------
 *
 * Purpose:   	Decode telemetry information.
 *		Point out where it violates the protocol spec and
 *		other applications might not interpret it properly.
 *
 * References:	APRS Protocol, chapter 13.
 *		http://www.aprs.org/doc/APRS101.PDF
 *
 *		Base 91 compressed format
 *		http://he.fi/doc/aprs-base91-comment-telemetry.txt
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdio.h>
// #include <unistd.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <string.h>
// #include <math.h>
// #include <ctype.h>
// #include "ax25_pad.h"			// for packet_t, AX25_MAX_ADDR_LEN
// #include "decode_aprs.h"		// for decode_aprs_t, G_UNKNOWN
// #include "textcolor.h"
import "C"

import (
	"fmt"
	"strconv"
	"strings"
)

const T_NUM_ANALOG = 5  /* Number of analog channels. */
const T_NUM_DIGITAL = 8 /* Number of digital channels. */

// FIXME KG #define T_STR_LEN 32				/* Max len for labels and units. */

type t_metadata_s struct {
	pnext *t_metadata_s /* Next in linked list. */

	station string /* Station name with optional SSID. */

	project string /* Description for data. */
	/* "Project Name" or "project title" in the spec. */

	name [T_NUM_ANALOG + T_NUM_DIGITAL]string
	/* Names for channels.  e.g. Battery, Temperature */

	unit [T_NUM_ANALOG + T_NUM_DIGITAL]string
	/* Units for channels.  e.g. Volts, Deg.C */

	coeff [T_NUM_ANALOG][3]float64 /* a, b, c coefficients for scaling. */

	coeff_ndp [T_NUM_ANALOG][3]int /* Number of decimal places for above. */

	sense [T_NUM_DIGITAL]bool /* Polarity for digital channels. */
}

const C_A = 0 /* Scaling coefficient positions. */
const C_B = 1
const C_C = 2

var md_list_head *t_metadata_s

/*-------------------------------------------------------------------
 *
 * Name:        t_get_metadata
 *
 * Purpose:     Obtain pointer to metadata for specified station.
 *		If not found, allocate a fresh one and initialize with defaults.
 *
 * Inputs:	station		- Station name with optional SSID.
 *
 * Returns:	Pointer to metadata.
 *
 *--------------------------------------------------------------------*/

func t_get_metadata(station string) *t_metadata_s {

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("t_get_metadata (station=%s)\n", station);
	#endif
	*/

	for p := md_list_head; p != nil; p = p.pnext {
		if station == p.station {

			return (p)
		}
	}

	var p = new(t_metadata_s)

	p.station = station

	for n := 0; n < T_NUM_ANALOG; n++ {
		p.name[n] = fmt.Sprintf("A%d", n+1)
	}
	for n := 0; n < T_NUM_DIGITAL; n++ {
		p.name[T_NUM_ANALOG+n] = fmt.Sprintf("D%d", n+1)
	}

	for n := 0; n < T_NUM_ANALOG; n++ {
		p.coeff[n][C_A] = 0.
		p.coeff[n][C_B] = 1.
		p.coeff[n][C_C] = 0.
		p.coeff_ndp[n][C_A] = 0
		p.coeff_ndp[n][C_B] = 0
		p.coeff_ndp[n][C_C] = 0
	}

	for n := 0; n < T_NUM_DIGITAL; n++ {
		p.sense[n] = true
	}

	p.pnext = md_list_head
	md_list_head = p

	return (p)

} /* end t_get_metadata */

/*-------------------------------------------------------------------
 *
 * Name:        t_ndp
 *
 * Purpose:     Count number of digits after any decimal point.
 *
 * Inputs:	str	- Number in text format.
 *
 * Returns:	Number digits after decimal point.  Examples, in -. out.
 *
 *			1	--> 0
 *			1.	--> 0
 *			1.2	--> 1
 *			1.23	--> 2
 *			etc.
 *
 *--------------------------------------------------------------------*/

func t_ndp(str string) int {

	var p = strings.Index(str, ".")
	if p == -1 {
		return (0)
	} else {
		return len(str) - (p + 1)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_data_original
 *
 * Purpose:     Interpret telemetry data in the original format.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *		info 	- Pointer to packet Information field.
 *		quiet	- suppress error messages.
 *
 * Outputs:	output	- Decoded telemetry in human readable format.
 *				TODO:  How big does it need to be?  (buffer overflow?)
 *		comment	- Any comment after the data.
 *
 * Description:	The first character, after the "T" data type indicator, must be "#"
 *		followed by a sequence number.  Up to 5 analog and 8 digital channel
 *		values are specified as in this example from the protocol spec.
 *
 *			T#005,199,000,255,073,123,01101001
 *
 *		The analog values are supposed to be 3 digit integers in the
 *		range of 000 to 255 in fixed columns.  After reading the discussion
 *		groups it seems that few adhere to those restrictions.  When I
 *		started to look for some local signals, this was the first one
 *		to appear:
 *
 *			KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000
 *
 *		Not integers.  Not fixed width fields.
 *
 *		Originally I printed a warning if values were not in range of 000 to 255
 *		but later took it out because no one pays attention to that original
 *		restriction anymore.
 *
 *--------------------------------------------------------------------*/

func telemetry_data_original(station string, info string, _quiet C.int, output *C.char, outputsize C.size_t, comment *C.char, commentsize C.size_t) {

	var quiet = _quiet != 0

	/* FIXME KG
	int n;
	char stemp[256];
	char *next;
	char *p;

	float araw[T_NUM_ANALOG];
	int ndp[T_NUM_ANALOG];
	int draw[T_NUM_DIGITAL];
	*/

	/* TODO KG
	   #if DEBUG1
	   	text_color_set(DW_COLOR_DEBUG);

	   	dw_printf ("\n%s\n\n", info);
	   #endif
	*/

	C.strcpy(output, C.CString(""))
	C.strcpy(comment, C.CString(""))

	var pm = t_get_metadata(station)

	var araw [T_NUM_ANALOG]float64
	var ndp [T_NUM_ANALOG]int
	for n := 0; n < T_NUM_ANALOG; n++ {
		araw[n] = G_UNKNOWN
		ndp[n] = 0
	}

	var draw [T_NUM_DIGITAL]int
	for n := 0; n < T_NUM_DIGITAL; n++ {
		draw[n] = G_UNKNOWN
	}

	if !strings.HasPrefix(info, "T#") {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error: Information part of telemetry packet must begin with \"T#\"\n")
		}
		return
	}

	/*
	 * Make a copy of the input string (excluding T#) because this will alter it.
	 * Remove any trailing CR/LF.
	 */

	var stemp = info[2:]
	stemp = strings.TrimSpace(stemp)

	var seqStr, rest, found = strings.Cut(stemp, ",")

	if !found {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Nothing after \"T#\" for telemetry data.\n")
		}
		return
	}

	var seq, _ = strconv.Atoi(seqStr)
	var parts = strings.SplitN(rest, ",", T_NUM_ANALOG+1)
	for n, p := range parts {
		if n < T_NUM_ANALOG {
			if len(p) > 0 {
				var f, _ = strconv.ParseFloat(p, 64)
				araw[n] = f
				ndp[n] = t_ndp(p)
			}
			// Version 1.3: Suppress this message.
			// No one pays attention to the original 000 to 255 range.
			// BTW, this doesn't trap values like 0.0 or 1.0
			//if (strlen(p) != 3 || araw[n] < 0 || araw[n] > 255 || araw[n] != (int)(araw[n])) {
			//  if ( ! quiet) {
			//    text_color_set(DW_COLOR_ERROR);
			//    dw_printf("Telemetry analog values should be 3 digit integer values in range of 000 to 255.\n");
			//    dw_printf("Some applications might not interpret \"%s\" properly.\n", p);
			//  }
			//}
		}

		if n == T_NUM_ANALOG {
			/* We expect to have 8 digits of 0 and 1. */
			/* Anything left over is a comment. */
			if len(p) < 8 {
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Expected to find 8 binary digits after \"%s\" for the digital values.\n", p)
				}
			}
			if len(p) > 8 {
				C.strcpy(comment, C.CString(p[8:]))
				p = p[:8]
			}

			for k, v := range p {
				switch v {
				case '0':
					draw[k] = 0
				case '1':
					draw[k] = 1
				default:
					if !quiet {
						text_color_set(DW_COLOR_ERROR)
						dw_printf("Found \"%c\" when expecting 0 or 1 for digital value %d.\n", v, k+1)
					}
				}
			}
		}
	}

	if len(parts) < T_NUM_ANALOG+1 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Found fewer than expected number of telemetry data values.\n")
		}
	}

	/*
	 * Now process the raw data with any metadata available.
	 */

	/* TODO KG
	#if DEBUG1
	text_color_set(DW_COLOR_DECODED)

	dw_printf("%d: %.3f %.3f %.3f %.3f %.3f \n",
		seq, araw[0], araw[1], araw[2], araw[3], araw[4])

	dw_printf("%d %d %d %d %d %d %d %d \"%s\"\n",
		draw[0], draw[1], draw[2], draw[3], draw[4], draw[5], draw[6], draw[7], C.GoString(comment))
		#endif
	*/

	t_data_process(pm, seq, araw, ndp, draw, output, outputsize)
} /* end telemtry_data_original */

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_data_base91
 *
 * Purpose:     Interpret telemetry data in the base 91 compressed format.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *		cdata 	- Compressed data as character string.
 *
 * Outputs:	output	- Telemetry in human readable form.
 *
 * Description:	We are expecting from 2 to 7 pairs of base 91 digits.
 *		The first pair is the sequence number.
 *		Next we have 1 to 5 analog values.
 *		If digital values are present, all 5 analog values must be present.
 *
 *--------------------------------------------------------------------*/

func telemetry_data_base91(station string, cdata string, output *C.char, outputsize C.size_t) {

	/* TODO KG
	#if DEBUG2
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("\n%s\n\n", cdata);
	#endif
	*/

	C.strcpy(output, C.CString(""))

	var pm = t_get_metadata(station)

	var araw [T_NUM_ANALOG]float64
	var ndp [T_NUM_ANALOG]int
	for n := 0; n < T_NUM_ANALOG; n++ {
		araw[n] = G_UNKNOWN
		ndp[n] = 0
	}

	var draw [T_NUM_DIGITAL]int
	for n := 0; n < T_NUM_DIGITAL; n++ {
		draw[n] = G_UNKNOWN
	}

	if len(cdata) < 4 || len(cdata) > 14 || (len(cdata)%2 == 1) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Internal error: Expected even number of 2 to 14 characters but got \"%s\"\n", cdata)
		return
	}

	var seq = two_base91_to_i(cdata[0], cdata[1])
	cdata = cdata[2:]

	for n := 0; n < T_NUM_ANALOG+1 && 2*n < len(cdata); n++ {
		if n < T_NUM_ANALOG {
			araw[n] = float64(two_base91_to_i(cdata[2*n], cdata[2*n+1]))
		} else {
			var b = two_base91_to_i(cdata[2*n], cdata[2*n+1])
			for k := 0; k < T_NUM_DIGITAL; k++ {
				draw[k] = b & 1
				b >>= 1
			}
		}
	}

	/*
	 * Now process the raw data with any metadata available.
	 */

	/* TODO KG
	#if DEBUG2
		text_color_set(DW_COLOR_DECODED);

		dw_printf ("%d: %.3f %.3f %.3f %.3f %.3f \n",
			seq, araw[0], araw[1], araw[2], araw[3], araw[4]);

		dw_printf ("%d %d %d %d %d %d %d %d \n",
			draw[0], draw[1], draw[2], draw[3], draw[4], draw[5], draw[6], draw[7]);

	#endif
	*/

	t_data_process(pm, seq, araw, ndp, draw, output, outputsize)

} /* end telemtry_data_base91 */

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_name_message
 *
 * Purpose:     Interpret message with names for analog and digital channels.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *			  In this case it is the destination for the message,
 *			  not the sender.
 *		msg 	- Rest of message after "PARM."
 *
 * Outputs:	Stored for future use when data values are received.
 *
 * Description:	The first 5 characters of the message are "PARM." and the
 *		rest is a variable length list of comma separated names.
 *
 *		The original spec has different maximum lengths for different
 *		fields which we will ignore.
 *
 * TBD:		What should we do if some, but not all, names are specified?
 *		Clear the others or keep the defaults?
 *
 *--------------------------------------------------------------------*/

func telemetry_name_message(station string, msg string) {

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("\n%s\n\n", msg);
	#endif
	*/

	msg = strings.TrimSpace(msg)

	var pm = t_get_metadata(station)

	var parts = strings.Split(msg, ",")
	for n, p := range parts {
		if n < T_NUM_ANALOG+T_NUM_DIGITAL {
			if p != "-" {
				pm.name[n] = p
			}
		}
	}

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("names:\n");
		for (n = 0; n < T_NUM_ANALOG + T_NUM_DIGITAL; n++) {
		  dw_printf ("%d=\"%s\"\n", n, pm.name[n]);
		}
	#endif
	*/

} /* end telemetry_name_message */

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_unit_label_message
 *
 * Purpose:     Interpret message with units/labels for analog and digital channels.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *			  In this case it is the destination for the message,
 *			  not the sender.
 *		msg 	- Rest of message after "UNIT."
 *
 * Outputs:	Stored for future use when data values are received.
 *
 * Description:	The first 5 characters of the message are "UNIT." and the
 *		rest is a variable length list of comma separated units/labels.
 *
 *		The original spec has different maximum lengths for different
 *		fields which we will ignore.
 *
 *--------------------------------------------------------------------*/

func telemetry_unit_label_message(station string, msg string) {
	/* FIXME KG
	int n;
	char *next;
	char *p;
	*/

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("\n%s\n\n", msg);
	#endif
	*/

	/*
	 * Make a copy of the input string because this will alter it.
	 * Remove any trailing CR LF.
	 */

	var stemp = strings.TrimSpace(msg)

	var pm = t_get_metadata(station)

	var parts = strings.Split(stemp, ",")
	for n, p := range parts {
		if n < T_NUM_ANALOG+T_NUM_DIGITAL {
			pm.unit[n] = p
		}
	}

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("units/labels:\n");
		for (n = 0; n < T_NUM_ANALOG + T_NUM_DIGITAL; n++) {
		  dw_printf ("%d=\"%s\"\n", n, pm.unit[n]);
		}
	#endif
	*/

} /* end telemetry_unit_label_message */

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_coefficents_message
 *
 * Purpose:     Interpret message with scaling coefficients for analog channels.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *			  In this case it is the destination for the message,
 *			  not the sender.
 *		msg 	- Rest of message after "EQNS."
 *		quiet	- suppress error messages.
 *
 * Outputs:	Stored for future use when data values are received.
 *
 * Description:	The first 5 characters of the message are "EQNS." and the
 *		rest is a comma separated list of 15 floating point values.
 *
 *		The spec appears to require all 15 so we will issue an
 *		error if fewer found.
 *
 *--------------------------------------------------------------------*/

func telemetry_coefficents_message(station string, msg string, _quiet C.int) {

	var quiet = _quiet != 0

	/* TODO
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("\n%s\n\n", msg);
	#endif
	*/

	/*
	 * Make a copy of the input string because this will alter it.
	 * Remove any trailing CR LF.
	 */

	var stemp = strings.TrimSpace(msg)

	var pm = t_get_metadata(station)

	var n = 0
	for _, p := range strings.Split(stemp, ",") {
		if n < T_NUM_ANALOG*3 {
			// Keep default (or earlier value) for an empty field.
			if len(p) > 0 {
				pm.coeff[n/3][n%3], _ = strconv.ParseFloat(p, 64)
				pm.coeff_ndp[n/3][n%3] = t_ndp(p)
			} else {
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Equation coefficient position A%d%c is empty.\n", n/3+1, n%3+'a')
					dw_printf("Some applications might not handle this correctly.\n")
				}
			}
		}
		n++
	}

	if n != T_NUM_ANALOG*3 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Found %d equation coefficients when 15 were expected.\n", n)
			dw_printf("Some applications might not handle this correctly.\n")
		}
	}

	/* TODO KG
	   #if DEBUG3
	   	text_color_set(DW_COLOR_DEBUG);

	   	dw_printf ("coeff:\n");
	   	for (n = 0; n < T_NUM_ANALOG; n++) {
	   	  dw_printf ("A%d  a=%.*f  b=%.*f  c=%.*f\n", n+1,
	   			pm.coeff_ndp[n][C_A], pm.coeff[n][C_A],
	   			pm.coeff_ndp[n][C_B], pm.coeff[n][C_B],
	   			pm.coeff_ndp[n][C_C], pm.coeff[n][C_C]);
	   	}
	   #endif
	*/

} /* end telemetry_coefficents_message */

/*-------------------------------------------------------------------
 *
 * Name:        telemetry_bit_sense_message
 *
 * Purpose:     Interpret message with scaling coefficients for analog channels.
 *
 * Inputs:	station	- Name of station reporting telemetry.
 *			  In this case it is the destination for the message,
 *			  not the sender.
 *		msg 	- Rest of message after "BITS."
 *		quiet	- suppress error messages.
 *
 * Outputs:	Stored for future use when data values are received.
 *
 * Description:	The first 5 characters of the message are "BITS."
 *		It should contain eight binary digits for the digital active states.
 *		Anything left over is the project name or title.
 *
 *--------------------------------------------------------------------*/

func telemetry_bit_sense_message(station string, msg string, _quiet C.int) {

	var quiet = _quiet != 0

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("\n%s\n\n", msg);
	#endif
	*/

	var pm = t_get_metadata(station)

	if len(msg) < 8 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("The telemetry bit sense message should have at least 8 characters.\n")
		}
	}

	var n int
	for n = 0; n < T_NUM_DIGITAL && n < len(msg); n++ {
		switch msg[n] {
		case '1':
			pm.sense[n] = true
		case '0':
			pm.sense[n] = false
		default:
			if !quiet {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Bit position %d sense value was \"%c\" when 0 or 1 was expected.\n", n+1, msg[n])
			}
		}
	}

	/*
	 * Skip comma if first character of comment field.
	 *
	 * The protocol spec is inconsistent here.
	 * The definition shows the Project Title immediately after a fixed width field of 8 binary digits.
	 * The example has a comma in there.
	 *
	 * The toolkit telem-bits.pl script does insert the comma because it seems more sensible.
	 * Here we accept it either way.  i.e. Discard first character after data values if it is comma.
	 */

	if n < len(msg) && msg[n] == ',' {
		n++
	}

	pm.project = msg[n:]

	/* TODO KG
	#if DEBUG3
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("bit sense, project:\n");
		dw_printf ("%d %d %d %d %d %d %d %d \"%s\"\n",
			pm.sense[0],
			pm.sense[1],
			pm.sense[2],
			pm.sense[3],
			pm.sense[4],
			pm.sense[5],
			pm.sense[6],
			pm.sense[7],
			pm.project);
	#endif
	*/

} /* end telemetry_bit_sense_message */

/*-------------------------------------------------------------------
 *
 * Name:        t_data_process
 *
 * Purpose:     Interpret telemetry data in the original format.
 *
 * Inputs:	pm	- Pointer to metadata.
 *		seq	- Sequence number.
 *		araw	- 5 analog raw values.
 *		ndp	- Number of decimal points for each.
 *		draw	- 8 digital raw vales.
 *
 * Outputs:	output	- Decoded telemetry in human readable format.
 *
 * Description:	Process raw data according to any metadata available
 *		and put into human readable form.
 *
 *--------------------------------------------------------------------*/

const VAL_STR_SIZE = 64

func fval_to_str(x float64, ndp int) string {
	if x == G_UNKNOWN {
		return "?"
	} else {
		return fmt.Sprintf("%.*f", ndp, x)
	}
}

func ival_to_str(x int) string {
	if x == G_UNKNOWN {
		return "?"
	} else {
		return strconv.Itoa(x)
	}
}

func t_data_process(pm *t_metadata_s, seq int, araw [T_NUM_ANALOG]float64, ndp [T_NUM_ANALOG]int, draw [T_NUM_DIGITAL]int, _output *C.char, outputsize C.size_t) {
	/* FIXME KG
	int n;
	char val_str[VAL_STR_SIZE];
	*/

	Assert(pm != nil)

	var output string

	if len(pm.project) > 0 {
		output = pm.project + ": "
	}

	output += "Seq=" + ival_to_str(seq)

	for n := 0; n < T_NUM_ANALOG; n++ {

		// Display all or only defined values?  Only defined for now.

		if araw[n] != G_UNKNOWN {
			var fval float64
			var fndp int

			output += ", " + pm.name[n] + "="

			// Scaling and suitable number of decimal places for display.

			fval = pm.coeff[n][C_A]*araw[n]*araw[n] +
				pm.coeff[n][C_B]*araw[n] +
				pm.coeff[n][C_C]

			var z = IfThenElse(pm.coeff_ndp[n][C_A] == 0, 0, pm.coeff_ndp[n][C_A]+ndp[n]+ndp[n])
			fndp = max(z, max(pm.coeff_ndp[n][C_B]+ndp[n], pm.coeff_ndp[n][C_C]))

			var val_str = fval_to_str(fval, fndp)
			output += val_str
			if len(pm.unit[n]) > 0 {
				output += " " + pm.unit[n]
			}

		}
	}

	for n := 0; n < T_NUM_DIGITAL; n++ {

		// Display all or only defined values?  Only defined for now.

		if draw[n] != G_UNKNOWN {
			var dval int

			output += ", " + pm.name[T_NUM_ANALOG+n] + "="

			// Possible inverting for bit sense.

			dval = draw[n]
			if !pm.sense[n] {
				dval = 1 - dval
			}

			var val_str = ival_to_str(dval)
			output += val_str
			if len(pm.unit[T_NUM_ANALOG+n]) > 0 {
				output += " " + pm.unit[T_NUM_ANALOG+n]
			}
		}
	}

	C.strcpy(_output, C.CString(output))

	/* TODO KG
	#if DEBUG4
		text_color_set(DW_COLOR_DEBUG);

		dw_printf ("%s\n", output);
	#endif
	*/

} /* end t_data_process */

/* end telemetry.c */
