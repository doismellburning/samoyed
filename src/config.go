//nolint:gochecknoglobals
package direwolf

//#define DEBUG 1

/*------------------------------------------------------------------
 *
 * Purpose:   	Read configuration information from a file.
 *
 * Description:	This started out as a simple little application with a few
 *		command line options.  Due to creeping featurism, it's now
 *		time to add a configuration file to specify options.
 *
 *---------------------------------------------------------------*/

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
	"github.com/tzneal/coordconv"
)

const DEFAULT_GPSD_PORT = 2947 // Taken from gps.h

/*
 * All the leftovers.
 * This wasn't thought out.  It just happened.
 */

type beacon_type_e int

const (
	BEACON_IGNORE beacon_type_e = iota
	BEACON_POSITION
	BEACON_OBJECT
	BEACON_TRACKER
	BEACON_CUSTOM
	BEACON_IGATE
)

type sendto_type_e int

const (
	SENDTO_XMIT sendto_type_e = iota
	SENDTO_IGATE
	SENDTO_RECV
)

const MAX_BEACONS = 30
const MAX_KISS_TCP_PORTS = (MAX_RADIO_CHANS + 1)

const WPL_FORMAT_NMEA_GENERIC = 0x01 /* N	$GPWPL */
const WPL_FORMAT_GARMIN = 0x02       /* G	$PGRMW */
const WPL_FORMAT_MAGELLAN = 0x04     /* M	$PMGNWPL */
const WPL_FORMAT_KENWOOD = 0x08      /* K	$PKWDWPL */
const WPL_FORMAT_AIS = 0x10          /* A	!AIVDM */

type beacon_s struct {
	btype beacon_type_e /* Position or object. */

	lineno int /* Line number from config file for later error messages. */

	sendto_type sendto_type_e

	/* SENDTO_XMIT	- Usually beacons go to a radio transmitter. */
	/*		  chan, below is the channel number. */
	/* SENDTO_IGATE	- Send to IGate, probably to announce my position */
	/* 		  rather than relying on someone else to hear */
	/* 		  me on the radio and report me. */
	/* SENDTO_RECV	- Pretend this was heard on the specified */
	/* 		  radio channel.  Mostly for testing. It is a */
	/* 		  convenient way to send packets to attached apps. */

	sendto_chan int /* Transmit or simulated receive channel for above.  Should be 0 for IGate. */

	delay int /* Seconds to delay before first transmission. */

	slot maybe.Maybe[int] /* Seconds after hour for slotted time beacons. */
	/* If specified, it overrides any 'delay' value. */

	every int /* Time between transmissions, seconds. */
	/* Remains fixed for PBEACON and OBEACON. */
	/* Dynamically adjusted for TBEACON. */

	next time.Time /* Unix time to transmit next one. */

	source string /* Empty or explicit AX.25 source address to use instead of the mycall value for the channel. */

	dest string /* Empty or explicit AX.25 destination to use instead of the software version such as APDW11. */

	compress bool /* Use more compact form? */

	objname string /* Object name.  Any printable characters. */

	via string /* Path, e.g. "WIDE1-1,WIDE2-1" or NULL. */

	custom_info string /* Info part for handcrafted custom beacon. Ignore the rest below if this is set. */

	custom_infocmd string /* Command to generate info part. Again, other options below are then ignored. */

	messaging bool /* Set messaging attribute for position report. */
	/* i.e. Data Type Indicator of '=' rather than '!' */

	lat       maybe.Maybe[float64] /* Latitude and longitude. */
	lon       maybe.Maybe[float64]
	ambiguity int                  /* Number of lower digits to trim from location. 0 (default), 1, 2, 3, 4. */
	alt_m     maybe.Maybe[float64] /* Altitude in meters. */

	symtab byte /* Symbol table: / or \ or overlay character. */
	symbol byte /* Symbol code. */

	power  float64 /* For PHG. */
	height float64 /* HAAT in feet */
	gain   float64 /* Original protocol spec was unclear. */
	/* Addendum 1.1 clarifies it is dBi not dBd. */

	dir string /* 1 or 2 of N,E,W,S, or empty for omni. */

	freq   maybe.Maybe[float64] /* MHz. */
	tone   maybe.Maybe[float64] /* Hz. */
	offset maybe.Maybe[float64] /* MHz. */

	comment    string /* Comment or empty. */
	commentcmd string /* Command to append more to Comment or empty. */
}

// agwpe_login_s is one user name and password pair accepted by the "AGW TCPIP
// Socket Interface".  AGWPE has a list of these rather than a single one, so a
// station can hand out separate credentials and withdraw one of them later.
type agwpe_login_s struct {
	user     string
	password string
}

type misc_config_s struct {
	agwpe_port int /* TCP Port number for the "AGW TCPIP Socket Interface" */

	agwpe_logins []agwpe_login_s /* User names and passwords, any one of which a client */
	/* may supply in an AGW "Application Login" frame before any of its other */
	/* commands are honoured.  Empty (the default) means no login is required. */

	metrics_port int /* TCP Port number for the Prometheus "/metrics" HTTP endpoint. */
	/* 0 (default) disables it. */

	// Previously we allowed only a single TCP port for KISS.
	// An increasing number of people want to run multiple radios.
	// Unfortunately, most applications don't know how to deal with multi-radio TNCs.
	// They ignore the channel on receive and always transmit to channel 0.
	// Running multiple instances of direwolf is a work-around but this leads to
	// more complex configuration and we lose the cross-channel digipeating capability.
	// In release 1.7 we add a new feature to assign a single radio channel to a TCP port.
	// e.g.
	//	KISSPORT 8001		# default, all channels.  Radio channel = KISS channel.
	//
	//	KISSPORT 7000 0		# Only radio channel 0 for receive.
	//				# Transmit to radio channel 0, ignoring KISS channel.
	//
	//	KISSPORT 7001 1		# Only radio channel 1 for receive.  KISS channel set to 0.
	//				# Transmit to radio channel 1, ignoring KISS channel.

	kiss_port [MAX_KISS_TCP_PORTS]int /* TCP Port number for the "TCP KISS" protocol. */
	kiss_chan [MAX_KISS_TCP_PORTS]int /* Radio Channel number for this port or -1 for all.  */

	kiss_copy      bool /* Data from network KISS client is copied to all others. */
	enable_kiss_pt bool /* Enable pseudo terminal for KISS. */
	/* Want this to be off by default because it hangs */
	/* after a while if nothing is reading from other end. */

	kiss_serial_port string
	/* Serial port name for our end of the */
	/* virtual null modem for native Windows apps. */
	/* Version 1.5 add same capability for Linux. */

	kiss_serial_speed int /* Speed, in bps, for the KISS serial port. */
	/* If 0, just leave what was already there. */

	kiss_serial_poll int /* When using Bluetooth KISS, the /dev/rfcomm0 device */
	/* will appear and disappear as the remote application */
	/* opens and closes the virtual COM port. */
	/* When this is n>0, we will check every n seconds to */
	/* see if the device has appeared and we will open it. */

	gpsnmea_port string /* Serial port name for reading NMEA sentences from GPS. e.g. COM22, /dev/ttyACM0 */

	gpsnmea_speed int /* Speed for above, baud, default 4800. */

	gpsd_host string /* Host for gpsd server. e.g. localhost, 192.168.1.2 */

	gpsd_port int /* Port number for gpsd server. */
	/* Default is  2947. */

	waypoint_serial_port string /* Serial port name for sending NMEA waypoint sentences */
	/* to a GPS map display or other mapping application. */
	/* e.g. COM22, /dev/ttyACM0 */
	/* Currently no option for setting non-standard speed. */
	/* This was done in 2014 and no one has complained yet. */

	waypoint_udp_hostname string /* Destination host when using UDP. */

	waypoint_udp_portnum int /* UDP port. */

	waypoint_formats int /* Which sentence formats should be generated? */

	log_daily_names bool /* True to generate new log file each day. */

	log_path string /* Either directory or full file name depending on above. */

	dns_sd_enabled bool   /* DNS Service Discovery announcement enabled. */
	dns_sd_name    string /* Name announced on dns-sd; defaults to "Dire Wolf on <hostname>" */

	sb_configured bool /* TRUE if SmartBeaconing is configured. */
	sb_fast_speed int  /* MPH */
	sb_fast_rate  int  /* seconds */
	sb_slow_speed int  /* MPH */
	sb_slow_rate  int  /* seconds */
	sb_turn_time  int  /* seconds */
	sb_turn_angle int  /* degrees */
	sb_turn_slope int  /* degrees * MPH */

	// AX.25 connected mode.

	frack int /* Number of seconds to wait for ack to transmission. */

	retry int /* Number of times to retry before giving up. */

	paclen int /* Max number of bytes in information part of frame. */

	maxframe_basic int /* Max frames to send before ACK.  mod 8 "Window" size. */

	maxframe_extended int /* Max frames to send before ACK.  mod 128 "Window" size. */

	maxv22 int /* Maximum number of unanswered SABME frames sent before */
	/* switching to SABM.  This is to handle the case of an old */
	/* TNC which simply ignores SABME rather than replying with FRMR. */

	v20_addrs []string /* Stations known to understand only AX.25 v2.0 so we don't waste time trying v2.2 first. */

	v20_count int /* Number of station addresses in array above. */

	noxid_addrs []string /* Stations known not to understand XID command so don't */
	/* waste time sending it and eventually giving up. */
	/* AX.25 for Linux is the one known case, so far, where */
	/* SABME is implemented but XID is not. */

	noxid_count int /* Number of station addresses in array above. */

	// Beacons.

	num_beacons int /* Number of beacons defined. */

	beacon [MAX_BEACONS]beacon_s
}

const MIN_IP_PORT_NUMBER = 1024
const MAX_IP_PORT_NUMBER = 49151

const DEFAULT_AGWPE_PORT = 8000 /* Like everyone else. */
const DEFAULT_KISS_PORT = 8001  /* Above plus 1. */

const DEFAULT_NULLMODEM = "COM3" /* should be equiv. to /dev/ttyS2 on Cygwin */

/*
 * Conversions from various units to meters.
 * There is some disagreement about the exact values for some of these.
 * Close enough for our purposes.
 * Parsec, light year, and angstrom are probably not useful.
 */

type units_s struct {
	name   string
	meters float64
}

var units = []*units_s{
	{"barleycorn", 0.008466667},
	{"inch", 0.0254},
	{"in", 0.0254},
	{"hand", 0.1016},
	{"shaku", 0.3030},
	{"foot", 0.304801},
	{"ft", 0.304801},
	{"cubit", 0.4572},
	{"megalithicyard", 0.8296},
	{"my", 0.8296},
	{"yard", 0.914402},
	{"yd", 0.914402},
	{"m", 1.},
	{"meter", 1.},
	{"metre", 1.},
	{"ell", 1.143},
	{"ken", 1.818},
	{"hiro", 1.818},
	{"fathom", 1.8288},
	{"fath", 1.8288},
	{"toise", 1.949},
	{"jo", 3.030},
	{"twain", 3.6576074},
	{"rod", 5.0292},
	{"rd", 5.0292},
	{"perch", 5.0292},
	{"pole", 5.0292},
	{"rope", 6.096},
	{"dekameter", 10.},
	{"dekametre", 10.},
	{"dam", 10.},
	{"chain", 20.1168},
	{"ch", 20.1168},
	{"actus", 35.47872},
	{"arpent", 58.471},
	{"hectometer", 100.},
	{"hectometre", 100.},
	{"hm", 100.},
	{"cho", 109.1},
	{"furlong", 201.168},
	{"fur", 201.168},
	{"kilometer", 1000.},
	{"kilometre", 1000.},
	{"km", 1000.},
	{"mile", 1609.344},
	{"mi", 1609.344},
	{"ri", 3927.},
	{"league", 4828.032},
	{"lea", 4828.032}}

/* Do we have a string of all digits? */

func alldigits(p string) bool {
	return !strings.ContainsFunc(p, func(r rune) bool {
		return !unicode.IsDigit(r)
	})
}

/* Do we have a string of all letters or + or -  ? */

func alllettersorpm(p string) bool {
	return !strings.ContainsFunc(p, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '+' && r != '-'
	})
}

/*------------------------------------------------------------------
 *
 * Name:        parse_ll
 *
 * Purpose:     Parse latitude or longitude from configuration file.
 *
 * Inputs:      str	- String like [-]deg[^min][hemisphere]
 *
 *		which	- LAT or LON for error checking and message.
 *
 *		line	- Line number for use in error message.
 *
 * Returns:     Coordinate in signed degrees, or Nothing if the string does not
 *		give one: a number that can't be read, isn't finite, or is
 *		outside the range the hemisphere allows.
 *
 *----------------------------------------------------------------*/

/* Acceptable symbols to separate degrees & minutes. */
/* Degree symbol is not in ASCII so documentation says to use "^" instead. */
/* Some wise guy will try to use degree symbol. */
/* UTF-8 is more difficult because it is a two byte sequence, c2 b0. */

type parse_ll_which_e int

const LAT parse_ll_which_e = 0
const LON parse_ll_which_e = 1

func parse_ll_maybe(str string, which parse_ll_which_e, line int) (maybe.Maybe[float64], error) {
	// One bad coordinate can be wrong in more than one way - a hemisphere that
	// does not belong to this axis and minutes that are not minutes - so gather
	// the complaints rather than stopping at the first.
	var problems []error

	var stemp = str

	/*
	 * Nothing to parse, and nothing to index into either.
	 */
	if stemp == "" {
		return maybe.Nothing[float64](), fmt.Errorf("line %d: Missing %s", line, coordinateName(which))
	}

	/*
	 * Remove any negative sign.
	 */
	var sign = 1

	if stemp[0] == '-' {
		stemp = stemp[1:]
		sign = -1
	}

	/*
	 * Process any hemisphere on the end.
	 */
	if len(stemp) >= 2 {
		var lastChar = rune(stemp[len(stemp)-1])

		if unicode.IsLetter(lastChar) {
			var hemi = lastChar
			stemp = stemp[:len(stemp)-1]

			hemi = unicode.ToUpper(hemi)

			if hemi == 'W' || hemi == 'S' {
				sign = -sign
			}

			if which == LAT {
				if hemi != 'N' && hemi != 'S' {
					problems = append(problems, fmt.Errorf("line %d: Latitude hemisphere in \"%s\" is not N or S", line, str))
				}
			} else {
				if hemi != 'E' && hemi != 'W' {
					problems = append(problems, fmt.Errorf("line %d: Longitude hemisphere in \"%s\" is not E or W", line, str))
				}
			}
		}
	}

	var degreesStr = stemp
	var minutesStr string

	var minutesFound = false
	if strings.Contains(degreesStr, "^") {
		degreesStr, minutesStr, minutesFound = strings.Cut(stemp, "^")
	} else if strings.Contains(degreesStr, "°") {
		degreesStr, minutesStr, minutesFound = strings.Cut(stemp, "°")
	}

	var degrees, degreesErr = strconv.ParseFloat(degreesStr, 64)
	if degreesErr != nil {
		problems = append(problems, fmt.Errorf("line %d: Number of degrees in \"%s\" is not a number: %w", line, degreesStr, degreesErr))

		return maybe.Nothing[float64](), errors.Join(problems...)
	}

	if minutesFound {
		var minutes, minutesErr = strconv.ParseFloat(minutesStr, 64)
		if minutesErr != nil {
			problems = append(problems, fmt.Errorf("line %d: Number of minutes in \"%s\" is not a number: %w", line, minutesStr, minutesErr))

			return maybe.Nothing[float64](), errors.Join(problems...)
		}

		if minutes >= 60.0 {
			problems = append(problems, fmt.Errorf("line %d: Number of minutes in \"%s\" is >= 60", line, minutesStr))
		}

		degrees += minutes / 60
	}

	degrees *= float64(sign)

	if math.IsNaN(degrees) || math.IsInf(degrees, 0) {
		problems = append(problems, fmt.Errorf("line %d: %s \"%s\" is not a finite number", line, coordinateName(which), str))

		return maybe.Nothing[float64](), errors.Join(problems...)
	}

	var limit = float64(IfThenElse(which == LAT, 90, 180))
	if degrees < -limit || degrees > limit {
		problems = append(problems, fmt.Errorf("line %d: %s \"%s\" is out of range of +- %.0f degrees", line, coordinateName(which), str, limit))

		return maybe.Nothing[float64](), errors.Join(problems...)
	}

	return maybe.Just(degrees), errors.Join(problems...)
}

// coordinateName names a coordinate for an error message.
func coordinateName(which parse_ll_which_e) string {
	return IfThenElse(which == LAT, "latitude", "longitude")
}

// parse_ll is parse_ll_maybe for the callers that have nowhere to put the
// absence yet and so treat a coordinate they can't use as zero; see issue
// #619.
func parse_ll(str string, which parse_ll_which_e, line int) (float64, error) {
	var ll, err = parse_ll_maybe(str, which, line)

	return maybe.FromMaybe(0, ll), err
}

/*------------------------------------------------------------------
 *
 * Name:        parse_utm_zone
 *
 * Purpose:     Parse UTM zone from configuration file.
 *
 * Inputs:      szone	- String like [-]number[letter]
 *
 * Returns:	latband	- Latitude band if specified, otherwise space or -.
 *
 *		hemi	- Hemisphere, always one of 'N' or 'S'.
 *
 * Returns:	Zone as number.
 *
 * Errors:	Prints message and return 0.
 *
 * Description:
 *		It seems there are multiple conventions for specifying the UTM hemisphere.
 *
 *		  - MGRS latitude band.  North if missing or >= 'N'.
 *		  - Negative zone for south.
 *		  - Separate North or South.
 *
 *		I'm using the first alternative.
 *		GEOTRANS uses the third.
 *		We will also recognize the second one but I'm not sure if I want to document it.
 *
 *----------------------------------------------------------------*/

func parse_utm_zone(szone string) (rune, rune, int, error) {
	// A zone can be wrong in both its band letter and its number, so gather the
	// complaints rather than stopping at the first.
	var problems []error

	var latband = ' '
	var hemi = 'N' /* default */

	var lastRune = rune(szone[len(szone)-1])
	if unicode.IsLetter(lastRune) {
		szone = szone[:len(szone)-1]
	} else {
		lastRune = 0
	}

	var lzone, _ = strconv.Atoi(szone)

	if lastRune == 0 {
		/* Number is not followed by letter something else.  */
		/* Allow negative number to mean south. */
		if lzone < 0 {
			latband = '-'
			hemi = 'S'
			lzone = (-lzone)
		}
	} else {
		lastRune = unicode.ToUpper(lastRune)

		latband = lastRune
		if strings.ContainsRune("CDEFGHJKLMNPQRSTUVWX", lastRune) {
			if lastRune < 'N' {
				hemi = 'S'
			}
		} else {
			problems = append(problems, fmt.Errorf("latitudinal band in \"%s\" must be one of CDEFGHJKLMNPQRSTUVWX", szone))

			hemi = '?'
		}
	}

	if lzone < 1 || lzone > 60 {
		problems = append(problems, fmt.Errorf("UTM Zone number %d must be in range of 1 to 60", lzone))
	}

	return latband, hemi, lzone, errors.Join(problems...)
} /* end parse_utm_zone */

/*
#if 0
main ()
{

	parse_ll ("12.5", LAT);
	parse_ll ("12.5N", LAT);
	parse_ll ("12.5E", LAT);	// error

	parse_ll ("-12.5", LAT);
	parse_ll ("12.5S", LAT);
	parse_ll ("12.5W", LAT);	// error

	parse_ll ("12.5", LON);
	parse_ll ("12.5E", LON);
	parse_ll ("12.5N", LON);	// error

	parse_ll ("-12.5", LON);
	parse_ll ("12.5W", LON);
	parse_ll ("12.5S", LON);	// error

	parse_ll ("12^30", LAT);
	parse_ll ("12\xb030", LAT);			// ISO Latin-1 degree symbol

	parse_ll ("91", LAT);		// out of range
	parse_ll ("91", LON);
	parse_ll ("181", LON);		// out of range

	parse_ll ("12&5", LAT);		// bad character
}
#endif
*/

/*------------------------------------------------------------------
 *
 * Name:        parse_interval
 *
 * Purpose:     Parse time interval from configuration file.
 *
 * Inputs:      str	- String like 10 or 9:30
 *
 *		line	- Line number for use in error message.
 *
 * Returns:     Number of seconds, and whether it could be read at all.  A
 *		value that can't be read is not a value: the zero it would
 *		otherwise become is a beacon interval that divides by zero in
 *		IS_GOOD and a next-transmission time that never advances.
 *
 * Description:	This is used by the BEACON configuration items
 *		for initial delay or time between beacons.
 *
 *		The format is either minutes or minutes:seconds.
 *
 *----------------------------------------------------------------*/

func parse_interval(keyword string, str string, line int) (int, error) {
	var minutesStr, secondsStr, found = strings.Cut(str, ":")

	var minutes, minutesErr = strconv.Atoi(minutesStr)
	var interval = 60 * minutes

	var secondsErr error

	if found {
		var seconds int
		seconds, secondsErr = strconv.Atoi(secondsStr)
		interval += seconds
	}

	if minutesErr != nil || secondsErr != nil {
		return 0, fmt.Errorf("line %d: Time interval for %s, \"%s\", must be of the form minutes or minutes:seconds.  Ignoring it", line, keyword, str)
	}

	return interval, nil
} /* end parse_interval */

/*------------------------------------------------------------------
 *
 * Name:        check_via_path
 *
 * Purpose:     Check for valid path in beacons, IGate, and APRStt configuration.
 *
 * Inputs:      via_path	- Zero or more comma separated stations.
 *
 * Returns:	Maximum number of digipeater hops or -1 for error.
 *
 * Description:	Beacons and IGate can use via paths such as:
 *
 *			WIDE1-1,MA3-3
 *			N2GH,RARA-7
 *
 * 		Each part could be a specific station, an alias, or a path
 *		from the "New n-N Paradigm."
 *		In the first example above, the maximum number of digipeater
 *		hops would be 4.  In the second example, 2.
 *
 *----------------------------------------------------------------*/

// Put something like this in the config file as a quick test.
// Not worth adding to "make check" regression tests.
//
// 	IBEACON via=
//	IBEACON via=W2UB
//	IBEACON via=W2UB-7
//	IBEACON via=WIDE1-1,WIDE2-2,WIDE3-3
//	IBEACON via=Lower
//	IBEACON via=T00LONG
//	IBEACON via=W2UB-16
//	IBEACON via=D1,D2,D3,D4,D5,D6,D7,D8
//	IBEACON via=D1,D2,D3,D4,D5,D6,D7,D8,D9
//
// Define below and visually check results.

//#define DEBUG8 1

func check_via_path(via_path string) (int, error) {
	logrus.WithField("via_path", via_path).Debug("check_via_path")
	var parts = strings.Split(via_path, ",")
	var num_digi = 0
	var max_digi_hops = 0

	for _, part := range parts {
		num_digi++

		var addr, ssid, _, ok = ax25_parse_addr(AX25_REPEATER_1-1+num_digi, part, addrStrictNoStar)

		if !ok {
			logrus.Debug("check_via_path bad address")

			return -1, nil
		}

		/* Based on assumption that a callsign can't end with a digit. */
		/* For something of the form xxx9-9, we take the ssid as max hop count. */

		if ssid > 0 && len(addr) >= 2 && unicode.IsDigit(rune(addr[len(addr)-1])) {
			max_digi_hops += ssid
		} else {
			max_digi_hops++
		}
	}

	if num_digi > AX25_MAX_REPEATERS {
		return -1, errors.New("maximum of 8 digipeaters has been exceeded")
	}

	logrus.WithFields(logrus.Fields{
		"num_digi":      num_digi,
		"max_digi_hops": max_digi_hops,
	}).Debug("check_via_path")

	return max_digi_hops, nil
} /* end check_via_path */

/*-------------------------------------------------------------------
 *
 * Name:        split
 *
 * Purpose:     Separate a line into command and parameters.
 *
 * Inputs:	string		- Complete command line to start process.
 *				  nil for subsequent calls.
 *
 *		rest_of_line	- Caller wants remainder of line, not just
 *				  the next parameter.
 *
 * Returns:	Pointer to next part with any quoting removed.
 *
 * Description:	the configuration file started out very simple and strtok
 *		was used to split up the lines.  As more complicated options
 *		were added, there were several different situations where
 *		parameter values might contain spaces.  These were handled
 *		inconsistently in different places.  In version 1.3, we now
 *		treat them consistently in one place.
 *
 *
 *--------------------------------------------------------------------*/

