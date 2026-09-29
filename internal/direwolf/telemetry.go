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

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

const numAnalog = 5  /* Number of analog channels. */
const numDigital = 8 /* Number of digital channels. */

type stationMetadata struct {
	next *stationMetadata /* Next in linked list. */

	station string /* Station name with optional SSID. */

	project string /* Description for data. */
	/* "Project Name" or "project title" in the spec. */

	name [numAnalog + numDigital]string
	/* Names for channels.  e.g. Battery, Temperature */

	unit [numAnalog + numDigital]string
	/* Units for channels.  e.g. Volts, Deg.C */

	coeff [numAnalog][3]float64 /* a, b, c coefficients for scaling. */

	coeffNDP [numAnalog][3]int /* Number of decimal places for above. */

	sense [numDigital]bool /* Polarity for digital channels. */
}

const coeffA = 0 /* Scaling coefficient positions. */
const coeffB = 1
const coeffC = 2

type TelemetryState struct {
	mdListHead *stationMetadata
}

func NewTelemetryState() *TelemetryState {
	return new(TelemetryState)
}

/*-------------------------------------------------------------------
 *
 * Name:        getMetadata
 *
 * Purpose:     Obtain pointer to metadata for specified station.
 *		If not found, allocate a fresh one and initialize with defaults.
 *
 * Inputs:	station		- Station name with optional SSID.
 *
 * Returns:	Pointer to metadata.
 *
 *--------------------------------------------------------------------*/

func (ts *TelemetryState) getMetadata(station string) *stationMetadata {
	logrus.WithField("station", station).Debug("getMetadata")
	for p := ts.mdListHead; p != nil; p = p.next {
		if station == p.station {
			return (p)
		}
	}

	var p = new(stationMetadata)

	p.station = station

	for n := range numAnalog {
		p.name[n] = fmt.Sprintf("A%d", n+1)
	}

	for n := range numDigital {
		p.name[numAnalog+n] = fmt.Sprintf("D%d", n+1)
	}

	for n := range numAnalog {
		p.coeff[n][coeffA] = 0.
		p.coeff[n][coeffB] = 1.
		p.coeff[n][coeffC] = 0.
		p.coeffNDP[n][coeffA] = 0
		p.coeffNDP[n][coeffB] = 0
		p.coeffNDP[n][coeffC] = 0
	}

	for n := range numDigital {
		p.sense[n] = true
	}

	p.next = ts.mdListHead
	ts.mdListHead = p

	return (p)
} /* end getMetadata */

/*-------------------------------------------------------------------
 *
 * Name:        decimalPlaces
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

func decimalPlaces(str string) int {
	var p = strings.Index(str, ".")
	if p == -1 {
		return (0)
	} else {
		return len(str) - (p + 1)
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        dataOriginal
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

func (ts *TelemetryState) dataOriginal(station string, info string, quiet bool) (string, string) {
	logrus.WithField("info", info).Debug("dataOriginal")
	var pm = ts.getMetadata(station)

	// The zero value of a Maybe is Nothing, so an unreported channel needs no
	// initialisation to say so.

	var araw [numAnalog]maybe.Maybe[float64]

	var ndp [numAnalog]int

	var draw [numDigital]maybe.Maybe[int]

	if !strings.HasPrefix(info, "T#") {
		if !quiet {
			logrus.WithFields(logrus.Fields{
				"station": station,
				"info":    info,
			}).Warn("Information part of telemetry packet must begin with \"T#\"")
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
			logrus.WithField("station", station).Warn("Nothing after \"T#\" for telemetry data")
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

	var parts = strings.SplitN(rest, ",", numAnalog+1)
	for n, p := range parts {
		if n < numAnalog {
			if len(p) > 0 {
				// Likewise an analog value that will not parse.

				var f, err = strconv.ParseFloat(p, 64)
				if err == nil {
					araw[n] = maybe.Just(f)
					ndp[n] = decimalPlaces(p)
				}
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

		if n == numAnalog {
			/* We expect to have 8 digits of 0 and 1. */
			/* Anything left over is a comment. */
			if len(p) < 8 {
				if !quiet {
					logrus.WithFields(logrus.Fields{
						"station": station,
						"digital": p,
					}).Warn("Expected to find 8 binary digits for the digital values")
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
						logrus.WithFields(logrus.Fields{
							"station":  station,
							"value":    string(v),
							"position": k + 1,
						}).Warn("Expected 0 or 1 for a digital value")
					}
				}
			}
		}
	}

	if len(parts) < numAnalog+1 {
		if !quiet {
			logrus.WithField("station", station).Warn("Found fewer than expected number of telemetry data values")
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
	}).Debug("dataOriginal: raw data")

	return formatData(pm, seq, araw, ndp, draw), comment
} /* end dataOriginal */

