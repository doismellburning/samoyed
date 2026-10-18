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

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/coordconvutil"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/phy"
	"github.com/doismellburning/samoyed/internal/touchtone"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
	"github.com/tzneal/coordconv"
)

/* Error codes for sending responses to user. */

const ErrorOK = 0             /* Success. */
const ErrorDMsg = 1           /* D was first char of field.  Not implemented yet. */
const ErrorInternal = 2       /* Internal error.  Shouldn't be here. */
const ErrorMacroNoMatch = 3   /* No definition for digit sequence. */
const ErrorBadChecksum = 4    /* Bad checksum on call. */
const ErrorInvalidCall = 5    /* Invalid callsign. */
const ErrorInvalidObjName = 6 /* Invalid object name. */
const ErrorInvalidSymbol = 7  /* Invalid symbol specification. */
const ErrorInvalidLoc = 8     /* Invalid location. */
const ErrorNoCall = 9         /* No call or object name included. */
const ErrorInvalidMHead = 10  /* Invalid Maidenhead Locator. */
const ErrorInvalidSatSq = 11  /* Satellite square must be 4 digits. */
const ErrorSuffixNoCall = 12  /* No known callsign for suffix. */
const ErrorCount = 13         /* Number of items above.  i.e. Last number plus 1. */

func ErrorName(errCode int) string { // TODO Fix the type
	switch errCode {
	case ErrorOK:
		return "OK"
	case ErrorDMsg:
		return "D_MSG"
	case ErrorInternal:
		return "INTERNAL"
	case ErrorMacroNoMatch:
		return "MACRO_NOMATCH"
	case ErrorBadChecksum:
		return "BAD_CHECKSUM"
	case ErrorInvalidCall:
		return "INVALID_CALL"
	case ErrorInvalidObjName:
		return "INVALID_OBJNAME"
	case ErrorInvalidSymbol:
		return "INVALID_SYMBOL"
	case ErrorInvalidLoc:
		return "INVALID_LOC"
	case ErrorNoCall:
		return "NO_CALL"
	case ErrorInvalidMHead:
		return "INVALID_MHEAD"
	case ErrorInvalidSatSq:
		return "INVALID_SATSQ"
	case ErrorSuffixNoCall:
		return "SUFFIX_NO_CALL"
	default:
		panic(fmt.Sprintf("Unknown errCode: %d", errCode))
	}
}

/*
 * Configuration options for APRStt.
 */

const MaxXmits = 10

const TT_MTEXT_LEN = 64

const APRSTT_LOC_DESC_LEN = 32 /* Need at least 26 */

const APRSTT_DEFAULT_SYMTAB = '\\'
const APRSTT_DEFAULT_SYMBOL = 'A'

/*
 * For holding location format specifications from config file.
 * Same thing is also useful for macro definitions.
 * We have exactly the same situation of looking for a pattern
 * match and extracting fixed size groups of digits.
 */

type LocType int

const (
	LocPoint LocType = iota
	LocVector
	LocGrid
	LocUTM
	LocMGRS
	LocUSNG
	LocMacro
	LocMHead
	LocSatSq
	LocAmbig
)

type PointLoc struct {
	Lat float64 /* Specific locations. */
	Lon float64
}

type VectorLoc struct {
	Lat   float64 /* For bearing/direction. */
	Lon   float64
	Scale float64 /* conversion to meters */
}

type GridLoc struct {
	Lat0 float64 /* yyy all zeros. */
	Lon0 float64 /* xxx */
	Lat9 float64 /* yyy all nines. */
	Lon9 float64 /* xxx */
}

type UTMLoc struct {
	Scale   float64
	XOffset float64
	YOffset float64
	Zone    int  /* UTM zone, should be 1-60 */
	LatBand rune /* Latitude band if specified, otherwise space or - */
	Hemi    rune /* UTM Hemisphere, should be 'N' or 'S'. */
}

type MGRSLoc struct {
	Zone string /* Zone and square for USNG/MGRS */
}

type MHeadLoc struct {
	Prefix string /* should be 10, 6, or 4 digits to be */
	/* prepended to the received sequence. */
}

type MacroLoc struct {
	Definition string
}

type Loc struct {
	Type LocType

	Pattern string /* e.g. B998, B5bbbdddd, B2xxyy, Byyyxxx, BAxxxx */
	/* For macros, it should be all fixed digits, */
	/* and the letters x, y, z.  e.g.  911, xxyyyz */

	Point  PointLoc
	Vector VectorLoc
	Grid   GridLoc
	UTM    UTMLoc
	MGRS   MGRSLoc
	MHead  MHeadLoc
	Macro  MacroLoc
}

type Config struct {
	GatewayEnabled int /* Send DTMF sequences to APRStt gateway. */

	ObjRecvChan int /* Channel to listen for tones. */

	ObjXmitChan int /* Channel to transmit object report. */
	/* -1 for none.  This could happen if we */
	/* are only sending to application */
	/* and/or IGate. */

	ObjSendToApp int /* send to attached application(s). */

	ObjSendToIG int /* send to IGate. */

	ObjXmitVia string
	/* e.g.  empty or "WIDE2-1,WIDE1-1" */

	RetainTime int /* Seconds to keep information about a user. */

	NumXmits int /* Number of times to transmit object report. */

	XmitDelay [MaxXmits]int /* Delay between them. */
	/* e.g.  3 seconds before first transmission then */
	/* delays of 16, 32, seconds etc. in between repeats. */

	Locs []*Loc

	CorralLat       float64 /* The "corral" for unknown locations. */
	CorralLon       float64
	CorralOffset    float64
	CorralAmbiguity int

	Status [10]string /* Up to 9 status messages. e.g.  "/enroute" */
	/* Position 0 means none and can't be changed. */

	Response [ErrorCount]struct {
		Method string /* SPEECH or MORSE[-n] */
		MText  string /* Message text. */
	}

	Cmd string /* Command to generate custom audible response. */
}