const MAXCMDLEN = 1200

var splitCmd string

func split(str string, rest_of_line bool) string {
	/*
	 * If string is provided, make a copy.
	 * Drop any CRLF at the end.
	 * Change any tabs to spaces so we don't have to check for it later.
	 */
	if str != "" {
		splitCmd = ""

		for _, c := range str {
			switch c {
			case '\t':
				splitCmd += " "
			case '\n', '\r':
				// Nothing
			default:
				splitCmd += string(c)
			}
		}
	}

	/*
	 * Get next part, separated by whitespace, keeping spaces within quotes.
	 * Quotation marks inside need to be doubled.
	 */

	splitCmd = strings.TrimSpace(splitCmd)

	var token strings.Builder
	var in_quotes = false

	var parsedLen int

outerLoop:
	for parsedLen = 0; parsedLen < len(splitCmd); parsedLen++ {
		var c = splitCmd[parsedLen]
		switch c {
		case '"':
			if in_quotes {
				if parsedLen+1 < len(splitCmd) && splitCmd[parsedLen+1] == '"' {
					token.WriteString(string(c))
					parsedLen++
				} else {
					in_quotes = false
				}
			} else {
				in_quotes = true
			}
		case ' ':
			if in_quotes || rest_of_line {
				token.WriteString(string(c))
			} else {
				break outerLoop
			}
		default:
			token.WriteString(string(c))
		}
	}

	splitCmd = splitCmd[parsedLen:]

	// dw_printf("split out: '%s'\n", token);

	return token.String()
} /* end split */

/*-------------------------------------------------------------------
 *
 * Name:        config_init
 *
 * Purpose:     Read configuration file when application starts up.
 *
 * Inputs:	fname		- Name of configuration file.  Either default of direwolf.conf
 *					or specified by user with -c command line option.
 *
 * Outputs:	p_audio_config		- Radio channel parameters stored here.
 *
 *		p_digi_config	- APRS Digipeater configuration stored here.
 *
 *		p_cdigi_config	- Connected Digipeater configuration stored here.
 *
 *		p_tt_config	- APRStt stuff.
 *
 *		p_igate_config	- Internet Gateway.
 *
 *		p_misc_config	- Everything else.  This wasn't thought out well.
 *
 * Description:	Apply default values for various parameters then read the
 *		the configuration file which can override those values.
 *
 * Errors:	For invalid input, display line number and message on stdout (not stderr).
 *		In many cases this will result in keeping the default rather than aborting.
 *
 * Bugs:	Very simple-minded parsing.
 *		Not much error checking.  (e.g. atoi() will return 0 for invalid string.)
 *		Not very forgiving about sloppy input.
 *
 *--------------------------------------------------------------------*/

// rtfm points at the documentation.  It is a trailer on someone else's
// complaint rather than a complaint of its own, so it is not counted.
func rtfm() {
	text_color_set(DW_COLOR_ERROR)
	dw_printf("See online documentation:\n")
	dw_printf("    stable release:    https://github.com/wb2osz/direwolf/tree/master/doc\n")
	dw_printf("    development version:    https://github.com/wb2osz/direwolf/tree/dev/doc\n")
	dw_printf("    additional topics:    https://github.com/wb2osz/direwolf-doc\n")
	dw_printf("    general APRS info:    https://how.aprs.works\n")
}

// parseState holds the mutable parsing context threaded through config_init.
type parseState struct {
	channel int
	adevice int
	line    int
	text    string // current raw scanner line
	keyword string // original (not uppercased) keyword token

	audio *audio_s
	digi  *digi_config_s
	cdigi *cdigi_config_s
	tt    *tt_config_s
	igate *igate_config_s
	misc  *misc_config_s

	// Tallies of what the file turned out to be like, for the caller to act on.
	// An error means the configuration is wrong - a directive that could not be
	// obeyed, or was obeyed only by falling back to a default.  A warning means
	// it parsed but looks suspect, which is advice rather than grounds for
	// refusing to start.
	nerrors   int
	nwarnings int
}

// Complaints about the configuration file are written the way Go wants an error
// string - starting lower case and with no full stop, so that one still reads
// correctly wrapped inside another.  Nobody configuring a TNC wants to read
// that, though, so printProblem renders one as a sentence on its way out.

// errorf reports a problem with the current line and counts it.
//
// A handler that has nothing more to do with the line returns its complaint
// instead, and config_init reports it; errorf is for the sites that carry on -
// "out of range, using the default" and the like - and so have more of the line
// to get through before they can return.
func (ps *parseState) errorf(format string, a ...any) {
	ps.nerrors++

	ps.printProblem(fmt.Sprintf(format, a...))
}

// warnf reports something that parses but looks suspect.  Warnings are counted
// separately from errors: they are advice, and do not on their own mean the
// configuration should be rejected.
func (ps *parseState) warnf(format string, a ...any) {
	ps.nwarnings++

	ps.printProblem(fmt.Sprintf(format, a...))
}

// report prints a problem a handler returned, and counts it.  errors.Join
// gathers several complaints about one line into a single error, so unwrap
// those and take them one at a time - otherwise a line with three things wrong
// with it would be counted once.
func (ps *parseState) report(err error) {
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		for _, e := range joined.Unwrap() {
			ps.report(e)
		}

		return
	}

	ps.nerrors++

	ps.printProblem(err.Error())
}

// printProblem writes one complaint out for whoever wrote the configuration
// file: a capital letter to start and a full stop to finish, unless the
// complaint already ends in punctuation of its own.
func (ps *parseState) printProblem(msg string) {
	text_color_set(DW_COLOR_ERROR)
	dw_printf("%s\n", asSentence(msg))
}

// asSentence renders an error string as a sentence.
func asSentence(msg string) string {
	if msg == "" {
		return msg
	}

	var first, size = utf8.DecodeRuneInString(msg)
	if unicode.IsLower(first) {
		msg = string(unicode.ToUpper(first)) + msg[size:]
	}

	switch msg[len(msg)-1] {
	case '.', '?', '!', ':':
		return msg
	}

	return msg + "."
}

// reportedOK reports err, if there is one, and says whether the value beside it
// can be used.  It keeps the "read a number, or leave the option alone" shape
// of the beacon options readable at each of the dozen places it appears.
func (ps *parseState) reportedOK(err error) bool {
	if err != nil {
		ps.report(err)

		return false
	}

	return true
}

// parseLL reads a coordinate from the current line, reporting anything wrong
// with it.  A coordinate that cannot be read at all comes back as zero, as it
// did before there was anywhere to put its absence (see issue #619).
func (ps *parseState) parseLL(str string, which parse_ll_which_e) float64 {
	var ll, err = parse_ll(str, which, ps.line)
	if err != nil {
		ps.report(err)
	}

	return ll
}

// parseLLMaybe is parseLL for the callers that can represent a coordinate that
// is not there.
func (ps *parseState) parseLLMaybe(str string, which parse_ll_which_e) maybe.Maybe[float64] {
	var ll, err = parse_ll_maybe(str, which, ps.line)
	if err != nil {
		ps.report(err)
	}

	return ll
}

// parseUTMZone reads a UTM zone from the current line, reporting anything wrong
// with it.
func (ps *parseState) parseUTMZone(szone string) (rune, rune, int) {
	var latband, hemi, lzone, err = parse_utm_zone(szone)
	if err != nil {
		ps.report(err)
	}

	return latband, hemi, lzone
}

// checkViaPath validates a digipeater path from the current line, reporting
// anything wrong with it.  A path that is no good is a negative hop count, as
// it was before.
func (ps *parseState) checkViaPath(via_path string) int {
	var hops, err = check_via_path(via_path)
	if err != nil {
		ps.report(err)
	}

	return hops
}

// configHandler is a keyword handler.  A nil return means the directive was
// accepted; anything else is a problem with the line, which config_init reports
// and counts.  A handler with several complaints about one line joins them with
// errors.Join.
type configHandler func(ps *parseState) error

var configHandlers = map[string]configHandler{
	"ARATE":          handleARATE,
	"ACHANNELS":      handleACHANNELS,
	"CHANNEL":        handleCHANNEL,
	"ICHANNEL":       handleICHANNEL,
	"NCHANNEL":       handleNCHANNEL,
	"MYCALL":         handleMYCALL,
	"MODEM":          handleMODEM,
	"DTMF":           handleDTMF,
	"FIX_BITS":       handleFIX_BITS,
	"PTT":            handlePTTDCDCON,
	"DCD":            handlePTTDCDCON,
	"CON":            handlePTTDCDCON,
	"TXINH":          handleTXINH,
	"DWAIT":          handleDWAIT,
	"SLOTTIME":       handleSLOTTIME,
	"PERSIST":        handlePERSIST,
	"TXDELAY":        handleTXDELAY,
	"TXTAIL":         handleTXTAIL,
	"FULLDUP":        handleFULLDUP,
	"SPEECH":         handleSPEECH,
	"FX25TX":         handleFX25TX,
	"FX25AUTO":       handleFX25AUTO,
	"IL2PTX":         handleIL2PTX,
	"IL2PVERSION":    handleIL2PVERSION,
	"DIGIPEAT":       handleDIGIPEAT,
	"DIGIPEATER":     handleDIGIPEAT,
	"DEDUPE":         handleDEDUPE,
	"REGEN":          handleREGEN,
	"CDIGIPEAT":      handleCDIGIPEAT,
	"CDIGIPEATER":    handleCDIGIPEAT,
	"FILTER":         handleFILTER,
	"CFILTER":        handleCFILTER,
	"TTCORRAL":       handleTTCORRAL,
	"TTPOINT":        handleTTPOINT,
	"TTVECTOR":       handleTTVECTOR,
	"TTGRID":         handleTTGRID,
	"TTUTM":          handleTTUTM,
	"TTUSNG":         handleTTUSNGMGRS,
	"TTMGRS":         handleTTUSNGMGRS,
	"TTMHEAD":        handleTTMHEAD,
	"TTSATSQ":        handleTTSATSQ,
	"TTAMBIG":        handleTTAMBIG,
	"TTMACRO":        handleTTMACRO,
	"TTOBJ":          handleTTOBJ,
	"TTERR":          handleTTERR,
	"TTSTATUS":       handleTTSTATUS,
	"TTCMD":          handleTTCMD,
	"IGSERVER":       handleIGSERVER,
	"IGLOGIN":        handleIGLOGIN,
	"IGTXVIA":        handleIGTXVIA,
	"IGFILTER":       handleIGFILTER,
	"IGTXLIMIT":      handleIGTXLIMIT,
	"IGMSP":          handleIGMSP,
	"SATGATE":        handleSATGATE,
	"AGWPORT":        handleAGWPORT,
	"AGWLOGIN":       handleAGWLOGIN,
	"METRICSPORT":    handleMETRICSPORT,
	"KISSPORT":       handleKISSPORT,
	"NULLMODEM":      handleNULLMODEM,
	"SERIALKISS":     handleNULLMODEM,
	"SERIALKISSPOLL": handleSERIALKISSPOLL,
	"KISSCOPY":       handleKISSCOPY,
	"DNSSD":          handleDNSSD,
	"DNSSDNAME":      handleDNSSDNAME,
	"GPSNMEA":        handleGPSNMEA,
	"GPSD":           handleGPSD,
	"WAYPOINT":       handleWAYPOINT,
	"LOGDIR":         handleLOGDIR,
	"LOGFILE":        handleLOGFILE,
	"BEACON":         handleBEACON,
	"PBEACON":        handleXBEACON,
	"OBEACON":        handleXBEACON,
	"TBEACON":        handleXBEACON,
	"CBEACON":        handleXBEACON,
	"IBEACON":        handleXBEACON,
	"SMARTBEACON":    handleSMARTBEACON,
	"SMARTBEACONING": handleSMARTBEACON,
	"FRACK":          handleFRACK,
	"RETRY":          handleRETRY,
	"PACLEN":         handlePACLEN,
	"MAXFRAME":       handleMAXFRAME,
	"EMAXFRAME":      handleEMAXFRAME,
	"MAXV22":         handleMAXV22,
	"V20":            handleV20,
	"NOXID":          handleNOXID,
}

// config_init reads the configuration file, applying defaults first so that the
// file can override them.  It returns how many errors and how many warnings the
// file drew, for a caller that wants to act on them - see the ---config-check
// option in DirewolfMain.
func config_init(fname string, p_audio_config *audio_s,
	p_digi_config *digi_config_s,
	p_cdigi_config *cdigi_config_s,
	p_tt_config *tt_config_s,
	p_igate_config *igate_config_s,
	p_misc_config *misc_config_s) (nerrors int, nwarnings int) {
	logrus.WithField("fname", fname).Debug("config_init")

	/*
	 * First apply defaults.
	 */
	p_audio_config.igate_vchannel = -1 // none.

	/* First audio device is always available with defaults. */
	/* Others must be explicitly defined before use. */

	for adevice := range MAX_ADEVS {
		p_audio_config.adev[adevice].adevice_in = DEFAULT_ADEVICE
		p_audio_config.adev[adevice].adevice_out = DEFAULT_ADEVICE
		p_audio_config.adev[adevice].adevice_out_specified = false

		p_audio_config.adev[adevice].defined = 0
		p_audio_config.adev[adevice].copy_from = -1
		p_audio_config.adev[adevice].num_channels = DEFAULT_NUM_CHANNELS       /* -2 stereo */
		p_audio_config.adev[adevice].samples_per_sec = DEFAULT_SAMPLES_PER_SEC /* -r option */
		p_audio_config.adev[adevice].bits_per_sample = DEFAULT_BITS_PER_SAMPLE /* -8 option for 8 instead of 16 bits */
	}

	p_audio_config.adev[0].defined = 2 // 2 means it was done by default and not the user's config file.

	// MAX_TOTAL_CHANS
	for channel := range MAX_TOTAL_CHANS {
		p_audio_config.chan_medium[channel] = MEDIUM_NONE /* One or both channels will be */
		/* set to radio when corresponding */
		/* audio device is defined. */
	}

	// MAX_RADIO_CHANS for achan[]
	// Maybe achan should be renamed to radiochan to make it clearer.
	for channel := range MAX_RADIO_CHANS {
		p_audio_config.achan[channel].modem_type = MODEM_AFSK
		p_audio_config.achan[channel].v26_alternative = V26_UNSPECIFIED
		p_audio_config.achan[channel].mark_freq = DEFAULT_MARK_FREQ   /* -m option */
		p_audio_config.achan[channel].space_freq = DEFAULT_SPACE_FREQ /* -s option */
		p_audio_config.achan[channel].baud = DEFAULT_BAUD             /* -b option */

		/* None.  Will set default later based on other factors. */
		p_audio_config.achan[channel].profiles = ""

		p_audio_config.achan[channel].num_freq = 1
		p_audio_config.achan[channel].offset = 0

		p_audio_config.achan[channel].layer2_xmit = LAYER2_AX25
		p_audio_config.achan[channel].il2p_max_fec = 1
		p_audio_config.achan[channel].il2p_version = IL2P_VERSION_0_6
		p_audio_config.achan[channel].il2p_invert_polarity = 0
		p_audio_config.achan[channel].il2p_crc = true

		p_audio_config.achan[channel].fix_bits = DEFAULT_FIX_BITS
		p_audio_config.achan[channel].sanity_test = SANITY_APRS

		for ot := range NUM_OCTYPES {
			p_audio_config.achan[channel].octrl[ot].ptt_method = PTT_METHOD_NONE
			p_audio_config.achan[channel].octrl[ot].ptt_device = ""
			p_audio_config.achan[channel].octrl[ot].ptt_line = PTT_LINE_NONE
			p_audio_config.achan[channel].octrl[ot].ptt_line2 = PTT_LINE_NONE
			p_audio_config.achan[channel].octrl[ot].out_gpio_num = 0
			p_audio_config.achan[channel].octrl[ot].ptt_lpt_bit = 0
		}

		for it := range NUM_ICTYPES {
			p_audio_config.achan[channel].ictrl[it].method = PTT_METHOD_NONE
			p_audio_config.achan[channel].ictrl[it].in_gpio_num = 0
		}

		p_audio_config.achan[channel].dwait = DEFAULT_DWAIT
		p_audio_config.achan[channel].slottime = DEFAULT_SLOTTIME
		p_audio_config.achan[channel].persist = DEFAULT_PERSIST
		p_audio_config.achan[channel].txdelay = DEFAULT_TXDELAY
		p_audio_config.achan[channel].txtail = DEFAULT_TXTAIL
		p_audio_config.achan[channel].fulldup = DEFAULT_FULLDUP
	}

	p_audio_config.fx25_auto_enable = AX25_N2_RETRY_DEFAULT / 2

	/* First channel should always be valid. */
	/* If there is no ADEVICE, it uses default device in mono. */

	p_audio_config.chan_medium[0] = MEDIUM_RADIO

	p_digi_config.dedupe_time = DEFAULT_DEDUPE

	p_tt_config.gateway_enabled = 0

	/* Retention time and decay algorithm from 13 Feb 13 version of */
	/* http://www.aprs.org/aprstt/aprstt-coding24.txt */
	/* Reduced by transmit count by one.  An 8 minute delay in between transmissions seems awful long. */

	p_tt_config.retain_time = 80 * 60
	p_tt_config.num_xmits = 6
	Assert(p_tt_config.num_xmits <= TT_MAX_XMITS)
	p_tt_config.xmit_delay[0] = 3 /* Before initial transmission. */
	p_tt_config.xmit_delay[1] = 16
	p_tt_config.xmit_delay[2] = 32
	p_tt_config.xmit_delay[3] = 64
	p_tt_config.xmit_delay[4] = 2 * 60
	p_tt_config.xmit_delay[5] = 4 * 60
	p_tt_config.xmit_delay[6] = 8 * 60 // not currently used.

	p_tt_config.status[0] = ""
	p_tt_config.status[1] = "/off duty"
	p_tt_config.status[2] = "/enroute"
	p_tt_config.status[3] = "/in service"
	p_tt_config.status[4] = "/returning"
	p_tt_config.status[5] = "/committed"
	p_tt_config.status[6] = "/special"
	p_tt_config.status[7] = "/priority"
	p_tt_config.status[8] = "/emergency"
	p_tt_config.status[9] = "/custom 1"

	for m := range TT_ERROR_MAXP1 {
		p_tt_config.response[m].method = "MORSE"
		p_tt_config.response[m].mtext = "?"
	}

	p_tt_config.response[TT_ERROR_OK].mtext = "R"

	p_misc_config.agwpe_port = DEFAULT_AGWPE_PORT
	p_misc_config.metrics_port = 0 // Disabled by default.

	for i := range MAX_KISS_TCP_PORTS {
		p_misc_config.kiss_port[i] = 0 // entry not used.
		p_misc_config.kiss_chan[i] = -1
	}

	p_misc_config.kiss_port[0] = DEFAULT_KISS_PORT
	p_misc_config.kiss_chan[0] = -1 // all channels.

	p_misc_config.enable_kiss_pt = false /* -p option */
	p_misc_config.kiss_copy = false

	p_misc_config.dns_sd_enabled = true

	/* Defaults from http://info.aprs.net/index.php?title=SmartBeaconing */

	p_misc_config.sb_configured = false /* TRUE if SmartBeaconing is configured. */
	p_misc_config.sb_fast_speed = 60    /* MPH */
	p_misc_config.sb_fast_rate = 180    /* seconds */
	p_misc_config.sb_slow_speed = 5     /* MPH */
	p_misc_config.sb_slow_rate = 1800   /* seconds */
	p_misc_config.sb_turn_time = 15     /* seconds */
	p_misc_config.sb_turn_angle = 30    /* degrees */
	p_misc_config.sb_turn_slope = 255   /* degrees * MPH */

	p_igate_config.t2_server_port = DEFAULT_IGATE_PORT
	p_igate_config.tx_chan = -1 /* IS to RF not enabled */
	p_igate_config.tx_limit_1 = IGATE_TX_LIMIT_1_DEFAULT
	p_igate_config.tx_limit_5 = IGATE_TX_LIMIT_5_DEFAULT
	p_igate_config.igmsp = 1
	p_igate_config.rx2ig_dedupe_time = IGATE_RX2IG_DEDUPE_TIME

	/* People find this confusing. */
	/* Ideally we'd like to figure out if com0com is installed */
	/* and automatically enable this.  */

	p_misc_config.kiss_serial_port = ""
	p_misc_config.kiss_serial_speed = 0
	p_misc_config.kiss_serial_poll = 0

	p_misc_config.gpsnmea_port = ""
	p_misc_config.waypoint_serial_port = ""

	p_misc_config.log_daily_names = false
	p_misc_config.log_path = ""

	/* connected mode. */

	p_misc_config.frack = AX25_T1V_FRACK_DEFAULT /* Number of seconds to wait for ack to transmission. */

	p_misc_config.retry = AX25_N2_RETRY_DEFAULT /* Number of times to retry before giving up. */

	p_misc_config.paclen = AX25_N1_PACLEN_DEFAULT /* Max number of bytes in information part of frame. */

	p_misc_config.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT /* Max frames to send before ACK.  mod 8 "Window" size. */

	p_misc_config.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT /* Max frames to send before ACK.  mod 128 "Window" size. */

	// Send SABME this many times before falling back to SABM.  Negative means
	// the config file did not say, and the end of config_init works it out from
	// whatever RETRY ended up as.
	p_misc_config.maxv22 = -1
	p_misc_config.v20_addrs = nil /* Go directly to v2.0 for stations listed */
	/* without trying v2.2 first. */
	p_misc_config.v20_count = 0
	p_misc_config.noxid_addrs = nil /* Don't send XID to these stations. */
	/* Might work with a partial v2.2 implementation */
	/* on the other end. */
	p_misc_config.noxid_count = 0

	// Persistent context as we work through the file
	var ps = &parseState{
		channel: 0,
		adevice: 0,
		line:    0,
		text:    "",
		keyword: "",
		audio:   p_audio_config,
		digi:    p_digi_config,
		cdigi:   p_cdigi_config,
		tt:      p_tt_config,
		igate:   p_igate_config,
		misc:    p_misc_config,

		nerrors:   0,
		nwarnings: 0,
	}

	/*
	 * Try to extract options from a file.
	 */

	/*
	 * There have been cases where someone had multiple direwolf.conf files
	 * in different places and wasted a lot of time and effort because the
	 * wrong one was being used.
	 *
	 * In version 1.8, I will attempt to display the full absolute path so there
	 * is no confusion.
	 */
	var absFilePath, absFilePathErr = filepath.Abs(fname)
	if absFilePathErr != nil {
		dw_printf("Error getting absolute path for config file %s: %s\n", fname, absFilePathErr)
		os.Exit(1)
	}

	var fp, fpErr = os.Open(absFilePath) //nolint:gosec
	if fpErr != nil {
		ps.errorf(
			"ERROR - Could not open configuration file %s: %s\n"+
				"Try using -c command line option for alternate location.\n"+
				"A sample direwolf.conf file should be found in one of:\n"+
				"    /usr/local/share/doc/direwolf/conf/\n"+
				"    /usr/share/doc/direwolf/conf/",
			absFilePath,
			fpErr,
		)
		rtfm()
		os.Exit(1)
	} else {
		defer fp.Close()
	}

	dw_printf("\nReading config file %s\n", absFilePath)

	var scanner = bufio.NewScanner(fp)
	for scanner.Scan() {
		ps.text = scanner.Text()
		ps.line++

		if ps.text == "" || ps.text[0] == '#' || ps.text[0] == '*' {
			continue
		}

		var t = split(ps.text, false)

		if t == "" {
			continue
		}

		ps.keyword = t

		var keyword = strings.ToUpper(t)

		var err error
		// Some config keywords actually incorporate a device number, e.g. ADEVICE0
		switch {
		case strings.HasPrefix(keyword, "ADEVICE"):
			err = handleADEVICE(ps)
		case strings.HasPrefix(keyword, "PAIDEVICE"):
			err = handlePAIDEVICE(ps)
		case strings.HasPrefix(keyword, "PAODEVICE"):
			err = handlePAODEVICE(ps)
		default:
			if handler, ok := configHandlers[keyword]; ok {
				err = handler(ps)
			} else {
				/*
				 * Invalid command.
				 */
				err = fmt.Errorf("config file: Unrecognized command '%s' on line %d", t, ps.line)
			}
		}

		if err != nil {
			ps.report(err)
		}
	}

	// Scan stops on a read failure, or on a line too long for the scanner's
	// buffer, and says so only here.  The rest of the file went unread, so
	// nothing below can be trusted - say so, rather than letting a file we only
	// got halfway through look like one with nothing wrong with it.
	var scanErr = scanner.Err()
	if scanErr != nil {
		ps.errorf("config file: Could not read %s past line %d: %v", absFilePath, ps.line, scanErr)
	}

	/*
	 * A little error checking for option interactions.
	 */

	/*
	 * Require that MYCALL be set when digipeating or IGating.
	 *
	 * Suggest that beaconing be enabled when digipeating.
	 */

	for i := range MAX_TOTAL_CHANS {
		for j := range MAX_TOTAL_CHANS {
			/* APRS digipeating. */
			if ps.digi.enabled[i][j] {
				if IsNoCall(ps.audio.mycall[i]) {
					ps.errorf("config file: MYCALL must be set for receive channel %d before digipeating is allowed", i)
					ps.digi.enabled[i][j] = false
				}

				if IsNoCall(ps.audio.mycall[j]) {
					ps.errorf("config file: MYCALL must be set for transmit channel %d before digipeating is allowed", j)
					ps.digi.enabled[i][j] = false
				}

				var b = 0

				for k := range ps.misc.num_beacons {
					if ps.misc.beacon[k].sendto_chan == j {
						b++
					}
				}

				if b == 0 {
					ps.warnf("config file: Beaconing should be configured for channel %d when digipeating is enabled", j)
					// It's a recommendation, not a requirement.
					// Was there some good reason to turn it off in earlier version?
					//ps.digi.enabled[i][j] = 0;
				}
			}

			/* Connected mode digipeating. */

			if i < MAX_RADIO_CHANS && j < MAX_RADIO_CHANS && ps.cdigi.enabled[i][j] {
				if IsNoCall(ps.audio.mycall[i]) {
					ps.errorf("config file: MYCALL must be set for receive channel %d before digipeating is allowed", i)
					ps.cdigi.enabled[i][j] = false
				}

				if IsNoCall(ps.audio.mycall[j]) {
					ps.errorf("config file: MYCALL must be set for transmit channel %d before digipeating is allowed", j)
					ps.cdigi.enabled[i][j] = false
				}

				var b = 0

				for k := range ps.misc.num_beacons {
					if ps.misc.beacon[k].sendto_chan == j {
						b++
					}
				}

				if b == 0 {
					ps.warnf("config file: Beaconing should be configured for channel %d when digipeating is enabled", j)
					// It's a recommendation, not a requirement.
				}
			}
		}

		/* When IGate is enabled, all radio channels must have a callsign associated. */

		if len(ps.igate.t2_login) > 0 &&
			(ps.audio.chan_medium[i] == MEDIUM_RADIO || ps.audio.chan_medium[i] == MEDIUM_NETTNC) {
			if IsNoCall(ps.audio.mycall[i]) {
				ps.errorf("config file: MYCALL must be set for receive channel %d before Rx IGate is allowed", i)

				ps.igate.t2_login = ""
			}
			// Currently we can have only one transmit channel.
			// This might be generalized someday to allow more.
			if ps.igate.tx_chan >= 0 && IsNoCall(ps.audio.mycall[ps.igate.tx_chan]) {
				ps.errorf("config file: MYCALL must be set for transmit channel %d before Tx IGate is allowed", i)

				ps.igate.tx_chan = -1
			}
		}
	}

	// Apply default IS>RF IGate filter if none specified.  New in 1.4.
	// This will handle eventual case of multiple transmit channels.

	if len(ps.igate.t2_login) > 0 {
		for j := range MAX_TOTAL_CHANS {
			if ps.audio.chan_medium[j] == MEDIUM_RADIO || ps.audio.chan_medium[j] == MEDIUM_NETTNC {
				if ps.digi.filter_str[MAX_TOTAL_CHANS][j] == "" {
					ps.digi.filter_str[MAX_TOTAL_CHANS][j] = "i/180"
				}
			}
		}
	}

	// Terrible hack.  But what can we do?

	if ps.misc.maxv22 < 0 {
		ps.misc.maxv22 = ps.misc.retry / 3
	}

	return ps.nerrors, ps.nwarnings
} /* end config_init */

