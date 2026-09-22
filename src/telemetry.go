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

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

const T_NUM_ANALOG = 5  /* Number of analog channels. */
const T_NUM_DIGITAL = 8 /* Number of digital channels. */

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

type TelemetryState struct {
	mdListHead *t_metadata_s
}

func NewTelemetryState() *TelemetryState {
	return new(TelemetryState)
}

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

func (ts *TelemetryState) t_get_metadata(station string) *t_metadata_s {
	logrus.WithField("station", station).Debug("t_get_metadata")
	for p := ts.mdListHead; p != nil; p = p.pnext {
		if station == p.station {
			return (p)
		}
	}

	var p = new(t_metadata_s)

	p.station = station

	for n := range T_NUM_ANALOG {
		p.name[n] = fmt.Sprintf("A%d", n+1)
	}

	for n := range T_NUM_DIGITAL {
		p.name[T_NUM_ANALOG+n] = fmt.Sprintf("D%d", n+1)
	}

	for n := range T_NUM_ANALOG {
		p.coeff[n][C_A] = 0.
		p.coeff[n][C_B] = 1.
		p.coeff[n][C_C] = 0.
		p.coeff_ndp[n][C_A] = 0
		p.coeff_ndp[n][C_B] = 0
		p.coeff_ndp[n][C_C] = 0
	}

	for n := range T_NUM_DIGITAL {
		p.sense[n] = true
	}

	p.pnext = ts.mdListHead
	ts.mdListHead = p

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
 * Returns:	output	- Decoded telemetry in human readable format.
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

func (ts *TelemetryState) telemetry_data_original(station string, info string, quiet bool) (string, string) {
	logrus.WithField("info", info).Debug("telemetry_data_original")
	var pm = ts.t_get_metadata(station)

	// The zero value of a Maybe is Nothing, so an unreported channel needs no
	// initialisation to say so.

	var araw [T_NUM_ANALOG]maybe.Maybe[float64]

	var ndp [T_NUM_ANALOG]int

	var draw [T_NUM_DIGITAL]maybe.Maybe[int]

	if !strings.HasPrefix(info, "T#") {
		if !quiet {
			logrus.Warn("Error: Information part of telemetry packet must begin with \"T#\"")
		}

		return "", ""
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
			logrus.Warn("Nothing after \"T#\" for telemetry data.")
		}

		return "", ""
	}

	var comment string

	// A sequence number that is not a number at all is unknown, rather than
	// the zero that strconv hands back along with the error.

	var seq maybe.Maybe[int]

	var seqNum, seqErr = strconv.Atoi(seqStr)
	if seqErr == nil {
		seq = maybe.Just(seqNum)
	}

	var parts = strings.SplitN(rest, ",", T_NUM_ANALOG+1)
	for n, p := range parts {
		if n < T_NUM_ANALOG {
			if len(p) > 0 {
				// Likewise an analog value that will not parse.

				var f, err = strconv.ParseFloat(p, 64)
				if err == nil {
					araw[n] = maybe.Just(f)
					ndp[n] = t_ndp(p)
				}
			}
			// Version 1.3: Suppress this message.
			// No one pays attention to the original 000 to 255 range.
			// BTW, this doesn't trap values like 0.0 or 1.0
			//if (strlen(p) != 3 || araw[n] < 0 || araw[n] > 255 || araw[n] != (int)(araw[n])) {
			//  if ( ! quiet) {
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
					logrus.Warnf("Expected to find 8 binary digits after \"%s\" for the digital values.", p)
				}
			}

			if len(p) > 8 {
				comment = p[8:]
				p = p[:8]
			}

			for k, v := range p {
				switch v {
				case '0':
					draw[k] = maybe.Just(0)
				case '1':
					draw[k] = maybe.Just(1)
				default:
					if !quiet {
						logrus.Warnf("Found \"%c\" when expecting 0 or 1 for digital value %d.", v, k+1)
					}
				}
			}
		}
	}

	if len(parts) < T_NUM_ANALOG+1 {
		if !quiet {
			logrus.Warn("Found fewer than expected number of telemetry data values.")
		}
	}

	/*
	 * Now process the raw data with any metadata available.
	 */

	logrus.WithFields(logrus.Fields{
		"seq":     seq,
		"araw":    araw,
		"draw":    draw,
		"comment": comment,
	}).Debug("telemetry_data_original: raw data")

	return t_data_process(pm, seq, araw, ndp, draw), comment
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
 * Returns:	output	- Telemetry in human readable form.
 *
 * Description:	We are expecting from 2 to 7 pairs of base 91 digits.
 *		The first pair is the sequence number.
 *		Next we have 1 to 5 analog values.
 *		If digital values are present, all 5 analog values must be present.
 *
 *--------------------------------------------------------------------*/

