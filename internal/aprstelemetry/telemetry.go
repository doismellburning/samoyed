// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package aprstelemetry decodes APRS telemetry: data in the original "T#"
// format and in the base 91 compressed comment format, and the PARM, UNIT,
// EQNS and BITS metadata messages that name, label, scale and set the
// polarity of each station's channels.
//
// From Dire Wolf's telemetry.c:
//
//	Purpose:	Decode telemetry information.
//			Point out where it violates the protocol spec and
//			other applications might not interpret it properly.
//
//	References:	APRS Protocol, chapter 13.
//			http://www.aprs.org/doc/APRS101.PDF
//
//			Base 91 compressed format
//			http://he.fi/doc/aprs-base91-comment-telemetry.txt
package aprstelemetry

import (
	"container/list"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// maxStations is how many stations' metadata a State keeps.  Metadata can
// come from anyone on frequency or on APRS-IS, so without a limit a stream
// of stations would grow it for ever.
const maxStations = 1000

const numAnalog = 5  // Number of analog channels.
const numDigital = 8 // Number of digital channels.

type stationMetadata struct {
	station string // Station name with optional SSID.

	// Description for data.
	// "Project Name" or "project title" in the spec.
	project string

	// Names for channels.  e.g. Battery, Temperature
	name [numAnalog + numDigital]string

	// Units for channels.  e.g. Volts, Deg.C
	unit [numAnalog + numDigital]string

	coeff [numAnalog][3]float64 // a, b, c coefficients for scaling.

	coeffNDP [numAnalog][3]int // Number of decimal places for above.

	sense [numDigital]bool // Polarity for digital channels.
}

const coeffA = 0 // Scaling coefficient positions.
const coeffB = 1
const coeffC = 2

// State holds the telemetry metadata - channel names, units, scaling and
// bit sense - each station has sent, for decoding its later data.  It keeps
// the most recently used stations' metadata, up to maxStations of them; a
// station whose metadata has been dropped to make room is back to the
// defaults until it sends more.  It is safe for use by more than one
// goroutine.
type State struct {
	// mu guards everything below, and is held for the whole of each exported
	// method, since decoding data reads the metadata it looks up.
	mu sync.Mutex

	// capacity is how many stations to keep: maxStations, bar in tests.
	capacity int

	// stations finds a station's element in recency, whose values are
	// *stationMetadata, most recently used at the front.
	stations map[string]*list.Element
	recency  *list.List

	// defaults is what a station that has sent no metadata decodes with.  It
	// is shared by all such stations, so nothing may change it.
	defaults *stationMetadata
}

// New returns a State that has heard no metadata, so every station starts
// with the defaults.
func New() *State {
	var ts = new(State)

	ts.capacity = maxStations
	ts.stations = make(map[string]*list.Element)
	ts.recency = list.New()
	ts.defaults = newStationMetadata("")

	return ts
}

// newStationMetadata returns metadata for station with the defaults: channels
// named A1-A5 and D1-D8 with no units, analog values unscaled, and every bit
// active high.
func newStationMetadata(station string) *stationMetadata {
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

	return p
}

// stored returns the metadata kept for station, if any, making the station
// the most recently used.
func (ts *State) stored(station string) (*stationMetadata, bool) {
	var e, ok = ts.stations[station]
	if !ok {
		return nil, false
	}

	ts.recency.MoveToFront(e)

	return e.Value.(*stationMetadata), true //nolint:forcetypeassert // recency holds nothing else
}

// lookup returns the metadata for decoding station's data: what it has sent,
// or the shared defaults, which must not be changed, if it has sent none.  Unlike
// getMetadata it keeps nothing, so data alone takes up no room.
func (ts *State) lookup(station string) *stationMetadata {
	if p, ok := ts.stored(station); ok {
		return p
	}

	return ts.defaults
}

// getMetadata returns the metadata for station, a station name with optional
// SSID, for a metadata message to change, first allocating one with the
// defaults if the station has sent none.  Either way the station becomes the
// most recently used, and allocating one drops the least recently used station
// if the State is full.
func (ts *State) getMetadata(station string) *stationMetadata {
	logrus.WithField("station", station).Debug("getMetadata")

	if p, ok := ts.stored(station); ok {
		return p
	}

	var p = newStationMetadata(station)

	if ts.recency.Len() >= ts.capacity {
		var oldest = ts.recency.Back()
		var dropped = ts.recency.Remove(oldest).(*stationMetadata) //nolint:forcetypeassert // recency holds nothing else

		delete(ts.stations, dropped.station)
		logrus.WithField("station", dropped.station).Debug("Dropped least recently used telemetry metadata")
	}

	ts.stations[station] = ts.recency.PushFront(p)

	return (p)
}

// decimalPlaces counts the digits after any decimal point in str, a number in
// text form:
//
//	1	--> 0
//	1.	--> 0
//	1.2	--> 1
//	1.23	--> 2
func decimalPlaces(str string) int {
	var p = strings.Index(str, ".")
	if p == -1 {
		return (0)
	} else {
		return len(str) - (p + 1)
	}
}

// DataOriginal decodes telemetry data in the original format, from the
// Information field info of a packet from station.  It returns the telemetry
// in human readable form, and any comment after the data.  quiet suppresses
// the complaints about data that breaks the spec.
//
// The first character, after the "T" data type indicator, must be "#"
// followed by a sequence number.  Up to 5 analog and 8 digital channel values
// are specified as in this example from the protocol spec.
//
//	T#005,199,000,255,073,123,01101001
//
// The analog values are supposed to be 3 digit integers in the range of 000
// to 255 in fixed columns.  After reading the discussion groups it seems that
// few adhere to those restrictions.  When I started to look for some local
// signals, this was the first one to appear:
//
//	KB1GKN-10>APRX27,UNCAN,WIDE1*:T#491,4.9,0.3,25.0,0.0,1.0,00000000
//
// Not integers.  Not fixed width fields.
//
// Originally I printed a warning if values were not in range of 000 to 255
// but later took it out because no one pays attention to that original
// restriction anymore.
func (ts *State) DataOriginal(station string, info string, quiet bool) (string, string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("info", info).Debug("DataOriginal")
	var pm = ts.lookup(station)

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

	// Everything after the T#, less any trailing CR/LF.

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
			// We expect to have 8 digits of 0 and 1.
			// Anything left over is a comment.
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

	// Now process the raw data with any metadata available.

	logrus.WithFields(logrus.Fields{
		"seq":     seq,
		"araw":    araw,
		"draw":    draw,
		"comment": comment,
	}).Debug("DataOriginal: raw data")

	return formatData(pm, seq, araw, ndp, draw), comment
}

// DataBase91 decodes telemetry data from station in the base 91 compressed
// format, cdata being the characters between the | delimiters, and returns it
// in human readable form.
//
// We are expecting from 2 to 7 pairs of base 91 digits.  The first pair is
// the sequence number.  Next we have 1 to 5 analog values.  If digital values
// are present, all 5 analog values must be present.
func (ts *State) DataBase91(station string, cdata string) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("cdata", cdata).Debug("DataBase91")
	var pm = ts.lookup(station)

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

	var seq = base91Pair(cdata[0], cdata[1])
	cdata = cdata[2:]

	for n := 0; n < numAnalog+1 && 2*n < len(cdata); n++ {
		// An invalid base 91 character leaves this value unknown; taking an
		// absent value apart would invent telemetry readings.

		var v, ok = base91Pair(cdata[2*n], cdata[2*n+1]).Get()
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

	// Now process the raw data with any metadata available.

	logrus.WithFields(logrus.Fields{
		"seq":  seq,
		"araw": araw,
		"draw": draw,
	}).Debug("DataBase91: raw data")

	return formatData(pm, seq, araw, ndp, draw)
}

// NameMessage stores the names for station's analog and digital channels,
// for use when its data values are received.  station is the message's
// addressee, not its sender, and msg is the rest of the message after
// "PARM.": a variable length list of comma separated names, "-" keeping a
// channel's existing name.
//
// The original spec has different maximum lengths for different fields which
// we will ignore.
//
// TBD: What should we do if some, but not all, names are specified?  Clear the
// others or keep the defaults?
func (ts *State) NameMessage(station string, msg string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("msg", msg).Debug("NameMessage")
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
}

// UnitLabelMessage stores the units/labels for station's analog and digital
// channels, for use when its data values are received.  station is the
// message's addressee, not its sender, and msg is the rest of the message
// after "UNIT.": a variable length list of comma separated units/labels.
//
// The original spec has different maximum lengths for different fields which
// we will ignore.
func (ts *State) UnitLabelMessage(station string, msg string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("msg", msg).Debug("UnitLabelMessage")

	// Remove any trailing CR LF.
	var stemp = strings.TrimSpace(msg)

	var pm = ts.getMetadata(station)

	var parts = strings.Split(stemp, ",")
	for n, p := range parts {
		if n < numAnalog+numDigital {
			pm.unit[n] = p
		}
	}

	logrus.WithField("unit", pm.unit).Debug("units/labels")
}

// CoefficientsMessage stores the scaling coefficients for station's analog
// channels, for use when its data values are received.  station is the
// message's addressee, not its sender, and msg is the rest of the message
// after "EQNS.": a comma separated list of 15 floating point values, a, b and
// c for each channel in turn.  quiet suppresses the complaints about a message
// that breaks the spec.
//
// The spec appears to require all 15 so we complain if fewer are found.
func (ts *State) CoefficientsMessage(station string, msg string, quiet bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("msg", msg).Debug("CoefficientsMessage")

	// Remove any trailing CR LF.
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
}

// BitSenseMessage stores the polarity of station's digital channels, and its
// project name or title, for use when its data values are received.  station
// is the message's addressee, not its sender, and msg is the rest of the
// message after "BITS.": eight binary digits for the digital active states,
// with anything left over the project name or title.  quiet suppresses the
// complaints about a message that breaks the spec.
func (ts *State) BitSenseMessage(station string, msg string, quiet bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	logrus.WithField("msg", msg).Debug("BitSenseMessage")
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

	// Skip comma if first character of comment field.
	//
	// The protocol spec is inconsistent here.
	// The definition shows the Project Title immediately after a fixed width field of 8 binary digits.
	// The example has a comma in there.
	//
	// The toolkit telem-bits.pl script does insert the comma because it seems more sensible.
	// Here we accept it either way.  i.e. Discard first character after data values if it is comma.

	if n < len(msg) && msg[n] == ',' {
		n++
	}

	pm.project = msg[n:]

	logrus.WithFields(logrus.Fields{
		"sense":   pm.sense,
		"project": pm.project,
	}).Debug("bit sense, project")
}

// formatData puts a sequence number seq, 5 raw analog values araw, each with
// ndp decimal places, and 8 raw digital values draw into human readable form,
// scaled, named and labelled according to the station metadata pm.  An
// unknown sequence number shows as "?", and an unknown channel value is left
// out.
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
}