// handleADEVICE handles the ADEVICE[n] keyword.
func handleADEVICE(ps *parseState) error {
	/*
	 * ADEVICE[n] 		- Name of input sound device, and optionally output, if different.
	 *
	 *			ADEVICE    plughw:1,0				-- same for in and out.
	 *			ADEVICE	   plughw:2,0  plughw:3,0		-- different in/out for a channel or channel pair.
	 *			ADEVICE1   udp:7355  default			-- input from SDR via UDP; output to soundcard.
	 *			ADEVICE    default   udp:localhost:7355		-- input from soundcard; output via UDP.
	 *
	 *	New in 1.8: Ability to map to another audio device.
	 *	This allows multiple modems (i.e. data speeds) on the same audio interface.
	 *
	 *			ADEVICEn   = n				-- Copy from different already defined channel.
	 */
	/* Note that ALSA name can contain comma such as hw:1,0 */
	/* "ADEVICE" is equivalent to "ADEVICE0". */
	ps.adevice = 0

	// ps.keyword holds the original token e.g. "ADEVICE" or "ADEVICE1".
	if len(ps.keyword) >= 8 {
		var i, iErr = strconv.Atoi(ps.keyword[7:])
		if iErr != nil {
			return fmt.Errorf("config file: Could not parse ADEVICE number on line %d: %w", ps.line, iErr)
		}

		if i < 0 || i >= MAX_ADEVS {
			ps.errorf(
				"Config file: Device number %d out of range for ADEVICE command on line %d.\nIf you really need more than %d audio devices, increase MAX_ADEVS and recompile.",
				i,
				ps.line,
				MAX_ADEVS,
			)

			ps.adevice = 0

			return nil
		}

		ps.adevice = i
	}

	var t = split("", false)
	if t == "" {
		// Reported here rather than returned, so that the pointer at the
		// documentation still follows the complaint it belongs to.
		ps.errorf("config file: Missing name of audio device for ADEVICE command on line %d", ps.line)
		rtfm()

		return nil
	}

	// Do not allow same adevice to be defined more than once.
	// Overriding the default for adevice 0 is ok.
	// In that case defined was 2.  That's why we check for 1, not just non-zero.

	if ps.audio.adev[ps.adevice].defined == 1 { // 1 means defined by user.
		return fmt.Errorf("config file: ADEVICE%d can't be defined more than once. Line %d", ps.adevice, ps.line)
	}

	// New case for release 1.8.

	if t == "=" {
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file: ADEVICE%d mapping syntax requires a source device number on line %d", ps.adevice, ps.line)
		}

		return fmt.Errorf("config file: ADEVICE%d = %s mapping syntax is not implemented on line %d", ps.adevice, t, ps.line)
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	// This might be changed to UDP or STDIN when the device name is examined.
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_in = t
	ps.audio.adev[ps.adevice].adevice_out = t

	t = split("", false)
	if t != "" {
		// Different audio devices for receive and transmit.
		ps.audio.adev[ps.adevice].adevice_out = t
		ps.audio.adev[ps.adevice].adevice_out_specified = true
	}

	return nil
}

// handlePAIDEVICE handles PAIDEVICE[n].
func handlePAIDEVICE(ps *parseState) error {
	// ps.keyword holds the original token e.g. "PAIDEVICE" or "PAIDEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		ps.errorf("config file: Device number %d out of range for PAIDEVICE command on line %d", ps.adevice, ps.line)
		ps.adevice = 0

		return nil
	}

	var t = split("", true)
	if t == "" {
		return fmt.Errorf("config file: Missing name of audio device for PAIDEVICE command on line %d", ps.line)
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_in = t

	return nil
}

// handlePAODEVICE handles PAODEVICE[n].
func handlePAODEVICE(ps *parseState) error {
	// ps.keyword holds the original token e.g. "PAODEVICE" or "PAODEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		ps.errorf("config file: Device number %d out of range for PAODEVICE command on line %d", ps.adevice, ps.line)
		ps.adevice = 0

		return nil
	}

	var t = split("", true)
	if t == "" {
		return fmt.Errorf("config file: Missing name of audio device for PAODEVICE command on line %d", ps.line)
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_out = t
	ps.audio.adev[ps.adevice].adevice_out_specified = true

	return nil
}

// handleARATE handles the ARATE keyword.
func handleARATE(ps *parseState) error {
	/*
	 * ARATE 		- Audio samples per second, 11025, 22050, 44100, etc.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing audio sample rate for ARATE command", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= MIN_SAMPLES_PER_SEC && n <= MAX_SAMPLES_PER_SEC {
		ps.audio.adev[ps.adevice].samples_per_sec = n
	} else {
		ps.errorf("line %d: Use a more reasonable audio sample rate in range of %d - %d", ps.line, MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC)
	}

	return nil
}

// handleACHANNELS handles the ACHANNELS keyword.
func handleACHANNELS(ps *parseState) error {
	/*
	 * ACHANNELS 		- Number of audio channels for current device: 1 or 2
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing number of audio channels for ACHANNELS command", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n == 1 || n == 2 {
		ps.audio.adev[ps.adevice].num_channels = n

		/* Set valid channels depending on mono or stereo. */

		ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO
		if n == 2 {
			ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)+1] = MEDIUM_RADIO
		}
	} else {
		ps.errorf("line %d: Number of audio channels must be 1 or 2", ps.line)
	}

	return nil
}

// handleCHANNEL handles the CHANNEL keyword.
func handleCHANNEL(ps *parseState) error {
	/*
	 * ==================== Radio channel parameters ====================
	 */

	/*
	 * CHANNEL n		- Set channel for channel-specific commands.  Only for modem/radio channels.
	 */

	// TODO: allow full range so mycall can be set for network channels.
	// Watch out for achan[] out of bounds.
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing channel number for CHANNEL command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Channel number must be numeric for CHANNEL command", ps.line)
	}
	if n >= 0 && n < MAX_RADIO_CHANS {
		ps.channel = n

		if ps.audio.chan_medium[n] != MEDIUM_RADIO {
			if ps.audio.adev[ACHAN2ADEV(n)].defined == 0 {
				ps.errorf("line %d: Channel number %d is not valid because audio device %d is not defined", ps.line, n, ACHAN2ADEV(n))
			} else {
				ps.errorf("line %d: Channel number %d is not valid because audio device %d is not in stereo", ps.line, n, ACHAN2ADEV(n))
			}
		}
	} else {
		ps.errorf("line %d: Channel number must in range of 0 to %d", ps.line, MAX_RADIO_CHANS-1)
	}

	return nil
}

// handleICHANNEL handles the ICHANNEL keyword.
func handleICHANNEL(ps *parseState) error {
	/*
	 * ICHANNEL n			- Define IGate virtual channel.
	 *
	 *	This allows a client application to talk to to APRS-IS
	 *	by using a channel number outside the normal range for modems.
	 *	In the future there might be other typs of virtual channels.
	 *	This does not change the current channel number used by MODEM, PTT, etc.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing virtual channel number for ICHANNEL command", ps.line)
	}

	var ichan, _ = strconv.Atoi(t)
	if ichan >= MAX_RADIO_CHANS && ichan < MAX_TOTAL_CHANS {
		if ps.audio.chan_medium[ichan] == MEDIUM_NONE {
			ps.audio.chan_medium[ichan] = MEDIUM_IGATE

			// This is redundant but saves the time of searching through all
			// the channels for each packet.
			ps.audio.igate_vchannel = ichan
		} else {
			ps.errorf("line %d: ICHANNEL can't use channel %d because it is already in use", ps.line, ichan)
		}
	} else {
		ps.errorf("line %d: ICHANNEL number must in range of %d to %d", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}

	return nil
}

// handleNCHANNEL handles the NCHANNEL keyword.
func handleNCHANNEL(ps *parseState) error {
	/*
	 * NCHANNEL chan addr port			- Define Network TNC virtual channel.
	 *
	 *	This allows a client application to talk to to an external TNC over TCP KISS
	 *	by using a channel number outside the normal range for modems.
	 *	This does not change the current channel number used by MODEM, PTT, etc.
	 *
	 *	chan = direwolf channel.
	 *	addr = hostname or IP address of network TNC.
	 *	port = KISS TCP port on network TNC.
	 *
	 *	Future: Might allow selection of channel on the network TNC.
	 *	For now, ignore incoming and set to 0 for outgoing.
	 *
	 * FIXME: Can't set mycall for nchannel.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing virtual channel number for NCHANNEL command", ps.line)
	}

	var nchan, _ = strconv.Atoi(t)
	if nchan < MAX_RADIO_CHANS || nchan >= MAX_TOTAL_CHANS {
		return fmt.Errorf("line %d: NCHANNEL number must be in range of %d to %d", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}
	if ps.audio.chan_medium[nchan] != MEDIUM_NONE {
		return fmt.Errorf("line %d: NCHANNEL can't use channel %d because it is already in use", ps.line, nchan)
	}

	var addr = split("", false)
	if addr == "" {
		return fmt.Errorf("line %d: Missing network TNC address for NCHANNEL command", ps.line)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing network TNC TCP port for NCHANNEL command", ps.line)
	}
	var n, nErr = strconv.Atoi(t)
	if nErr != nil || n < MIN_IP_PORT_NUMBER || n > MAX_IP_PORT_NUMBER {
		return fmt.Errorf("line %d: Invalid TCP port number \"%s\" for NCHANNEL command. Must be in range %d to %d", ps.line, t, MIN_IP_PORT_NUMBER, MAX_IP_PORT_NUMBER)
	}

	// Claim the channel only once the whole line has parsed: nettnc_init
	// attaches to every MEDIUM_NETTNC channel and exits if it cannot, so a
	// half-read line would otherwise take the program down at startup.
	ps.audio.chan_medium[nchan] = MEDIUM_NETTNC
	ps.audio.nettnc_addr[nchan] = addr
	ps.audio.nettnc_port[nchan] = n

	return nil
}

// handleMYCALL handles the MYCALL keyword.
func handleMYCALL(ps *parseState) error {
	/*
	 * MYCALL station
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing value for MYCALL command on line %d", ps.line)
	} else {
		/* Silently force upper case. */
		/* Might change to warning someday. */
		t = strings.ToUpper(t)

		var _, _, _, ok = ax25_parse_addr(-1, t, addrStrictNoStar)

		if !ok {
			return fmt.Errorf("config file: Invalid value for MYCALL command on line %d", ps.line)
		}

		// Definitely set for current channel.
		// Set for other channels which have not been set yet.

		for c := range MAX_TOTAL_CHANS {
			if c == ps.channel || IsNoCall(ps.audio.mycall[c]) {
				ps.audio.mycall[c] = t
			}
		}
	}

	return nil
}