func (ts *TelemetryState) telemetry_data_base91(station string, cdata string) string {
	logrus.WithField("cdata", cdata).Debug("telemetry_data_base91")
	var pm = ts.t_get_metadata(station)

	// The zero value of a Maybe is Nothing, so an unreported channel needs no
	// initialisation to say so.

	var araw [T_NUM_ANALOG]maybe.Maybe[float64]

	var ndp [T_NUM_ANALOG]int

	var draw [T_NUM_DIGITAL]maybe.Maybe[int]

	if len(cdata) < 4 || len(cdata) > 14 || (len(cdata)%2 == 1) {
		logrus.Errorf("Internal error: Expected even number of 2 to 14 characters but got \"%s\"", cdata)

		return ""
	}

	var seq = two_base91_to_i(cdata[0], cdata[1])
	cdata = cdata[2:]

	for n := 0; n < T_NUM_ANALOG+1 && 2*n < len(cdata); n++ {
		// An invalid base 91 character leaves this value unknown; taking an
		// absent value apart would invent telemetry readings.

		var v, ok = two_base91_to_i(cdata[2*n], cdata[2*n+1]).Get()
		if !ok {
			continue
		}

		if n < T_NUM_ANALOG {
			araw[n] = maybe.Just(float64(v))
		} else {
			for k := range T_NUM_DIGITAL {
				draw[k] = maybe.Just(v & 1)
				v >>= 1
			}
		}
	}

	/*
	 * Now process the raw data with any metadata available.
	 */

	logrus.WithFields(logrus.Fields{
		"seq":  seq,
		"araw": araw,
		"draw": draw,
	}).Debug("telemetry_data_base91: raw data")

	return t_data_process(pm, seq, araw, ndp, draw)
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

func (ts *TelemetryState) telemetry_name_message(station string, msg string) {
	logrus.WithField("msg", msg).Debug("telemetry_name_message")
	msg = strings.TrimSpace(msg)

	var pm = ts.t_get_metadata(station)

	var parts = strings.Split(msg, ",")
	for n, p := range parts {
		if n < T_NUM_ANALOG+T_NUM_DIGITAL {
			if p != "-" {
				pm.name[n] = p
			}
		}
	}

	logrus.WithField("name", pm.name).Debug("names")
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

func (ts *TelemetryState) telemetry_unit_label_message(station string, msg string) {
	logrus.WithField("msg", msg).Debug("telemetry_unit_label_message")

	/*
	 * Make a copy of the input string because this will alter it.
	 * Remove any trailing CR LF.
	 */
	var stemp = strings.TrimSpace(msg)

	var pm = ts.t_get_metadata(station)

	var parts = strings.Split(stemp, ",")
	for n, p := range parts {
		if n < T_NUM_ANALOG+T_NUM_DIGITAL {
			pm.unit[n] = p
		}
	}

	logrus.WithField("unit", pm.unit).Debug("units/labels")
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

func (ts *TelemetryState) telemetry_coefficents_message(station string, msg string, quiet bool) {
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

	var pm = ts.t_get_metadata(station)

	var n = 0
	for p := range strings.SplitSeq(stemp, ",") {
		if n < T_NUM_ANALOG*3 {
			// Keep default (or earlier value) for an empty field.
			if len(p) > 0 {
				pm.coeff[n/3][n%3], _ = strconv.ParseFloat(p, 64)
				pm.coeff_ndp[n/3][n%3] = t_ndp(p)
			} else {
				if !quiet {
					logrus.Warnf("Equation coefficient position A%d%c is empty. Some applications might not handle this correctly.", n/3+1, n%3+'a')
				}
			}
		}

		n++
	}

	if n != T_NUM_ANALOG*3 {
		if !quiet {
			logrus.Warnf("Found %d equation coefficients when 15 were expected. Some applications might not handle this correctly.", n)
		}
	}

	logrus.WithFields(logrus.Fields{
		"coeff":     pm.coeff,
		"coeff_ndp": pm.coeff_ndp,
	}).Debug("coeff")
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

func (ts *TelemetryState) telemetry_bit_sense_message(station string, msg string, quiet bool) {
	logrus.WithField("msg", msg).Debug("telemetry_bit_sense_message")
	var pm = ts.t_get_metadata(station)

	if len(msg) < 8 {
		if !quiet {
			logrus.Warn("The telemetry bit sense message should have at least 8 characters.")
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
				logrus.Warnf("Bit position %d sense value was \"%c\" when 0 or 1 was expected.", n+1, msg[n])
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

	logrus.WithFields(logrus.Fields{
		"sense":   pm.sense,
		"project": pm.project,
	}).Debug("bit sense, project")
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

func t_data_process(pm *t_metadata_s, seq maybe.Maybe[int], araw [T_NUM_ANALOG]maybe.Maybe[float64], ndp [T_NUM_ANALOG]int, draw [T_NUM_DIGITAL]maybe.Maybe[int]) string {
	var output strings.Builder

	if len(pm.project) > 0 {
		output.WriteString(pm.project)
		output.WriteString(": ")
	}

	output.WriteString("Seq=")
	output.WriteString(maybe.Fold("?", strconv.Itoa, seq))

	for n := range T_NUM_ANALOG {
		// Display all or only defined values?  Only defined for now.
		var raw, known = araw[n].Get()
		if known {
			output.WriteString(", ")
			output.WriteString(pm.name[n])
			output.WriteString("=")

			// Scaling and suitable number of decimal places for display.

			var fval = pm.coeff[n][C_A]*raw*raw +
				pm.coeff[n][C_B]*raw +
				pm.coeff[n][C_C]

			var z = IfThenElse(pm.coeff_ndp[n][C_A] == 0, 0, pm.coeff_ndp[n][C_A]+ndp[n]+ndp[n])
			var fndp = max(z, max(pm.coeff_ndp[n][C_B]+ndp[n], pm.coeff_ndp[n][C_C]))

			fmt.Fprintf(&output, "%.*f", fndp, fval)
			if len(pm.unit[n]) > 0 {
				output.WriteString(" ")
				output.WriteString(pm.unit[n])
			}
		}
	}

	for n := range T_NUM_DIGITAL {
		// Display all or only defined values?  Only defined for now.
		var raw, known = draw[n].Get()
		if known {
			output.WriteString(", ")
			output.WriteString(pm.name[T_NUM_ANALOG+n])
			output.WriteString("=")

			// Possible inverting for bit sense.

			var dval = IfThenElse(pm.sense[n], raw, 1-raw)

			output.WriteString(strconv.Itoa(dval))
			if len(pm.unit[T_NUM_ANALOG+n]) > 0 {
				output.WriteString(" ")
				output.WriteString(pm.unit[T_NUM_ANALOG+n])
			}
		}
	}

	var result = output.String()

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithField("output", result).Debug("t_data_process")
	}

	return result
} /* end t_data_process */