/*
 * Touch Tone sequences are accumulated here until # terminator found.
 * Kept separate for each audio channel so the gateway CAN be listening
 * on multiple channels at the same time.
 */

const MaxMsgLen = 100

// ttMaxMacroDepth bounds how deep macros may expand within macros, so one
// whose definition expands to itself fails rather than recursing forever.
const ttMaxMacroDepth = 16

// ttParseState holds temporary state accumulated during a single Sequence call.
type ttParseState struct {
	callsign        string /* really object name */
	symtabOrOverlay byte
	symbolCode      byte
	locText         string
	longitude       maybe.Maybe[float64]
	latitude        maybe.Maybe[float64]
	ambiguity       maybe.Maybe[int]
	comment         string
	freq            string
	ctcss           string
	micE            byte
	dao             [5]byte
	ssid            int /* Default 12 for APRStt user. */
	macroDepth      int /* Macros expanded within macros so far. */
}

func newTTParseState() ttParseState {
	return ttParseState{ //nolint:exhaustruct_v5
		symtabOrOverlay: APRSTT_DEFAULT_SYMTAB,
		symbolCode:      APRSTT_DEFAULT_SYMBOL,
		dao:             [5]byte{'!', 'T', ' ', ' ', '!'},
		ssid:            12,
	}
}

// setDAOKeys records the first two keys of location field e in the DAO, as
// "!TB5!" for the standard bearing and range.  A field of one key has a space
// for the second.
func (s *ttParseState) setDAOKeys(e string) {
	s.dao[2] = e[0]
	s.dao[3] = ttFieldByte(e, 1)

	if s.dao[3] == 0 {
		s.dao[3] = ' '
	}
}

// ttFieldByte is the key at index i of field e, or 0 past its end, as the
// Dire Wolf parsers read C's terminating NUL there.
func ttFieldByte(e string, i int) byte {
	if i < len(e) {
		return e[i]
	}

	return 0
}

// ObjectReportSink is where the gateway sends an object report, with the
// channel it concerns.
type ObjectReportSink func(channel int, pp *ax25.Packet)

// Gateway is the APRStt gateway. Construct one with NewGateway.
type Gateway struct {
	config         *Config
	debug          int
	users          *ttUsers
	msgStr         [phy.MaxRadioChans]string
	pollPeriod     int
	runningTests   bool
	lastParseState ttParseState

	// audioLevel reports a channel's received audio level, for a touch
	// tone message to carry, or is nil for none to report.
	audioLevel dwcore.AudioLevelFunc

	// recFrame is where a raw touch tone message goes, as though received,
	// or nil to drop it.
	recFrame frameReceiver

	// transmit is where an audible response goes, to be transmitted, or nil to
	// drop it.
	transmit dwcore.FrameSender
}

/*------------------------------------------------------------------
 *
 * Name:        NewGateway
 *
 * Purpose:     Initialize and return a new Gateway.
 *
 * Inputs:      mycall	- A channel's callsign, which object reports are sent
 *			  from.
 *		p	- Pointer to configuration options gathered by config.c.
 *		toApps	- Sends an object report to the client applications,
 *			  when configured to; nil for none.
 *		remember - Called with each object report we transmit, and its
 *			  channel, so the digipeater doesn't repeat our own;
 *			  nil for nobody to tell.
 *		toIGate	- Sends an object report to APRS-IS; nil for no
 *			  IGate.
 *		recFrame - Where a raw touch tone message goes, as though
 *			  received; nil to drop it.
 *		transmit - Where audible responses and object reports go, to
 *			  be transmitted; nil to drop them.
 *		audioLevel - Reports a channel's received audio level, for a
 *			  touch tone message to carry; nil for none.
 *		debug	- Debug printing control.
 *
 * Returns:     Pointer to new Gateway.
 *
 * Description:	The main program needs to call this at application
 *		start up time after reading the configuration file.
 *
 *----------------------------------------------------------------*/

func NewGateway(
	mycall func(channel int) string,
	p *Config,
	toApps ObjectReportSink,
	remember ObjectReportSink,
	toIGate ObjectReportSink,
	recFrame frameReceiver,
	transmit dwcore.FrameSender,
	audioLevel dwcore.AudioLevelFunc,
	debug int,
) *Gateway {
	var g = &Gateway{debug: debug} //nolint:exhaustruct_v5

	g.config = p
	g.audioLevel = audioLevel
	g.recFrame = recFrame
	g.transmit = transmit
	g.users = newTTUsers(mycall, p)
	g.users.toApps = toApps
	g.users.remember = remember
	g.users.toIGate = toIGate
	g.users.transmit = transmit

	return g
}

/*------------------------------------------------------------------
 *
 * Name:        Button
 *
 * Purpose:     Process one received button press.
 *
 * Inputs:      channel		- Audio channel it came from.
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

func (g *Gateway) Button(channel int, button rune) {
	dwutil.Assert(channel >= 0 && channel < phy.MaxRadioChans)

	// if (button != '.') {
	//   dw_printf ("aprs_tt_button (%d, '%c')\n", channel, button);
	// }

	// TODO:  Might make more sense to put timeout here rather in the dtmf decoder.

	if button == '$' {
		/* Timeout reset. */
		g.msgStr[channel] = ""
	} else if button != '.' && button != ' ' {
		if len(g.msgStr[channel]) < MaxMsgLen {
			g.msgStr[channel] += string(button)
		}

		if button == '#' {
			/*
			 * Put into the receive queue like any other packet.
			 * This way they are all processed by the common receive thread
			 * rather than the thread associated with the particular audio device.
			 */
			var alevel ax25.ALevel
			if g.audioLevel != nil {
				alevel = g.audioLevel(channel, 0)
			}

			raw_tt_data_to_app(channel, g.msgStr[channel], alevel, g.recFrame)

			g.msgStr[channel] = ""
		}
	} else {
		/*
		 * Idle time. Poll occasionally for processing.
		 * Timing would be off we we are listening to more than
		 * one channel so do this only for the one specified
		 * in the TTOBJ command.
		 */
		if channel == g.config.ObjRecvChan {
			g.pollPeriod++
			if g.pollPeriod >= 39 {
				g.pollPeriod = 0

				g.users.background()
			}
		}
	}
} /* end Button */