// handleMODEM handles the MODEM keyword.
func handleMODEM(ps *parseState) error {
	/*
	 * MODEM	- Set modem properties for current channel.
	 *
	 *
	 * Old style:
	 * 	MODEM  baud [ mark  space  [A][B][C][+]  [  num-decoders spacing ] ]
	 *
	 * New style, version 1.2:
	 *	MODEM  speed [ option ] ...
	 *
	 * Options:
	 *	mark:space	- AFSK tones.  Defaults based on speed.
	 *	num@offset	- Multiple decoders on different frequencies.
	 *	/9		- Divide sample rate by specified number.
	 *	*9		- Upsample ratio for G3RUH.
	 *	[A-Z+-]+	- Letters, plus, minus for the demodulator "profile."
	 *	g3ruh		- This modem type regardless of default for speed.
	 *	v26a or v26b	- V.26 alternative.  a=original, b=MFJ compatible
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: MODEM can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing data transmission speed for MODEM command", ps.line)
	}

	var n int
	if strings.EqualFold(t, "AIS") {
		n = MAX_BAUD - 1 // Hack - See special case later.
	} else if strings.EqualFold(t, "EAS") {
		n = MAX_BAUD - 2 // Hack - See special case later.
	} else {
		n, _ = strconv.Atoi(t)
	}

	if n >= MIN_BAUD && n <= MAX_BAUD {
		ps.audio.achan[ps.channel].baud = n
		if n != 300 && n != 1200 && n != 2400 && n != 4800 && n != 9600 && n != 19200 && n != MAX_BAUD-1 && n != MAX_BAUD-2 {
			ps.warnf("line %d: Warning: Non-standard data rate of %d bits per second.  Are you sure?", ps.line, n)
		}
	} else {
		ps.audio.achan[ps.channel].baud = DEFAULT_BAUD

		ps.errorf("line %d: Unreasonable data rate. Using %d bits per second", ps.line, ps.audio.achan[ps.channel].baud)
	}

	/* Set defaults based on speed. */
	/* Should be same as -B command line option in direwolf.c. */

	/* We have similar logic in direwolf.c, config.c, gen_packets.c, and atest.c, */
	/* that need to be kept in sync.  Maybe it could be a common function someday. */

	if ps.audio.achan[ps.channel].baud < 600 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AFSK
		ps.audio.achan[ps.channel].mark_freq = 1600
		ps.audio.achan[ps.channel].space_freq = 1800
	} else if ps.audio.achan[ps.channel].baud < 1800 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AFSK
		ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ
		ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ
	} else if ps.audio.achan[ps.channel].baud < 3600 {
		ps.audio.achan[ps.channel].modem_type = MODEM_QPSK
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud < 7200 {
		ps.audio.achan[ps.channel].modem_type = MODEM_8PSK
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud == MAX_BAUD-1 {
		ps.audio.achan[ps.channel].modem_type = MODEM_AIS
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	} else if ps.audio.achan[ps.channel].baud == MAX_BAUD-2 {
		ps.audio.achan[ps.channel].modem_type = MODEM_EAS
		ps.audio.achan[ps.channel].baud = 521 // Actually 520.83 but we have an integer field here.
		// Will make more precise in afsk demod init.
		ps.audio.achan[ps.channel].mark_freq = 2083  // Actually 2083.3 - logic 1.
		ps.audio.achan[ps.channel].space_freq = 1563 // Actually 1562.5 - logic 0.
		// ? strlcpy (p_audio_config.achan[channel].profiles, "A", sizeof(p_audio_config.achan[channel].profiles));
	} else {
		ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
		ps.audio.achan[ps.channel].mark_freq = 0
		ps.audio.achan[ps.channel].space_freq = 0
	}

	/* Get any options. */

	t = split("", false)
	if t == "" {
		/* all done. */
		return nil
	}

	if alldigits(t) {
		/* old style */
		ps.errorf("line %d: Old style (pre version 1.2) format will no longer be supported in next version", ps.line)

		n, _ = strconv.Atoi(t)
		/* Originally the upper limit was 3000. */
		/* Version 1.0 increased to 5000 because someone */
		/* wanted to use 2400/4800 Hz AFSK. */
		/* Of course the MIC and SPKR connections won't */
		/* have enough bandwidth so radios must be modified. */
		if n >= 300 && n <= 5000 {
			ps.audio.achan[ps.channel].mark_freq = n
		} else {
			ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ

			ps.errorf("line %d: Unreasonable mark tone frequency. Using %d", ps.line, ps.audio.achan[ps.channel].mark_freq)
		}

		/* Get space frequency */

		t = split("", false)
		if t == "" {
			return fmt.Errorf("line %d: Missing tone frequency for space", ps.line)
		}

		n, _ = strconv.Atoi(t)
		if n >= 300 && n <= 5000 {
			ps.audio.achan[ps.channel].space_freq = n
		} else {
			ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

			ps.errorf("line %d: Unreasonable space tone frequency. Using %d", ps.line, ps.audio.achan[ps.channel].space_freq)
		}

		/* Gently guide users toward new format. */

		if ps.audio.achan[ps.channel].baud == 1200 &&
			ps.audio.achan[ps.channel].mark_freq == 1200 &&
			ps.audio.achan[ps.channel].space_freq == 2200 {
			ps.errorf("line %d: The AFSK frequencies can be omitted when using the 1200 baud default 1200:2200", ps.line)
		}

		if ps.audio.achan[ps.channel].baud == 300 &&
			ps.audio.achan[ps.channel].mark_freq == 1600 &&
			ps.audio.achan[ps.channel].space_freq == 1800 {
			ps.errorf("line %d: The AFSK frequencies can be omitted when using the 300 baud default 1600:1800", ps.line)
		}

		/* New feature in 0.9 - Optional filter profile(s). */

		t = split("", false)
		if t != "" {
			/* Look for some combination of letter(s) and + */
			if unicode.IsLetter(rune(t[0])) || t[0] == '+' {
				/* Here we only catch something other than letters and + mixed in. */
				/* Later, we check for valid letters and no more than one letter if + specified. */
				if strings.ContainsFunc(t, func(r rune) bool {
					return !unicode.IsLetter(r) && r != '+' && r != '-'
				}) {
					ps.errorf("line %d: Demodulator type can only contain letters and + character", ps.line)
				}

				ps.audio.achan[ps.channel].profiles = t

				t = split("", false)
				if len(ps.audio.achan[ps.channel].profiles) > 1 && t != "" {
					return fmt.Errorf("line %d: Can't combine multiple demodulator types and multiple frequencies", ps.line)
				}
			}
		}

		/* New feature in 0.9 - optional number of decoders and frequency offset between. */

		if t != "" {
			n, _ = strconv.Atoi(t)
			if n < 1 || n > MAX_SUBCHANS {
				ps.errorf("line %d: Number of demodulators is out of range. Using 3", ps.line)

				n = 3
			}

			ps.audio.achan[ps.channel].num_freq = n

			t = split("", false)
			if t != "" {
				n, _ = strconv.Atoi(t)
				if n < 5 || n > int(math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2) {
					ps.errorf("line %d: Unreasonable value for offset between modems.  Using 50 Hz", ps.line)

					n = 50
				}

				ps.audio.achan[ps.channel].offset = n

				ps.errorf("line %d: New style for multiple demodulators is %d@%d", ps.line,
					ps.audio.achan[ps.channel].num_freq, ps.audio.achan[ps.channel].offset)
			} else {
				ps.errorf("line %d: Missing frequency offset between modems.  Using 50 Hz", ps.line)

				ps.audio.achan[ps.channel].offset = 50
			}
		}
	} else {
		/* New style in version 1.2. */
		for t != "" {
			if strings.Contains(t, ":") { /* mark:space */
				var markStr, spaceStr, _ = strings.Cut(t, ":")
				var mark, _ = strconv.Atoi(markStr)
				var space, _ = strconv.Atoi(spaceStr)

				ps.audio.achan[ps.channel].mark_freq = mark
				ps.audio.achan[ps.channel].space_freq = space

				if ps.audio.achan[ps.channel].mark_freq == 0 && ps.audio.achan[ps.channel].space_freq == 0 {
					ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
				} else {
					ps.audio.achan[ps.channel].modem_type = MODEM_AFSK

					if ps.audio.achan[ps.channel].mark_freq < 300 || ps.audio.achan[ps.channel].mark_freq > 5000 {
						ps.audio.achan[ps.channel].mark_freq = DEFAULT_MARK_FREQ

						ps.errorf("line %d: Unreasonable mark tone frequency. Using %d instead", ps.line, ps.audio.achan[ps.channel].mark_freq)
					}

					if ps.audio.achan[ps.channel].space_freq < 300 || ps.audio.achan[ps.channel].space_freq > 5000 {
						ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

						ps.errorf("line %d: Unreasonable space tone frequency. Using %d instead", ps.line, ps.audio.achan[ps.channel].space_freq)
					}
				}
			} else if strings.Contains(t, "@") { /* num@offset */
				var numStr, offsetStr, _ = strings.Cut(t, "@")
				var num, _ = strconv.Atoi(numStr)
				var offset, _ = strconv.Atoi(offsetStr)

				ps.audio.achan[ps.channel].num_freq = num
				ps.audio.achan[ps.channel].offset = offset

				if ps.audio.achan[ps.channel].num_freq < 1 || ps.audio.achan[ps.channel].num_freq > MAX_SUBCHANS {
					ps.errorf("line %d: Number of demodulators is out of range. Using 3", ps.line)

					ps.audio.achan[ps.channel].num_freq = 3
				}

				if ps.audio.achan[ps.channel].offset < 5 ||
					float64(ps.audio.achan[ps.channel].offset) > math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2 {
					ps.errorf("line %d: Offset between demodulators is unreasonable. Using 50 Hz", ps.line)

					ps.audio.achan[ps.channel].offset = 50
				}
			} else if strings.EqualFold(t, "BPSK") { /* Force BPSK modem (1 bit/symbol, carrier 1800 Hz). */
				ps.audio.achan[ps.channel].modem_type = MODEM_BPSK
				ps.audio.achan[ps.channel].mark_freq = 0
				ps.audio.achan[ps.channel].space_freq = 0
			} else if strings.EqualFold(t, "G3RUH") { /* Force G3RUH modem regardless of default for speed. New in 1.6. */
				ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
				ps.audio.achan[ps.channel].mark_freq = 0
				ps.audio.achan[ps.channel].space_freq = 0
			} else if strings.EqualFold(t, "V26A") || /* Compatible with direwolf versions <= 1.5.  New in 1.6. */
				strings.EqualFold(t, "V26B") { /* Compatible with MFJ-2400.  New in 1.6. */
				if ps.audio.achan[ps.channel].modem_type != MODEM_QPSK ||
					ps.audio.achan[ps.channel].baud != 2400 {
					return fmt.Errorf("line %d: %s option can only be used with 2400 bps PSK", ps.line, t)
				}

				ps.audio.achan[ps.channel].v26_alternative = IfThenElse((strings.EqualFold(t, "V26A")), V26_A, V26_B)
			} else if t[0] == '/' { /* /div */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 8 {
					ps.audio.achan[ps.channel].decimate = n
				} else {
					ps.errorf("line %d: Ignoring unreasonable sample rate division factor of %d", ps.line, n)
				}
			} else if t[0] == '*' { /* *upsample */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 4 {
					ps.audio.achan[ps.channel].upsample = n
				} else {
					ps.errorf("line %d: Ignoring unreasonable upsample ratio of %d", ps.line, n)
				}
			} else if alllettersorpm(t) { /* profile of letter(s) + - */
				// Will be validated later.
				ps.audio.achan[ps.channel].profiles = t
			} else {
				ps.errorf("line %d: Unrecognized option for MODEM: %s", ps.line, t)
			}

			t = split("", false)
		}

		/* A later place catches disallowed combination of + and @. */
		/* A later place sets /n for 300 baud if not specified by user. */

		//dw_printf ("debug: div = %d\n", p_audio_config.achan[channel].decimate);
	}

	return nil
}

// handleDTMF handles the DTMF keyword.
func handleDTMF(ps *parseState) error {
	/*
	 * DTMF  		- Enable DTMF decoder.
	 *
	 * Future possibilities:
	 *	Option to determine if it goes to APRStt gateway and/or application.
	 *	Disable normal demodulator to reduce CPU requirements.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: DTMF can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	ps.audio.achan[ps.channel].dtmf_decode = DTMF_DECODE_ON

	return nil
}

// handleFIX_BITS handles the FIX_BITS keyword.
func handleFIX_BITS(ps *parseState) error {
	/*
	 * FIX_BITS  n  [ APRS | AX25 | NONE ] [ PASSALL ]
	 *
	 *	- Attempt to fix frames with bad FCS.
	 *	- n is maximum number of bits to attempt fixing.
	 *	- Optional sanity check & allow everything even with bad FCS.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: FIX_BITS can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for FIX_BITS command", ps.line)
	}

	// An unreadable level leaves the one already configured; the options after
	// it on the line are still worth reading.
	var n, nErr = strconv.Atoi(t)
	switch {
	case nErr != nil:
		ps.errorf("line %d: Value must be numeric for FIX_BITS command. Keeping %d", ps.line, ps.audio.achan[ps.channel].fix_bits)
	case BitFixLevel(n) >= BitFixNone && BitFixLevel(n) <= BitFixLevelHighest:
		ps.audio.achan[ps.channel].fix_bits = BitFixLevel(n)
	default:
		ps.audio.achan[ps.channel].fix_bits = DEFAULT_FIX_BITS

		ps.errorf("line %d: Invalid value %d for FIX_BITS. Using default of %d", ps.line, n, ps.audio.achan[ps.channel].fix_bits)
	}

	if ps.audio.achan[ps.channel].fix_bits > DEFAULT_FIX_BITS {
		ps.warnf("line %d: Using a FIX_BITS value greater than %d is not recommended for normal operation.\n"+
			"FIX_BITS > 1 was an interesting experiment but turned out to be a bad idea.\n"+
			"Don't be surprised if it takes 100%% CPU, direwolf can't keep up with the audio stream,\n"+
			"and you see messages like \"Audio input device 0 error code -32: Broken pipe\"",
			ps.line, DEFAULT_FIX_BITS)
	}

	t = split("", false)
	for t != "" {
		// If more than one sanity test, we silently take the last one.
		if strings.EqualFold(t, "APRS") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_APRS
		} else if strings.EqualFold(t, "AX25") || strings.EqualFold(t, "AX.25") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_AX25
		} else if strings.EqualFold(t, "NONE") {
			ps.audio.achan[ps.channel].sanity_test = SANITY_NONE
		} else if strings.EqualFold(t, "PASSALL") {
			ps.audio.achan[ps.channel].passall = true

			ps.errorf(
				"Line %d: There is an old saying, \"Be careful what you ask for because you might get it.\"\n"+
					"The PASSALL option means allow all frames even when they are invalid.\n"+
					"You are asking to receive random trash and you WILL get your wish.\n"+
					"Don't complain when you see all sorts of random garbage.  That's what you asked for.",
				ps.line,
			)
		} else {
			ps.errorf("line %d: Invalid option '%s' for FIX_BITS", ps.line, t)
		}

		t = split("", false)
	}

	return nil
}

// handlePTTDCDCON handles the PTTDCDCON keyword.
func handlePTTDCDCON(ps *parseState) error {
	/*
	 * PTT 		- Push To Talk signal line.
	 * DCD		- Data Carrier Detect indicator.
	 * CON		- Connected to another station indicator.
	 *
	 * xxx  serial-port [-]rts-or-dtr [ [-]rts-or-dtr ]
	 * xxx  GPIO  [-]gpio-num
	 * xxx  LPT  [-]bit-num
	 * PTT  RIG  model  port [ rate ]
	 * PTT  RIG  AUTO  port [ rate ]
	 * PTT  CM108 [ [-]bit-num ] [ hid-device ]
	 *
	 * 		When model is 2, port would host:port like 127.0.0.1:4532
	 *		Otherwise, port would be a serial port like /dev/ttyS0
	 *
	 *
	 * Applies to most recent CHANNEL command.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: PTT can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}
	var ot int
	var otname string

	if strings.EqualFold(ps.keyword, "PTT") {
		ot = OCTYPE_PTT
		otname = "PTT"
	} else if strings.EqualFold(ps.keyword, "DCD") {
		ot = OCTYPE_DCD
		otname = "DCD"
	} else {
		ot = OCTYPE_CON
		otname = "CON"
	}

	// Work on a copy of the control and commit it at the end, so that a line
	// rejected part way through leaves whatever an earlier line configured
	// rather than a mixture of the two.  ptt_init reads these fields together.
	var octrl = ps.audio.achan[ps.channel].octrl[ot]

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file line %d: Missing output control device for %s command", ps.line, otname)
	}

	if strings.EqualFold(t, "GPIO") {
		/* GPIO case, Linux only. */

		/* TODO KG
		   #if __WIN32__
		   	      text_color_set(DW_COLOR_ERROR);
		   	      dw_printf ("Config file line %d: %s with GPIO is only available on Linux.\n", ps.line, otname);
		   #else
		*/
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing GPIO number for %s", ps.line, otname)
		}

		var gpio, gpioErr = strconv.Atoi(t)
		if gpioErr != nil {
			return fmt.Errorf("config file line %d: GPIO number must be numeric for %s", ps.line, otname)
		}
		if gpio < 0 {
			octrl.out_gpio_num = -1 * gpio
			octrl.ptt_invert = true
		} else {
			octrl.out_gpio_num = gpio
			octrl.ptt_invert = false
		}

		octrl.ptt_method = PTT_METHOD_GPIO
		// #endif
	} else if strings.EqualFold(t, "GPIOD") {
		/*
			#if __WIN32__
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Config file line %d: %s with GPIOD is only available on Linux.\n", ps.line, otname);
			#else
		*/
		// #if defined(USE_GPIOD)
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing GPIO chip name for %s.\nUse the \"gpioinfo\" command to get a list of gpio chip names and corresponding I/O lines", ps.line, otname)
		}

		// Issue 590.  Originally we used the chip name, like gpiochip3, and fed it into
		// gpiod_chip_open_by_name.   This function has disappeared in Debian 13 Trixie.
		// We must now specify the full device path, like /dev/gpiochip3, for the only
		// remaining open function gpiod_chip_open.
		// We will allow the user to specify either the name or full device path.
		// While we are here, also allow only the number as used by the gpiod utilities.

		if t[0] == '/' { // Looks like device path.  Use as given.
			octrl.out_gpio_name = t
		} else if unicode.IsDigit(rune(t[0])) { // or if digit, prepend "/dev/gpiochip"
			octrl.out_gpio_name = "/dev/gpiochip" + t
		} else { // otherwise, prepend "/dev/" to the name
			octrl.out_gpio_name = "/dev/" + t
		}

		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing GPIO number for %s", ps.line, otname)
		}

		var gpio, gpioErr = strconv.Atoi(t)
		if gpioErr != nil {
			return fmt.Errorf("config file line %d: GPIO number must be numeric for %s", ps.line, otname)
		}

		if gpio < 0 {
			octrl.out_gpio_num = -1 * gpio
			octrl.ptt_invert = true
		} else {
			octrl.out_gpio_num = gpio
			octrl.ptt_invert = false
		}

		octrl.ptt_method = PTT_METHOD_GPIOD
		/* TODO KG
		#else
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Application was not built with optional support for GPIOD.\n");
			      dw_printf ("Install packages gpiod and libgpiod-dev, remove 'build' subdirectory, then rebuild.\n");
		#endif // USE_GPIOD
		*/
		//#endif /* __WIN32__ */
	} else if strings.EqualFold(t, "LPT") {
		/* Parallel printer case, x86 Linux only. */

		//#if  ( defined(__i386__) || defined(__x86_64__) ) && ( defined(__linux__) || defined(__unix__) )
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing LPT bit number for %s", ps.line, otname)
		}

		var lpt, lptErr = strconv.Atoi(t)
		if lptErr != nil {
			return fmt.Errorf("config file line %d: LPT bit number must be numeric for %s", ps.line, otname)
		}
		if lpt < 0 {
			octrl.ptt_lpt_bit = -1 * lpt
			octrl.ptt_invert = true
		} else {
			octrl.ptt_lpt_bit = lpt
			octrl.ptt_invert = false
		}

		octrl.ptt_method = PTT_METHOD_LPT
		/*
			#else
				      text_color_set(DW_COLOR_ERROR);
				      dw_printf ("Config file line %d: %s with LPT is only available on x86 Linux.\n", ps.line, otname);
			#endif
		*/
	} else if strings.EqualFold(t, "RIG") {
		// TODO KG #ifdef USE_HAMLIB
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing model number for hamlib", ps.line)
		}

		if strings.EqualFold(t, "AUTO") {
			octrl.ptt_model = -1
		} else {
			if !alldigits(t) {
				return fmt.Errorf(
					"config file line %d: A rig number, not a name, is required here.\n"+
						"For example, if you have a Yaesu FT-847, specify 101.\n"+
						"See https://github.com/Hamlib/Hamlib/wiki/Supported-Radios for more details",
					ps.line,
				)
			}

			var n, _ = strconv.Atoi(t)
			if n < 1 || n > 9999 {
				return fmt.Errorf("config file line %d: Unreasonable model number %d for hamlib", ps.line, n)
			}

			octrl.ptt_model = n
		}

		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing port for hamlib", ps.line)
		}

		octrl.ptt_device = t

		// Optional serial port rate for CAT control PTT.

		t = split("", false)
		if t != "" {
			if !alldigits(t) {
				return fmt.Errorf("config file line %d: An optional number is required here for CAT serial port speed: %s", ps.line, t)
			}
			var n, _ = strconv.Atoi(t)
			octrl.ptt_rate = n
		}

		t = split("", false)
		if t != "" {
			ps.errorf("config file line %d: %s was not expected after model & port for hamlib", ps.line, t)
		}

		octrl.ptt_method = PTT_METHOD_HAMLIB
	} else if strings.EqualFold(t, "CM108") {
		/* CM108 - GPIO of USB sound card. case, Linux and Windows only. */

		// TODO KG #if USE_CM108
		if ot != OCTYPE_PTT {
			// Future project:  Allow DCD and CON via the same device.
			// This gets more complicated because we can't selectively change a single GPIO bit.
			// We would need to keep track of what is currently there, change one bit, in our local
			// copy of the status and then write out the byte for all of the pins.
			// Let's keep it simple with just PTT for the first stab at this.

			return fmt.Errorf("config file line %d: PTT CM108 option is only valid for PTT, not %s", ps.line, otname)
		}

		octrl.out_gpio_num = 3 // All known designs use GPIO 3.
		// User can override for special cases.
		octrl.ptt_invert = false // High for transmit.
		octrl.ptt_device = ""

		// Try to find PTT device for audio output device.
		// Simplifiying assumption is that we have one radio per USB Audio Adapter.
		// Failure at this point is not an error.
		// See if config file sets it explicitly before complaining.

		var found_ptt, find_ptt_err = cm108_find_ptt(ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)

		octrl.ptt_device = found_ptt

		if find_ptt_err != nil {
			// A device we don't recognise may still be the right one, so that
			// is advice; anything else means there is no PTT to be had here.
			if errors.Is(find_ptt_err, ErrUnknownCM108Device) {
				ps.warnf("warning: %v", find_ptt_err)
			} else {
				ps.errorf("can't automatically find matching HID for PTT: %v", find_ptt_err)
			}
		}

		for {
			t = split("", false)
			if t == "" {
				break
			}

			if t[0] == '-' {
				var gpio, _ = strconv.Atoi(t[1:])
				octrl.out_gpio_num = -1 * gpio
				octrl.ptt_invert = true
			} else if unicode.IsDigit(rune(t[0])) {
				var gpio, _ = strconv.Atoi(t)
				octrl.out_gpio_num = gpio
				octrl.ptt_invert = false
			} else if t[0] == '/' {
				octrl.ptt_device = t
			} else {
				return fmt.Errorf("config file line %d: Found \"%s\" when expecting GPIO number or device name like /dev/hidraw1", ps.line, t)
			}
		}

		if octrl.out_gpio_num < 1 || octrl.out_gpio_num > 8 {
			return fmt.Errorf("config file line %d: CM108 GPIO number %d is not in range of 1 thru 8", ps.line,
				octrl.out_gpio_num)
		}

		if octrl.ptt_device == "" {
			/* TODO KG
			#if __WIN32__
				        dw_printf ("You must explicitly mention a HID path.\n");
			#else
			*/
			return fmt.Errorf("config file line %d: Could not determine USB Audio GPIO PTT device for audio output %s\n"+
				"You must explicitly mention a device name such as /dev/hidraw1.\n"+
				"Run \"cm108\" utility to get a list.\n"+
				"See Interface Guide for details",
				ps.line, ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)
		}

		octrl.ptt_method = PTT_METHOD_CM108

		/* TODO KG
		#else
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Config file line %d: %s with CM108 is only available when USB Audio GPIO support is enabled.\n", ps.line, otname);
			      dw_printf ("You must rebuild direwolf with CM108 Audio Adapter GPIO PTT support.\n");
			      dw_printf ("See Interface Guide for details.\n");
			      rtfm();
			      exit (EXIT_FAILURE);
		#endif
		*/
	} else {
		/* serial port case. */
		octrl.ptt_device = t

		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing RTS or DTR after %s device name", ps.line, otname)
		}

		if strings.EqualFold(t, "rts") {
			octrl.ptt_line = PTT_LINE_RTS
			octrl.ptt_invert = false
		} else if strings.EqualFold(t, "dtr") {
			octrl.ptt_line = PTT_LINE_DTR
			octrl.ptt_invert = false
		} else if strings.EqualFold(t, "-rts") {
			octrl.ptt_line = PTT_LINE_RTS
			octrl.ptt_invert = true
		} else if strings.EqualFold(t, "-dtr") {
			octrl.ptt_line = PTT_LINE_DTR
			octrl.ptt_invert = true
		} else {
			return fmt.Errorf("config file line %d: Expected RTS or DTR after %s device name", ps.line, otname)
		}

		octrl.ptt_method = PTT_METHOD_SERIAL

		/* In version 1.2, we allow a second one for same serial port. */
		/* Some interfaces want the two control lines driven with opposite polarity. */
		/* e.g.   PTT COM1 RTS -DTR  */

		t = split("", false)
		if t != "" {
			if strings.EqualFold(t, "rts") {
				octrl.ptt_line2 = PTT_LINE_RTS
				octrl.ptt_invert2 = false
			} else if strings.EqualFold(t, "dtr") {
				octrl.ptt_line2 = PTT_LINE_DTR
				octrl.ptt_invert2 = false
			} else if strings.EqualFold(t, "-rts") {
				octrl.ptt_line2 = PTT_LINE_RTS
				octrl.ptt_invert2 = true
			} else if strings.EqualFold(t, "-dtr") {
				octrl.ptt_line2 = PTT_LINE_DTR
				octrl.ptt_invert2 = true
			} else {
				return fmt.Errorf("config file line %d: Expected RTS or DTR after first RTS or DTR", ps.line)
			}

			/* Would not make sense to specify the same one twice. */

			if octrl.ptt_line == octrl.ptt_line2 {
				ps.errorf("config file line %d: Doesn't make sense to specify the some control line twice", ps.line)
			}
		} /* end of second serial port control ps.line. */
	} /* end of serial port case. */
	/* end of PTT, DCD, CON */

	ps.audio.achan[ps.channel].octrl[ot] = octrl

	return nil
}

// handleTXINH handles the TXINH keyword.
func handleTXINH(ps *parseState) error {
	/*
	 * INPUTS
	 *
	 * TXINH - TX holdoff input
	 *
	 * TXINH GPIO [-]gpio-num (only type supported so far)
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: TXINH can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}
	var itname = "TXINH"

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file line %d: Missing input type name for %s command", ps.line, itname)
	}

	if strings.EqualFold(t, "GPIO") {
		/* TODO KG
		#if __WIN32__
			      text_color_set(DW_COLOR_ERROR);
			      dw_printf ("Config file line %d: %s with GPIO is only available on Linux.\n", ps.line, itname);
		#else
		*/
		t = split("", false)
		if t == "" {
			return fmt.Errorf("config file line %d: Missing GPIO number for %s", ps.line, itname)
		}

		var gpio, gpioErr = strconv.Atoi(t)
		if gpioErr != nil {
			return fmt.Errorf("config file line %d: GPIO number must be numeric for %s", ps.line, itname)
		}
		if gpio < 0 {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = true
		} else {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = false
		}

		ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
		// #endif
	} else {
		return fmt.Errorf("config file line %d: Unrecognized input type name \"%s\" for %s command.  GPIO is the only one supported", ps.line, t, itname)
	}

	return nil
}