/*-------------------------------------------------------------------
 *
 * Name:        dataBase91
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

func (ts *TelemetryState) dataBase91(station string, cdata string) string {
	logrus.WithField("cdata", cdata).Debug("dataBase91")
	var pm = ts.getMetadata(station)

	// The zero value of a Maybe is Nothing, so an unreported channel needs no
	// initialisation to say so.

	var araw [numAnalog]maybe.Maybe[float64]

	var ndp [numAnalog]int

	var draw [numDigital]maybe.Maybe[int]

	if len(cdata) < 4 || len(cdata) > 14 || (len(cdata)%2 == 1) {
		logrus.WithFields(logrus.Fields{
			"station": station,
			"cdata":   cdata,
		}).Error("Internal error: Expected even number of 2 to 14 characters of base 91 telemetry")

		return ""
	}

	var seq = two_base91_to_i(cdata[0], cdata[1])
	cdata = cdata[2:]

	for n := 0; n < numAnalog+1 && 2*n < len(cdata); n++ {
		// An invalid base 91 character leaves this value unknown; taking an
		// absent value apart would invent telemetry readings.

		var v, ok = two_base91_to_i(cdata[2*n], cdata[2*n+1]).Get()
		if !ok {
			continue
		}

		if n < numAnalog {
			araw[n] = maybe.Just(float64(v))
		} else {
			for k := range numDigital {
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
	}).Debug("dataBase91: raw data")

	return formatData(pm, seq, araw, ndp, draw)
} /* end dataBase91 */

/*-------------------------------------------------------------------
 *
 * Name:        nameMessage
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

func (ts *TelemetryState) nameMessage(station string, msg string) {
	logrus.WithField("msg", msg).Debug("nameMessage")
	msg = strings.TrimSpace(msg)

	var pm = ts.getMetadata(station)

	var parts = strings.Split(msg, ",")
	for n, p := range parts {
		if n < numAnalog+numDigital {
			if p != "-" {
				pm.name[n] = p
			}
		}
	}

	logrus.WithField("name", pm.name).Debug("names")
} /* end nameMessage */

/*-------------------------------------------------------------------
 *
 * Name:        unitLabelMessage
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

func (ts *TelemetryState) unitLabelMessage(station string, msg string) {
	logrus.WithField("msg", msg).Debug("unitLabelMessage")

	/*
	 * Make a copy of the input string because this will alter it.
	 * Remove any trailing CR LF.
	 */
	var stemp = strings.TrimSpace(msg)

	var pm = ts.getMetadata(station)

	var parts = strings.Split(stemp, ",")
	for n, p := range parts {
		if n < numAnalog+numDigital {
			pm.unit[n] = p
		}
	}

	logrus.WithField("unit", pm.unit).Debug("units/labels")
} /* end unitLabelMessage */