/*------------------------------------------------------------------
 *
 * Name:        Sequence
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

func (g *Gateway) Sequence(ctx context.Context, channel int, msg string) {
	logrus.WithField("msg", msg).Debug("aprs_tt Sequence")

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
	var state = newTTParseState()

	/*
	 * Parse the touch tone sequence.
	 */
	var err = g.parseFields(&state, msg)

	logrus.WithFields(logrus.Fields{
		"callsign":  state.callsign,
		"ssid":      state.ssid,
		"symbol":    string([]byte{state.symtabOrOverlay, state.symbolCode}),
		"freq":      state.freq,
		"ctcss":     state.ctcss,
		"comment":   state.comment,
		"latitude":  state.latitude,
		"longitude": state.longitude,
		"dao":       string(state.dao[:]),
	}).Debug("APRStt parsed fields")

	g.lastParseState = state

	if g.runningTests {
		return
	}

	/*
	 * If digested successfully.  Add to our list of users and schedule transmissions.
	 */

	if err == 0 {
		err = g.users.heard(state.callsign, state.ssid, state.symtabOrOverlay, state.symbolCode,
			state.locText, state.latitude, state.longitude, state.ambiguity,
			state.freq, state.ctcss, state.comment, state.micE, string(state.dao[:]))
	}

	/*
	 * If a command / script was supplied, run it now.
	 * This can do additional processing and provide a custom audible response.
	 * This is done only for the success case.
	 * It might be useful to run it for error cases as well but we currently
	 * don't pass in the success / failure code to know the difference.
	 */
	var script_response string

	if err == 0 && len(g.config.Cmd) > 0 {
		var _script_response, _ = dwutil.RunCmd(ctx, g.config.Cmd, 1)
		script_response = string(_script_response)
	}

	/*
	 * Send response to user by constructing packet with SPEECH or MORSE as destination.
	 * Source shouldn't matter because it doesn't get transmitted as AX.25 frame.
	 * Use high priority queue for consistent timing.
	 *
	 * Anything from script, above, will override other predefined responses.
	 */

	var response = g.config.Response[err].MText
	if len(script_response) > 0 {
		response = script_response
	}

	var audible_response = fmt.Sprintf("APRSTT>%s:%s", g.config.Response[err].Method, response)

	var pp = ax25.FromText(audible_response, false)

	if pp == nil {
		logrus.WithField("response", audible_response).Error("Internal error: couldn't make frame from audible response")

		return
	}

	if g.transmit != nil {
		g.transmit(channel, dwcore.PrioHigh, pp)
	}
} /* end Sequence */

/*------------------------------------------------------------------
 *
 * Name:        parseFields
 *
 * Purpose:     Separate the complete string of touch tone characters
 *		into fields, delimited by *, and process each.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		msg		- String of DTMF buttons.
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
 * Returns:	0 for success or one of the Error... codes.
 *
 *----------------------------------------------------------------*/

func (g *Gateway) parseFields(state *ttParseState, msg string) int {
	var fields = strings.FieldsFunc(msg, func(r rune) bool {
		return strings.ContainsRune("*#", r)
	})

	var err int

	for _, e := range fields {
		// text_color_set(DW_COLOR_DEBUG);
		// dw_printf ("parse_fields () field = %s\n", e);
		switch e[0] {
		case 'A':
			// A lone A is a malformed callsign, and parseCallsign says so.
			switch ttFieldByte(e, 1) {
			case 'A': /* AA object-name */
				err = g.parseObjectName(state, e)
				if err != 0 {
					return (err)
				}
			case 'B': /* AB symbol */
				err = g.parseSymbol(state, e)
				if err != 0 {
					return (err)
				}
			case 'C': /* AC new-style-callsign */
				err = g.parseAprstt3Call(state, e)
				if err != 0 {
					return (err)
				}
			default: /* Traditional style call or suffix */
				err = g.parseCallsign(state, e)
				if err != 0 {
					return (err)
				}
			}
		case 'B':
			err = g.parseLocation(state, e)
			if err != 0 {
				return (err)
			}
		case 'C':
			err = g.parseComment(state, e)
			if err != 0 {
				return (err)
			}
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			err = g.expandMacro(state, e)
			if err != 0 {
				return (err)
			}
		default:
			logrus.WithField("field", e).Error("Touch tone field does not start with A, B, C, or digit")

			return (ErrorDMsg)
		}
	}

	// text_color_set(DW_COLOR_DEBUG);
	// dw_printf ("parse_fields () normal return\n");

	return (0)
} /* end parseFields */

/*------------------------------------------------------------------
 *
 * Name:        expandMacro
 *
 * Purpose:     Expand compact form "macro" to full format then process.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should contain only digits.
 *
 * Returns:	0 for success or one of the Error... codes.
 *
 * Description:	Separate out the fields, perform substitution,
 *		call parseFields for processing.
 *
 *
 * Future:	Generalize this to allow any lower case letter for substitution?
 *
 *----------------------------------------------------------------*/