// handleDWAIT handles the DWAIT keyword.
func handleDWAIT(ps *parseState) error {
	/*
	 * DWAIT n		- Extra delay for receiver squelch. n = 10 mS units.
	 *
	 * Why did I do this?  Just add more to TXDELAY.
	 * Now undocumented in User Guide.  Might disappear someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: DWAIT can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing delay time for DWAIT command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Delay time must be numeric for DWAIT command. Keeping %d", ps.line, ps.audio.achan[ps.channel].dwait)
	}
	if n >= 0 && n <= 255 {
		ps.audio.achan[ps.channel].dwait = n
	} else {
		ps.audio.achan[ps.channel].dwait = DEFAULT_DWAIT

		ps.errorf("line %d: Invalid delay time for DWAIT. Using %d", ps.line, ps.audio.achan[ps.channel].dwait)
	}

	return nil
}

// handleSLOTTIME handles the SLOTTIME keyword.
func handleSLOTTIME(ps *parseState) error {
	/*
	 * SLOTTIME n		- For non-digipeat transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: SLOTTIME can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing delay time for SLOTTIME command", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n < 50 {
		// 0 = User has no clue.  This would be no delay.
		// 10 = Default.
		// 50 = Half second.  User might think it is mSec and use 100.
		ps.audio.achan[ps.channel].slottime = n
	} else {
		ps.audio.achan[ps.channel].slottime = DEFAULT_SLOTTIME

		ps.errorf(
			"Line %d: Invalid delay time for persist algorithm. Using default %d.\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default?",
			ps.line,
			ps.audio.achan[ps.channel].slottime,
		)
	}

	return nil
}

// handlePERSIST handles the PERSIST keyword.
func handlePERSIST(ps *parseState) error {
	/*
	 * PERSIST 		- For non-digipeat transmit delay timing.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: PERSIST can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing probability for PERSIST command", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n <= 250 {
		ps.audio.achan[ps.channel].persist = n
	} else {
		ps.audio.achan[ps.channel].persist = DEFAULT_PERSIST

		ps.errorf(
			"Line %d: Invalid probability for persist algorithm. Using default %d.\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default?",
			ps.line,
			ps.audio.achan[ps.channel].persist,
		)
	}

	return nil
}

// handleTXDELAY handles the TXDELAY keyword.
func handleTXDELAY(ps *parseState) error {
	/*
	 * TXDELAY n		- For transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: TXDELAY can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing time for TXDELAY command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Time must be numeric for TXDELAY command. Keeping %d", ps.line, ps.audio.achan[ps.channel].txdelay)
	}
	if n >= 0 && n <= 255 {
		if n < 10 {
			ps.warnf("line %d: Setting TXDELAY this small is a REALLY BAD idea if you want other stations to hear you.\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default rather than reducing reliability?", ps.line)
		} else if n >= 100 {
			ps.warnf("line %d: Keeping with tradition, going back to the 1980s, TXDELAY is in 10 millisecond units.\n"+
				"Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default?", ps.line, ps.line, n, float64(n)*10./1000.)
		}

		ps.audio.achan[ps.channel].txdelay = n
	} else {
		ps.audio.achan[ps.channel].txdelay = DEFAULT_TXDELAY

		ps.errorf("line %d: Invalid time for transmit delay. Using %d", ps.line, ps.audio.achan[ps.channel].txdelay)
	}

	return nil
}

// handleTXTAIL handles the TXTAIL keyword.
func handleTXTAIL(ps *parseState) error {
	/*
	 * TXTAIL n		- For transmit timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: TXTAIL can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing time for TXTAIL command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Time must be numeric for TXTAIL command. Keeping %d", ps.line, ps.audio.achan[ps.channel].txtail)
	}
	if n >= 0 && n <= 255 {
		if n < 5 {
			ps.warnf("line %d: Setting TXTAIL that small is a REALLY BAD idea if you want other stations to hear you.\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default rather than reducing reliability?", ps.line)
		} else if n >= 50 {
			ps.warnf("line %d: Keeping with tradition, going back to the 1980s, TXTAIL is in 10 millisecond units.\n"+
				"Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n"+
				"Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n"+
				"section, to understand what this means.\n"+
				"Why don't you just use the default?", ps.line, ps.line, n, float64(n)*10./1000.)
		}

		ps.audio.achan[ps.channel].txtail = n
	} else {
		ps.audio.achan[ps.channel].txtail = DEFAULT_TXTAIL

		ps.errorf("line %d: Invalid time for transmit timing. Using %d", ps.line, ps.audio.achan[ps.channel].txtail)
	}

	return nil
}

// handleFULLDUP handles the FULLDUP keyword.
func handleFULLDUP(ps *parseState) error {
	/*
	 * FULLDUP  {on|off} 		- Full Duplex
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: FULLDUP can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing parameter for FULLDUP command.  Expecting ON or OFF", ps.line)
	}

	if strings.EqualFold(t, "ON") {
		ps.audio.achan[ps.channel].fulldup = true
	} else if strings.EqualFold(t, "OFF") {
		ps.audio.achan[ps.channel].fulldup = false
	} else {
		ps.audio.achan[ps.channel].fulldup = false

		ps.errorf("line %d: Expected ON or OFF for FULLDUP", ps.line)
	}

	return nil
}

// handleSPEECH handles the SPEECH keyword.
func handleSPEECH(ps *parseState) error {
	/*
	 * SPEECH  script
	 *
	 * Specify script for text-to-speech function.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: SPEECH can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing script for Text-to-Speech function", ps.line)
	}

	// Dire Wolf tried running the script here, to report a broken one at
	// startup.  xmit_speak_it does that every time it speaks instead.
	ps.audio.tts_script = t

	return nil
}

// handleFX25TX handles the FX25TX keyword.
func handleFX25TX(ps *parseState) error {
	/*
	 * FX25TX n		- Enable FX.25 transmission.  Default off.
	 *				0 = off, 1 = auto mode, others are suggestions for testing
	 *				or special cases.  16, 32, 64 is number of parity bytes to add.
	 *				Also set by "-X n" command line option.
	 *				V1.7 changed from global to per-channel setting.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: FX25TX can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing FEC mode for FX25TX command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: FEC mode must be numeric for FX25TX command. Keeping %d", ps.line, ps.audio.achan[ps.channel].fx25_strength)
	}
	if n == 0 {
		// 0 is off: -X 0 enables nothing either, though it cannot switch off
		// what the config file turned on.  Leaving the channel on LAYER2_FX25
		// would mean every frame tried FX.25 with no usable mode, complained,
		// and fell back to AX.25 anyway.
		ps.audio.achan[ps.channel].fx25_strength = 0
		if ps.audio.achan[ps.channel].layer2_xmit == LAYER2_FX25 {
			ps.audio.achan[ps.channel].layer2_xmit = LAYER2_AX25
		}
	} else if n > 0 && n < 200 {
		ps.audio.achan[ps.channel].fx25_strength = n
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25
	} else {
		ps.audio.achan[ps.channel].fx25_strength = 1
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25

		ps.errorf("line %d: Unreasonable value for FX.25 transmission mode. Using %d", ps.line, ps.audio.achan[ps.channel].fx25_strength)
	}

	return nil
}

// handleFX25AUTO handles the FX25AUTO keyword.
func handleFX25AUTO(ps *parseState) error {
	/*
	 * FX25AUTO n		- Enable Automatic use of FX.25 for connected mode.  *** Not Implemented ***
	 *				Automatically enable, for that session only, when an identical
	 *				frame is sent more than this number of times.
	 *				Default 5 based on half of default RETRY.
	 *				0 to disable feature.
	 *				Current a global setting.  Could be per channel someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: FX25AUTO can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing count for FX25AUTO command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Count must be numeric for FX25AUTO command. Keeping %d", ps.line, ps.audio.fx25_auto_enable)
	}
	if n >= 0 && n < 20 {
		ps.audio.fx25_auto_enable = n
	} else {
		ps.audio.fx25_auto_enable = AX25_N2_RETRY_DEFAULT / 2

		ps.errorf("line %d: Unreasonable count for connected mode automatic FX.25. Using %d", ps.line, ps.audio.fx25_auto_enable)
	}

	return nil
}

// handleIL2PTX handles the IL2PTX keyword.
func handleIL2PTX(ps *parseState) error {
	/*
	 * IL2PTX  [ + - ] [ 0 1 ]	- Enable IL2P transmission.  Default off.
	 *				"+" means normal polarity. Redundant since it is the default.
	 *					(command line -I for first channel)
	 *				"-" means inverted polarity. Do not use for 1200 bps.
	 *					(command line -i for first channel)
	 *				"0" means weak FEC.  Not recommended, and only v0.4
	 *					has it, so it does nothing unless IL2PVERSION 0.4.
	 *				"1" means stronger FEC.  "Max FEC."  Default if not specified.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: IL2PTX can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	ps.audio.achan[ps.channel].layer2_xmit = LAYER2_IL2P
	ps.audio.achan[ps.channel].il2p_max_fec = 1
	ps.audio.achan[ps.channel].il2p_invert_polarity = 0
	ps.audio.achan[ps.channel].il2p_crc = true

	for {
		var t = split("", false)
		if t == "" {
			break
		}

		for _, c := range t {
			switch c {
			case '+':
				ps.audio.achan[ps.channel].il2p_invert_polarity = 0
			case '-':
				ps.audio.achan[ps.channel].il2p_invert_polarity = 1
			case '0':
				ps.audio.achan[ps.channel].il2p_max_fec = 0
			case '1':
				ps.audio.achan[ps.channel].il2p_max_fec = 1
			case 'C':
				ps.audio.achan[ps.channel].il2p_crc = true
			case 'c':
				ps.audio.achan[ps.channel].il2p_crc = false
			default:
				ps.errorf("line %d: Invalid parameter '%c' for IL2PTX command", ps.line, c)

				continue
			}
		}
	}

	return nil
}

// handleIL2PVERSION handles the IL2PVERSION keyword.
func handleIL2PVERSION(ps *parseState) error {
	/*
	 * IL2PVERSION  0.4 | 0.6 | COMPAT	- IL2P protocol version, transmit and receive.
	 *				"0.6" means 16 parity symbols per payload block and
	 *					that bit is RESERVED.  Default.
	 *				"0.4" means the header FEC Level bit selects the
	 *					number of payload parity symbols.
	 *				"COMPAT" means transmit 0.4 but receive 0.6.
	 *					With max FEC, 0.4 frames are understood by both,
	 *					so use this to reach v0.4 stations as well.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		return fmt.Errorf("line %d: IL2PVERSION can only be used with radio channel 0 - %d", ps.line, MAX_RADIO_CHANS-1)
	}

	var t = split("", false)

	var version, ok = il2p_parse_version(t)
	if !ok {
		return fmt.Errorf("line %d: Invalid IL2P version '%s'.  Expected 0.4, 0.6, or COMPAT", ps.line, t)
	}

	ps.audio.achan[ps.channel].il2p_version = version

	return nil
}

// handleDIGIPEAT handles the DIGIPEAT keyword.
func handleDIGIPEAT(ps *parseState) error {
	/*
	 * ==================== APRS Digipeater parameters ====================
	 */

	/*
	 * DIGIPEAT  from-chan  to-chan  alias-pattern  wide-pattern  [ OFF|DROP|MARK|TRACE | ATGP=alias ]
	 *
	 * ATGP is an ugly hack for the specific need of ATGP which needs more that 8 digipeaters.
	 * DO NOT put this in the User Guide.  On a need to know basis.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing FROM-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number", ps.line, t)
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS {
		return fmt.Errorf("config file: FROM-channel must be in range of 0 to %d on line %d", MAX_TOTAL_CHANS-1, ps.line)
	}

	// Channels specified must be radio channels or network TNCs.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
		return fmt.Errorf("config file, line %d: FROM-channel %d is not valid", ps.line, from_chan)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing TO-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for TO-channel.  It must be a number", ps.line, t)
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS {
		return fmt.Errorf("config file: TO-channel must be in range of 0 to %d on line %d", MAX_TOTAL_CHANS-1, ps.line)
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
		return fmt.Errorf("config file, line %d: TO-channel %d is not valid", ps.line, to_chan)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing alias pattern on line %d", ps.line)
	}

	var r, err = regexp.Compile(t)
	if err != nil {
		return fmt.Errorf("config file: Invalid alias matching pattern on line %d:\n%w", ps.line, err)
	}

	ps.digi.alias[from_chan][to_chan] = r

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing wide pattern on line %d", ps.line)
	}

	r, err = regexp.Compile(t)
	if err != nil {
		return fmt.Errorf("config file: Invalid wide matching pattern on line %d:\n%w", ps.line, err)
	}

	ps.digi.wide[from_chan][to_chan] = r

	ps.digi.enabled[from_chan][to_chan] = true
	ps.digi.preempt[from_chan][to_chan] = PREEMPT_OFF

	t = split("", false)
	if t != "" {
		if strings.EqualFold(t, "OFF") {
			ps.digi.preempt[from_chan][to_chan] = PREEMPT_OFF
			t = split("", false)
		} else if strings.EqualFold(t, "DROP") {
			ps.errorf(
				"Config file, line %d: Preemptive digipeating DROP option is discouraged.\nIt can create a via path which is misleading about the actual path taken.\nPREEMPT is the best choice for this feature.",
				ps.line,
			)

			ps.digi.preempt[from_chan][to_chan] = PREEMPT_DROP
			t = split("", false)
		} else if strings.EqualFold(t, "MARK") {
			ps.errorf(
				"Config file, line %d: Preemptive digipeating MARK option is discouraged.\nIt can create a via path which is misleading about the actual path taken.\nPREEMPT is the best choice for this feature.",
				ps.line,
			)

			ps.digi.preempt[from_chan][to_chan] = PREEMPT_MARK
			t = split("", false)
		} else if (strings.EqualFold(t, "TRACE")) || (strings.HasPrefix(strings.ToUpper(t), "PREEMPT")) {
			ps.digi.preempt[from_chan][to_chan] = PREEMPT_TRACE
			t = split("", false)
		} else if strings.HasPrefix(strings.ToUpper(t), "ATGP=") {
			ps.digi.atgp[from_chan][to_chan] = t[5:]
			t = split("", false)
		}
	}

	if t != "" {
		ps.errorf("config file, line %d: Found \"%s\" where end of line was expected", ps.line, t)
	}

	return nil
}

// handleDEDUPE handles the DEDUPE keyword.
func handleDEDUPE(ps *parseState) error {
	/*
	 * DEDUPE 		- Time to suppress digipeating of duplicate APRS packets.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing time for DEDUPE command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Time must be numeric for DEDUPE command. Keeping %d", ps.line, ps.digi.dedupe_time)
	}
	if n >= 0 && n < 600 {
		ps.digi.dedupe_time = n
	} else {
		ps.digi.dedupe_time = DEFAULT_DEDUPE

		ps.errorf("line %d: Unreasonable value for dedupe time. Using %d", ps.line, ps.digi.dedupe_time)
	}

	return nil
}

// handleREGEN handles the REGEN keyword.
func handleREGEN(ps *parseState) error {
	/*
	 * REGEN 		- Signal regeneration.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing FROM-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number", ps.line, t)
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		return fmt.Errorf("config file: FROM-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	// Only radio channels are valid for regenerate.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: FROM-channel %d is not valid", ps.line, from_chan)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing TO-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for TO-channel.  It must be a number", ps.line, t)
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		return fmt.Errorf("config file: TO-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: TO-channel %d is not valid", ps.line, to_chan)
	}

	ps.digi.regen[from_chan][to_chan] = true

	return nil
}

// handleCDIGIPEAT handles the CDIGIPEAT keyword.
func handleCDIGIPEAT(ps *parseState) error {
	/*
	 * ==================== Connected Digipeater parameters ====================
	 */

	/*
	 * CDIGIPEAT  from-chan  to-chan [ alias-pattern ]
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing FROM-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number", ps.line, t)
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		return fmt.Errorf("config file: FROM-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	// For connected mode Link layer, only internal modems should be allowed.
	// A network TNC probably would not provide information about channel status.
	// There is discussion about this in the document called
	// Why-is-9600-only-twice-as-fast-as-1200.pdf

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: FROM-channel %d is not valid.\nOnly internal modems can be used for connected mode packet", ps.line, from_chan)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing TO-channel on line %d", ps.line)
	}

	if !alldigits(t) {
		return fmt.Errorf("config file, line %d: '%s' is not allowed for TO-channel.  It must be a number", ps.line, t)
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		return fmt.Errorf("config file: TO-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: TO-channel %d is not valid.\nOnly internal modems can be used for connected mode packet", ps.line, to_chan)
	}

	t = split("", false)
	if t != "" {
		var r, err = regexp.Compile(t)
		if err == nil {
			ps.cdigi.alias[from_chan][to_chan] = r
			ps.cdigi.has_alias[from_chan][to_chan] = true
		} else {
			return fmt.Errorf("config file: Invalid alias matching pattern on line %d:\n%w", ps.line, err)
		}

		t = split("", false)
	}

	ps.cdigi.enabled[from_chan][to_chan] = true

	if t != "" {
		ps.errorf("config file, line %d: Found \"%s\" where end of line was expected", ps.line, t)
	}

	return nil
}

// handleFILTER handles the FILTER keyword.
func handleFILTER(ps *parseState) error {
	/*
	 * ==================== Packet Filtering for APRS digipeater or IGate ====================
	 */

	/*
	 * FILTER  from-chan  to-chan  filter_specification_expression
	 * FILTER  from-chan  IG       filter_specification_expression
	 * FILTER  IG         to-chan  filter_specification_expression
	 *
	 *
	 * Note that we have three different config file filter commands:
	 *
	 *	FILTER		- Originally for APRS digipeating but later enhanced
	 *			  to include IGate client side.  Maybe it should be
	 *			  renamed AFILTER to make it clearer after adding CFILTER.
	 *
	 *			  Both internal modem and NET TNC channels allowed here.
	 *			  "IG" should be used for the IGate, NOT a virtual channel
	 *			  assigned to it.
	 *
	 *	CFILTER		- Similar for connected moded digipeater.
	 *
	 *			  Only internal modems can be used because they provide
	 *			  information about radio channel status.
	 *			  A remote network TNC might not provide the necessary
	 *			  status for correct operation.
	 *			  There is discussion about this in the document called
	 *			  Why-is-9600-only-twice-as-fast-as-1200.pdf
	 *
	 *	IGFILTER	- APRS-IS (IGate) server side - completely different.
	 *			  I'm not happy with this name because IG sounds like IGate
	 *			  which is really the client side.  More comments later.
	 *			  Maybe it should be called subscribe or something like that
	 *			  because the subscriptions are cumulative.
	 */
	var from_chan int
	var to_chan int

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing FROM-channel on line %d", ps.line)
	}

	if t[0] == 'i' || t[0] == 'I' {
		from_chan = MAX_TOTAL_CHANS

		ps.warnf(
			"Config file: FILTER IG ... on line %d.\n"+
				"Warning! Don't mess with IS>RF filtering unless you are an expert and have an unusual situation.\n"+
				"Warning! The default is fine for nearly all situations.\n"+
				"Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .\n"+
				"Warning! If you insist, be sure to add \" | i/180 \" so you don't break messaging.",
			ps.line,
		)
	} else {
		var fromChanErr error

		from_chan, fromChanErr = strconv.Atoi(t)
		if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS || fromChanErr != nil {
			return fmt.Errorf("config file: Filter FROM-channel must be in range of 0 to %d or \"IG\" on line %d", MAX_TOTAL_CHANS-1, ps.line)
		}

		if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
			return fmt.Errorf("config file, line %d: FROM-channel %d is not valid", ps.line, from_chan)
		}

		if ps.audio.chan_medium[from_chan] == MEDIUM_IGATE {
			return fmt.Errorf("config file, line %d: Use 'IG' rather than %d for FROM-channel", ps.line, from_chan)
		}
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing TO-channel on line %d", ps.line)
	}

	if t[0] == 'i' || t[0] == 'I' {
		to_chan = MAX_TOTAL_CHANS

		ps.warnf(
			"Config file: FILTER ... IG ... on line %d.\n"+
				"Warning! Don't mess with RF>IS filtering unless you are an expert and have an unusual situation.\n"+
				"Warning! Expected behavior is for everything to go from RF to IS.\n"+
				"Warning! The default is fine for nearly all situations.\n"+
				"Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .",
			ps.line,
		)
	} else {
		var toChanErr error

		to_chan, toChanErr = strconv.Atoi(t)
		if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS || toChanErr != nil {
			return fmt.Errorf("config file: Filter TO-channel must be in range of 0 to %d or \"IG\" on line %d", MAX_TOTAL_CHANS-1, ps.line)
		}

		if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
			return fmt.Errorf("config file, line %d: TO-channel %d is not valid", ps.line, to_chan)
		}

		if ps.audio.chan_medium[to_chan] == MEDIUM_IGATE {
			return fmt.Errorf("config file, line %d: Use 'IG' rather than %d for TO-channel", ps.line, to_chan)
		}
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	var err = pfilter_validate(from_chan, to_chan, t, true)
	if err != nil {
		return fmt.Errorf("config file, line %d: Invalid FILTER expression:\n%w", ps.line, err)
	}

	if ps.digi.filter_str[from_chan][to_chan] != "" {
		ps.errorf("config file, line %d: Replacing previous filter for same from/to pair:\n        %s", ps.line, ps.digi.filter_str[from_chan][to_chan])
		ps.digi.filter_str[from_chan][to_chan] = ""
	}

	ps.digi.filter_str[from_chan][to_chan] = t

	return nil
}

// handleCFILTER handles the CFILTER keyword.
func handleCFILTER(ps *parseState) error {
	/*
	 * ==================== Packet Filtering for connected digipeater ====================
	 */

	/*
	 * CFILTER  from-chan  to-chan  filter_specification_expression
	 *
	 * Why did I put this here?
	 * What would be a useful use case?  Perhaps block by source or destination?
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing FROM-channel on line %d", ps.line)
	}

	var from_chan, fromChanErr = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS || fromChanErr != nil {
		return fmt.Errorf("config file: Filter FROM-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	// DO NOT allow a network TNC here.
	// Must be internal modem to have necessary knowledge about channel status.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: FROM-channel %d is not valid", ps.line, from_chan)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing TO-channel on line %d", ps.line)
	}

	var to_chan, toChanErr = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS || toChanErr != nil {
		return fmt.Errorf("config file: Filter TO-channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: TO-channel %d is not valid", ps.line, to_chan)
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	var err = pfilter_validate(from_chan, to_chan, t, false)
	if err != nil {
		return fmt.Errorf("config file, line %d: Invalid CFILTER expression:\n%w", ps.line, err)
	}

	ps.cdigi.cfilter_str[from_chan][to_chan] = t

	return nil
}

// handleTTCORRAL handles the TTCORRAL keyword.
func handleTTCORRAL(ps *parseState) error {
	/*
	 * ==================== APRStt gateway ====================
	 */

	/*
	 * TTCORRAL 		- How to handle unknown positions
	 *
	 * TTCORRAL  latitude  longitude  offset-or-ambiguity
	 */

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing latitude for TTCORRAL command", ps.line)
	}
	ps.tt.corral_lat = ps.parseLL(t, LAT)

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing longitude for TTCORRAL command", ps.line)
	}
	ps.tt.corral_lon = ps.parseLL(t, LON)

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing offset-or-ambiguity for TTCORRAL command", ps.line)
	}
	ps.tt.corral_offset = ps.parseLL(t, LAT)
	if ps.tt.corral_offset == 1 ||
		ps.tt.corral_offset == 2 ||
		ps.tt.corral_offset == 3 {
		ps.tt.corral_ambiguity = int(ps.tt.corral_offset)
		ps.tt.corral_offset = 0
	}

	// dw_printf ("DEBUG: corral %f %f %f %d\n", p_tt_config.corral_lat,
	//
	//	p_tt_config.corral_lon, p_tt_config.corral_offset, p_tt_config.corral_ambiguity);
	return nil
}