/*-------------------------------------------------------------------
 *
 * Name:        coefficientsMessage
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

func (ts *TelemetryState) coefficientsMessage(station string, msg string, quiet bool) {
	logrus.WithField("msg", msg).Debug("coefficientsMessage")

	/*
	 * Make a copy of the input string because this will alter it.
	 * Remove any trailing CR LF.
	 */
	var stemp = strings.TrimSpace(msg)

	var pm = ts.getMetadata(station)

	var n = 0
	for p := range strings.SplitSeq(stemp, ",") {
		if n < numAnalog*3 {
			// Keep default (or earlier value) for an empty field.
			if len(p) > 0 {
				pm.coeff[n/3][n%3], _ = strconv.ParseFloat(p, 64)
				pm.coeffNDP[n/3][n%3] = decimalPlaces(p)
			} else {
				if !quiet {
					logrus.WithFields(logrus.Fields{
						"station":  station,
						"position": fmt.Sprintf("A%d%c", n/3+1, n%3+'a'),
						"hint":     "Some applications might not handle this correctly",
					}).Warn("Equation coefficient is empty")
				}
			}
		}

		n++
	}

	if n != numAnalog*3 {
		if !quiet {
			logrus.WithFields(logrus.Fields{
				"station": station,
				"found":   n,
				"hint":    "Some applications might not handle this correctly",
			}).Warn("Expected 15 equation coefficients")
		}
	}

	logrus.WithFields(logrus.Fields{
		"coeff":    pm.coeff,
		"coeffNDP": pm.coeffNDP,
	}).Debug("coeff")
} /* end coefficientsMessage */

/*-------------------------------------------------------------------
 *
 * Name:        bitSenseMessage
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

func (ts *TelemetryState) bitSenseMessage(station string, msg string, quiet bool) {
	logrus.WithField("msg", msg).Debug("bitSenseMessage")
	var pm = ts.getMetadata(station)

	if len(msg) < 8 {
		if !quiet {
			logrus.WithFields(logrus.Fields{
				"station": station,
				"msg":     msg,
			}).Warn("The telemetry bit sense message should have at least 8 characters")
		}
	}

	var n int
	for n = 0; n < numDigital && n < len(msg); n++ {
		switch msg[n] {
		case '1':
			pm.sense[n] = true
		case '0':
			pm.sense[n] = false
		default:
			if !quiet {
				logrus.WithFields(logrus.Fields{
					"station":  station,
					"value":    string(msg[n]),
					"position": n + 1,
				}).Warn("Expected 0 or 1 for a bit sense value")
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
} /* end bitSenseMessage */

/*-------------------------------------------------------------------
 *
 * Name:        formatData
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

func formatData(pm *stationMetadata, seq maybe.Maybe[int], araw [numAnalog]maybe.Maybe[float64], ndp [numAnalog]int, draw [numDigital]maybe.Maybe[int]) string {
	var output strings.Builder

	if len(pm.project) > 0 {
		output.WriteString(pm.project)
		output.WriteString(": ")
	}

	output.WriteString("Seq=")
	output.WriteString(maybe.Fold("?", strconv.Itoa, seq))

	for n := range numAnalog {
		// Display all or only defined values?  Only defined for now.
		var raw, known = araw[n].Get()
		if known {
			output.WriteString(", ")
			output.WriteString(pm.name[n])
			output.WriteString("=")

			// Scaling and suitable number of decimal places for display.

			var fval = pm.coeff[n][coeffA]*raw*raw +
				pm.coeff[n][coeffB]*raw +
				pm.coeff[n][coeffC]

			var z = dwutil.IfThenElse(pm.coeffNDP[n][coeffA] == 0, 0, pm.coeffNDP[n][coeffA]+ndp[n]+ndp[n])
			var fndp = max(z, max(pm.coeffNDP[n][coeffB]+ndp[n], pm.coeffNDP[n][coeffC]))

			fmt.Fprintf(&output, "%.*f", fndp, fval)
			if len(pm.unit[n]) > 0 {
				output.WriteString(" ")
				output.WriteString(pm.unit[n])
			}
		}
	}

	for n := range numDigital {
		// Display all or only defined values?  Only defined for now.
		var raw, known = draw[n].Get()
		if known {
			output.WriteString(", ")
			output.WriteString(pm.name[numAnalog+n])
			output.WriteString("=")

			// Possible inverting for bit sense.

			var dval = dwutil.IfThenElse(pm.sense[n], raw, 1-raw)

			output.WriteString(strconv.Itoa(dval))
			if len(pm.unit[numAnalog+n]) > 0 {
				output.WriteString(" ")
				output.WriteString(pm.unit[numAnalog+n])
			}
		}
	}

	var result = output.String()

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.WithField("output", result).Debug("formatData")
	}

	return result
} /* end formatData */