func (g *Gateway) expandMacro(state *ttParseState, e string) int {
	logrus.WithField("sequence", e).Debug("Macro tone sequence")

	var xstr, ystr, zstr, _, _, ipat = g.findTTLocMatch(e)

	if ipat >= 0 {
		// Why did we print b & d here?
		// Documentation says only x, y, z can be used with macros.
		// Only those 3 are processed below.

		// dw_printf ("Matched pattern %3d: '%s', x=%s, y=%s, z=%s, b=%s, d=%s\n", ipat, g.config.ttlocs[ipat].pattern, xstr, ystr, zstr, bstr, dstr);
		logrus.WithFields(logrus.Fields{
			"pattern_index": ipat,
			"pattern":       g.config.Locs[ipat].Pattern,
			"x":             xstr,
			"y":             ystr,
			"z":             zstr,
			"definition":    g.config.Locs[ipat].Macro.Definition,
		}).Debug("Macro matched pattern")

		if g.config.Locs[ipat].Type != LocMacro {
			/* Found match to a different type.  Really shouldn't be here. */
			/* Print internal error message... */
			logrus.WithField("pattern_index", ipat).Error("Internal error: expandMacro matched a pattern that is not a macro")

			return (ErrorInternal)
		}

		if state.macroDepth >= ttMaxMacroDepth {
			logrus.WithFields(logrus.Fields{
				"pattern":    g.config.Locs[ipat].Pattern,
				"definition": g.config.Locs[ipat].Macro.Definition,
				"max_depth":  ttMaxMacroDepth,
			}).Error("Touch tone macros expand too deep; does one expand to itself?")

			return (ErrorInternal)
		}

		/*
		 * We found a match for the length and any fixed digits.
		 * Substitute values in to the definition.
		 */

		var stemp strings.Builder

		var definition = g.config.Locs[ipat].Macro.Definition
		for i, d := range definition {
			if i != len(definition)-1 {
				if (d == 'x' || d == 'y' || d == 'z') && d == rune(definition[i+1]) {
					// Collapse adjacent matching substitution characters.
					continue
				}
			}

			switch d {
			case 'x':
				stemp.WriteString(xstr)
			case 'y':
				stemp.WriteString(ystr)
			case 'z':
				stemp.WriteString(zstr)
			default:
				fmt.Fprintf(&stemp, "%c", d)
			}
		}
		/*
		 * Process as if we heard this over the air.
		 */

		logrus.WithField("sequence", stemp.String()).Debug("Macro after substitution")

		state.macroDepth++
		defer func() { state.macroDepth-- }()

		return (g.parseFields(state, stemp.String()))
	} else {
		/* Send reject sound. */
		/* Does not match any macro definitions. */
		logrus.WithField("sequence", e).Error("Tone sequence did not match any pattern")

		return (ErrorMacroNoMatch)
	}
}

/*------------------------------------------------------------------
 *
 * Name:        parseCallsign
 *
 * Purpose:     Extract traditional format callsign or object name from touch tone sequence.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "A" then a digit.
 *
 * Outputs:	state.callsign
 *
 *		state.symtabOrOverlay - Set to 0-9 or A-Z if specified.
 *
 *		state.symbolCode	- Always set to 'A' (Box, DTMF or RFID)
 *					If you want a different symbol, use the new
 *					object name format and separate symbol specification.
 *
 * Returns:	0 for success or one of the Error... codes.
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
			logrus.WithField("char", string(c)).Error("Bad character in touch tone checksum calculation")
		}
	}

	var expected = rune('0' + (sum % 10))

	if expected != found {
		logrus.WithFields(logrus.Fields{
			"value":    str[:length],
			"expected": string(expected),
			"received": string(found),
		}).Error("Bad touch tone checksum")

		return (ErrorBadChecksum)
	}

	return (0)
}

// twoKeyOverlay translates the keys of a two key overlay.  Keys that don't all
// translate are no overlay at all, as for a callsign in two key format.
func twoKeyOverlay(keys string) (byte, bool) {
	var text, errors = touchtone.TwoKeyToText(keys, false)
	if errors != 0 || text == "" {
		return 0, false
	}

	return text[0], true
}

// twoKeyCallsign translates a callsign in two key format.  Keys that don't all
// translate are no callsign: what's left, such as the digits of a letter whose
// column key went missing, would be taken for a different user.
func twoKeyCallsign(keys string) (string, bool) {
	var text, errors = touchtone.TwoKeyToText(keys, false)
	if errors != 0 || text == "" {
		logrus.WithField("keys", keys).Error("Touch tone callsign in two key format doesn't translate")

		return "", false
	}

	return text, true
}

func (g *Gateway) parseCallsign(state *ttParseState, e string) int {
	if g.debug > 0 {
		logrus.WithField("entry", e).Debug("APRStt parse callsign (starts with A then digit)")
	}

	dwutil.Assert(e[0] == 'A')

	var length = len(e)

	/*
	 * special case: 3 digit tactical call.
	 */

	if length == 4 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) {
		state.callsign = e[1:]

		if g.debug > 0 {
			logrus.WithField("callsign", state.callsign).Debug("Special case, 3 digit tactical call")
		}

		return (0)
	}

	/*
	 * 3 digit abbreviation:  We only do the parsing here.
	 * Another part of application will try to find corresponding full call.
	 */

	if (length == 6 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) &&
		unicode.IsDigit(rune(e[4])) && unicode.IsDigit(rune(e[5]))) ||
		(length == 7 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) &&
			unicode.IsDigit(rune(e[4])) && unicode.IsUpper(rune(e[5])) && unicode.IsDigit(rune(e[6]))) {
		var cs_err = checksum_not_ok(e[1:length-1], length-2, rune(e[length-1]))

		if cs_err != 0 {
			return (cs_err)
		}

		state.callsign = e[1:4]

		if length == 7 {
			var overlay, ok = twoKeyOverlay(string(e[length-3]) + string(e[length-2]))
			if !ok {
				return (ErrorInvalidSymbol)
			}

			state.symbolCode = APRSTT_DEFAULT_SYMBOL
			state.symtabOrOverlay = overlay

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign": state.callsign,
					"symbol":   string(state.symbolCode),
					"overlay":  string(state.symtabOrOverlay),
					"checksum": string(e[length-1]),
				}).Debug("Three digit abbreviation (two key overlay)")
			}
		} else {
			state.symbolCode = APRSTT_DEFAULT_SYMBOL
			state.symtabOrOverlay = e[length-2]

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign": state.callsign,
					"symbol":   string(state.symbolCode),
					"overlay":  string(state.symtabOrOverlay),
					"checksum": string(e[length-1]),
				}).Debug("Three digit abbreviation")
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
			var callsign, callOK = twoKeyCallsign(e[1 : length-3])
			if !callOK {
				return (ErrorInvalidCall)
			}

			state.callsign = callsign

			var overlay, ok = twoKeyOverlay(string(e[length-3]) + string(e[length-2]))
			if !ok {
				return (ErrorInvalidSymbol)
			}

			state.symbolCode = APRSTT_DEFAULT_SYMBOL
			state.symtabOrOverlay = overlay

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign": state.callsign,
					"symbol":   string(state.symbolCode),
					"overlay":  string(state.symtabOrOverlay),
					"checksum": string(e[length-1]),
				}).Debug("Callsign in two key format (two key overlay)")
			}
		} else {
			var callsign, callOK = twoKeyCallsign(e[1 : length-2])
			if !callOK {
				return (ErrorInvalidCall)
			}

			state.callsign = callsign

			state.symbolCode = APRSTT_DEFAULT_SYMBOL
			state.symtabOrOverlay = e[length-2]

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"callsign": state.callsign,
					"symbol":   string(state.symbolCode),
					"overlay":  string(state.symtabOrOverlay),
					"checksum": string(e[length-1]),
				}).Debug("Callsign in two key format")
			}
		}

		return (0)
	}

	logrus.WithField("entry", e).Error("Touch tone callsign not valid")

	return (ErrorInvalidCall)
}