// handleTTPOINT handles the TTPOINT keyword.
func handleTTPOINT(ps *parseState) error {
	/*
	 * TTPOINT 		- Define a point represented by touch tone sequence.
	 *
	 * TTPOINT   pattern  latitude  longitude
	 */

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_POINT

	// Pattern: B and digits

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTPOINT command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		ps.errorf("line %d: TTPOINT pattern must begin with upper case 'B'", ps.line)
	}

	for _, j := range t[1:] {
		if !unicode.IsDigit(j) {
			ps.errorf("line %d: TTPOINT pattern must be B and digits only", ps.line)
		}
	}

	// Latitude

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing latitude for TTPOINT command", ps.line)
	}
	tl.point.lat = ps.parseLL(t, LAT)

	// Longitude

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing longitude for TTPOINT command", ps.line)
	}
	tl.point.lon = ps.parseLL(t, LON)

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTVECTOR handles the TTVECTOR keyword.
func handleTTVECTOR(ps *parseState) error {
	/*
	 * TTVECTOR 		- Touch tone location with bearing and distance.
	 *
	 * TTVECTOR   pattern  latitude  longitude  scale  unit
	 */

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_VECTOR
	tl.pattern = ""
	tl.vector.lat = 0
	tl.vector.lon = 0
	tl.vector.scale = 1

	// Pattern: B5bbbd...

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTVECTOR command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		ps.errorf("line %d: TTVECTOR pattern must begin with upper case 'B'", ps.line)
	}
	if !strings.HasPrefix(t[1:], "5bbb") {
		ps.errorf("line %d: TTVECTOR pattern would normally contain \"5bbb\"", ps.line)
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'b' && t[j] != 'd' {
			ps.errorf("line %d: TTVECTOR pattern must contain only B, digits, b, and d", ps.line)
		}
	}

	// Latitude

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing latitude for TTVECTOR command", ps.line)
	}
	tl.vector.lat = ps.parseLL(t, LAT)

	// Longitude

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing longitude for TTVECTOR command", ps.line)
	}
	tl.vector.lon = ps.parseLL(t, LON)

	// Longitude

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing scale for TTVECTOR command", ps.line)
	}
	var scale, scaleErr = strconv.ParseFloat(t, 64)
	if scaleErr != nil {
		return fmt.Errorf("line %d: Invalid scale \"%s\" for TTVECTOR command", ps.line, t)
	}

	// Unit.

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing unit for TTVECTOR command", ps.line)
	}

	var meters float64
	for j := 0; j < len(units) && meters == 0; j++ {
		if strings.EqualFold(units[j].name, t) {
			meters = units[j].meters
		}
	}
	if meters == 0 {
		ps.errorf("line %d: Unrecognized unit for TTVECTOR command.  Using miles", ps.line)
		meters = 1609.344
	}
	tl.vector.scale = scale * meters

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTGRID handles the TTGRID keyword.
func handleTTGRID(ps *parseState) error {
	/*
	 * TTGRID 		- Define a grid for touch tone locations.
	 *
	 * TTGRID   pattern  min-latitude  min-longitude  max-latitude  max-longitude
	 */

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_GRID

	// Pattern: B [digit] x... y...

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTGRID command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		ps.errorf("line %d: TTGRID pattern must begin with upper case 'B'", ps.line)
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			ps.errorf("line %d: TTGRID pattern must be B, optional digit, xxx, yyy", ps.line)
		}
	}

	// Minimum Latitude - all zeros in received data

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing minimum latitude for TTGRID command", ps.line)
	}
	tl.grid.lat0 = ps.parseLL(t, LAT)

	// Minimum Longitude - all zeros in received data

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing minimum longitude for TTGRID command", ps.line)
	}
	tl.grid.lon0 = ps.parseLL(t, LON)

	// Maximum Latitude - all nines in received data

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing maximum latitude for TTGRID command", ps.line)
	}
	tl.grid.lat9 = ps.parseLL(t, LAT)

	// Maximum Longitude - all nines in received data

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing maximum longitude for TTGRID command", ps.line)
	}
	tl.grid.lon9 = ps.parseLL(t, LON)

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTUTM handles the TTUTM keyword.
func handleTTUTM(ps *parseState) error {
	/*
	 * TTUTM 		- Specify UTM zone for touch tone locations.
	 *
	 * TTUTM   pattern  zone [ scale [ x-offset y-offset ] ]
	 */

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_UTM
	tl.utm.scale = 1

	// Pattern: B [digit] x... y...

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTUTM command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		return fmt.Errorf("line %d: TTUTM pattern must begin with upper case 'B'", ps.line)
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			ps.errorf("line %d: TTUTM pattern must be B, optional digit, xxx, yyy", ps.line)
			// Bail out somehow.  continue would match inner for.
		}
	}

	// Zone 1 - 60 and optional latitudinal letter.

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing zone for TTUTM command", ps.line)
	}

	tl.utm.latband, tl.utm.hemi, tl.utm.lzone = ps.parseUTMZone(t)

	// Optional scale.

	t = split("", false)
	if t != "" {
		var scaleVal, scaleErr = strconv.ParseFloat(t, 64)
		if scaleErr != nil {
			return fmt.Errorf("line %d: Invalid scale \"%s\" for TTUTM command", ps.line, t)
		}

		tl.utm.scale = scaleVal

		// Optional x offset.

		t = split("", false)
		if t != "" {
			var xOffset, xErr = strconv.ParseFloat(t, 64)
			if xErr != nil {
				return fmt.Errorf("line %d: Invalid x offset \"%s\" for TTUTM command", ps.line, t)
			}

			tl.utm.x_offset = xOffset

			// Optional y offset.

			t = split("", false)
			if t != "" {
				var yOffset, yErr = strconv.ParseFloat(t, 64)
				if yErr != nil {
					return fmt.Errorf("line %d: Invalid y offset \"%s\" for TTUTM command", ps.line, t)
				}

				tl.utm.y_offset = yOffset
			}
		}
	}

	// Practice run to see if conversion might fail later with actual location.

	var utm = coordconv.UTMCoord{
		Zone:       tl.utm.lzone,
		Hemisphere: HemisphereRuneToCoordconvHemisphere(tl.utm.hemi),
		Easting:    tl.utm.x_offset + 5*tl.utm.scale,
		Northing:   tl.utm.y_offset + 5*tl.utm.scale,
	}
	var _, geoErr = coordconv.DefaultUTMConverter.ConvertToGeodetic(utm)

	if geoErr != nil {
		return fmt.Errorf("line %d: Invalid UTM location: \n%w", ps.line, geoErr)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTUSNGMGRS handles the TTUSNGMGRS keyword.
func handleTTUSNGMGRS(ps *parseState) error {
	/*
	 * TTUSNG, TTMGRS 		- Specify zone/square for touch tone locations.
	 *
	 * TTUSNG   pattern  zone_square
	 * TTMGRS   pattern  zone_square
	 */

	var tl = new(ttloc_s)

	// TODO1.2: in progress...
	if strings.EqualFold(ps.keyword, "TTMGRS") {
		tl.ttlocType = TTLOC_MGRS
	} else {
		tl.ttlocType = TTLOC_USNG
	}

	// Pattern: B [digit] x... y...

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTUSNG/TTMGRS command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		return fmt.Errorf("line %d: TTUSNG/TTMGRS pattern must begin with upper case 'B'", ps.line)
	}
	var num_x = 0
	var num_y = 0
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			ps.errorf("line %d: TTUSNG/TTMGRS pattern must be B, optional digit, xxx, yyy", ps.line)
			// Bail out somehow.  continue would match inner for.
		}
		if t[j] == 'x' {
			num_x++
		}
		if t[j] == 'y' {
			num_y++
		}
	}
	if num_x < 1 || num_x > 5 || num_x != num_y {
		return fmt.Errorf("line %d: TTUSNG/TTMGRS must have 1 to 5 x and same number y", ps.line)
	}

	// Zone 1 - 60 and optional latitudinal letter.

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing zone & square for TTUSNG/TTMGRS command", ps.line)
	}
	tl.mgrs.zone = t

	// Try converting it rather do our own error checking.

	var _, convertErr = coordconv.DefaultMGRSConverter.ConvertToGeodetic(tl.mgrs.zone)
	if convertErr != nil {
		return fmt.Errorf("line %d: Invalid USNG/MGRS zone & square:  %s\n%w", ps.line, tl.mgrs.zone, convertErr)
	}

	// Should be the end.

	t = split("", false)
	if t != "" {
		ps.errorf("line %d: Unexpected stuff at end ignored:  %s", ps.line, t)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTMHEAD handles the TTMHEAD keyword.
func handleTTMHEAD(ps *parseState) error {
	/*
	 * TTMHEAD 		- Define pattern to be used for Maidenhead Locator.
	 *
	 * TTMHEAD   pattern   [ prefix ]
	 *
	 *			Pattern would be  B[0-9A-D]xxxx...
	 *			Optional prefix is 10, 6, or 4 digits.
	 *
	 *			The total number of digits in both must be 4, 6, 10, or 12.
	 */

	// TODO1.3:  TTMHEAD needs testing.

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_MHEAD

	// Pattern: B, optional additional button, some number of xxxx... for matching

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTMHEAD command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		return fmt.Errorf("line %d: TTMHEAD pattern must begin with upper case 'B'", ps.line)
	}

	// Optionally one of 0-9ABCD

	var j int
	if len(t) > 1 && (strings.ContainsRune("ABCD", rune(t[1])) || unicode.IsDigit(rune(t[1]))) {
		j = 2
	} else {
		j = 1
	}

	var count_x = 0
	var count_other = 0
	for k := j; k < len(t); k++ {
		if t[k] == 'x' {
			count_x++
		} else {
			count_other++
		}
	}

	if count_other != 0 {
		return fmt.Errorf("line %d: TTMHEAD must have only lower case x to match received data", ps.line)
	}

	// optional prefix

	t = split("", false)
	if t != "" {
		tl.mhead.prefix = t

		if !alldigits(t) || (len(t) != 4 && len(t) != 6 && len(t) != 10) {
			return fmt.Errorf("line %d: TTMHEAD prefix must be 4, 6, or 10 digits", ps.line)
		}

		var _, mhErrors = TTMheadToText(t, false)
		if mhErrors != 0 {
			return fmt.Errorf("line %d: TTMHEAD prefix not a valid DTMF sequence", ps.line)
		}
	}

	var k = len(tl.mhead.prefix) + count_x

	if k != 4 && k != 6 && k != 10 && k != 12 {
		return fmt.Errorf("line %d: TTMHEAD prefix and user data must have a total of 4, 6, 10, or 12 digits", ps.line)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTSATSQ handles the TTSATSQ keyword.
func handleTTSATSQ(ps *parseState) error {
	/*
	 * TTSATSQ 		- Define pattern to be used for Satellite square.
	 *
	 * TTSATSQ   pattern
	 *
	 *			Pattern would be  B[0-9A-D]xxxx
	 *
	 *			Must have exactly 4 x.
	 */

	// TODO1.2:  TTSATSQ To be continued...

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_SATSQ

	// Pattern: B, optional additional button, exactly xxxx for matching

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTSATSQ command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		return fmt.Errorf("line %d: TTSATSQ pattern must begin with upper case 'B'", ps.line)
	}

	// Optionally one of 0-9ABCD

	var j int
	if len(t) > 1 && (strings.ContainsRune("ABCD", rune(t[1])) || unicode.IsDigit(rune(t[1]))) {
		j = 2
	} else {
		j = 1
	}

	if t[j:] != "xxxx" {
		return fmt.Errorf("line %d: TTSATSQ pattern must end with exactly xxxx in lower case", ps.line)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTAMBIG handles the TTAMBIG keyword.
func handleTTAMBIG(ps *parseState) error {
	/*
	 * TTAMBIG 		- Define pattern to be used for Object Location Ambiguity.
	 *
	 * TTAMBIG   pattern
	 *
	 *			Pattern would be  B[0-9A-D]x
	 *
	 *			Must have exactly one x.
	 */

	// TODO1.3:  TTAMBIG To be continued...

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_AMBIG

	// Pattern: B, optional additional button, exactly x for matching

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTAMBIG command", ps.line)
	}
	tl.pattern = t

	if t[0] != 'B' {
		return fmt.Errorf("line %d: TTAMBIG pattern must begin with upper case 'B'", ps.line)
	}

	// Optionally one of 0-9ABCD

	var j int
	if len(t) > 1 && (strings.ContainsRune("ABCD", rune(t[1])) || unicode.IsDigit(rune(t[1]))) {
		j = 2
	} else {
		j = 1
	}

	if t[j:] != "x" {
		return fmt.Errorf("line %d: TTAMBIG pattern must end with exactly one x in lower case", ps.line)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)

	return nil
}

// handleTTMACRO handles the TTMACRO keyword.
func handleTTMACRO(ps *parseState) error {
	/*
	 * TTMACRO 		- Define compact message format with full expansion
	 *
	 * TTMACRO   pattern  definition
	 *
	 *		pattern can contain:
	 *			0-9 which must match exactly.
	 *				In version 1.2, also allow A,B,C,D for exact match.
	 *			x, y, z which are used for matching of variable fields.
	 *
	 *		definition can contain:
	 *			0-9, A, B, C, D, *, #, x, y, z.
	 *			Not sure why # was included in there.
	 *
	 *	    new for version 1.3 - in progress
	 *
	 *			AA{objname}
	 *			AB{symbol}
	 *			AC{call}
	 *
	 *		These provide automatic conversion from plain text to the TT encoding.
	 *
	 */

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_MACRO

	// Pattern: Any combination of digits, x, y, and z.
	// Also make note of which letters are used in pattern and definition.
	// Version 1.2: also allow A,B,C,D in the pattern.

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing pattern for TTMACRO command", ps.line)
	}
	tl.pattern = t

	var p_count [3]int
	var tt_error = 0

	for j := range len(t) {
		if !strings.ContainsRune("0123456789ABCDxyz", rune(t[j])) {
			ps.errorf("line %d: TTMACRO pattern can contain only digits, A, B, C, D, and lower case x, y, or z", ps.line)
			tt_error++

			break
		}
		// Count how many x, y, z in the pattern.
		if t[j] >= 'x' && t[j] <= 'z' {
			p_count[t[j]-'x']++
		}
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Line %d: TTMACRO pattern \"%s\" p_count = %d %d %d.\n", line, t, p_count[0], p_count[1], p_count[2]);

	// Next we should find the definition.
	// It can contain touch tone characters and lower case x, y, z for substitutions.

	t = split("", true)
	if t == "" {
		ps.errorf("line %d: Missing definition for TTMACRO command", ps.line)
		tl.macro.definition = "" // Don't die on null pointer later.

		return nil
	}

	// Make a pass over the definition, looking for the xx{...} substitutions.
	// These are done just once when reading the configuration file.

	var tmp = t               // Chomp through this
	var otemp strings.Builder // Result after any substitution

	tmp = strings.TrimSpace(tmp)

	for len(tmp) != 0 {
		if strings.HasPrefix(tmp, "AC{") {
			// Convert to fixed length 10 digit callsign.
			tmp = tmp[3:]
			var stemp strings.Builder
			for len(tmp) > 0 && tmp[0] != '}' && tmp[0] != '*' {
				stemp.WriteString(string(tmp[0]))
				tmp = tmp[1:]
			}
			if len(tmp) > 0 && tmp[0] == '}' {
				var ttemp, errs = TTTextToCall10(stemp.String(), false)
				if errs == 0 {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("DEBUG Line %d: AC{%s} -> AC%s\n", line, stemp, ttemp);
					otemp.WriteString("AC" + ttemp)
				} else {
					ps.errorf("line %d: AC{%s} could not be converted to tones for callsign", ps.line, stemp.String())
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				ps.errorf("line %d: AC{... is missing matching } in TTMACRO definition", ps.line)
				tt_error++
			}
		} else if strings.HasPrefix(tmp, "AA{") {
			// Convert to object name.

			tmp = tmp[3:]
			var sb strings.Builder
			for len(tmp) > 0 && tmp[0] != '}' && tmp[0] != '*' {
				sb.WriteByte(tmp[0])
				tmp = tmp[1:]
			}
			var stemp = sb.String()
			if len(tmp) > 0 && tmp[0] == '}' {
				if len(stemp) > 9 {
					ps.errorf("line %d: Object name %s has been truncated to 9 characters", ps.line, stemp)
					stemp = stemp[:9]
				}
				var ttemp, errs = TTTextToTwoKey(stemp, false)
				if errs == 0 {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("DEBUG Line %d: AA{%s} -> AA%s\n", line, stemp, ttemp);
					otemp.WriteString("AA" + ttemp)
				} else {
					ps.errorf("line %d: AA{%s} could not be converted to tones for object name", ps.line, stemp)
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				ps.errorf("line %d: AA{... is missing matching } in TTMACRO definition", ps.line)
				tt_error++
			}
		} else if strings.HasPrefix(tmp, "AB{") {
			// Attempt conversion from description to symbol code.

			tmp = tmp[3:]
			var stemp strings.Builder
			for len(tmp) > 0 && tmp[0] != '}' && tmp[0] != '*' {
				stemp.WriteString(string(tmp[0]))
				tmp = tmp[1:]
			}
			if len(tmp) > 0 && tmp[0] == '}' {
				// First try to find something matching the description.

				var symtab, symbol, ok = aprsSymbolData.symbols_code_from_description(' ', stemp.String())

				if !ok {
					ps.errorf("line %d: Couldn't convert \"%s\" to APRS symbol code.  Using default", ps.line, stemp.String())
					symtab = '\\' // Alternate
					symbol = 'A'  // Box
				}

				// Convert symtab(overlay) & symbol to tone sequence.

				var ttemp = aprsSymbolData.symbols_to_tones(symtab, symbol)

				//text_color_set(DW_COLOR_DEBUG);
				//dw_printf ("DEBUG config file Line %d: AB{%s} -> %s\n", line, stemp, ttemp);

				otemp.WriteString(ttemp)
				tmp = tmp[1:]
			} else {
				ps.errorf("line %d: AB{... is missing matching } in TTMACRO definition", ps.line)
				tt_error++
			}
		} else if strings.HasPrefix(tmp, "CA{") {
			// Convert to enhanced comment that can contain any ASCII character.

			tmp = tmp[3:]
			var stemp strings.Builder
			for len(tmp) > 0 && tmp[0] != '}' && tmp[0] != '*' {
				stemp.WriteString(string(tmp[0]))
				tmp = tmp[1:]
			}
			if len(tmp) > 0 && tmp[0] == '}' {
				var ttemp, errs = tt_text_to_ascii2d(stemp.String(), false)
				if errs == 0 {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("DEBUG Line %d: CA{%s} -> CA%s\n", line, stemp, ttemp);
					otemp.WriteString("CA" + ttemp)
				} else {
					ps.errorf("line %d: CA{%s} could not be converted to tones for enhanced comment", ps.line, stemp.String())
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				ps.errorf("line %d: CA{... is missing matching } in TTMACRO definition", ps.line)
				tt_error++
			}
		} else if strings.ContainsRune("0123456789ABCD*#xyz", rune(tmp[0])) {
			otemp.WriteString(string(tmp[0]))
			tmp = tmp[1:]
		} else {
			ps.errorf("line %d: TTMACRO definition can contain only 0-9, A, B, C, D, *, #, x, y, z", ps.line)
			tt_error++
			tmp = tmp[1:]
		}
	}

	// Make sure that number of x, y, z, in pattern and definition match.

	var d_count [3]int

	var otempStr = otemp.String()
	for j := range len(otempStr) {
		if otempStr[j] >= 'x' && otempStr[j] <= 'z' {
			d_count[otempStr[j]-'x']++
		}
	}

	// A little validity checking.

	for j := range 3 {
		if p_count[j] > 0 && d_count[j] == 0 {
			ps.errorf("line %d: '%c' is in TTMACRO pattern but is not used in definition", ps.line, 'x'+j)
		}
		if d_count[j] > 0 && p_count[j] == 0 {
			ps.errorf("line %d: '%c' is referenced in TTMACRO definition but does not appear in the pattern", ps.line, 'x'+j)
		}
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("DEBUG Config Line %d: %s -> %s\n", line, t, otemp);

	if tt_error == 0 {
		tl.macro.definition = otemp.String()
	}

	if tt_error == 0 {
		ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	} else {
		// Each of those errors was reported and counted as it was found, so
		// this says what became of the line rather than being a complaint of
		// its own.
		dw_printf("Line %d: Errors found in TTMACRO, skipping.\n", ps.line)
	}

	return nil
}

// handleTTOBJ handles the TTOBJ keyword.
func handleTTOBJ(ps *parseState) error {
	/*
	 * TTOBJ 		- TT Object Report options.
	 *
	 * TTOBJ  recv-chan  where-to  [ via-path ]
	 *
	 *	whereto is any combination of transmit channel, APP, IG.
	 */

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing DTMF receive channel for TTOBJ command", ps.line)
	}

	var r, rErr = strconv.Atoi(t)
	if r < 0 || r > MAX_RADIO_CHANS-1 || rErr != nil {
		return fmt.Errorf("config file: DTMF receive channel must be in range of 0 to %d on line %d", MAX_RADIO_CHANS-1, ps.line)
	}

	// I suppose we need internal modem channel here.
	// otherwise a DTMF decoder would not be available.

	if ps.audio.chan_medium[r] != MEDIUM_RADIO {
		return fmt.Errorf("config file, line %d: TTOBJ DTMF receive channel %d is not valid", ps.line, r)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing transmit channel for TTOBJ command", ps.line)
	}

	// Can have any combination of channel number, APP, IG, separated by commas.
	// Parse as a comma-separated list so multi-digit channels (>= 10) work correctly.

	var x = -1
	var app = 0
	var ig = 0
	var whereToValid = true

	for part := range strings.SplitSeq(t, ",") {
		part = strings.TrimSpace(part)
		if strings.EqualFold(part, "APP") || strings.EqualFold(part, "A") {
			app = 1
		} else if strings.EqualFold(part, "IG") || strings.EqualFold(part, "I") {
			ig = 1
		} else {
			var chanNum, chanErr = strconv.Atoi(part)
			if chanErr == nil {
				x = chanNum
				if x < 0 || x > MAX_TOTAL_CHANS-1 {
					ps.errorf("config file: Transmit channel must be in range of 0 to %d on line %d", MAX_TOTAL_CHANS-1, ps.line)
					x = -1
					whereToValid = false
				} else if ps.audio.chan_medium[x] != MEDIUM_RADIO &&
					ps.audio.chan_medium[x] != MEDIUM_NETTNC {
					ps.errorf("config file, line %d: TTOBJ transmit channel %d is not valid", ps.line, x)
					x = -1
					whereToValid = false
				}
			} else {
				/* Check for legacy compact form like "APPIG" or "AIG" accepted by the old parser. */
				var isLegacy = true
				for _, c := range part {
					if !unicode.IsDigit(c) && !strings.ContainsRune("aApPiIgG", c) {
						isLegacy = false

						break
					}
				}
				if isLegacy {
					for _, c := range part {
						switch {
						case c == 'a' || c == 'A':
							app = 1
						case c == 'i' || c == 'I':
							ig = 1
						case unicode.IsDigit(c):
							x = int(c - '0')
							if x < 0 || x > MAX_TOTAL_CHANS-1 {
								ps.errorf("config file: Transmit channel must be in range of 0 to %d on line %d", MAX_TOTAL_CHANS-1, ps.line)
								x = -1
								whereToValid = false
							} else if ps.audio.chan_medium[x] != MEDIUM_RADIO &&
								ps.audio.chan_medium[x] != MEDIUM_NETTNC {
								ps.errorf("config file, line %d: TTOBJ transmit channel %d is not valid", ps.line, x)
								x = -1
								whereToValid = false
							}
						}
					}
				} else {
					ps.errorf("config file, line %d: Expected comma separated list with some combination of transmit channel, APP, and IG", ps.line)
					whereToValid = false
				}
			}
		}
	}

	if !whereToValid {
		return nil
	}

	// This enables the DTMF decoder on the specified channel.
	// Additional channels can be enabled with the DTMF command.
	// Note that DTMF command does not enable the APRStt gateway.

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Debug TTOBJ r=%d, x=%d, app=%d, ig=%d\n", r, x, app, ig);

	ps.audio.achan[r].dtmf_decode = DTMF_DECODE_ON
	ps.tt.gateway_enabled = 1
	ps.tt.obj_recv_chan = r
	ps.tt.obj_xmit_chan = x
	ps.tt.obj_send_to_app = app
	ps.tt.obj_send_to_ig = ig

	t = split("", false)
	if t != "" {
		if ps.checkViaPath(t) >= 0 {
			ps.tt.obj_xmit_via = t
		} else {
			ps.errorf("config file, line %d: invalid via path", ps.line)
		}
	}

	return nil
}

// handleTTERR handles the TTERR keyword.
func handleTTERR(ps *parseState) error {
	/*
	 * TTERR 		- TT responses for success or errors.
	 *
	 * TTERR  msg_id  method  text...
	 */

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing message identifier for TTERR command", ps.line)
	}

	var msg_num = -1
	for n := range TT_ERROR_MAXP1 {
		if strings.EqualFold(t, ttErrorString(n)) {
			msg_num = n

			break
		}
	}
	if msg_num < 0 {
		ps.errorf("line %d: Invalid message identifier for TTERR command", ps.line)
		// pick one of ...
		return nil
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing method (SPEECH, MORSE) for TTERR command", ps.line)
	}

	t = strings.ToUpper(t)

	var method, _, _, ok = ax25_parse_addr(-1, t, addrStrict)
	if !ok {
		return nil // function above prints any error message
	}

	if method != "MORSE" && method != "SPEECH" {
		return fmt.Errorf("line %d: Response method of %s must be SPEECH or MORSE for TTERR command", ps.line, method)
	}

	t = split("", true)
	if t == "" {
		return fmt.Errorf("line %d: Missing response text for TTERR command", ps.line)
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Line %d: TTERR debug %d %s-%d \"%s\"\n", line, msg_num, method, ssid, t);

	Assert(msg_num >= 0 && msg_num < TT_ERROR_MAXP1)

	ps.tt.response[msg_num].method = method

	// TODO1.3: Need SSID too!

	ps.tt.response[msg_num].mtext = t

	return nil
}

// handleTTSTATUS handles the TTSTATUS keyword.
func handleTTSTATUS(ps *parseState) error {
	/*
	 * TTSTATUS 		- TT custom status messages.
	 *
	 * TTSTATUS  status_id  text...
	 */

	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing status number for TTSTATUS command", ps.line)
	}

	var status_num, _ = strconv.Atoi(t)

	if status_num < 1 || status_num > 9 {
		return fmt.Errorf("line %d: Status number for TTSTATUS command must be in range of 1 to 9", ps.line)
	}

	t = split("", true)
	if t == "" {
		return fmt.Errorf("line %d: Missing status text for TTSTATUS command", ps.line)
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Line %d: TTSTATUS debug %d \"%s\"\n", line, status_num, t);

	t = strings.TrimSpace(t)

	ps.tt.status[status_num] = t

	return nil
}

// handleTTCMD handles the TTCMD keyword.
func handleTTCMD(ps *parseState) error {
	/*
	 * TTCMD 		- Command to run when valid sequence is received.
	 *			  Any text generated will be sent back to user.
	 *
	 * TTCMD ...
	 */
	var t = split("", true)
	if t == "" {
		return fmt.Errorf("line %d: Missing command for TTCMD command", ps.line)
	}

	ps.tt.ttcmd = t

	return nil
}

// handleIGSERVER handles the IGSERVER keyword.
func handleIGSERVER(ps *parseState) error {
	/*
	 * ==================== Internet gateway ====================
	 */

	/*
	 * IGSERVER 		- Name of IGate server.
	 *
	 * IGSERVER  hostname [ port ] 				-- original implementation.
	 *
	 * IGSERVER  hostname:port				-- more in line with usual conventions.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing IGate server name for IGSERVER command", ps.line)
	}

	ps.igate.t2_server_name = t

	/* If there is a : in the name, split it out as the port number.
	 * Use net.SplitHostPort to correctly handle bracketed IPv6 addresses like [::1]:8080.
	 */

	if strings.Contains(t, ":") {
		var hostname, portStr, splitErr = net.SplitHostPort(t)
		if splitErr == nil {
			ps.igate.t2_server_name = hostname

			var port, portErr = strconv.Atoi(portStr)
			if port >= MIN_IP_PORT_NUMBER && port <= MAX_IP_PORT_NUMBER && portErr == nil {
				ps.igate.t2_server_port = port
			} else {
				ps.igate.t2_server_port = DEFAULT_IGATE_PORT

				ps.errorf("line %d: Invalid port number for IGate server. Using default %d", ps.line, ps.igate.t2_server_port)
			}
		} else {
			/* net.SplitHostPort failed (e.g. malformed input like "host:"); fall back to simple cut. */
			ps.errorf("line %d: Could not parse IGate server address '%s': %v", ps.line, t, splitErr)
			ps.igate.t2_server_name, _, _ = strings.Cut(t, ":")
		}
	}

	/* Alternatively, the port number could be separated by white space. */

	t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER {
			ps.igate.t2_server_port = n
		} else {
			ps.igate.t2_server_port = DEFAULT_IGATE_PORT

			ps.errorf("line %d: Invalid port number for IGate server. Using default %d", ps.line, ps.igate.t2_server_port)
		}
	}
	// dw_printf ("DEBUG  server=%s   port=%d\n", p_igate_config.t2_server_name, p_igate_config.t2_server_port);
	// exit (0);
	return nil
}

// handleIGLOGIN handles the IGLOGIN keyword.
func handleIGLOGIN(ps *parseState) error {
	/*
	 * IGLOGIN 		- Login callsign and passcode for IGate server
	 *
	 * IGLOGIN  callsign  passcode
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing login callsign for IGLOGIN command", ps.line)
	}
	// TODO: Wouldn't hurt to do validity checking of format.
	ps.igate.t2_login = t

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing passcode for IGLOGIN command", ps.line)
	}

	ps.igate.t2_passcode = t

	return nil
}

// handleIGTXVIA handles the IGTXVIA keyword.
func handleIGTXVIA(ps *parseState) error {
	/*
	 * IGTXVIA 		- Transmit channel and VIA path for messages from IGate server
	 *
	 * IGTXVIA  channel  [ path ]
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing transmit channel for IGTXVIA command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil || n < 0 || n > MAX_TOTAL_CHANS-1 {
		return fmt.Errorf("config file: Transmit channel must be in range of 0 to %d on line %d", MAX_TOTAL_CHANS-1, ps.line)
	}

	ps.igate.tx_chan = n

	t = split("", false)
	if t != "" {
		// TODO KG#if 1	// proper checking
		n = ps.checkViaPath(t)
		if n >= 0 {
			ps.igate.max_digi_hops = n
			ps.igate.tx_via = "," + t
		} else {
			ps.errorf("config file, line %d: invalid via path", ps.line)
		}

		/* TODO KG #else	// previously

		   	      char *p;
		   	      ps.igate.tx_via[0] = ',';
		   	      strlcpy (ps.igate.tx_via + 1, t, sizeof(ps.igate.tx_via)-1);
		   	      for (p = ps.igate.tx_via; *p != 0; p++) {
		   	        if (islower(*p)) {
		   		  *p = toupper(*p);	// silently force upper case.
		   	        }
		   	      }
		   #endif
		*/
	}

	return nil
}