/*------------------------------------------------------------------
 *
 * Name:        parseObjectName
 *
 * Purpose:     Extract object name from touch tone sequence.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AA".
 *
 * Outputs:	state.callsign
 *
 *		state.ssid		- Cleared to remove the default of 12.
 *
 * Returns:	0 for success or one of the Error... codes.
 *
 * Description:	Data format
 *
 *		AAtt...tt	- Symbol name, two key method, up to 9 characters.
 *
 *----------------------------------------------------------------*/

func (g *Gateway) parseObjectName(state *ttParseState, e string) int {
	if g.debug > 0 {
		logrus.WithField("entry", e).Debug("APRStt parse object name (starts with AA)")
	}

	dwutil.Assert(e[0] == 'A')
	dwutil.Assert(e[1] == 'A')

	var length = len(e)

	/*
	 * Object name in two key format.
	 */

	if length >= 2+1 && length <= 30 {
		var _callsign, errors = touchtone.TwoKeyToText(e[2:], false)
		if errors == 0 {
			state.callsign = _callsign
			if len(state.callsign) > 9 {
				state.callsign = state.callsign[:9]
			}

			state.ssid = 0 /* No ssid for object name */

			if g.debug > 0 {
				logrus.WithField("object", state.callsign).Debug("Object name in two key format")
			}

			return (0)
		}
	}

	logrus.WithField("entry", e).Error("Touch tone object name not valid")

	return (ErrorInvalidObjName)
} /* end parseObjectName */

/*------------------------------------------------------------------
 *
 * Name:        parseSymbol
 *
 * Purpose:     Extract symbol from touch tone sequence.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AB".
 *
 * Outputs:	state.symtabOrOverlay
 *
 * 		state.symbolCode
 *
 * Returns:	0 for success or one of the Error... codes.
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

func (g *Gateway) parseSymbol(state *ttParseState, e string) int {
	if g.debug > 0 {
		logrus.WithField("entry", e).Debug("APRStt parse symbol (starts with AB)")
	}

	dwutil.Assert(e[0] == 'A')
	dwutil.Assert(e[1] == 'B')

	var length = len(e)

	if length >= 4 && length <= 10 {
		// Normally two digits, but take the one there is in a field of four.
		var nstr = e[3:min(length, 5)]

		var nn, _ = strconv.Atoi(nstr)

		if nn < 1 {
			nn = 1
		} else if nn > 94 {
			nn = 94
		}

		switch e[2] {
		case '1':
			state.symtabOrOverlay = '/'
			state.symbolCode = byte(32 + nn)

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"symbol": string(state.symbolCode),
					"table":  string(state.symtabOrOverlay),
				}).Debug("Symbol from primary symbol table")
			}

			return (0)

		case '2':
			state.symtabOrOverlay = '\\'
			state.symbolCode = byte(32 + nn)

			if g.debug > 0 {
				logrus.WithFields(logrus.Fields{
					"symbol": string(state.symbolCode),
					"table":  string(state.symtabOrOverlay),
				}).Debug("Symbol from alternate symbol table")
			}

			return (0)

		case '0':
			if length >= 6 {
				var overlay, ok = twoKeyOverlay(e[5:])
				if ok {
					state.symbolCode = byte(32 + nn)
					state.symtabOrOverlay = overlay

					if g.debug > 0 {
						logrus.WithFields(logrus.Fields{
							"symbol":  string(state.symbolCode),
							"overlay": string(state.symtabOrOverlay),
						}).Debug("Symbol from alternate symbol table with overlay")
					}

					return (0)
				}
			}
		}
	}

	logrus.WithField("entry", e).Error("Touch tone symbol not valid")

	return (ErrorInvalidSymbol)
} /* end parseSymbol */

/*------------------------------------------------------------------
 *
 * Name:        parseAprstt3Call
 *
 * Purpose:     Extract QIKcom-2 / APRStt 3 ten digit call or five digit suffix.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "AC".
 *
 * Outputs:	state.callsign
 *
 * Returns:	0 for success or one of the Error... codes.
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

func (g *Gateway) parseAprstt3Call(state *ttParseState, e string) int {
	dwutil.Assert(e[0] == 'A')
	dwutil.Assert(e[1] == 'C')

	if g.debug > 0 {
		logrus.WithField("entry", e).Debug("APRStt parse QIKcom-2 / APRStt 3 ten digit call or five digit suffix (starts with AC)")
	}

	if len(e) == 2+10 {
		var call, errors = touchtone.Call10ToText(e[2:], true)

		if errors == 0 {
			state.callsign = call
		} else {
			return (ErrorInvalidCall) /* Could not convert to text */
		}
	} else if len(e) == 2+5 {
		var suffix, errs = touchtone.Call5SuffixToText(e[2:], true)
		if errs == 0 {
			if g.runningTests {
				/* For unit test, use suffix rather than trying lookup. */
				state.callsign = suffix
			} else {
				var _call, _idx = g.users.threeCharSuffixSearch(suffix)

				/* In normal operation, try to find full callsign for the suffix received. */

				if _idx >= 0 {
					logrus.WithFields(logrus.Fields{
						"suffix":   suffix,
						"callsign": _call,
					}).Info("Touch tone suffix converted to full callsign")

					state.callsign = _call
				} else {
					logrus.WithField("suffix", suffix).Error("Couldn't find full callsign for touch tone suffix")

					return (ErrorSuffixNoCall) /* Don't know this user. */
				}
			}
		} else {
			return (ErrorInvalidCall) /* Could not convert to text */
		}
	} else {
		return (ErrorInvalidCall) /* Invalid length, not 2+ (10 ir 5) */
	}

	return (0)
} /* end parseAprstt3Call */