// handleIGFILTER handles the IGFILTER keyword.
func handleIGFILTER(ps *parseState) error {
	/*
	 * IGFILTER 		- IGate Server side filters.
	 *			  Is this name too confusing.  Too similar to FILTER IG 0 ...
	 *			  Maybe SSFILTER suggesting Server Side.
	 *			  SUBSCRIBE might be better because it's not a filter that limits.
	 *
	 * IGFILTER  filter-spec ...
	 */
	var t = split("", true) /* Take rest of ps.line as one string. */

	if ps.igate.t2_filter != "" {
		ps.warnf("line %d: Warning - IGFILTER already configured (%s), this one (%s) will be ignored", ps.line, ps.igate.t2_filter, t)

		return nil
	}

	if t != "" {
		ps.igate.t2_filter = t

		ps.warnf(
			"Line %d: Warning - IGFILTER is a rarely needed expert level feature.\n"+
				"If you don't have a special situation and a good understanding of\n"+
				"how this works, you probably should not be messing with it.\n"+
				"The default behavior is appropriate for most situations.\n"+
				"Please read \"Successful-APRS-IGate-Operation.pdf\".",
			ps.line,
		)
	}

	return nil
}

// handleIGTXLIMIT handles the IGTXLIMIT keyword.
func handleIGTXLIMIT(ps *parseState) error {
	/*
	 * IGTXLIMIT 		- Limit transmissions during 1 and 5 minute intervals.
	 *
	 * IGTXLIMIT  one-minute-limit  five-minute-limit
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing one minute limit for IGTXLIMIT command", ps.line)
	}

	// An unreadable limit leaves that one as it was; the other one on the line
	// is still worth reading.
	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		ps.errorf("line %d: One minute limit must be numeric for IGTXLIMIT command. Keeping %d", ps.line, ps.igate.tx_limit_1)
	} else if n < 1 {
		ps.igate.tx_limit_1 = 1
	} else if n <= IGATE_TX_LIMIT_1_MAX {
		ps.igate.tx_limit_1 = n
	} else {
		ps.igate.tx_limit_1 = IGATE_TX_LIMIT_1_MAX

		ps.errorf("line %d: One minute transmit limit has been reduced to %d.\nYou won't make friends by setting a limit this high", ps.line, ps.igate.tx_limit_1)
	}

	t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing five minute limit for IGTXLIMIT command", ps.line)
	}

	n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Five minute limit must be numeric for IGTXLIMIT command. Keeping %d", ps.line, ps.igate.tx_limit_5)
	}
	if n < 1 {
		ps.igate.tx_limit_5 = 1
	} else if n <= IGATE_TX_LIMIT_5_MAX {
		ps.igate.tx_limit_5 = n
	} else {
		ps.igate.tx_limit_5 = IGATE_TX_LIMIT_5_MAX

		ps.errorf("line %d: Five minute transmit limit has been reduced to %d.\nYou won't make friends by setting a limit this high", ps.line, ps.igate.tx_limit_5)
	}

	return nil
}

// handleIGMSP handles the IGMSP keyword.
func handleIGMSP(ps *parseState) error {
	/*
	 * IGMSP 		- Number of times to send position of message sender.
	 *
	 * IGMSP  n
	 */
	var t = split("", false)
	if t != "" {
		var n, nErr = strconv.Atoi(t)
		if nErr != nil {
			return fmt.Errorf("line %d: Number of times must be numeric for IGMSP command. Keeping %d", ps.line, ps.igate.igmsp)
		}
		if n >= 0 && n <= 10 {
			ps.igate.igmsp = n
		} else {
			ps.igate.igmsp = 1

			ps.errorf("line %d: Unreasonable number of times for message sender position.  Using default 1", ps.line)
		}
	} else {
		ps.igate.igmsp = 1

		ps.errorf("line %d: Missing number of times for message sender position.  Using default 1", ps.line)
	}

	return nil
}

// handleSATGATE handles the SATGATE keyword.
func handleSATGATE(ps *parseState) error {
	/*
	 * SATGATE 		- Special SATgate mode to delay packets heard directly.
	 *
	 * SATGATE [ n ]
	 */
	ps.warnf("line %d: SATGATE is pretty useless and will be removed in a future version", ps.line)

	var t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= MIN_SATGATE_DELAY && n <= MAX_SATGATE_DELAY {
			ps.igate.satgate_delay = n
		} else {
			ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY

			ps.errorf("line %d: Unreasonable SATgate delay.  Using default", ps.line)
		}
	} else {
		ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY
	}

	return nil
}

// handleAGWPORT handles the AGWPORT keyword.
func handleAGWPORT(ps *parseState) error {
	/*
	 * ==================== All the left overs ====================
	 */

	/*
	 * AGWPORT 		- Port number for "AGW TCPIP Socket Interface"
	 *
	 * In version 1.2 we allow 0 to disable listening.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing port number for AGWPORT command", ps.line)
	}
	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Invalid port number \"%s\" for AGWPORT command", ps.line, t)
	}

	t = split("", false)
	if t != "" {
		return fmt.Errorf("line %d: Unexpected \"%s\" after the port number.\nPerhaps you were trying to use feature available only with KISSPORT", ps.line, t)
	}

	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		ps.misc.agwpe_port = n
	} else {
		ps.misc.agwpe_port = DEFAULT_AGWPE_PORT

		ps.errorf("line %d: Invalid port number for AGW TCPIP Socket Interface. Using %d", ps.line, ps.misc.agwpe_port)
	}

	return nil
}

// handleAGWLOGIN handles the AGWLOGIN keyword.
func handleAGWLOGIN(ps *parseState) error {
	/*
	 * AGWLOGIN		- User name and password for the "AGW TCPIP Socket Interface"
	 *
	 * AGWLOGIN  user  password
	 *
	 * Without this, anyone who can reach the AGW port can use it.  With it, a
	 * client has to send a matching "Application Login" frame before any of its
	 * other commands are honoured.
	 *
	 * May appear more than once; a client may use any one of the sets.
	 */
	var user = split("", false)
	if user == "" {
		return fmt.Errorf("line %d: Missing user name for AGWLOGIN command", ps.line)
	}

	var password = split("", false)
	if password == "" {
		return fmt.Errorf("line %d: Missing password for AGWLOGIN command", ps.line)
	}

	/*
	 * The protocol has a fixed size field for each, so anything longer could
	 * never be sent, never mind matched.
	 */
	if len(user) > AGW_LOGIN_FIELD_LEN || len(password) > AGW_LOGIN_FIELD_LEN {
		return fmt.Errorf("line %d: User name and password for AGWLOGIN must each be %d characters or fewer", ps.line, AGW_LOGIN_FIELD_LEN)
	}

	/* Each line adds another set of credentials, rather than replacing the last. */
	var login = new(agwpe_login_s)
	login.user = user
	login.password = password
	ps.misc.agwpe_logins = append(ps.misc.agwpe_logins, *login)

	return nil
}

// handleMETRICSPORT handles the METRICSPORT keyword.
func handleMETRICSPORT(ps *parseState) error {
	/*
	 * METRICSPORT 	- Port number for the Prometheus "/metrics" HTTP endpoint.
	 *
	 * 0 disables it.  Disabled by default.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing port number for METRICSPORT command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Invalid port number \"%s\" for METRICSPORT command", ps.line, t)
	}

	t = split("", false)
	if t != "" {
		return fmt.Errorf("line %d: Unexpected \"%s\" after the port number", ps.line, t)
	}

	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		ps.misc.metrics_port = n
	} else {
		ps.misc.metrics_port = 0

		ps.errorf("line %d: Invalid port number for the metrics endpoint. Disabling it", ps.line)
	}

	return nil
}

// handleKISSPORT handles the KISSPORT keyword.
func handleKISSPORT(ps *parseState) error {
	/*
	 * KISSPORT port [ chan ]		- Port number for KISS over IP.
	 */

	// Previously we allowed only a single TCP port for KISS.
	// An increasing number of people want to run multiple radios.
	// Unfortunately, most applications don't know how to deal with multi-radio TNCs.
	// They ignore the channel on receive and always transmit to channel 0.
	// Running multiple instances of direwolf is a work-around but this leads to
	// more complex configuration and we lose the cross-channel digipeating capability.
	// In release 1.7 we add a new feature to assign a single radio channel to a TCP port.
	// e.g.
	//
	//	KISSPORT 8001		# default, all channels.  Radio channel = KISS channel.
	//
	//	KISSPORT 7000 0		# Only radio channel 0 for receive.
	//				# Transmit to radio channel 0, ignoring KISS channel.
	//
	//	KISSPORT 7001 1		# Only radio channel 1 for receive.  KISS channel set to 0.
	//				# Transmit to radio channel 1, ignoring KISS channel.
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing TCP port number for KISSPORT command", ps.line)
	}
	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: Invalid TCP port number \"%s\" for KISSPORT command", ps.line, t)
	}

	var tcp_port int
	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		tcp_port = n
	} else {
		return fmt.Errorf("line %d: Invalid TCP port number for KISS TCPIP Socket Interface.\nUse something in the range of %d to %d", ps.line, MIN_IP_PORT_NUMBER, MAX_IP_PORT_NUMBER)
	}

	t = split("", false)
	var kissChannel = -1 // optional.  default to all if not specified.

	if t != "" {
		var channelErr error

		kissChannel, channelErr = strconv.Atoi(t)
		if kissChannel < 0 || kissChannel >= MAX_TOTAL_CHANS || channelErr != nil {
			return fmt.Errorf("line %d: Invalid channel %d for KISSPORT command.  Must be in range 0 thru %d", ps.line, kissChannel, MAX_TOTAL_CHANS-1)
		}
	}

	// "KISSPORT 0" is used to remove the default entry.

	if tcp_port == 0 {
		ps.misc.kiss_port[0] = 0 // Should all be wiped out?
	} else {
		// Try to find an empty slot.
		// A duplicate TCP port number will overwrite the previous value.
		var slot = -1
		for i := 0; i < MAX_KISS_TCP_PORTS && slot == -1; i++ {
			if ps.misc.kiss_port[i] == tcp_port { //nolint:staticcheck
				slot = i
				if slot != 0 || tcp_port != DEFAULT_KISS_PORT {
					ps.warnf("line %d: Warning: Duplicate TCP port %d will overwrite previous value", ps.line, tcp_port)
				}
			} else if ps.misc.kiss_port[i] == 0 {
				slot = i
			}
		}

		if slot >= 0 {
			ps.misc.kiss_port[slot] = tcp_port
			ps.misc.kiss_chan[slot] = kissChannel
		} else {
			ps.errorf("line %d: Too many KISSPORT commands", ps.line)
		}
	}

	return nil
}

// handleNULLMODEM handles the NULLMODEM keyword.
func handleNULLMODEM(ps *parseState) error {
	/*
	 * NULLMODEM name [ speed ]	- Device name for serial port or our end of the virtual "null modem"
	 * SERIALKISS name  [ speed ]
	 *
	 * Version 1.5:  Added SERIALKISS which is equivalent to NULLMODEM.
	 * The original name sort of made sense when it was used only for one end of a virtual
	 * null modem cable on Windows only.  Now it is also available for Linux.
	 * TODO1.5: In retrospect, this doesn't seem like such a good name.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing serial port name on line %d", ps.line)
	}

	var port = t
	var speed = 0

	t = split("", false)
	if t != "" {
		var n, nErr = strconv.Atoi(t)
		if nErr != nil {
			return fmt.Errorf("config file: Invalid speed \"%s\" for NULLMODEM/SERIALKISS command on line %d", t, ps.line)
		}

		speed = n
	}

	// Commit the line only once all of it has parsed, so that a rejected line
	// leaves the port configured by an earlier one - and the warning below
	// describes a replacement that is actually happening.
	if ps.misc.kiss_serial_port != "" {
		ps.warnf("config file: Warning serial port name on line %d replaces earlier value", ps.line)
	}

	ps.misc.kiss_serial_port = port
	ps.misc.kiss_serial_speed = speed
	ps.misc.kiss_serial_poll = 0

	return nil
}

// handleSERIALKISSPOLL handles the SERIALKISSPOLL keyword.
func handleSERIALKISSPOLL(ps *parseState) error {
	/*
	 * SERIALKISSPOLL name		- Poll for serial port name that might come and go.
	 *			  	  e.g. /dev/rfcomm0 for bluetooth.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing serial port name on line %d", ps.line)
	} else {
		if ps.misc.kiss_serial_port != "" {
			ps.warnf("config file: Warning serial port name on line %d replaces earlier value", ps.line)
		}

		ps.misc.kiss_serial_port = t
		ps.misc.kiss_serial_speed = 0
		ps.misc.kiss_serial_poll = 1 // set polling.
	}

	return nil
}

// handleKISSCOPY handles the KISSCOPY keyword.
func handleKISSCOPY(ps *parseState) error {
	/*
	 * KISSCOPY 		- Data from network KISS client is copied to all others.
	 *			  This does not apply to pseudo terminal KISS.
	 */
	ps.misc.kiss_copy = true

	return nil
}

// handleDNSSD handles the DNSSD keyword.
func handleDNSSD(ps *parseState) error {
	/*
	 * DNSSD 		- Enable or disable (1/0) dns-sd, DNS Service Discovery announcements
	 * DNSSDNAME            - Set DNS-SD service name, defaults to "Dire Wolf on <hostname>"
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing integer value for DNSSD command", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil || (n != 0 && n != 1) {
		ps.misc.dns_sd_enabled = false

		ps.errorf("line %d: Invalid integer value for DNSSD. Disabling dns-sd", ps.line)
	} else {
		ps.misc.dns_sd_enabled = n != 0
	}

	return nil
}

// handleDNSSDNAME handles the DNSSDNAME keyword.
func handleDNSSDNAME(ps *parseState) error {
	var t = split("", true)
	if t == "" {
		return fmt.Errorf("line %d: Missing service name for DNSSDNAME", ps.line)
	} else {
		ps.misc.dns_sd_name = t
	}

	return nil
}

// handleGPSNMEA handles the GPSNMEA keyword.
func handleGPSNMEA(ps *parseState) error {
	/*
	 * GPSNMEA  serial-device  [ speed ]		- Direct connection to GPS receiver.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file, line %d: Missing serial port name for GPS receiver", ps.line)
	}

	var port = t

	// The standard at one time, for a line that gives no speed.
	var speed = 4800

	t = split("", false)
	if t != "" {
		var n, nErr = strconv.Atoi(t)
		if nErr != nil {
			return fmt.Errorf("config file, line %d: Invalid speed \"%s\" for GPSNMEA command", ps.line, t)
		}

		speed = n
	}

	// Commit the port only once its speed is known: dwgpsnmea_init opens
	// whatever port is configured at whatever speed is beside it, so a rejected
	// line must not leave one without the other.
	ps.misc.gpsnmea_port = port
	ps.misc.gpsnmea_speed = speed

	return nil
}

// handleGPSD handles the GPSD keyword.
func handleGPSD(ps *parseState) error {
	/*
	 * GPSD		- Use GPSD server.
	 *
	 * GPSD [ host [ port ] ]
	 */

	ps.misc.gpsd_host = "localhost"
	ps.misc.gpsd_port = DEFAULT_GPSD_PORT

	var t = split("", false)
	if t != "" {
		ps.misc.gpsd_host = t

		t = split("", false)
		if t != "" {
			var n, nErr = strconv.Atoi(t)
			if nErr != nil {
				return fmt.Errorf("line %d: Port number must be numeric for GPSD. Using default of %d", ps.line, ps.misc.gpsd_port)
			}
			if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
				ps.misc.gpsd_port = n
			} else {
				ps.misc.gpsd_port = DEFAULT_GPSD_PORT

				ps.errorf("line %d: Invalid port number for GPSD Socket Interface. Using default of %d", ps.line, ps.misc.gpsd_port)
			}
		}
	}

	return nil
}

// handleWAYPOINT handles the WAYPOINT keyword.
func handleWAYPOINT(ps *parseState) error {
	/*
	 * WAYPOINT		- Generate WPL and AIS NMEA sentences for display on map.
	 *
	 * WAYPOINT  serial-device [ formats ]
	 * WAYPOINT  host:udpport [ formats ]
	 *
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing output device for WAYPOINT on line %d", ps.line)
	}

	/* If there is a ':' in the name, split it into hostname:udpportnum. */
	/* Otherwise assume it is serial port name. */

	if strings.Contains(t, ":") {
		var hostname, portStr, _ = strings.Cut(t, ":")

		var port, _ = strconv.Atoi(portStr)
		if port >= MIN_IP_PORT_NUMBER && port <= MAX_IP_PORT_NUMBER {
			ps.misc.waypoint_udp_hostname = hostname
			if ps.misc.waypoint_udp_hostname == "" {
				ps.misc.waypoint_udp_hostname = "localhost"
			}

			ps.misc.waypoint_udp_portnum = port
		} else {
			ps.errorf("line %d: Invalid UDP port number %d for sending waypoints", ps.line, port)
		}
	} else {
		ps.misc.waypoint_serial_port = t
	}

	/* Anything remaining is the formats to enable. */

	t = split("", true)
	for _, c := range t {
		switch unicode.ToUpper(c) {
		case 'N':
			ps.misc.waypoint_formats |= WPL_FORMAT_NMEA_GENERIC
		case 'G':
			ps.misc.waypoint_formats |= WPL_FORMAT_GARMIN
		case 'M':
			ps.misc.waypoint_formats |= WPL_FORMAT_MAGELLAN
		case 'K':
			ps.misc.waypoint_formats |= WPL_FORMAT_KENWOOD
		case 'A':
			ps.misc.waypoint_formats |= WPL_FORMAT_AIS
		case ' ', ',':
		default:
			ps.errorf("config file: Invalid output format '%c' for WAYPOINT on line %d", c, ps.line)
		}
	}

	return nil
}

// handleLOGDIR handles the LOGDIR keyword.
func handleLOGDIR(ps *parseState) error {
	/*
	 * LOGDIR	- Directory name for automatically named daily log files.  Use "." for current working directory.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing directory name for LOGDIR on line %d", ps.line)
	} else {
		if ps.misc.log_path != "" {
			ps.errorf("config file: LOGDIR on line %d is replacing an earlier LOGDIR or LOGFILE", ps.line)
		}

		ps.misc.log_daily_names = true
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		ps.errorf("config file: LOGDIR on line %d should have directory path and nothing more", ps.line)
	}

	return nil
}

// handleLOGFILE handles the LOGFILE keyword.
func handleLOGFILE(ps *parseState) error {
	/*
	 * LOGFILE	- Log file name, including any directory part.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("config file: Missing file name for LOGFILE on line %d", ps.line)
	} else {
		if ps.misc.log_path != "" {
			ps.errorf("config file: LOGFILE on line %d is replacing an earlier LOGDIR or LOGFILE", ps.line)
		}

		ps.misc.log_daily_names = false
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		ps.errorf("config file: LOGFILE on line %d should have file name and nothing more", ps.line)
	}

	return nil
}

// handleBEACON handles the BEACON keyword.
func handleBEACON(ps *parseState) error {
	/*
	 * BEACON channel delay every message
	 *
	 * Original handcrafted style.  Removed in version 1.0.
	 */

	return fmt.Errorf("config file, line %d: Old style 'BEACON' has been replaced with new commands.\nUse PBEACON, OBEACON, TBEACON, or CBEACON instead", ps.line)
}

// handleXBEACON handles the XBEACON keyword.
func handleXBEACON(ps *parseState) error {
	/*
	 * PBEACON keyword=value ...
	 * OBEACON keyword=value ...
	 * TBEACON keyword=value ...
	 * CBEACON keyword=value ...
	 * IBEACON keyword=value ...
	 *
	 * New style with keywords for options.
	 */

	// TODO: maybe add proportional pathing so multiple beacon timing does not need to be manually constructed?
	// http://www.aprs.org/newN/ProportionalPathing.txt
	if ps.misc.num_beacons < MAX_BEACONS {
		/* A beacon line whose options don't parse leaves num_beacons alone, so
		 * the next beacon line reuses this array slot.  Start it blank rather
		 * than inheriting whatever the rejected line managed to set. */
		var blank beacon_s

		ps.misc.beacon[ps.misc.num_beacons] = blank

		if strings.EqualFold(ps.keyword, "PBEACON") {
			ps.misc.beacon[ps.misc.num_beacons].btype = BEACON_POSITION
		} else if strings.EqualFold(ps.keyword, "OBEACON") {
			ps.misc.beacon[ps.misc.num_beacons].btype = BEACON_OBJECT
		} else if strings.EqualFold(ps.keyword, "TBEACON") {
			ps.misc.beacon[ps.misc.num_beacons].btype = BEACON_TRACKER
		} else if strings.EqualFold(ps.keyword, "IBEACON") {
			ps.misc.beacon[ps.misc.num_beacons].btype = BEACON_IGATE
		} else {
			ps.misc.beacon[ps.misc.num_beacons].btype = BEACON_CUSTOM
		}

		/* Save line number because some errors will be reported later. */
		ps.misc.beacon[ps.misc.num_beacons].lineno = ps.line

		// Pass "" so beacon_options continues from the current split() position
		// rather than reinitialising the tokenizer. The main parse loop already
		// called split(ps.text, false) to extract the keyword, leaving any
		// options as the remaining state; passing "" here reads those options
		// correctly and also handles the case where there are none.
		if beacon_options("", &(ps.misc.beacon[ps.misc.num_beacons]), ps, ps.audio) == nil {
			ps.misc.num_beacons++
		}
	} else {
		return fmt.Errorf("config file: Maximum number of beacons exceeded on line %d", ps.line)
	}

	return nil
}