/*------------------------------------------------------------------
 *
 * Name:        parseLocation
 *
 * Purpose:     Extract location from touch tone sequence.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "B".
 *
 * Outputs:	state.latitude
 *		state.longitude
 *
 *		state.dao	It should previously be "!T  !" to mean unknown or none.
 *				We generally take the first two tones of the field.
 *				For example, "!TB5!" for the standard bearing & range.
 *				The point type is an exception where we use "!Tn !" for
 *				one of ten positions or "!Tnn" for one of a hundred.
 *				If this ever changes, be sure to update corresponding
 *				section in process_comment() in decode_aprs.c
 *
 *		state.ambiguity
 *
 * Returns:	0 for success or one of the Error... codes.
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

// R_M is the average radius of earth in meters.
const R_M = 6371000.0

func (g *Gateway) parseLocation(state *ttParseState, e string) int {
	if g.debug > 0 {
		logrus.WithField("entry", e).Debug("APRStt parse location (starts with B)")
		// TODO: more detail later...
	}

	dwutil.Assert(e[0] == 'B')

	var xstr, ystr, _, bstr, dstr, ipat = g.findTTLocMatch(e)

	if ipat >= 0 {
		// dw_printf ("ipat=%d, x=%s, y=%s, b=%s, d=%s\n", ipat, xstr, ystr, bstr, dstr);
		var ttloc_type = g.config.Locs[ipat].Type
		switch ttloc_type {
		case LocPoint:
			state.latitude = maybe.Just(float64(g.config.Locs[ipat].Point.Lat))
			state.longitude = maybe.Just(float64(g.config.Locs[ipat].Point.Lon))

			/* Is it one of ten or a hundred positions? */
			/* It's not hardwired to always be B0n or B9nn.  */
			/* This is a pretty good approximation. */

			state.setDAOKeys(e)

			if len(e) == 3 { /* probably B0n -->  !Tn ! */
				state.dao[2] = e[2]
				state.dao[3] = ' '
			}

			if len(e) == 4 { /* probably B9nn -->  !Tnn! */
				state.dao[2] = e[2]
				state.dao[3] = e[3]
			}

		case LocVector:
			if len(bstr) != 3 {
				logrus.WithField("bearing", bstr).Error("Touch tone bearing should be 3 digits")
				// return error code?
			}

			if len(dstr) < 1 {
				logrus.WithField("distance", dstr).Error("Touch tone distance should be 1 or more digits")
				// return error code?
			}

			var lat0 = dwutil.D2R(float64(g.config.Locs[ipat].Vector.Lat))
			var lon0 = dwutil.D2R(float64(g.config.Locs[ipat].Vector.Lon))
			var d, _ = strconv.ParseFloat(dstr, 64)
			var dist = d * float64(g.config.Locs[ipat].Vector.Scale)
			var b, _ = strconv.ParseFloat(bstr, 64)
			var bearing = dwutil.D2R(b)

			/* Equations and caluculators found here: */
			/* http://movable-type.co.uk/scripts/latlong.html */
			/* This should probably be a function in latlong.c in case we have another use for it someday. */

			var latitude = dwutil.R2D(math.Asin(math.Sin(lat0)*math.Cos(dist/R_M) + math.Cos(lat0)*math.Sin(dist/R_M)*math.Cos(bearing)))

			state.latitude = maybe.Just(latitude)

			state.longitude = maybe.Just(dwutil.R2D(lon0 + math.Atan2(math.Sin(bearing)*math.Sin(dist/R_M)*math.Cos(lat0),
				math.Cos(dist/R_M)-math.Sin(lat0)*math.Sin(dwutil.D2R(latitude)))))

			state.setDAOKeys(e)

		case LocGrid:
			if len(xstr) == 0 {
				logrus.Error("Touch tone location missing X coordinate")

				xstr = "0"
			}

			if len(ystr) == 0 {
				logrus.Error("Touch tone location missing Y coordinate")

				ystr = "0"
			}

			var lat0 = float64(g.config.Locs[ipat].Grid.Lat0)
			var lat9 = float64(g.config.Locs[ipat].Grid.Lat9)
			var yrange = lat9 - lat0
			var y, _ = strconv.ParseFloat(ystr, 64)
			var user_y_max = math.Round(math.Pow(10., float64(len(ystr))) - 1.) // e.g. 999 for 3 digits
			state.latitude = maybe.Just(lat0 + yrange*y/user_y_max)

			logrus.WithFields(logrus.Fields{
				"min":        lat0,
				"max":        lat9,
				"range":      yrange,
				"user_y":     y,
				"user_y_max": user_y_max,
				"latitude":   state.latitude,
			}).Debug("TTLOC_GRID LAT")

			var lon0 = float64(g.config.Locs[ipat].Grid.Lon0)
			var lon9 = float64(g.config.Locs[ipat].Grid.Lon9)
			var xrange = lon9 - lon0
			var x, _ = strconv.ParseFloat(xstr, 64)
			var user_x_max = math.Round(math.Pow(10., float64(len(xstr))) - 1.)
			state.longitude = maybe.Just(lon0 + xrange*x/user_x_max)

			logrus.WithFields(logrus.Fields{
				"min":        lon0,
				"max":        lon9,
				"range":      xrange,
				"user_x":     x,
				"user_x_max": user_x_max,
				"longitude":  state.longitude,
			}).Debug("TTLOC_GRID LON")

			state.setDAOKeys(e)

		case LocUTM:
			if len(xstr) == 0 {
				logrus.Error("Touch tone location missing X coordinate")
				/* Avoid divide by zero later.  Put in middle of range. */
				xstr = "5"
			}

			if len(ystr) == 0 {
				logrus.Error("Touch tone location missing Y coordinate")
				/* Avoid divide by zero later.  Put in middle of range. */
				ystr = "5"
			}

			var x, _ = strconv.ParseFloat(xstr, 64)
			var easting = x*float64(g.config.Locs[ipat].UTM.Scale) + float64(g.config.Locs[ipat].UTM.XOffset)

			var y, _ = strconv.ParseFloat(ystr, 64)
			var northing = y*float64(g.config.Locs[ipat].UTM.Scale) + float64(g.config.Locs[ipat].UTM.YOffset)

			if unicode.IsLetter(g.config.Locs[ipat].UTM.LatBand) {
				state.locText = fmt.Sprintf("%d%c %.0f %.0f", g.config.Locs[ipat].UTM.Zone, g.config.Locs[ipat].UTM.LatBand, easting, northing)
			} else if g.config.Locs[ipat].UTM.LatBand == '-' {
				state.locText = fmt.Sprintf("%d %.0f %.0f", -g.config.Locs[ipat].UTM.Zone, easting, northing)
			} else {
				state.locText = fmt.Sprintf("%d %.0f %.0f", g.config.Locs[ipat].UTM.Zone, easting, northing)
			}

			var hemi = coordconvutil.HemisphereFromRune(g.config.Locs[ipat].UTM.Hemi)

			var utm = coordconv.UTMCoord{
				Zone:       g.config.Locs[ipat].UTM.Zone,
				Hemisphere: hemi,
				Easting:    easting,
				Northing:   northing,
			}

			var geo, geoErr = coordconv.DefaultUTMConverter.ConvertToGeodetic(utm)
			if geoErr == nil {
				state.latitude = maybe.Just(dwutil.R2D(float64(geo.Lat)))
				state.longitude = maybe.Just(dwutil.R2D(float64(geo.Lng)))

				// dw_printf ("DEBUG: from UTM, latitude = %.6f, longitude = %.6f\n", state.latitude, state.longitude);
			} else {
				logrus.WithError(geoErr).Error("Conversion from UTM failed")
			}

			state.setDAOKeys(e)

		case LocMGRS, LocUSNG:
			if len(xstr) == 0 {
				logrus.Error("MGRS/USNG: Missing X (easting) coordinate")
				/* Should not be possible to get here. Fake it and carry on. */
				xstr = "5"
			}

			if len(ystr) == 0 {
				logrus.Error("MGRS/USNG: Missing Y (northing) coordinate")
				/* Should not be possible to get here. Fake it and carry on. */
				ystr = "5"
			}

			var loc = g.config.Locs[ipat].MGRS.Zone
			loc += xstr
			loc += ystr

			// text_color_set(DW_COLOR_DEBUG);
			// dw_printf ("MGRS/USNG location debug:  %s\n", loc);

			state.locText = loc

			// Apparently also does USNG!
			var geo, convertErr = coordconv.DefaultMGRSConverter.ConvertToGeodetic(loc)
			if convertErr == nil {
				state.latitude = maybe.Just(dwutil.R2D(float64(geo.Lat)))
				state.longitude = maybe.Just(dwutil.R2D(float64(geo.Lng)))

				// dw_printf ("DEBUG: from MGRS/USNG, latitude = %.6f, longitude = %.6f\n", state.latitude, state.longitude);
			} else {
				logrus.WithError(convertErr).Error("Conversion from MGRS/USNG failed")
			}

			state.setDAOKeys(e)

		case LocMHead:
			/* Combine prefix from configuration and digits from user. */
			var stemp = g.config.Locs[ipat].MHead.Prefix
			stemp += xstr

			if len(stemp) != 4 && len(stemp) != 6 && len(stemp) != 10 && len(stemp) != 12 {
				logrus.WithFields(logrus.Fields{
					"prefix": g.config.Locs[ipat].MHead.Prefix,
					"x":      xstr,
				}).Error("Expected total of 4, 6, 10, or 12 digits for the Maidenhead Locator")

				return (ErrorInvalidMHead)
			}

			// text_color_set(DW_COLOR_DEBUG);
			// dw_printf ("Case MHEAD: Convert to text \"%s\".\n", stemp);

			var mh, errs = touchtone.MheadToText(stemp, false)
			if errs == 0 {
				// text_color_set(DW_COLOR_DEBUG);
				// dw_printf ("Case MHEAD: Resulting text \"%s\".\n", mh);
				state.locText = mh

				var lat, lon, err = latlong.FromGridSquare(state.locText)
				if err != nil {
					logrus.WithError(err).Error("Invalid touch tone grid square")

					return (ErrorInvalidMHead)
				}

				state.latitude = maybe.Just(lat)
				state.longitude = maybe.Just(lon)
			}

			state.setDAOKeys(e)

		case LocSatSq:
			if len(xstr) != 4 {
				logrus.WithField("x", xstr).Error("Expected 4 digits for the Satellite Square")

				return (ErrorInvalidSatSq)
			}

			/* Convert 4 digits to usual AA99 form, then to location. */

			var mh, errs = touchtone.SatsqToText(xstr, false)
			if errs == 0 {
				state.locText = mh

				var lat, lon, err = latlong.FromGridSquare(state.locText)
				if err != nil {
					logrus.WithError(err).Error("Invalid touch tone grid square")

					return (ErrorInvalidSatSq)
				}

				state.latitude = maybe.Just(lat)
				state.longitude = maybe.Just(lon)
			}

			state.setDAOKeys(e)

		case LocAmbig:
			if len(xstr) != 1 {
				logrus.WithField("x", xstr).Error("Expected 1 digit for the position ambiguity")

				return (ErrorInvalidLoc)
			}

			var ambiguity, _ = strconv.Atoi(xstr)
			state.ambiguity = maybe.Just(ambiguity)

		default:
			panic(fmt.Sprintf("Unknown ttloc type: %d", ttloc_type))
		}

		return (0)
	}

	/* Does not match any location specification. */

	logrus.WithField("entry", e).Error("Received touch tone location does not match any definitions")

	/* Send reject sound. */

	return (ErrorInvalidLoc)
} /* end parseLocation */

/*------------------------------------------------------------------
 *
 * Name:        findTTLocMatch
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

func (g *Gateway) findTTLocMatch(e string) (string, string, string, string, string, int) {
	// debug dw_printf ("findTTLocMatch: e=%s\n", e);
	var xstr, ystr, zstr, bstr, dstr string

	for ipat := range len(g.config.Locs) {
		var pattern = g.config.Locs[ipat].Pattern
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
					logrus.WithFields(logrus.Fields{"pattern": pattern, "char": string(mc)}).Error("Internal error: findTTLocMatch hit an unexpected pattern character")
					/* Shouldn't be here. */
					match = false
				} /* switch */
			} /* for k */

			if match {
				return xstr, ystr, zstr, bstr, dstr, ipat
			}
		} /* if strlen */
	}

	return xstr, ystr, zstr, bstr, dstr, -1
} /* end findTTLocMatch */

/*------------------------------------------------------------------
 *
 * Name:        parseComment
 *
 * Purpose:     Extract comment / status or other special information from touch tone message.
 *
 * Inputs:      state		- Parse state being accumulated.
 *		e		- An "entry" extracted from a complete
 *				  APRStt message.
 *				  In this case, it should start with "C".
 *
 * Outputs:	state.comment
 *		state.micE
 *
 * Returns:	0 for success or one of the Error... codes.
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

func (g *Gateway) parseComment(state *ttParseState, e string) int {
	dwutil.Assert(e[0] == 'C')

	var length = len(e)

	if strings.HasPrefix(e, "CA") {
		state.comment, _ = touchtone.ASCII2DToText(e[2:], false)

		return (0)
	}

	if length == 2 && unicode.IsDigit(rune(e[1])) {
		state.micE = e[1]

		return (0)
	}

	if length == 7 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) && unicode.IsDigit(rune(e[4])) && unicode.IsDigit(rune(e[5])) &&
		unicode.IsDigit(rune(e[6])) {
		state.freq = e[1:4] + "." + e[4:7] + "MHz"

		return (0)
	}

	if length == 4 && unicode.IsDigit(rune(e[1])) && unicode.IsDigit(rune(e[2])) && unicode.IsDigit(rune(e[3])) {
		state.ctcss = e[1:]

		return (0)
	}

	state.comment, _ = touchtone.MultipressToText(e[1:], false)

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
 *		alevel		- The channel's received audio level.
 *		recFrame	- Where the packet goes, as though received;
 *				  nil to drop it.
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

func raw_tt_data_to_app(channel int, msg string, alevel ax25.ALevel, recFrame frameReceiver) {
	// Set source and dest to something valid to keep rest of processing happy.
	// For lack of a better idea, make source "DTMF" to indicate where it came from.
	// Application version might be useful in case we end up using different
	// message formats in later versions.
	var src = "DTMF"
	var dest = fmt.Sprintf("%s%d%d", version.Tocall, version.Major, version.Minor)
	var raw_tt_msg = fmt.Sprintf("%s>%s:t%s", src, dest, msg)

	var pp = ax25.FromText(raw_tt_msg, true)

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
		alevel.Mark = -2
		alevel.Space = -2

		if recFrame != nil {
			recFrame(dwcore.ChannelSource(dwcore.SourceDTMF, channel), pp, dwcore.RxInfo{ALevel: alevel, Retries: phy.BitFixNone, FEC: phy.FECNone}, "tt")
		}
	} else {
		logrus.WithField("packet", raw_tt_msg).Error("Could not convert touch tone message into APRS packet")
	}
}

/* end aprs_tt.c */