// handleSMARTBEACON handles the SMARTBEACON keyword.
func handleSMARTBEACON(ps *parseState) error {
	/*
	 * SMARTBEACONING [ fast_speed fast_rate slow_speed slow_rate turn_time turn_angle turn_slope ]
	 *
	 * Parameters must be all or nothing.
	 */
	ps.errorf("SMARTBEACONING support currently disabled due to mid-stage porting complexity - line %d skipped", ps.line)

	/* TODO KG
	   #define SB_NUM(name,sbvar,minn,maxx,unit)  							\
	   	var t = split("", false);									\
	   	    if (t == "") {									\
	   	      if (strcmp(name, "fast speed") == 0) {						\
	   	        ps.misc.sb_configured = 1;						\
	   	        continue;									\
	   	      }											\
	   	      text_color_set(DW_COLOR_ERROR);							\
	   	      dw_printf ("Line %d: Missing %s for SmartBeaconing.\n", ps.line, name);		\
	   	      continue;										\
	   	    }											\
	   	    var n, _ = strconv.Atoi(t);									\
	               if (n >= minn && n <= maxx) {							\
	   	      ps.misc.sbvar = n;								\
	   	    }											\
	   	    else {										\
	   	      text_color_set(DW_COLOR_ERROR);							\
	                 dw_printf ("Line %d: Invalid %s for SmartBeaconing. Using default %d %s.\n",	\
	   			ps.line, name, ps.misc.sbvar, unit);				\
	      	    }
	*/

	/* TODO KG
	   #define SB_TIME(name,sbvar,minn,maxx,unit)  							\
	   	    t = split("", false);									\
	   	    if (t == "") {									\
	   	      text_color_set(DW_COLOR_ERROR);							\
	   	      dw_printf ("Line %d: Missing %s for SmartBeaconing.\n", ps.line, name);		\
	   	      continue;										\
	   	    }											\
	   	    n = parse_interval(t,ps.line);								\
	               if (n >= minn && n <= maxx) {							\
	   	      ps.misc.sbvar = n;								\
	   	    }											\
	   	    else {										\
	   	      text_color_set(DW_COLOR_ERROR);							\
	                 dw_printf ("Line %d: Invalid %s for SmartBeaconing. Using default %d %s.\n",	\
	   			ps.line, name, ps.misc.sbvar, unit);				\
	      	    }
	*/

	/* TODO KG
	   SB_NUM("fast speed", sb_fast_speed, 2, 90, "MPH")
	   SB_TIME("fast rate", sb_fast_rate, 10, 300, "seconds")

	   SB_NUM("slow speed", sb_slow_speed, 1, 30, "MPH")
	   SB_TIME("slow rate", sb_slow_rate, 30, 3600, "seconds")

	   SB_TIME("turn time", sb_turn_time, 5, 180, "seconds")
	   SB_NUM("turn angle", sb_turn_angle, 5, 90, "degrees")
	   SB_NUM("turn slope", sb_turn_slope, 1, 255, "deg*mph")

	   ps.misc.sb_configured = 1
	*/

	/* If I was ambitious, I might allow optional */
	/* unit at end for miles or km / hour. */
	return nil
}

// handleFRACK handles the FRACK keyword.
func handleFRACK(ps *parseState) error {
	/*
	 * ==================== AX.25 connected mode ====================
	 */

	/*
	 * FRACK  n 		- Number of seconds to wait for ack to transmission.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for FRACK", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_T1V_FRACK_MIN && n <= AX25_T1V_FRACK_MAX {
		ps.misc.frack = n
	} else {
		ps.errorf("line %d: Invalid FRACK time. Using default %d", ps.line, ps.misc.frack)
	}

	return nil
}

// handleRETRY handles the RETRY keyword.
func handleRETRY(ps *parseState) error {
	/*
	 * RETRY  n 		- Number of times to retry before giving up.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for RETRY", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N2_RETRY_MIN && n <= AX25_N2_RETRY_MAX {
		ps.misc.retry = n
	} else {
		ps.errorf("line %d: Invalid RETRY number. Using default %d", ps.line, ps.misc.retry)
	}

	return nil
}

// handlePACLEN handles the PACLEN keyword.
func handlePACLEN(ps *parseState) error {
	/*
	 * PACLEN  n 		- Maximum number of bytes in information part.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for PACLEN", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N1_PACLEN_MIN && n <= AX25_N1_PACLEN_MAX {
		ps.misc.paclen = n
	} else {
		ps.errorf("line %d: Invalid PACLEN value. Using default %d", ps.line, ps.misc.paclen)
	}

	return nil
}

// handleMAXFRAME handles the MAXFRAME keyword.
func handleMAXFRAME(ps *parseState) error {
	/*
	 * MAXFRAME  n 		- Max frames to send before ACK.  mod 8 "Window" size.
	 *
	 * Window size would make more sense but everyone else calls it MAXFRAME.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for MAXFRAME", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_BASIC_MIN && n <= AX25_K_MAXFRAME_BASIC_MAX {
		ps.misc.maxframe_basic = n
	} else {
		ps.misc.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT

		ps.errorf("line %d: Invalid MAXFRAME value outside range of %d to %d. Using default %d", ps.line, AX25_K_MAXFRAME_BASIC_MIN, AX25_K_MAXFRAME_BASIC_MAX, ps.misc.maxframe_basic)
	}

	return nil
}

// handleEMAXFRAME handles the EMAXFRAME keyword.
func handleEMAXFRAME(ps *parseState) error {
	/*
	 * EMAXFRAME  n 		- Max frames to send before ACK.  mod 128 "Window" size.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for EMAXFRAME", ps.line)
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_EXTENDED_MIN && n <= AX25_K_MAXFRAME_EXTENDED_MAX {
		ps.misc.maxframe_extended = n
	} else {
		ps.misc.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

		ps.errorf("line %d: Invalid EMAXFRAME value outside of range %d to %d. Using default %d", ps.line, AX25_K_MAXFRAME_EXTENDED_MIN, AX25_K_MAXFRAME_EXTENDED_MAX, ps.misc.maxframe_extended)
	}

	return nil
}

// handleMAXV22 handles the MAXV22 keyword.
func handleMAXV22(ps *parseState) error {
	/*
	 * MAXV22  n 		- Max number of SABME sent before trying SABM.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing value for MAXV22", ps.line)
	}

	var n, nErr = strconv.Atoi(t)
	if nErr != nil {
		return fmt.Errorf("line %d: MAXV22 number must be numeric. Ignoring this line", ps.line)
	}
	if n >= 0 && n <= AX25_N2_RETRY_MAX {
		ps.misc.maxv22 = n
	} else {
		ps.errorf("line %d: Invalid MAXV22 number. Will use half of RETRY", ps.line)
	}

	return nil
}

// handleV20 handles the V20 keyword.
func handleV20(ps *parseState) error {
	/*
	 * V20  address [ address ... ] 	- Stations known to support only AX.25 v2.0.
	 *					  When connecting to these, skip SABME and go right to SABM.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing address(es) for V20", ps.line)
	}

	for t != "" {
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, addrStrictNoStar)

		if ok {
			ps.misc.v20_addrs = append(ps.misc.v20_addrs, t)
			ps.misc.v20_count++
		} else {
			ps.errorf("line %d: Invalid station address for V20 command", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}

	return nil
}

// handleNOXID handles the NOXID keyword.
func handleNOXID(ps *parseState) error {
	/*
	 * NOXID  address [ address ... ] 	- Stations known not to understand XID.
	 *					  After connecting to these (with v2.2 obviously), don't try using XID command.
	 *					  AX.25 for Linux is the one known case so far.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		return fmt.Errorf("line %d: Missing address(es) for NOXID", ps.line)
	}

	for t != "" {
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, addrStrictNoStar)

		if ok {
			ps.misc.noxid_addrs = append(ps.misc.noxid_addrs, t)
			ps.misc.noxid_count++
		} else {
			ps.errorf("line %d: Invalid station address for NOXID command", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}

	return nil
}

// parse_beacon_number parses the value of a numeric beacon option, reporting
// failure rather than quietly settling for zero.  Zero is a real value for
// several of these options - TONE=0 is transmitted as "Toff", OFFSET=0 as
// "+000" and ALT=0 as "/A=000000" - so a typo would otherwise put on the air a
// value nobody asked for.
func parse_beacon_number(keyword string, value string, line int) (float64, error) {
	var f, err = strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("line %d: Invalid number for beacon option %s, \"%s\".  Ignoring it", line, keyword, value)
	}

	return f, nil
}

/*
 * Parse the PBEACON or OBEACON options.
 */

// FIXME: provide error messages when non applicable option is used for particular beacon type.
// e.g.  IBEACON DELAY=1 EVERY=1 SENDTO=IG OVERLAY=R SYMBOL="igate" LAT=37^44.46N LONG=122^27.19W COMMENT="N1KOL-1 IGATE"
// Just ignores overlay, symbol, lat, long, and comment.

func beacon_options(cmd string, b *beacon_s, ps *parseState, p_audio_config *audio_s) error { //nolint:unparam
	b.sendto_type = SENDTO_XMIT
	b.sendto_chan = 0
	b.delay = 60
	b.every = 600
	//b.delay = 6;		// temp test.
	//b.every = 3600;
	b.ambiguity = 0
	b.symtab = '/'
	b.symbol = '-' /* house */
	b.source = ""
	b.dest = ""

	var zone string
	var temp_symbol string
	var easting maybe.Maybe[float64]
	var northing maybe.Maybe[float64]

	for {
		var t = split("", false)
		if t == "" {
			break
		}

		var keyword, value, found = strings.Cut(t, "=")
		if !found {
			ps.errorf("config file: No = found in, %s, on line %d", t, ps.line)

			return fmt.Errorf("config file line %d: no = found in %q", ps.line, t)
		}

		// QUICK TEMP EXPERIMENT, maybe permanent new feature.
		// Recognize \xnn as hexadecimal value.  Handy for UTF-8 in comment.
		// Maybe recognize the <0xnn> form that we print.
		//
		// # Convert between languages here:  https://translate.google.com/  then
		// # Convert to UTF-8 bytes here: https://codebeautify.org/utf8-converter
		//
		// pbeacon delay=0:05 every=0:30 sendto=R0 lat=12.5N long=69.97W  comment="\xe3\x82\xa2\xe3\x83\x9e\xe3\x83\x81\xe3\x83\xa5\xe3\x82\xa2\xe7\x84\xa1\xe7\xb7\x9a
		//   \xce\xa1\xce\xb1\xce\xb4\xce\xb9\xce\xbf\xce\xb5\xcf\x81\xce\xb1\xcf\x83\xce\xb9\xcf\x84\xce\xb5\xcf\x87\xce\xbd\xce\xb9\xcf\x83\xce\xbc\xcf\x8c\xcf\x82"

		/* TODO KG I think we get this for free because Go just handles UTF8 etc.
		var temp [256]C.char
		var tlen = 0

		for p := value; *p != 0; {
			if p[0] == '\\' && p[1] == 'x' && strlen(p) >= 4 && isxdigit(p[2]) && isxdigit(p[3]) {
				var n = 0
				for i := 2; i < 4; i++ {
					n = n * 16
					if islower(p[i]) {
						n += p[i] - 'a' + 10
					} else if isupper(p[i]) {
						n += p[i] - 'A' + 10
					} else { // must be digit due to isxdigit test above.
						n += p[i] - '0'
					}
				}
				temp[tlen] = n
				tlen++
				p += 4
			} else {
				temp[tlen] = *p
				tlen++
				p++
			}
		}
		temp[tlen] = 0
		strlcpy(value, temp, sizeof(value))
		*/

		// end
		if strings.EqualFold(keyword, "DELAY") {
			var n, intervalErr = parse_interval(keyword, value, ps.line)
			if intervalErr != nil {
				ps.report(intervalErr)

				continue
			}

			if n < 0 {
				ps.errorf("config file, line %d: Beacon delay, %d, can't be negative", ps.line, n)

				continue
			}

			b.delay = n
		} else if strings.EqualFold(keyword, "SLOT") {
			var n, intervalErr = parse_interval(keyword, value, ps.line)
			if intervalErr != nil {
				ps.report(intervalErr)

				continue
			}

			if n < 1 || n > 3600 {
				ps.errorf("config file, line %d: Beacon time slot, %d, must be in range of 1 to 3600 seconds", ps.line, n)

				continue
			}

			b.slot = maybe.Just(n)
		} else if strings.EqualFold(keyword, "EVERY") {
			var n, intervalErr = parse_interval(keyword, value, ps.line)
			if intervalErr != nil {
				ps.report(intervalErr)

				continue
			}

			if n < 1 {
				ps.errorf("config file, line %d: Time between beacons, %d, must be at least 1 second", ps.line, n)

				continue
			}

			b.every = n
		} else if strings.EqualFold(keyword, "SENDTO") {
			if len(value) == 0 {
				ps.errorf("config file, line %d: Missing value for SENDTO option", ps.line)

				continue
			} else if value[0] == 'i' || value[0] == 'I' {
				b.sendto_type = SENDTO_IGATE
				b.sendto_chan = 0
			} else if value[0] == 'r' || value[0] == 'R' {
				var n, nErr = strconv.Atoi(value[1:])
				if nErr != nil {
					ps.errorf("config file, line %d: Non-numeric channel \"%s\" for SENDTO=r option", ps.line, value[1:])

					continue
				}
				if n < 0 || n >= MAX_TOTAL_CHANS {
					ps.errorf("config file, line %d: Simulated receive on channel %d is not valid", ps.line, n)

					continue
				}
				if p_audio_config.chan_medium[n] == MEDIUM_NONE {
					ps.errorf("config file, line %d: Simulated receive on channel %d is not valid", ps.line, n)

					continue
				}

				b.sendto_type = SENDTO_RECV
				b.sendto_chan = n
			} else if value[0] == 't' || value[0] == 'T' || value[0] == 'x' || value[0] == 'X' {
				var n, nErr = strconv.Atoi(value[1:])
				if nErr != nil {
					ps.errorf("config file, line %d: Non-numeric channel \"%s\" for SENDTO=t option", ps.line, value[1:])

					continue
				}
				if n < 0 || n >= MAX_TOTAL_CHANS {
					ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, n)

					continue
				}
				if p_audio_config.chan_medium[n] == MEDIUM_NONE {
					ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, n)

					continue
				}

				b.sendto_type = SENDTO_XMIT
				b.sendto_chan = n
			} else {
				var n, nErr = strconv.Atoi(value)
				if nErr != nil {
					ps.errorf("config file, line %d: Non-numeric channel \"%s\" for SENDTO option", ps.line, value)

					continue
				}
				if n < 0 || n >= MAX_TOTAL_CHANS {
					ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, n)

					continue
				}
				if p_audio_config.chan_medium[n] == MEDIUM_NONE {
					ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, n)

					continue
				}

				b.sendto_type = SENDTO_XMIT
				b.sendto_chan = n
			}
		} else if strings.EqualFold(keyword, "SOURCE") {
			b.source = strings.ToUpper(value) /* silently force upper case. */
			/* TODO KG Cap length
			if C.strlen(b.source) > 9 {
				b.source[9] = 0
			}
			*/
		} else if strings.EqualFold(keyword, "DEST") {
			b.dest = strings.ToUpper(value) /* silently force upper case. */
			/* TODO KG Cap length
			if C.strlen(b.dest) > 9 {
				b.dest[9] = 0
			}
			*/
		} else if strings.EqualFold(keyword, "VIA") {
			// #if 1	// proper checking
			if ps.checkViaPath(value) >= 0 {
				b.via = value
			} else {
				ps.errorf("config file, line %d: invalid via path", ps.line)
			}

			/* #else	// previously

			   	    b.via = strdup(value);
			   	    for (p = b.via; *p != 0; p++) {
			   	      if (islower(*p)) {
			   	        *p = toupper(*p);	// silently force upper case.
			   	      }
			   	    }
			   #endif
			*/
		} else if strings.EqualFold(keyword, "INFO") {
			b.custom_info = value
		} else if strings.EqualFold(keyword, "INFOCMD") {
			b.custom_infocmd = value
		} else if strings.EqualFold(keyword, "OBJNAME") {
			b.objname = value
		} else if strings.EqualFold(keyword, "LAT") {
			b.lat = ps.parseLLMaybe(value, LAT)
		} else if strings.EqualFold(keyword, "LONG") || strings.EqualFold(keyword, "LON") {
			b.lon = ps.parseLLMaybe(value, LON)
		} else if strings.EqualFold(keyword, "AMBIGUITY") || strings.EqualFold(keyword, "AMBIG") {
			var n, _ = strconv.Atoi(value)
			if n >= 0 && n <= 4 {
				b.ambiguity = n
			} else {
				ps.errorf("config file: Location ambiguity, on line %d, must be in range of 0 to 4", ps.line)
			}
		} else if strings.EqualFold(keyword, "ALT") || strings.EqualFold(keyword, "ALTITUDE") {
			// Parse something like "10 metres" or "10" or "10metres"
			var unitIndex = strings.IndexFunc(value, func(r rune) bool {
				return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			})

			var number = value
			var meters float64 = 1

			if unitIndex != -1 { // Did we find a unit string?
				var unit = value[unitIndex:]

				number = strings.TrimSpace(value[:unitIndex])

				meters = 0

				for _, u := range units {
					if strings.EqualFold(u.name, unit) {
						meters = u.meters
					}
				}

				if meters == 0 {
					ps.errorf("line %d: Unrecognized unit '%s' for altitude.  Using meter.\nTry using singular form.  e.g.  ft or foot rather than feet", ps.line, unit)

					meters = 1
				}
			}

			var f, numErr = parse_beacon_number(keyword, number, ps.line)
			if ps.reportedOK(numErr) {
				b.alt_m = maybe.Just(f * meters)
			}
		} else if strings.EqualFold(keyword, "ZONE") {
			zone = value
		} else if strings.EqualFold(keyword, "EAST") || strings.EqualFold(keyword, "EASTING") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				easting = maybe.Just(f)
			}
		} else if strings.EqualFold(keyword, "NORTH") || strings.EqualFold(keyword, "NORTHING") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				northing = maybe.Just(f)
			}
		} else if strings.EqualFold(keyword, "SYMBOL") {
			/* Defer processing in case overlay appears later. */
			temp_symbol = value
		} else if strings.EqualFold(keyword, "OVERLAY") {
			if len(value) == 1 && (unicode.IsUpper(rune(value[0])) || unicode.IsDigit(rune(value[0]))) {
				b.symtab = value[0]
			} else {
				ps.errorf("config file: Overlay must be one character in range of 0-9 or A-Z, upper case only, on line %d", ps.line)
			}
		} else if strings.EqualFold(keyword, "POWER") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.power = f
			}
		} else if strings.EqualFold(keyword, "HEIGHT") { // This is in feet.
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.height = f
			}
			// TODO: ability to add units suffix, e.g.  10m
		} else if strings.EqualFold(keyword, "GAIN") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.gain = f
			}
		} else if strings.EqualFold(keyword, "DIR") || strings.EqualFold(keyword, "DIRECTION") {
			b.dir = value
		} else if strings.EqualFold(keyword, "FREQ") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.freq = maybe.Just(f)
			}
		} else if strings.EqualFold(keyword, "TONE") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.tone = maybe.Just(f)
			}
		} else if strings.EqualFold(keyword, "OFFSET") || strings.EqualFold(keyword, "OFF") {
			var f, numErr = parse_beacon_number(keyword, value, ps.line)
			if ps.reportedOK(numErr) {
				b.offset = maybe.Just(f)
			}
		} else if strings.EqualFold(keyword, "COMMENT") {
			b.comment = value
		} else if strings.EqualFold(keyword, "COMMENTCMD") {
			b.commentcmd = value
		} else if strings.EqualFold(keyword, "COMPRESS") || strings.EqualFold(keyword, "COMPRESSED") {
			var n, _ = strconv.Atoi(value)
			b.compress = n != 0
		} else if strings.EqualFold(keyword, "MESSAGING") {
			var n, _ = strconv.Atoi(value)
			b.messaging = n != 0
		} else {
			ps.errorf("config file, line %d: Invalid option keyword, %s", ps.line, keyword)

			return fmt.Errorf("config file line %d: invalid option keyword %q", ps.line, keyword)
		}
	}

	if b.custom_info != "" && b.custom_infocmd != "" {
		ps.errorf("config file, line %d: Can't use both INFO and INFOCMD at the same time", ps.line)
	}

	if b.compress && b.ambiguity != 0 {
		ps.errorf("config file, line %d: Position ambiguity can't be used with compressed location format", ps.line)

		b.ambiguity = 0
	}

	/*
	 * Convert UTM coordinates to lat / long.
	 */
	if len(zone) > 0 || easting.IsJust() || northing.IsJust() {
		var east, eastKnown = easting.Get()
		var north, northKnown = northing.Get()

		if len(zone) > 0 && eastKnown && northKnown {
			var _, _hemi, lzone = ps.parseUTMZone(zone)

			var hemi = HemisphereRuneToCoordconvHemisphere(_hemi)

			var utm = coordconv.UTMCoord{
				Zone:       lzone,
				Hemisphere: hemi,
				Easting:    east,
				Northing:   north,
			}

			var geo, geoErr = coordconv.DefaultUTMConverter.ConvertToGeodetic(utm)
			if geoErr == nil {
				b.lat = maybe.Just(R2D(float64(geo.Lat)))
				b.lon = maybe.Just(R2D(float64(geo.Lng)))
			} else {
				ps.errorf("line %d: Invalid UTM location: \n%v", ps.line, geoErr)
			}
		} else {
			ps.errorf("config file, line %d: When any of ZONE, EASTING, NORTHING specified, they must all be specified", ps.line)
		}
	}

	/*
	 * Process symbol now that we have any later overlay.
	 *
	 * FIXME: Someone who used this was surprised to end up with Solar Power  (S-).
	 *	overlay=S symbol="/-"
	 * We should complain if overlay used with symtab other than \.
	 */
	if len(temp_symbol) > 0 {
		if len(temp_symbol) == 2 &&
			(temp_symbol[0] == '/' || temp_symbol[0] == '\\' || unicode.IsUpper(rune(temp_symbol[0])) || unicode.IsDigit(rune(temp_symbol[0]))) &&
			temp_symbol[1] >= '!' && temp_symbol[1] <= '~' {
			/* Explicit table and symbol. */
			if unicode.IsUpper(rune(b.symtab)) || unicode.IsDigit(rune(b.symtab)) {
				b.symbol = temp_symbol[1]
			} else {
				b.symtab = temp_symbol[0]
				b.symbol = temp_symbol[1]
			}
		} else {
			/* Try to look up by description. */
			var symtab, symbol, ok = aprsSymbolData.symbols_code_from_description(b.symtab, temp_symbol)
			if ok {
				b.symtab = symtab
				b.symbol = symbol
			} else {
				ps.errorf("config file, line %d: Could not find symbol matching %s", ps.line, temp_symbol)
			}
		}
	}

	/* Check is here because could be using default channel when SENDTO= is not specified. */

	if b.sendto_type == SENDTO_XMIT {
		if b.sendto_chan < 0 || b.sendto_chan >= MAX_TOTAL_CHANS {
			ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, b.sendto_chan)

			return fmt.Errorf("config file line %d: send-to channel %d is out of range", ps.line, b.sendto_chan)
		}

		if p_audio_config.chan_medium[b.sendto_chan] == MEDIUM_NONE {
			ps.errorf("config file, line %d: Send to channel %d is not valid", ps.line, b.sendto_chan)

			return fmt.Errorf("config file line %d: send-to channel %d has no medium configured", ps.line, b.sendto_chan)
		}

		if p_audio_config.chan_medium[b.sendto_chan] == MEDIUM_IGATE { // Prevent subscript out of bounds.
			// Will be using call from chan 0 later.
			if IsNoCall(p_audio_config.mycall[0]) {
				ps.errorf("config file: MYCALL must be set for channel %d before beaconing is allowed", 0)

				return fmt.Errorf("config file line %d: MYCALL must be set for channel 0 before beaconing", ps.line)
			}
		} else {
			if IsNoCall(p_audio_config.mycall[b.sendto_chan]) {
				ps.errorf("config file: MYCALL must be set for channel %d before beaconing is allowed", b.sendto_chan)

				return fmt.Errorf("config file line %d: MYCALL must be set for channel %d before beaconing", ps.line, b.sendto_chan)
			}
		}
	}

	return nil
}

func IsNoCall(callsign string) bool {
	return callsign == "" || strings.EqualFold(callsign, "NOCALL") || strings.EqualFold(callsign, "N0CALL")
}
