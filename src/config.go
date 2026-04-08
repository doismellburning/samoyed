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
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

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

	slot int /* Seconds after hour for slotted time beacons. */
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

	lat       float64 /* Latitude and longitude. */
	lon       float64
	ambiguity int     /* Number of lower digits to trim from location. 0 (default), 1, 2, 3, 4. */
	alt_m     float64 /* Altitude in meters. */

	symtab byte /* Symbol table: / or \ or overlay character. */
	symbol byte /* Symbol code. */

	power  float64 /* For PHG. */
	height float64 /* HAAT in feet */
	gain   float64 /* Original protocol spec was unclear. */
	/* Addendum 1.1 clarifies it is dBi not dBd. */

	dir string /* 1 or 2 of N,E,W,S, or empty for omni. */

	freq   float64 /* MHz. */
	tone   float64 /* Hz. */
	offset float64 /* MHz. */

	comment    string /* Comment or empty. */
	commentcmd string /* Command to append more to Comment or empty. */
}

type misc_config_s struct {
	agwpe_port int /* TCP Port number for the "AGW TCPIP Socket Interface" */

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
		return !(unicode.IsLetter(r) || r == '+' || r == '-')
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
 * Returns:     Coordinate in signed degrees.
 *
 *----------------------------------------------------------------*/

/* Acceptable symbols to separate degrees & minutes. */
/* Degree symbol is not in ASCII so documentation says to use "^" instead. */
/* Some wise guy will try to use degree symbol. */
/* UTF-8 is more difficult because it is a two byte sequence, c2 b0. */

type parse_ll_which_e int

const LAT parse_ll_which_e = 0
const LON parse_ll_which_e = 1

func parse_ll(str string, which parse_ll_which_e, line int) float64 {
	var stemp = str

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
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Latitude hemisphere in \"%s\" is not N or S.\n", line, str)
				}
			} else {
				if hemi != 'E' && hemi != 'W' {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Longitude hemisphere in \"%s\" is not E or W.\n", line, str)
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
		dw_printf("Line %d: Could not parse degrees string '%s': %s\n", line, degreesStr, degreesErr)
	}

	if minutesFound {
		var minutes, minutesErr = strconv.ParseFloat(minutesStr, 64)
		if minutesErr != nil {
			dw_printf("Line %d: Could not parse minutes string '%s': %s\n", line, minutesStr, minutesErr)
		}

		if minutes >= 60.0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Number of minutes in \"%s\" is >= 60.\n", line, minutesStr)
		}

		degrees += minutes / 60
	}

	degrees *= float64(sign)

	var limit = float64(IfThenElse(which == LAT, 90, 180))
	if degrees < -limit || degrees > limit {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Number of degrees in \"%s\" is out of range for %s\n", line, str,
			IfThenElse(which == LAT, "latitude", "longitude"))
	}
	//dw_printf ("%s = %f\n", str, degrees);
	return degrees
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

func parse_utm_zone(szone string) (rune, rune, int) {
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Latitudinal band in \"%s\" must be one of CDEFGHJKLMNPQRSTUVWX.\n", szone)

			hemi = '?'
		}
	}

	if lzone < 1 || lzone > 60 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("UTM Zone number %d must be in range of 1 to 60.\n", lzone)
	}

	return latband, hemi, lzone
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
 * Returns:     Number of seconds.
 *
 * Description:	This is used by the BEACON configuration items
 *		for initial delay or time between beacons.
 *
 *		The format is either minutes or minutes:seconds.
 *
 *----------------------------------------------------------------*/

func parse_interval(str string, line int) int { //nolint:unparam
	var minutesStr, secondsStr, _ = strings.Cut(str, ":") // Don't need to check found because if not, Cut returns `str, "", false`

	var minutes, _ = strconv.Atoi(minutesStr)
	var interval = 60 * minutes

	var seconds, _ = strconv.Atoi(secondsStr)
	interval += seconds

	/* TODO KG Better logging / error handling
	if bad > 0 || nc > 1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Time interval must be of the form minutes or minutes:seconds.\n", line)
	}
	*/

	return interval
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

func check_via_path(via_path string) int {
	/* TODO KG
	#if DEBUG8
		text_color_set(DW_COLOR_DEBUG);
	        dw_printf ("check_via_path %s\n", via_path);
	#endif
	*/
	var parts = strings.Split(via_path, ",")
	var num_digi = 0
	var max_digi_hops = 0

	for _, part := range parts {
		num_digi++

		var strictness = 2
		var addr, ssid, _, ok = ax25_parse_addr(AX25_REPEATER_1-1+num_digi, part, strictness)

		if !ok {
			/* TODO KG
			#if DEBUG8
				    text_color_set(DW_COLOR_DEBUG);
			            dw_printf ("check_via_path bad address\n");
			#endif
			*/
			return (-1)
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Maximum of 8 digipeaters has been exceeded.\n")

		return (-1)
	}

	/* TODO KG
	#if DEBUG8
		text_color_set(DW_COLOR_DEBUG);
	        dw_printf ("check_via_path %d addresses, %d max digi hops\n", num_digi, max_digi_hops);
	#endif
	*/

	return (max_digi_hops)
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
}

// configHandler is a keyword handler. It returns true if the outer scanner loop
// should `continue` (i.e. skip to the next line).
type configHandler func(ps *parseState) bool

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

func config_init(fname string, p_audio_config *audio_s,
	p_digi_config *digi_config_s,
	p_cdigi_config *cdigi_config_s,
	p_tt_config *tt_config_s,
	p_igate_config *igate_config_s,
	p_misc_config *misc_config_s) {
	/* TODO KG
	#if DEBUG
		text_color_set(DW_COLOR_DEBUG);
		dw_printf ("config_init ( %s )\n", fname);
	#endif
	*/

	/*
	 * First apply defaults.
	 */
	p_audio_config.igate_vchannel = -1 // none.

	/* First audio device is always available with defaults. */
	/* Others must be explicitly defined before use. */

	for adevice := range MAX_ADEVS {
		p_audio_config.adev[adevice].adevice_in = DEFAULT_ADEVICE
		p_audio_config.adev[adevice].adevice_out = DEFAULT_ADEVICE

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
		p_audio_config.achan[channel].il2p_invert_polarity = 0

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

	p_misc_config.maxv22 = AX25_N2_RETRY_DEFAULT / 3 /* Send SABME this many times before falling back to SABM. */
	p_misc_config.v20_addrs = nil                    /* Go directly to v2.0 for stations listed */
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("ERROR - Could not open configuration file %s: %s\n", absFilePath, fpErr)
		dw_printf("Try using -c command line option for alternate location.\n")
		dw_printf("A sample direwolf.conf file should be found in one of:\n")
		dw_printf("    /usr/local/share/doc/direwolf/conf/\n")
		dw_printf("    /usr/share/doc/direwolf/conf/\n")
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
		// Some config keywords actually incorporate a device number, e.g. ADEVICE0
		if strings.HasPrefix(keyword, "ADEVICE") {
			if handleADEVICE(ps) {
				continue
			}
		} else if strings.HasPrefix(keyword, "PAIDEVICE") {
			if handlePAIDEVICE(ps) {
				continue
			}
		} else if strings.HasPrefix(keyword, "PAODEVICE") {
			if handlePAODEVICE(ps) {
				continue
			}
		} else if handler, ok := configHandlers[keyword]; ok {
			if handler(ps) {
				continue
			}
		} else {
			/*
			 * Invalid command.
			 */
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Unrecognized command '%s' on line %d.\n", t, ps.line)
		}
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
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: MYCALL must be set for receive channel %d before digipeating is allowed.\n", i)
					ps.digi.enabled[i][j] = false
				}

				if IsNoCall(ps.audio.mycall[j]) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: MYCALL must be set for transmit channel %d before digipeating is allowed.\n", i)
					ps.digi.enabled[i][j] = false
				}

				var b = 0

				for k := 0; k < ps.misc.num_beacons; k++ {
					if ps.misc.beacon[k].sendto_chan == j {
						b++
					}
				}

				if b == 0 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: Beaconing should be configured for channel %d when digipeating is enabled.\n", j)
					// It's a recommendation, not a requirement.
					// Was there some good reason to turn it off in earlier version?
					//ps.digi.enabled[i][j] = 0;
				}
			}

			/* Connected mode digipeating. */

			if i < MAX_RADIO_CHANS && j < MAX_RADIO_CHANS && ps.cdigi.enabled[i][j] {
				if IsNoCall(ps.audio.mycall[i]) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: MYCALL must be set for receive channel %d before digipeating is allowed.\n", i)
					ps.cdigi.enabled[i][j] = false
				}

				if IsNoCall(ps.audio.mycall[j]) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: MYCALL must be set for transmit channel %d before digipeating is allowed.\n", i)
					ps.cdigi.enabled[i][j] = false
				}

				var b = 0

				for k := 0; k < ps.misc.num_beacons; k++ {
					if ps.misc.beacon[k].sendto_chan == j {
						b++
					}
				}

				if b == 0 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file: Beaconing should be configured for channel %d when digipeating is enabled.\n", j)
					// It's a recommendation, not a requirement.
				}
			}
		}

		/* When IGate is enabled, all radio channels must have a callsign associated. */

		if len(ps.igate.t2_login) > 0 &&
			(ps.audio.chan_medium[i] == MEDIUM_RADIO || ps.audio.chan_medium[i] == MEDIUM_NETTNC) {
			if IsNoCall(ps.audio.mycall[i]) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: MYCALL must be set for receive channel %d before Rx IGate is allowed.\n", i)

				ps.igate.t2_login = ""
			}
			// Currently we can have only one transmit channel.
			// This might be generalized someday to allow more.
			if ps.igate.tx_chan >= 0 && IsNoCall(ps.audio.mycall[ps.igate.tx_chan]) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: MYCALL must be set for transmit channel %d before Tx IGate is allowed.\n", i)

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
} /* end config_init */

// handleADEVICE handles the ADEVICE[n] keyword.
func handleADEVICE(ps *parseState) bool {
	/*
	 * ADEVICE[n] 		- Name of input sound device, and optionally output, if different.
	 *
	 *			ADEVICE    plughw:1,0			-- same for in and out.
	 *			ADEVICE	   plughw:2,0  plughw:3,0	-- different in/out for a channel or channel pair.
	 *			ADEVICE1   udp:7355  default		-- from Software defined radio (SDR) via UDP.
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
		var i, iErr = strconv.Atoi(string(ps.keyword[7]))
		if iErr != nil {
			dw_printf("Config file: Could not parse ADEVICE number on line %d: %s.\n", ps.line, iErr)
			return true
		}

		if i < 0 || i >= MAX_ADEVS {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Device number %d out of range for ADEVICE command on line %d.\n", ps.adevice, ps.line)
			dw_printf("If you really need more than %d audio devices, increase MAX_ADEVS and recompile.\n", MAX_ADEVS)

			ps.adevice = 0

			return true
		}

		ps.adevice = i
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for ADEVICE command on line %d.\n", ps.line)
		rtfm()
		exit(1)
	}

	// Do not allow same adevice to be defined more than once.
	// Overriding the default for adevice 0 is ok.
	// In that case defined was 2.  That's why we check for 1, not just non-zero.

	if ps.audio.adev[ps.adevice].defined == 1 { // 1 means defined by user.
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: ADEVICE%d can't be defined more than once. Line %d.\n", ps.adevice, ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	// New case for release 1.8.

	if t == "=" {
		t = split("", false)
		if t != "" { //nolint:staticcheck
		}

		/////////  to be continued....  FIXME
	} else {
		/* First channel of device is valid. */
		// This might be changed to UDP or STDIN when the device name is examined.
		ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

		ps.audio.adev[ps.adevice].adevice_in = t
		ps.audio.adev[ps.adevice].adevice_out = t

		t = split("", false)
		if t != "" {
			// Different audio devices for receive and transmit.
			ps.audio.adev[ps.adevice].adevice_out = t
		}
	}
	return false
}

// handlePAIDEVICE handles PAIDEVICE[n].
func handlePAIDEVICE(ps *parseState) bool {
	// ps.keyword holds the original token e.g. "PAIDEVICE" or "PAIDEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Device number %d out of range for PADEVICE command on line %d.\n", ps.adevice, ps.line)
		ps.adevice = 0

		return true
	}

	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for PADEVICE command on line %d.\n", ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_in = t
	return false
}

// handlePAODEVICE handles PAODEVICE[n].
func handlePAODEVICE(ps *parseState) bool {
	// ps.keyword holds the original token e.g. "PAODEVICE" or "PAODEVICE1".
	ps.adevice = 0
	if len(ps.keyword) > 9 && unicode.IsDigit(rune(ps.keyword[9])) {
		ps.adevice = int(ps.keyword[9] - '0')
	}

	if ps.adevice < 0 || ps.adevice >= MAX_ADEVS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Device number %d out of range for PADEVICE command on line %d.\n", ps.adevice, ps.line)
		ps.adevice = 0

		return true
	}

	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing name of audio device for PADEVICE command on line %d.\n", ps.line)

		return true
	}

	ps.audio.adev[ps.adevice].defined = 1

	/* First channel of device is valid. */
	ps.audio.chan_medium[ADEVFIRSTCHAN(ps.adevice)] = MEDIUM_RADIO

	ps.audio.adev[ps.adevice].adevice_out = t
	return false
}

// handleARATE handles the ARATE keyword.
func handleARATE(ps *parseState) bool {
	/*
	 * ARATE 		- Audio samples per second, 11025, 22050, 44100, etc.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing audio sample rate for ARATE command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= MIN_SAMPLES_PER_SEC && n <= MAX_SAMPLES_PER_SEC {
		ps.audio.adev[ps.adevice].samples_per_sec = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Use a more reasonable audio sample rate in range of %d - %d.\n",
			ps.line, MIN_SAMPLES_PER_SEC, MAX_SAMPLES_PER_SEC)
	}
	return false
}

// handleACHANNELS handles the ACHANNELS keyword.
func handleACHANNELS(ps *parseState) bool {
	/*
	 * ACHANNELS 		- Number of audio channels for current device: 1 or 2
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing number of audio channels for ACHANNELS command.\n", ps.line)

		return true
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Number of audio channels must be 1 or 2.\n", ps.line)
	}
	return false
}

// handleCHANNEL handles the CHANNEL keyword.
func handleCHANNEL(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing channel number for CHANNEL command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < MAX_RADIO_CHANS {
		ps.channel = n

		if ps.audio.chan_medium[n] != MEDIUM_RADIO {
			if ps.audio.adev[ACHAN2ADEV(n)].defined == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Channel number %d is not valid because audio device %d is not defined.\n",
					ps.line, n, ACHAN2ADEV(n))
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Channel number %d is not valid because audio device %d is not in stereo.\n",
					ps.line, n, ACHAN2ADEV(n))
			}
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Channel number must in range of 0 to %d.\n", ps.line, MAX_RADIO_CHANS-1)
	}
	return false
}

// handleICHANNEL handles the ICHANNEL keyword.
func handleICHANNEL(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing virtual channel number for ICHANNEL command.\n", ps.line)

		return true
	}

	var ichan, _ = strconv.Atoi(t)
	if ichan >= MAX_RADIO_CHANS && ichan < MAX_TOTAL_CHANS {
		if ps.audio.chan_medium[ichan] == MEDIUM_NONE {
			ps.audio.chan_medium[ichan] = MEDIUM_IGATE

			// This is redundant but saves the time of searching through all
			// the channels for each packet.
			ps.audio.igate_vchannel = ichan
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: ICHANNEL can't use channel %d because it is already in use.\n", ps.line, ichan)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: ICHANNEL number must in range of %d to %d.\n", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}
	return false
}

// handleNCHANNEL handles the NCHANNEL keyword.
func handleNCHANNEL(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing virtual channel number for NCHANNEL command.\n", ps.line)

		return true
	}

	var nchan, _ = strconv.Atoi(t)
	if nchan >= MAX_RADIO_CHANS && nchan < MAX_TOTAL_CHANS {
		if ps.audio.chan_medium[nchan] == MEDIUM_NONE {
			ps.audio.chan_medium[nchan] = MEDIUM_NETTNC
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: NCHANNEL can't use channel %d because it is already in use.\n", ps.line, nchan)
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: NCHANNEL number must in range of %d to %d.\n", ps.line, MAX_RADIO_CHANS, MAX_TOTAL_CHANS-1)
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing network TNC address for NCHANNEL command.\n", ps.line)

		return true
	}

	ps.audio.nettnc_addr[nchan] = t

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing network TNC TCP port for NCHANNEL command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)
	ps.audio.nettnc_port[nchan] = n
	return false
}

// handleMYCALL handles the MYCALL keyword.
func handleMYCALL(ps *parseState) bool {
	/*
	 * MYCALL station
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing value for MYCALL command on line %d.\n", ps.line)

		return true
	} else {
		var strictness = 2

		/* Silently force upper case. */
		/* Might change to warning someday. */
		t = strings.ToUpper(t)

		var _, _, _, ok = ax25_parse_addr(-1, t, strictness)

		if !ok {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid value for MYCALL command on line %d.\n", ps.line)

			return true
		}

		// Definitely set for current channel.
		// Set for other channels which have not been set yet.

		for c := range MAX_TOTAL_CHANS {
			if c == ps.channel || IsNoCall(ps.audio.mycall[c]) {
				ps.audio.mycall[c] = t
			}
		}
	}
	return false
}

// handleMODEM handles the MODEM keyword.
func handleMODEM(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: MODEM can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing data transmission speed for MODEM command.\n", ps.line)

		return true
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Warning: Non-standard data rate of %d bits per second.  Are you sure?\n", ps.line, n)
		}
	} else {
		ps.audio.achan[ps.channel].baud = DEFAULT_BAUD

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable data rate. Using %d bits per second.\n",
			ps.line, ps.audio.achan[ps.channel].baud)
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
		return true
	}

	if alldigits(t) {
		/* old style */
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Old style (pre version 1.2) format will no longer be supported in next version.\n", ps.line)

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

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable mark tone frequency. Using %d.\n",
				ps.line, ps.audio.achan[ps.channel].mark_freq)
		}

		/* Get space frequency */

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Missing tone frequency for space.\n", ps.line)

			return true
		}

		n, _ = strconv.Atoi(t)
		if n >= 300 && n <= 5000 {
			ps.audio.achan[ps.channel].space_freq = n
		} else {
			ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable space tone frequency. Using %d.\n",
				ps.line, ps.audio.achan[ps.channel].space_freq)
		}

		/* Gently guide users toward new format. */

		if ps.audio.achan[ps.channel].baud == 1200 &&
			ps.audio.achan[ps.channel].mark_freq == 1200 &&
			ps.audio.achan[ps.channel].space_freq == 2200 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: The AFSK frequencies can be omitted when using the 1200 baud default 1200:2200.\n", ps.line)
		}

		if ps.audio.achan[ps.channel].baud == 300 &&
			ps.audio.achan[ps.channel].mark_freq == 1600 &&
			ps.audio.achan[ps.channel].space_freq == 1800 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: The AFSK frequencies can be omitted when using the 300 baud default 1600:1800.\n", ps.line)
		}

		/* New feature in 0.9 - Optional filter profile(s). */

		t = split("", false)
		if t != "" {
			/* Look for some combination of letter(s) and + */
			if unicode.IsLetter(rune(t[0])) || t[0] == '+' {
				/* Here we only catch something other than letters and + mixed in. */
				/* Later, we check for valid letters and no more than one letter if + specified. */
				if strings.ContainsFunc(t, func(r rune) bool {
					return !(unicode.IsLetter(r) || r == '+' || r == '-')
				}) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Demodulator type can only contain letters and + character.\n", ps.line)
				}

				ps.audio.achan[ps.channel].profiles = t

				t = split("", false)
				if len(ps.audio.achan[ps.channel].profiles) > 1 && t != "" {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Can't combine multiple demodulator types and multiple frequencies.\n", ps.line)

					return true
				}
			}
		}

		/* New feature in 0.9 - optional number of decoders and frequency offset between. */

		if t != "" {
			n, _ = strconv.Atoi(t)
			if n < 1 || n > MAX_SUBCHANS {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Number of demodulators is out of range. Using 3.\n", ps.line)

				n = 3
			}

			ps.audio.achan[ps.channel].num_freq = n

			t = split("", false)
			if t != "" {
				n, _ = strconv.Atoi(t)
				if n < 5 || n > int(math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Unreasonable value for offset between modems.  Using 50 Hz.\n", ps.line)

					n = 50
				}

				ps.audio.achan[ps.channel].offset = n

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: New style for multiple demodulators is %d@%d\n", ps.line,
					ps.audio.achan[ps.channel].num_freq, ps.audio.achan[ps.channel].offset)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Missing frequency offset between modems.  Using 50 Hz.\n", ps.line)

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

						text_color_set(DW_COLOR_ERROR)
						dw_printf("Line %d: Unreasonable mark tone frequency. Using %d instead.\n",
							ps.line, ps.audio.achan[ps.channel].mark_freq)
					}

					if ps.audio.achan[ps.channel].space_freq < 300 || ps.audio.achan[ps.channel].space_freq > 5000 {
						ps.audio.achan[ps.channel].space_freq = DEFAULT_SPACE_FREQ

						text_color_set(DW_COLOR_ERROR)
						dw_printf("Line %d: Unreasonable space tone frequency. Using %d instead.\n",
							ps.line, ps.audio.achan[ps.channel].space_freq)
					}
				}
			} else if strings.Contains(t, "@") { /* num@offset */
				var numStr, offsetStr, _ = strings.Cut(t, "@")
				var num, _ = strconv.Atoi(numStr)
				var offset, _ = strconv.Atoi(offsetStr)

				ps.audio.achan[ps.channel].num_freq = num
				ps.audio.achan[ps.channel].offset = offset

				if ps.audio.achan[ps.channel].num_freq < 1 || ps.audio.achan[ps.channel].num_freq > MAX_SUBCHANS {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Number of demodulators is out of range. Using 3.\n", ps.line)

					ps.audio.achan[ps.channel].num_freq = 3
				}

				if ps.audio.achan[ps.channel].offset < 5 ||
					float64(ps.audio.achan[ps.channel].offset) > math.Abs(float64(ps.audio.achan[ps.channel].mark_freq-ps.audio.achan[ps.channel].space_freq))/2 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Offset between demodulators is unreasonable. Using 50 Hz.\n", ps.line)

					ps.audio.achan[ps.channel].offset = 50
				}
			} else if alllettersorpm(t) { /* profile of letter(s) + - */
				// Will be validated later.
				ps.audio.achan[ps.channel].profiles = t
			} else if t[0] == '/' { /* /div */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 8 {
					ps.audio.achan[ps.channel].decimate = n
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Ignoring unreasonable sample rate division factor of %d.\n", ps.line, n)
				}
			} else if t[0] == '*' { /* *upsample */
				var n, _ = strconv.Atoi(t[1:])

				if n >= 1 && n <= 4 {
					ps.audio.achan[ps.channel].upsample = n
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Ignoring unreasonable upsample ratio of %d.\n", ps.line, n)
				}
			} else if strings.EqualFold(t, "G3RUH") { /* Force G3RUH modem regardless of default for speed. New in 1.6. */
				ps.audio.achan[ps.channel].modem_type = MODEM_SCRAMBLE
				ps.audio.achan[ps.channel].mark_freq = 0
				ps.audio.achan[ps.channel].space_freq = 0
			} else if strings.EqualFold(t, "V26A") || /* Compatible with direwolf versions <= 1.5.  New in 1.6. */
				strings.EqualFold(t, "V26B") { /* Compatible with MFJ-2400.  New in 1.6. */
				if ps.audio.achan[ps.channel].modem_type != MODEM_QPSK ||
					ps.audio.achan[ps.channel].baud != 2400 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: %s option can only be used with 2400 bps PSK.\n", ps.line, t)

					return true
				}

				ps.audio.achan[ps.channel].v26_alternative = IfThenElse((strings.EqualFold(t, "V26A")), V26_A, V26_B)
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Unrecognized option for MODEM: %s\n", ps.line, t)
			}

			t = split("", false)
		}

		/* A later place catches disallowed combination of + and @. */
		/* A later place sets /n for 300 baud if not specified by user. */

		//dw_printf ("debug: div = %d\n", p_audio_config.achan[channel].decimate);
	}
	return false
}

// handleDTMF handles the DTMF keyword.
func handleDTMF(ps *parseState) bool {
	/*
	 * DTMF  		- Enable DTMF decoder.
	 *
	 * Future possibilities:
	 *	Option to determine if it goes to APRStt gateway and/or application.
	 *	Disable normal demodulator to reduce CPU requirements.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: DTMF can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	ps.audio.achan[ps.channel].dtmf_decode = DTMF_DECODE_ON
	return false
}

// handleFIX_BITS handles the FIX_BITS keyword.
func handleFIX_BITS(ps *parseState) bool {
	/*
	 * FIX_BITS  n  [ APRS | AX25 | NONE ] [ PASSALL ]
	 *
	 *	- Attempt to fix frames with bad FCS.
	 *	- n is maximum number of bits to attempt fixing.
	 *	- Optional sanity check & allow everything even with bad FCS.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FIX_BITS can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for FIX_BITS command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if BitFixLevel(n) >= RETRY_NONE && BitFixLevel(n) < RETRY_MAX { // MAX is actually last valid +1
		ps.audio.achan[ps.channel].fix_bits = BitFixLevel(n)
	} else {
		ps.audio.achan[ps.channel].fix_bits = DEFAULT_FIX_BITS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid value %d for FIX_BITS. Using default of %d.\n",
			ps.line, n, ps.audio.achan[ps.channel].fix_bits)
	}

	if ps.audio.achan[ps.channel].fix_bits > DEFAULT_FIX_BITS {
		text_color_set(DW_COLOR_INFO)
		dw_printf("Line %d: Using a FIX_BITS value greater than %d is not recommended for normal operation.\n",
			ps.line, DEFAULT_FIX_BITS)
		dw_printf("FIX_BITS > 1 was an interesting experiment but turned out to be a bad idea.\n")
		dw_printf("Don't be surprised if it takes 100%% CPU, direwolf can't keep up with the audio stream,\n")
		dw_printf("and you see messages like \"Audio input device 0 error code -32: Broken pipe\"\n")
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

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: There is an old saying, \"Be careful what you ask for because you might get it.\"\n", ps.line)
			dw_printf("The PASSALL option means allow all frames even when they are invalid.\n")
			dw_printf("You are asking to receive random trash and you WILL get your wish.\n")
			dw_printf("Don't complain when you see all sorts of random garbage.  That's what you asked for.\n")
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid option '%s' for FIX_BITS.\n", ps.line, t)
		}

		t = split("", false)
	}
	return false
}

// handlePTTDCDCON handles the PTTDCDCON keyword.
func handlePTTDCDCON(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: PTT can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
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

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: Missing output control device for %s command.\n",
			ps.line, otname)

		return true
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, otname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_GPIO
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO chip name for %s.\n", ps.line, otname)
			dw_printf("Use the \"gpioinfo\" command to get a list of gpio chip names and corresponding I/O lines.\n")

			return true
		}

		// Issue 590.  Originally we used the chip name, like gpiochip3, and fed it into
		// gpiod_chip_open_by_name.   This function has disappeared in Debian 13 Trixie.
		// We must now specify the full device path, like /dev/gpiochip3, for the only
		// remaining open function gpiod_chip_open.
		// We will allow the user to specify either the name or full device path.
		// While we are here, also allow only the number as used by the gpiod utilities.

		if t[0] == '/' { // Looks like device path.  Use as given.
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = t
		} else if unicode.IsDigit(rune(t[0])) { // or if digit, prepend "/dev/gpiochip"
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = "/dev/gpiochip" + t
		} else { // otherwise, prepend "/dev/" to the name
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_name = "/dev/" + t
		}

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, otname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_GPIOD
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing LPT bit number for %s.\n", ps.line, otname)

			return true
		}

		var lpt, _ = strconv.Atoi(t)
		if lpt < 0 {
			ps.audio.achan[ps.channel].octrl[ot].ptt_lpt_bit = -1 * lpt
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			ps.audio.achan[ps.channel].octrl[ot].ptt_lpt_bit = lpt
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_LPT
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing model number for hamlib.\n", ps.line)

			return true
		}

		if strings.EqualFold(t, "AUTO") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_model = -1
		} else {
			if !alldigits(t) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: A rig number, not a name, is required here.\n", ps.line)
				dw_printf("For example, if you have a Yaesu FT-847, specify 101.\n")
				dw_printf("See https://github.com/Hamlib/Hamlib/wiki/Supported-Radios for more details.\n")

				return true
			}

			var n, _ = strconv.Atoi(t)
			if n < 1 || n > 9999 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Unreasonable model number %d for hamlib.\n", ps.line, n)

				return true
			}

			ps.audio.achan[ps.channel].octrl[ot].ptt_model = n
		}

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing port for hamlib.\n", ps.line)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_device = t

		// Optional serial port rate for CAT control PTT.

		t = split("", false)
		if t != "" {
			if !alldigits(t) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: An optional number is required here for CAT serial port speed: %s\n", ps.line, t)

				return true
			}
			var n, _ = strconv.Atoi(t)
			ps.audio.achan[ps.channel].octrl[ot].ptt_rate = n
		}

		t = split("", false)
		if t != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: %s was not expected after model & port for hamlib.\n", ps.line, t)
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_HAMLIB

		// #else
		/* TODO KG
		   #if __WIN32__
		   	      text_color_set(DW_COLOR_ERROR);
		   	      dw_printf ("Config file line %d: Windows version of direwolf does not support HAMLIB.\n", ps.line);
		   	      exit (EXIT_FAILURE);
		   #else
		*/
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: %s with RIG is only available when hamlib support is enabled.\n", ps.line, otname)
		dw_printf("You must rebuild direwolf with hamlib support.\n")
		dw_printf("See User Guide for details.\n")
		// #endif

		//#endif
	} else if strings.EqualFold(t, "CM108") {
		/* CM108 - GPIO of USB sound card. case, Linux and Windows only. */

		// TODO KG #if USE_CM108
		if ot != OCTYPE_PTT {
			// Future project:  Allow DCD and CON via the same device.
			// This gets more complicated because we can't selectively change a single GPIO bit.
			// We would need to keep track of what is currently there, change one bit, in our local
			// copy of the status and then write out the byte for all of the pins.
			// Let's keep it simple with just PTT for the first stab at this.
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: PTT CM108 option is only valid for PTT, not %s.\n", ps.line, otname)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = 3 // All known designs use GPIO 3.
		// User can override for special cases.
		ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false // High for transmit.
		ps.audio.achan[ps.channel].octrl[ot].ptt_device = ""

		// Try to find PTT device for audio output device.
		// Simplifiying assumption is that we have one radio per USB Audio Adapter.
		// Failure at this point is not an error.
		// See if config file sets it explicitly before complaining.

		ps.audio.achan[ps.channel].octrl[ot].ptt_device = cm108_find_ptt(ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)

		for {
			t = split("", false)
			if t == "" {
				break
			}

			if t[0] == '-' {
				var gpio, _ = strconv.Atoi(t[1:])
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = -1 * gpio
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
			} else if unicode.IsDigit(rune(t[0])) {
				var gpio, _ = strconv.Atoi(t)
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num = gpio
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
			} else if t[0] == '/' {
				ps.audio.achan[ps.channel].octrl[ot].ptt_device = t
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Found \"%s\" when expecting GPIO number or device name like /dev/hidraw1.\n", ps.line, t)

				return true
			}
		}

		if ps.audio.achan[ps.channel].octrl[ot].out_gpio_num < 1 || ps.audio.achan[ps.channel].octrl[ot].out_gpio_num > 8 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: CM108 GPIO number %d is not in range of 1 thru 8.\n", ps.line,
				ps.audio.achan[ps.channel].octrl[ot].out_gpio_num)

			return true
		}

		if ps.audio.achan[ps.channel].octrl[ot].ptt_device == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Could not determine USB Audio GPIO PTT device for audio output %s.\n", ps.line,
				ps.audio.adev[ACHAN2ADEV(ps.channel)].adevice_out)
			/* TODO KG
			#if __WIN32__
				        dw_printf ("You must explicitly mention a HID path.\n");
			#else
			*/
			dw_printf("You must explicitly mention a device name such as /dev/hidraw1.\n")
			dw_printf("Run \"cm108\" utility to get a list.\n")
			dw_printf("See Interface Guide for details.\n")

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_CM108

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
		ps.audio.achan[ps.channel].octrl[ot].ptt_device = t

		t = split("", false)
		if t == "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing RTS or DTR after %s device name.\n",
				ps.line, otname)

			return true
		}

		if strings.EqualFold(t, "rts") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_RTS
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		} else if strings.EqualFold(t, "dtr") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_DTR
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = false
		} else if strings.EqualFold(t, "-rts") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_RTS
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else if strings.EqualFold(t, "-dtr") {
			ps.audio.achan[ps.channel].octrl[ot].ptt_line = PTT_LINE_DTR
			ps.audio.achan[ps.channel].octrl[ot].ptt_invert = true
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Expected RTS or DTR after %s device name.\n",
				ps.line, otname)

			return true
		}

		ps.audio.achan[ps.channel].octrl[ot].ptt_method = PTT_METHOD_SERIAL

		/* In version 1.2, we allow a second one for same serial port. */
		/* Some interfaces want the two control lines driven with opposite polarity. */
		/* e.g.   PTT COM1 RTS -DTR  */

		t = split("", false)
		if t != "" {
			if strings.EqualFold(t, "rts") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_RTS
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = false
			} else if strings.EqualFold(t, "dtr") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_DTR
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = false
			} else if strings.EqualFold(t, "-rts") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_RTS
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = true
			} else if strings.EqualFold(t, "-dtr") {
				ps.audio.achan[ps.channel].octrl[ot].ptt_line2 = PTT_LINE_DTR
				ps.audio.achan[ps.channel].octrl[ot].ptt_invert2 = true
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file line %d: Expected RTS or DTR after first RTS or DTR.\n",
					ps.line)

				return true
			}

			/* Would not make sense to specify the same one twice. */

			if ps.audio.achan[ps.channel].octrl[ot].ptt_line == ps.audio.achan[ps.channel].octrl[ot].ptt_line2 {
				dw_printf("Config file line %d: Doesn't make sense to specify the some control line twice.\n",
					ps.line)
			}
		} /* end of second serial port control ps.line. */
	} /* end of serial port case. */
	/* end of PTT, DCD, CON */
	return false
}

// handleTXINH handles the TXINH keyword.
func handleTXINH(ps *parseState) bool {
	/*
	 * INPUTS
	 *
	 * TXINH - TX holdoff input
	 *
	 * TXINH GPIO [-]gpio-num (only type supported so far)
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXINH can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}
	var itname = "TXINH"

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file line %d: Missing input type name for %s command.\n", ps.line, itname)

		return true
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file line %d: Missing GPIO number for %s.\n", ps.line, itname)

			return true
		}

		var gpio, _ = strconv.Atoi(t)
		if gpio < 0 {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = -1 * gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = true
		} else {
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].in_gpio_num = gpio
			ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].invert = false
		}

		ps.audio.achan[ps.channel].ictrl[ICTYPE_TXINH].method = PTT_METHOD_GPIO
		// #endif
	}
	return false
}

// handleDWAIT handles the DWAIT keyword.
func handleDWAIT(ps *parseState) bool {
	/*
	 * DWAIT n		- Extra delay for receiver squelch. n = 10 mS units.
	 *
	 * Why did I do this?  Just add more to TXDELAY.
	 * Now undocumented in User Guide.  Might disappear someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: DWAIT can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing delay time for DWAIT command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		ps.audio.achan[ps.channel].dwait = n
	} else {
		ps.audio.achan[ps.channel].dwait = DEFAULT_DWAIT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid delay time for DWAIT. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].dwait)
	}
	return false
}

// handleSLOTTIME handles the SLOTTIME keyword.
func handleSLOTTIME(ps *parseState) bool {
	/*
	 * SLOTTIME n		- For non-digipeat transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: SLOTTIME can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing delay time for SLOTTIME command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n < 50 {
		// 0 = User has no clue.  This would be no delay.
		// 10 = Default.
		// 50 = Half second.  User might think it is mSec and use 100.
		ps.audio.achan[ps.channel].slottime = n
	} else {
		ps.audio.achan[ps.channel].slottime = DEFAULT_SLOTTIME

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid delay time for persist algorithm. Using default %d.\n",
			ps.line, ps.audio.achan[ps.channel].slottime)
		dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
		dw_printf("section, to understand what this means.\n")
		dw_printf("Why don't you just use the default?\n")
	}
	return false
}

// handlePERSIST handles the PERSIST keyword.
func handlePERSIST(ps *parseState) bool {
	/*
	 * PERSIST 		- For non-digipeat transmit delay timing.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: PERSIST can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing probability for PERSIST command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 5 && n <= 250 {
		ps.audio.achan[ps.channel].persist = n
	} else {
		ps.audio.achan[ps.channel].persist = DEFAULT_PERSIST

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid probability for persist algorithm. Using default %d.\n",
			ps.line, ps.audio.achan[ps.channel].persist)
		dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
		dw_printf("section, to understand what this means.\n")
		dw_printf("Why don't you just use the default?\n")
	}
	return false
}

// handleTXDELAY handles the TXDELAY keyword.
func handleTXDELAY(ps *parseState) bool {
	/*
	 * TXDELAY n		- For transmit delay timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXDELAY can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for TXDELAY command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		text_color_set(DW_COLOR_ERROR)

		if n < 10 {
			dw_printf("Line %d: Setting TXDELAY this small is a REALLY BAD idea if you want other stations to hear you.\n",
				ps.line)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default rather than reducing reliability?\n")
		} else if n >= 100 {
			dw_printf("Line %d: Keeping with tradition, going back to the 1980s, TXDELAY is in 10 millisecond units.\n",
				ps.line)
			dw_printf("Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n",
				ps.line, n, float64(n)*10./1000.)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default?\n")
		}

		ps.audio.achan[ps.channel].txdelay = n
	} else {
		ps.audio.achan[ps.channel].txdelay = DEFAULT_TXDELAY

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid time for transmit delay. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].txdelay)
	}
	return false
}

// handleTXTAIL handles the TXTAIL keyword.
func handleTXTAIL(ps *parseState) bool {
	/*
	 * TXTAIL n		- For transmit timing. n = 10 mS units.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TXTAIL can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for TXTAIL command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= 255 {
		if n < 5 {
			dw_printf("Line %d: Setting TXTAIL that small is a REALLY BAD idea if you want other stations to hear you.\n",
				ps.line)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default rather than reducing reliability?\n")
		} else if n >= 50 {
			dw_printf("Line %d: Keeping with tradition, going back to the 1980s, TXTAIL is in 10 millisecond units.\n",
				ps.line)
			dw_printf("Line %d: The value %d would be %.3f seconds which seems rather excessive.  Are you sure you want that?\n",
				ps.line, n, float64(n)*10./1000.)
			dw_printf("Read the Dire Wolf User Guide, \"Radio Channel - Transmit Timing\"\n")
			dw_printf("section, to understand what this means.\n")
			dw_printf("Why don't you just use the default?\n")
		}

		ps.audio.achan[ps.channel].txtail = n
	} else {
		ps.audio.achan[ps.channel].txtail = DEFAULT_TXTAIL

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid time for transmit timing. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].txtail)
	}
	return false
}

// handleFULLDUP handles the FULLDUP keyword.
func handleFULLDUP(ps *parseState) bool {
	/*
	 * FULLDUP  {on|off} 		- Full Duplex
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FULLDUP can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing parameter for FULLDUP command.  Expecting ON or OFF.\n", ps.line)

		return true
	}

	if strings.EqualFold(t, "ON") {
		ps.audio.achan[ps.channel].fulldup = true
	} else if strings.EqualFold(t, "OFF") {
		ps.audio.achan[ps.channel].fulldup = false
	} else {
		ps.audio.achan[ps.channel].fulldup = false

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Expected ON or OFF for FULLDUP.\n", ps.line)
	}
	return false
}

// handleSPEECH handles the SPEECH keyword.
func handleSPEECH(ps *parseState) bool {
	/*
	 * SPEECH  script
	 *
	 * Specify script for text-to-speech function.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: SPEECH can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing script for Text-to-Speech function.\n", ps.line)

		return true
	}

	/* See if we can run it. */

	/*
	   TODO KG Do we *actually* want to do this...? If so, let's do it when we've ported this to Go...

	   	 if (xmit_speak_it(t, -1, " ") == 0) {
	   	   if (strlcpy (ps.audio.tts_script, t, sizeof(ps.audio.tts_script)) >= sizeof(ps.audio.tts_script)) {
	   	     text_color_set(DW_COLOR_ERROR);
	   	     dw_printf ("Line %d: Script for text-to-speech function is too long.\n", ps.line);
	   	   }
	   	 } else {
	   	   text_color_set(DW_COLOR_ERROR);
	   	   dw_printf ("Line %d: Error trying to run Text-to-Speech function.\n", ps.line);
	   	   continue;
	   	}
	*/
	return false
}

// handleFX25TX handles the FX25TX keyword.
func handleFX25TX(ps *parseState) bool {
	/*
	 * FX25TX n		- Enable FX.25 transmission.  Default off.
	 *				0 = off, 1 = auto mode, others are suggestions for testing
	 *				or special cases.  16, 32, 64 is number of parity bytes to add.
	 *				Also set by "-X n" command line option.
	 *				V1.7 changed from global to per-channel setting.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FX25TX can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing FEC mode for FX25TX command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 200 {
		ps.audio.achan[ps.channel].fx25_strength = n
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25
	} else {
		ps.audio.achan[ps.channel].fx25_strength = 1
		ps.audio.achan[ps.channel].layer2_xmit = LAYER2_FX25

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable value for FX.25 transmission mode. Using %d.\n",
			ps.line, ps.audio.achan[ps.channel].fx25_strength)
	}
	return false
}

// handleFX25AUTO handles the FX25AUTO keyword.
func handleFX25AUTO(ps *parseState) bool {
	/*
	 * FX25AUTO n		- Enable Automatic use of FX.25 for connected mode.  *** Not Implemented ***
	 *				Automatically enable, for that session only, when an identical
	 *				frame is sent more than this number of times.
	 *				Default 5 based on half of default RETRY.
	 *				0 to disable feature.
	 *				Current a global setting.  Could be per channel someday.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: FX25AUTO can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing count for FX25AUTO command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 20 {
		ps.audio.fx25_auto_enable = n
	} else {
		ps.audio.fx25_auto_enable = AX25_N2_RETRY_DEFAULT / 2

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable count for connected mode automatic FX.25. Using %d.\n",
			ps.line, ps.audio.fx25_auto_enable)
	}
	return false
}

// handleIL2PTX handles the IL2PTX keyword.
func handleIL2PTX(ps *parseState) bool {
	/*
	 * IL2PTX  [ + - ] [ 0 1 ]	- Enable IL2P transmission.  Default off.
	 *				"+" means normal polarity. Redundant since it is the default.
	 *					(command line -I for first channel)
	 *				"-" means inverted polarity. Do not use for 1200 bps.
	 *					(command line -i for first channel)
	 *				"0" means weak FEC.  Not recommended.
	 *				"1" means stronger FEC.  "Max FEC."  Default if not specified.
	 */
	if ps.channel < 0 || ps.channel >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: IL2PTX can only be used with radio channel 0 - %d.\n", ps.line, MAX_RADIO_CHANS-1)

		return true
	}

	ps.audio.achan[ps.channel].layer2_xmit = LAYER2_IL2P
	ps.audio.achan[ps.channel].il2p_max_fec = 1
	ps.audio.achan[ps.channel].il2p_invert_polarity = 0

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
			default:
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Invalid parameter '%c' for IL2PTX command.\n", ps.line, c)

				continue
			}
		}
	}
	return false
}

// handleDIGIPEAT handles the DIGIPEAT keyword.
func handleDIGIPEAT(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	// Channels specified must be radio channels or network TNCs.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
		ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing alias pattern on line %d.\n", ps.line)

		return true
	}

	var r, err = regexp.Compile(t)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Invalid alias matching pattern on line %d:\n%s\n", ps.line, err)

		return true
	}

	ps.digi.alias[from_chan][to_chan] = r

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing wide pattern on line %d.\n", ps.line)

		return true
	}

	r, err = regexp.Compile(t)
	if err != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Invalid wide matching pattern on line %d:\n%s\n", ps.line, err)

		return true
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Preemptive digipeating DROP option is discouraged.\n", ps.line)
			dw_printf("It can create a via path which is misleading about the actual path taken.\n")
			dw_printf("PREEMPT is the best choice for this feature.\n")

			ps.digi.preempt[from_chan][to_chan] = PREEMPT_DROP
			t = split("", false)
		} else if strings.EqualFold(t, "MARK") {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Preemptive digipeating MARK option is discouraged.\n", ps.line)
			dw_printf("It can create a via path which is misleading about the actual path taken.\n")
			dw_printf("PREEMPT is the best choice for this feature.\n")

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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Found \"%s\" where end of line was expected.\n", ps.line, t)
	}
	return false
}

// handleDEDUPE handles the DEDUPE keyword.
func handleDEDUPE(ps *parseState) bool {
	/*
	 * DEDUPE 		- Time to suppress digipeating of duplicate APRS packets.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing time for DEDUPE command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n < 600 {
		ps.digi.dedupe_time = n
	} else {
		ps.digi.dedupe_time = DEFAULT_DEDUPE

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unreasonable value for dedupe time. Using %d.\n",
			ps.line, ps.digi.dedupe_time)
	}
	return false
}

// handleREGEN handles the REGEN keyword.
func handleREGEN(ps *parseState) bool {
	/*
	 * REGEN 		- Signal regeneration.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// Only radio channels are valid for regenerate.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	ps.digi.regen[from_chan][to_chan] = true
	return false
}

// handleCDIGIPEAT handles the CDIGIPEAT keyword.
func handleCDIGIPEAT(ps *parseState) bool {
	/*
	 * ==================== Connected Digipeater parameters ====================
	 */

	/*
	 * CDIGIPEAT  from-chan  to-chan [ alias-pattern ]
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for FROM-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var from_chan, _ = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// For connected mode Link layer, only internal modems should be allowed.
	// A network TNC probably would not provide information about channel status.
	// There is discussion about this in the document called
	// Why-is-9600-only-twice-as-fast-as-1200.pdf

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)
		dw_printf("Only internal modems can be used for connected mode packet.\n")

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if !alldigits(t) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: '%s' is not allowed for TO-channel.  It must be a number.\n",
			ps.line, t)

		return true
	}

	var to_chan, _ = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)
		dw_printf("Only internal modems can be used for connected mode packet.\n")

		return true
	}

	t = split("", false)
	if t != "" {
		var r, err = regexp.Compile(t)
		if err == nil {
			ps.cdigi.alias[from_chan][to_chan] = r
			ps.cdigi.has_alias[from_chan][to_chan] = true
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid alias matching pattern on line %d:\n%s\n", ps.line, err)

			return true
		}

		t = split("", false)
	}

	ps.cdigi.enabled[from_chan][to_chan] = true

	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Found \"%s\" where end of line was expected.\n", ps.line, t)
	}
	return false
}

// handleFILTER handles the FILTER keyword.
func handleFILTER(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	if t[0] == 'i' || t[0] == 'I' {
		from_chan = MAX_TOTAL_CHANS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FILTER IG ... on line %d.\n", ps.line)
		dw_printf("Warning! Don't mess with IS>RF filtering unless you are an expert and have an unusual situation.\n")
		dw_printf("Warning! The default is fine for nearly all situations.\n")
		dw_printf("Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .\n")
		dw_printf("Warning! If you insist, be sure to add \" | i/180 \" so you don't break messaging.\n")
	} else {
		var fromChanErr error

		from_chan, fromChanErr = strconv.Atoi(t)
		if from_chan < 0 || from_chan >= MAX_TOTAL_CHANS || fromChanErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Filter FROM-channel must be in range of 0 to %d or \"IG\" on line %d.\n",
				MAX_TOTAL_CHANS-1, ps.line)

			return true
		}

		if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[from_chan] != MEDIUM_NETTNC {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
				ps.line, from_chan)

			return true
		}

		if ps.audio.chan_medium[from_chan] == MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Use 'IG' rather than %d for FROM-channel.\n",
				ps.line, from_chan)

			return true
		}
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	if t[0] == 'i' || t[0] == 'I' {
		to_chan = MAX_TOTAL_CHANS

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: FILTER ... IG ... on line %d.\n", ps.line)
		dw_printf("Warning! Don't mess with RF>IS filtering unless you are an expert and have an unusual situation.\n")
		dw_printf("Warning! Expected behavior is for everything to go from RF to IS.\n")
		dw_printf("Warning! The default is fine for nearly all situations.\n")
		dw_printf("Warning! Be sure to read carefully and understand  \"Successful-APRS-Gateway-Operation.pdf\" .\n")
	} else {
		var toChanErr error

		to_chan, toChanErr = strconv.Atoi(t)
		if to_chan < 0 || to_chan >= MAX_TOTAL_CHANS || toChanErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Filter TO-channel must be in range of 0 to %d or \"IG\" on line %d.\n",
				MAX_TOTAL_CHANS-1, ps.line)

			return true
		}

		if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO &&
			ps.audio.chan_medium[to_chan] != MEDIUM_NETTNC {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
				ps.line, to_chan)

			return true
		}

		if ps.audio.chan_medium[to_chan] == MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Use 'IG' rather than %d for TO-channel.\n",
				ps.line, to_chan)

			return true
		}
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	if ps.digi.filter_str[from_chan][to_chan] != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Replacing previous filter for same from/to pair:\n        %s\n", ps.line, ps.digi.filter_str[from_chan][to_chan])
		ps.digi.filter_str[from_chan][to_chan] = ""
	}

	ps.digi.filter_str[from_chan][to_chan] = t

	// TODO:  Do a test run to see errors now instead of waiting.
	return false
}

// handleCFILTER handles the CFILTER keyword.
func handleCFILTER(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing FROM-channel on line %d.\n", ps.line)

		return true
	}

	var from_chan, fromChanErr = strconv.Atoi(t)
	if from_chan < 0 || from_chan >= MAX_RADIO_CHANS || fromChanErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Filter FROM-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	// DO NOT allow a network TNC here.
	// Must be internal modem to have necessary knowledge about channel status.

	if ps.audio.chan_medium[from_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: FROM-channel %d is not valid.\n",
			ps.line, from_chan)

		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing TO-channel on line %d.\n", ps.line)

		return true
	}

	var to_chan, toChanErr = strconv.Atoi(t)
	if to_chan < 0 || to_chan >= MAX_RADIO_CHANS || toChanErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Filter TO-channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)

		return true
	}

	if ps.audio.chan_medium[to_chan] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TO-channel %d is not valid.\n",
			ps.line, to_chan)

		return true
	}

	t = split("", true) /* Take rest of ps.line including spaces. */

	if t == "" {
		t = " " /* Empty means permit nothing. */
	}

	ps.cdigi.cfilter_str[from_chan][to_chan] = t

	// TODO1.2:  Do a test run to see errors now instead of waiting.
	return false
}

// handleTTCORRAL handles the TTCORRAL keyword.
func handleTTCORRAL(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing latitude for TTCORRAL command.\n", ps.line)
		return true
	}
	ps.tt.corral_lat = parse_ll(t, LAT, ps.line)

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing longitude for TTCORRAL command.\n", ps.line)
		return true
	}
	ps.tt.corral_lon = parse_ll(t, LON, ps.line)

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing longitude for TTCORRAL command.\n", ps.line)
		return true
	}
	ps.tt.corral_offset = parse_ll(t, LAT, ps.line)
	if ps.tt.corral_offset == 1 ||
		ps.tt.corral_offset == 2 ||
		ps.tt.corral_offset == 3 {
		ps.tt.corral_ambiguity = int(ps.tt.corral_offset)
		ps.tt.corral_offset = 0
	}

	// dw_printf ("DEBUG: corral %f %f %f %d\n", p_tt_config.corral_lat,
	//
	//	p_tt_config.corral_lon, p_tt_config.corral_offset, p_tt_config.corral_ambiguity);
	return false
}

// handleTTPOINT handles the TTPOINT keyword.
func handleTTPOINT(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTPOINT command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTPOINT pattern must begin with upper case 'B'.\n", ps.line)
	}

	for _, j := range t[1:] {
		if !unicode.IsDigit(j) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTPOINT pattern must be B and digits only.\n", ps.line)
		}
	}

	// Latitude

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing latitude for TTPOINT command.\n", ps.line)
		return true
	}
	tl.point.lat = parse_ll(t, LAT, ps.line)

	// Longitude

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing longitude for TTPOINT command.\n", ps.line)
		return true
	}
	tl.point.lon = parse_ll(t, LON, ps.line)

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTVECTOR handles the TTVECTOR keyword.
func handleTTVECTOR(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTVECTOR command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTVECTOR pattern must begin with upper case 'B'.\n", ps.line)
	}
	if !strings.HasPrefix(t[1:], "5bbb") {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTVECTOR pattern would normally contain \"5bbb\".\n", ps.line)
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'b' && t[j] != 'd' {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTVECTOR pattern must contain only B, digits, b, and d.\n", ps.line)
		}
	}

	// Latitude

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing latitude for TTVECTOR command.\n", ps.line)
		return true
	}
	tl.vector.lat = parse_ll(t, LAT, ps.line)

	// Longitude

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing longitude for TTVECTOR command.\n", ps.line)
		return true
	}
	tl.vector.lon = parse_ll(t, LON, ps.line)

	// Longitude

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing scale for TTVECTOR command.\n", ps.line)
		return true
	}
	var scale, _ = strconv.ParseFloat(t, 64)

	// Unit.

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing unit for TTVECTOR command.\n", ps.line)
		return true
	}

	var meters float64
	for j := 0; j < len(units) && meters == 0; j++ {
		if strings.EqualFold(units[j].name, t) {
			meters = units[j].meters
		}
	}
	if meters == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unrecognized unit for TTVECTOR command.  Using miles.\n", ps.line)
		meters = 1609.344
	}
	tl.vector.scale = scale * meters

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTGRID handles the TTGRID keyword.
func handleTTGRID(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTGRID command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTGRID pattern must begin with upper case 'B'.\n", ps.line)
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTGRID pattern must be B, optional digit, xxx, yyy.\n", ps.line)
		}
	}

	// Minimum Latitude - all zeros in received data

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing minimum latitude for TTGRID command.\n", ps.line)
		return true
	}
	tl.grid.lat0 = parse_ll(t, LAT, ps.line)

	// Minimum Longitude - all zeros in received data

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing minimum longitude for TTGRID command.\n", ps.line)
		return true
	}
	tl.grid.lon0 = parse_ll(t, LON, ps.line)

	// Maximum Latitude - all nines in received data

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing maximum latitude for TTGRID command.\n", ps.line)
		return true
	}
	tl.grid.lat9 = parse_ll(t, LAT, ps.line)

	// Maximum Longitude - all nines in received data

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing maximum longitude for TTGRID command.\n", ps.line)
		return true
	}
	tl.grid.lon9 = parse_ll(t, LON, ps.line)

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTUTM handles the TTUTM keyword.
func handleTTUTM(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTUTM command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTUTM pattern must begin with upper case 'B'.\n", ps.line)
		return true
	}
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTUTM pattern must be B, optional digit, xxx, yyy.\n", ps.line)
			// Bail out somehow.  continue would match inner for.
		}
	}

	// Zone 1 - 60 and optional latitudinal letter.

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing zone for TTUTM command.\n", ps.line)
		return true
	}

	tl.utm.latband, tl.utm.hemi, tl.utm.lzone = parse_utm_zone(t)

	// Optional scale.

	t = split("", false)
	if t != "" {

		tl.utm.scale, _ = strconv.ParseFloat(t, 64)

		// Optional x offset.

		t = split("", false)
		if t != "" {

			tl.utm.x_offset, _ = strconv.ParseFloat(t, 64)

			// Optional y offset.

			t = split("", false)
			if t != "" {

				tl.utm.y_offset, _ = strconv.ParseFloat(t, 64)
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid UTM location: \n%s\n", ps.line, geoErr)
		return true
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTUSNGMGRS handles the TTUSNGMGRS keyword.
func handleTTUSNGMGRS(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTUSNG/TTMGRS command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTUSNG/TTMGRS pattern must begin with upper case 'B'.\n", ps.line)
		return true
	}
	var num_x = 0
	var num_y = 0
	for j := 1; j < len(t); j++ {
		if !unicode.IsDigit(rune(t[j])) && t[j] != 'x' && t[j] != 'y' {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTUSNG/TTMGRS pattern must be B, optional digit, xxx, yyy.\n", ps.line)
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTUSNG/TTMGRS must have 1 to 5 x and same number y.\n", ps.line)
		return true
	}

	// Zone 1 - 60 and optional latitudinal letter.

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing zone & square for TTUSNG/TTMGRS command.\n", ps.line)
		return true
	}
	tl.mgrs.zone = t

	// Try converting it rather do our own error checking.

	var _, convertErr = coordconv.DefaultMGRSConverter.ConvertToGeodetic(tl.mgrs.zone)
	if convertErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid USNG/MGRS zone & square:  %s\n%s\n", ps.line, tl.mgrs.zone, convertErr)
		return true
	}

	// Should be the end.

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unexpected stuff at end ignored:  %s\n", ps.line, t)
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTMHEAD handles the TTMHEAD keyword.
func handleTTMHEAD(ps *parseState) bool {
	/*
	 * TTMHEAD 		- Define pattern to be used for Maidenhead Locator.
	 *
	 * TTMHEAD   pattern   [ prefix ]
	 *
	 *			Pattern would be  B[0-9A-D]xxxx...
	 *			Optional prefix is 10, 6, or 4 digits.
	 *
	 *			The total number of digts in both must be 4, 6, 10, or 12.
	 */

	// TODO1.3:  TTMHEAD needs testing.

	var tl = new(ttloc_s)
	tl.ttlocType = TTLOC_MHEAD

	// Pattern: B, optional additional button, some number of xxxx... for matching

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTMHEAD command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTMHEAD pattern must begin with upper case 'B'.\n", ps.line)
		return true
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTMHEAD must have only lower case x to match received data.\n", ps.line)
		return true
	}

	// optional prefix

	t = split("", false)
	if t != "" {
		tl.mhead.prefix = t

		if !alldigits(t) || (len(t) != 4 && len(t) != 6 && len(t) != 10) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTMHEAD prefix must be 4, 6, or 10 digits.\n", ps.line)
			return true
		}

		var _, mhErrors = tt_mhead_to_text(t, false)
		if mhErrors != 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTMHEAD prefix not a valid DTMF sequence.\n", ps.line)
			return true
		}
	}

	var k = len(tl.mhead.prefix) + count_x

	if k != 4 && k != 6 && k != 10 && k != 12 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTMHEAD prefix and user data must have a total of 4, 6, 10, or 12 digits.\n", ps.line)
		return true
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTSATSQ handles the TTSATSQ keyword.
func handleTTSATSQ(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTSATSQ command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTSATSQ pattern must begin with upper case 'B'.\n", ps.line)
		return true
	}

	// Optionally one of 0-9ABCD

	var j int
	if len(t) > 1 && (strings.ContainsRune("ABCD", rune(t[1])) || unicode.IsDigit(rune(t[1]))) {
		j = 2
	} else {
		j = 1
	}

	if t[j:] != "xxxx" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTSATSQ pattern must end with exactly xxxx in lower case.\n", ps.line)
		return true
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTAMBIG handles the TTAMBIG keyword.
func handleTTAMBIG(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTAMBIG command.\n", ps.line)
		return true
	}
	tl.pattern = t

	if t[0] != 'B' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTAMBIG pattern must begin with upper case 'B'.\n", ps.line)
		return true
	}

	// Optionally one of 0-9ABCD

	var j int
	if len(t) > 1 && (strings.ContainsRune("ABCD", rune(t[1])) || unicode.IsDigit(rune(t[1]))) {
		j = 2
	} else {
		j = 1
	}

	if t[j:] != "x" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: TTAMBIG pattern must end with exactly one x in lower case.\n", ps.line)
		return true
	}

	ps.tt.ttlocs = append(ps.tt.ttlocs, tl)
	return false
}

// handleTTMACRO handles the TTMACRO keyword.
func handleTTMACRO(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing pattern for TTMACRO command.\n", ps.line)
		return true
	}
	tl.pattern = t

	var p_count [3]int
	var tt_error = 0

	for j := 0; j < len(t); j++ {
		if !strings.ContainsRune("0123456789ABCDxyz", rune(t[j])) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTMACRO pattern can contain only digits, A, B, C, D, and lower case x, y, or z.\n", ps.line)
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing definition for TTMACRO command.\n", ps.line)
		tl.macro.definition = "" // Don't die on null pointer later.
		return true
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
				var ttemp, errs = tt_text_to_call10(stemp.String(), false)
				if errs == 0 {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("DEBUG Line %d: AC{%s} -> AC%s\n", line, stemp, ttemp);
					otemp.WriteString("AC" + ttemp)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: AC{%s} could not be converted to tones for callsign.\n", ps.line, stemp.String())
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: AC{... is missing matching } in TTMACRO definition.\n", ps.line)
				tt_error++
			}
		} else if strings.HasPrefix(tmp, "AA{") {

			// Convert to object name.

			tmp = tmp[3:]
			var stemp string
			for len(tmp) > 0 && tmp[0] != '}' && tmp[0] != '*' {
				stemp += string(tmp[0])
				tmp = tmp[1:]
			}
			if len(tmp) > 0 && tmp[0] == '}' {
				if len(stemp) > 9 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Object name %s has been truncated to 9 characters.\n", ps.line, stemp)
					stemp = stemp[:9]
				}
				var ttemp, errs = tt_text_to_two_key(stemp, false)
				if errs == 0 {
					//text_color_set(DW_COLOR_DEBUG);
					//dw_printf ("DEBUG Line %d: AA{%s} -> AA%s\n", line, stemp, ttemp);
					otemp.WriteString("AA" + ttemp)
				} else {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: AA{%s} could not be converted to tones for object name.\n", ps.line, stemp)
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: AA{... is missing matching } in TTMACRO definition.\n", ps.line)
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
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Couldn't convert \"%s\" to APRS symbol code.  Using default.\n", ps.line, stemp.String())
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
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: AB{... is missing matching } in TTMACRO definition.\n", ps.line)
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
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: CA{%s} could not be converted to tones for enhanced comment.\n", ps.line, stemp.String())
					tt_error++
				}
				tmp = tmp[1:]
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: CA{... is missing matching } in TTMACRO definition.\n", ps.line)
				tt_error++
			}
		} else if strings.ContainsRune("0123456789ABCD*#xyz", rune(tmp[0])) {
			otemp.WriteString(string(tmp[0]))
			tmp = tmp[1:]
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: TTMACRO definition can contain only 0-9, A, B, C, D, *, #, x, y, z.\n", ps.line)
			tt_error++
			tmp = tmp[1:]
		}
	}

	// Make sure that number of x, y, z, in pattern and definition match.

	var d_count [3]int

	for j := 0; j < len(otemp.String()); j++ {
		if otemp.String()[j] >= 'x' && otemp.String()[j] <= 'z' {
			d_count[otemp.String()[j]-'x']++
		}
	}

	// A little validity checking.

	for j := range 3 {
		if p_count[j] > 0 && d_count[j] == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: '%c' is in TTMACRO pattern but is not used in definition.\n", ps.line, 'x'+j)
		}
		if d_count[j] > 0 && p_count[j] == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: '%c' is referenced in TTMACRO definition but does not appear in the pattern.\n", ps.line, 'x'+j)
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
		dw_printf("Line %d: Errors found in TTMACRO, skipping.\n", ps.line)
	}
	return false
}

// handleTTOBJ handles the TTOBJ keyword.
func handleTTOBJ(ps *parseState) bool {
	/*
	 * TTOBJ 		- TT Object Report options.
	 *
	 * TTOBJ  recv-chan  where-to  [ via-path ]
	 *
	 *	whereto is any combination of transmit channel, APP, IG.
	 */

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing DTMF receive channel for TTOBJ command.\n", ps.line)
		return true
	}

	var r, rErr = strconv.Atoi(t)
	if r < 0 || r > MAX_RADIO_CHANS-1 || rErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: DTMF receive channel must be in range of 0 to %d on line %d.\n",
			MAX_RADIO_CHANS-1, ps.line)
		return true
	}

	// I suppose we need internal modem channel here.
	// otherwise a DTMF decoder would not be available.

	if ps.audio.chan_medium[r] != MEDIUM_RADIO {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: TTOBJ DTMF receive channel %d is not valid.\n",
			ps.line, r)
		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing transmit channel for TTOBJ command.\n", ps.line)
		return true
	}

	// Can have any combination of number, APP, IG.
	// Would it be easier with strtok?

	var x = -1
	var app = 0
	var ig = 0

	for _, p := range t {
		if unicode.IsDigit(p) {
			x = int(p - '0')
			if x < 0 || x > MAX_TOTAL_CHANS-1 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: Transmit channel must be in range of 0 to %d on line %d.\n", MAX_TOTAL_CHANS-1, ps.line)
				x = -1
			} else if ps.audio.chan_medium[x] != MEDIUM_RADIO &&
				ps.audio.chan_medium[x] != MEDIUM_NETTNC {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: TTOBJ transmit channel %d is not valid.\n", ps.line, x)
				x = -1
			}
		} else if p == 'a' || p == 'A' {
			app = 1
		} else if p == 'i' || p == 'I' {
			ig = 1
		} else if strings.ContainsRune("pPgG,", p) {
			// Skip?
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Expected comma separated list with some combination of transmit channel, APP, and IG.\n", ps.line)
		}
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

		if check_via_path(t) >= 0 {
			ps.tt.obj_xmit_via = t
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: invalid via path.\n", ps.line)
		}
	}
	return false
}

// handleTTERR handles the TTERR keyword.
func handleTTERR(ps *parseState) bool {
	/*
	 * TTERR 		- TT responses for success or errors.
	 *
	 * TTERR  msg_id  method  text...
	 */

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing message identifier for TTERR command.\n", ps.line)
		return true
	}

	var msg_num = -1
	for n := range TT_ERROR_MAXP1 {
		if strings.EqualFold(t, ttErrorString(n)) {
			msg_num = n
			break
		}
	}
	if msg_num < 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid message identifier for TTERR command.\n", ps.line)
		// pick one of ...
		return true
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing method (SPEECH, MORSE) for TTERR command.\n", ps.line)
		return true
	}

	t = strings.ToUpper(t)

	var method, _, _, ok = ax25_parse_addr(-1, t, 1)
	if !ok {
		return true // function above prints any error message
	}

	if method != "MORSE" && method != "SPEECH" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Response method of %s must be SPEECH or MORSE for TTERR command.\n", ps.line, method)
		return true
	}

	t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing response text for TTERR command.\n", ps.line)
		return true
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Line %d: TTERR debug %d %s-%d \"%s\"\n", line, msg_num, method, ssid, t);

	Assert(msg_num >= 0 && msg_num < TT_ERROR_MAXP1)

	ps.tt.response[msg_num].method = method

	// TODO1.3: Need SSID too!

	ps.tt.response[msg_num].mtext = t
	return false
}

// handleTTSTATUS handles the TTSTATUS keyword.
func handleTTSTATUS(ps *parseState) bool {
	/*
	 * TTSTATUS 		- TT custom status messages.
	 *
	 * TTSTATUS  status_id  text...
	 */

	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing status number for TTSTATUS command.\n", ps.line)
		return true
	}

	var status_num, _ = strconv.Atoi(t)

	if status_num < 1 || status_num > 9 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Status number for TTSTATUS command must be in range of 1 to 9.\n", ps.line)
		return true
	}

	t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing status text for TTSTATUS command.\n", ps.line)
		return true
	}

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf ("Line %d: TTSTATUS debug %d \"%s\"\n", line, status_num, t);

	t = strings.TrimSpace(t)

	ps.tt.status[status_num] = t
	return false
}

// handleTTCMD handles the TTCMD keyword.
func handleTTCMD(ps *parseState) bool {
	/*
	 * TTCMD 		- Command to run when valid sequence is received.
	 *			  Any text generated will be sent back to user.
	 *
	 * TTCMD ...
	 */
	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing command for TTCMD command.\n", ps.line)
		return true
	}

	ps.tt.ttcmd = t
	return false
}

// handleIGSERVER handles the IGSERVER keyword.
func handleIGSERVER(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing IGate server name for IGSERVER command.\n", ps.line)

		return true
	}

	ps.igate.t2_server_name = t

	/* If there is a : in the name, split it out as the port number. */

	if strings.Contains(t, ":") {
		var hostname, portStr, _ = strings.Cut(t, ":")
		ps.igate.t2_server_name = hostname

		var port, portErr = strconv.Atoi(portStr)
		if port >= MIN_IP_PORT_NUMBER && port <= MAX_IP_PORT_NUMBER && portErr == nil {
			ps.igate.t2_server_port = port
		} else {
			ps.igate.t2_server_port = DEFAULT_IGATE_PORT

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid port number for IGate server. Using default %d.\n",
				ps.line, ps.igate.t2_server_port)
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

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid port number for IGate server. Using default %d.\n",
				ps.line, ps.igate.t2_server_port)
		}
	}
	// dw_printf ("DEBUG  server=%s   port=%d\n", p_igate_config.t2_server_name, p_igate_config.t2_server_port);
	// exit (0);
	return false
}

// handleIGLOGIN handles the IGLOGIN keyword.
func handleIGLOGIN(ps *parseState) bool {
	/*
	 * IGLOGIN 		- Login callsign and passcode for IGate server
	 *
	 * IGLOGIN  callsign  passcode
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing login callsign for IGLOGIN command.\n", ps.line)

		return true
	}
	// TODO: Wouldn't hurt to do validity checking of format.
	ps.igate.t2_login = t

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing passcode for IGLOGIN command.\n", ps.line)

		return true
	}

	ps.igate.t2_passcode = t
	return false
}

// handleIGTXVIA handles the IGTXVIA keyword.
func handleIGTXVIA(ps *parseState) bool {
	/*
	 * IGTXVIA 		- Transmit channel and VIA path for messages from IGate server
	 *
	 * IGTXVIA  channel  [ path ]
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing transmit channel for IGTXVIA command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n < 0 || n > MAX_TOTAL_CHANS-1 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Transmit channel must be in range of 0 to %d on line %d.\n",
			MAX_TOTAL_CHANS-1, ps.line)

		return true
	}

	ps.igate.tx_chan = n

	t = split("", false)
	if t != "" {
		// TODO KG#if 1	// proper checking
		n = check_via_path(t)
		if n >= 0 {
			ps.igate.max_digi_hops = n
			ps.igate.tx_via = "," + t
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: invalid via path.\n", ps.line)
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
	return false
}

// handleIGFILTER handles the IGFILTER keyword.
func handleIGFILTER(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Warning - IGFILTER already configured (%s), this one (%s) will be ignored.\n", ps.line, ps.igate.t2_filter, t)

		return true
	}

	if t != "" {
		ps.igate.t2_filter = t

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Warning - IGFILTER is a rarely needed expert level feature.\n", ps.line)
		dw_printf("If you don't have a special situation and a good understanding of\n")
		dw_printf("how this works, you probably should not be messing with it.\n")
		dw_printf("The default behavior is appropriate for most situations.\n")
		dw_printf("Please read \"Successful-APRS-IGate-Operation.pdf\".\n")
	}
	return false
}

// handleIGTXLIMIT handles the IGTXLIMIT keyword.
func handleIGTXLIMIT(ps *parseState) bool {
	/*
	 * IGTXLIMIT 		- Limit transmissions during 1 and 5 minute intervals.
	 *
	 * IGTXLIMIT  one-minute-limit  five-minute-limit
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing one minute limit for IGTXLIMIT command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n < 1 {
		ps.igate.tx_limit_1 = 1
	} else if n <= IGATE_TX_LIMIT_1_MAX {
		ps.igate.tx_limit_1 = n
	} else {
		ps.igate.tx_limit_1 = IGATE_TX_LIMIT_1_MAX

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: One minute transmit limit has been reduced to %d.\n",
			ps.line, ps.igate.tx_limit_1)
		dw_printf("You won't make friends by setting a limit this high.\n")
	}

	t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing five minute limit for IGTXLIMIT command.\n", ps.line)

		return true
	}

	n, _ = strconv.Atoi(t)
	if n < 1 {
		ps.igate.tx_limit_5 = 1
	} else if n <= IGATE_TX_LIMIT_5_MAX {
		ps.igate.tx_limit_5 = n
	} else {
		ps.igate.tx_limit_5 = IGATE_TX_LIMIT_5_MAX

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Five minute transmit limit has been reduced to %d.\n",
			ps.line, ps.igate.tx_limit_5)
		dw_printf("You won't make friends by setting a limit this high.\n")
	}
	return false
}

// handleIGMSP handles the IGMSP keyword.
func handleIGMSP(ps *parseState) bool {
	/*
	 * IGMSP 		- Number of times to send position of message sender.
	 *
	 * IGMSP  n
	 */
	var t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= 0 && n <= 10 {
			ps.igate.igmsp = n
		} else {
			ps.igate.igmsp = 1

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable number of times for message sender position.  Using default 1.\n", ps.line)
		}
	} else {
		ps.igate.igmsp = 1

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing number of times for message sender position.  Using default 1.\n", ps.line)
	}
	return false
}

// handleSATGATE handles the SATGATE keyword.
func handleSATGATE(ps *parseState) bool {
	/*
	 * SATGATE 		- Special SATgate mode to delay packets heard directly.
	 *
	 * SATGATE [ n ]
	 */
	text_color_set(DW_COLOR_INFO)
	dw_printf("Line %d: SATGATE is pretty useless and will be removed in a future version.\n", ps.line)

	var t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		if n >= MIN_SATGATE_DELAY && n <= MAX_SATGATE_DELAY {
			ps.igate.satgate_delay = n
		} else {
			ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY

			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Unreasonable SATgate delay.  Using default.\n", ps.line)
		}
	} else {
		ps.igate.satgate_delay = DEFAULT_SATGATE_DELAY
	}
	return false
}

// handleAGWPORT handles the AGWPORT keyword.
func handleAGWPORT(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing port number for AGWPORT command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Unexpected \"%s\" after the port number.\n", ps.line, t)
		dw_printf("Perhaps you were trying to use feature available only with KISSPORT.\n")

		return true
	}

	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		ps.misc.agwpe_port = n
	} else {
		ps.misc.agwpe_port = DEFAULT_AGWPE_PORT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid port number for AGW TCPIP Socket Interface. Using %d.\n",
			ps.line, ps.misc.agwpe_port)
	}
	return false
}

// handleKISSPORT handles the KISSPORT keyword.
func handleKISSPORT(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing TCP port number for KISSPORT command.\n", ps.line)

		return true
	}
	var n, _ = strconv.Atoi(t)

	var tcp_port int
	if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
		tcp_port = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid TCP port number for KISS TCPIP Socket Interface.\n", ps.line)
		dw_printf("Use something in the range of %d to %d.\n", MIN_IP_PORT_NUMBER, MAX_IP_PORT_NUMBER)

		return true
	}

	t = split("", false)
	var kissChannel = -1 // optional.  default to all if not specified.

	if t != "" {
		var channelErr error

		kissChannel, channelErr = strconv.Atoi(t)
		if ps.channel < 0 || kissChannel >= MAX_TOTAL_CHANS || channelErr != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid channel %d for KISSPORT command.  Must be in range 0 thru %d.\n", ps.line, kissChannel, MAX_TOTAL_CHANS-1)

			return true
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
				if !(slot == 0 && tcp_port == DEFAULT_KISS_PORT) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Warning: Duplicate TCP port %d will overwrite previous value.\n", ps.line, tcp_port)
				}
			} else if ps.misc.kiss_port[i] == 0 {
				slot = i
			}
		}

		if slot >= 0 {
			ps.misc.kiss_port[slot] = tcp_port
			ps.misc.kiss_chan[slot] = kissChannel
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Too many KISSPORT commands.\n", ps.line)
		}
	}
	return false
}

// handleNULLMODEM handles the NULLMODEM keyword.
func handleNULLMODEM(ps *parseState) bool {
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
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing serial port name on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.kiss_serial_port != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Warning serial port name on line %d replaces earlier value.\n", ps.line)
		}

		ps.misc.kiss_serial_port = t
		ps.misc.kiss_serial_speed = 0
		ps.misc.kiss_serial_poll = 0
	}

	t = split("", false)
	if t != "" {
		ps.misc.kiss_serial_speed, _ = strconv.Atoi(t)
	}
	return false
}

// handleSERIALKISSPOLL handles the SERIALKISSPOLL keyword.
func handleSERIALKISSPOLL(ps *parseState) bool {
	/*
	 * SERIALKISSPOLL name		- Poll for serial port name that might come and go.
	 *			  	  e.g. /dev/rfcomm0 for bluetooth.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing serial port name on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.kiss_serial_port != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Warning serial port name on line %d replaces earlier value.\n", ps.line)
		}

		ps.misc.kiss_serial_port = t
		ps.misc.kiss_serial_speed = 0
		ps.misc.kiss_serial_poll = 1 // set polling.
	}
	return false
}

// handleKISSCOPY handles the KISSCOPY keyword.
func handleKISSCOPY(ps *parseState) bool {
	/*
	 * KISSCOPY 		- Data from network KISS client is copied to all others.
	 *			  This does not apply to pseudo terminal KISS.
	 */
	ps.misc.kiss_copy = true
	return false
}

// handleDNSSD handles the DNSSD keyword.
func handleDNSSD(ps *parseState) bool {
	/*
	 * DNSSD 		- Enable or disable (1/0) dns-sd, DNS Service Discovery announcements
	 * DNSSDNAME            - Set DNS-SD service name, defaults to "Dire Wolf on <hostname>"
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing integer value for DNSSD command.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n == 0 || n == 1 {
		ps.misc.dns_sd_enabled = n != 0
	} else {
		ps.misc.dns_sd_enabled = false

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid integer value for DNSSD. Disabling dns-sd.\n", ps.line)
	}
	return false
}

// handleDNSSDNAME handles the DNSSDNAME keyword.
func handleDNSSDNAME(ps *parseState) bool {
	var t = split("", true)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing service name for DNSSDNAME.\n", ps.line)

		return true
	} else {
		ps.misc.dns_sd_name = t
	}
	return false
}

// handleGPSNMEA handles the GPSNMEA keyword.
func handleGPSNMEA(ps *parseState) bool {
	/*
	 * GPSNMEA  serial-device  [ speed ]		- Direct connection to GPS receiver.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Missing serial port name for GPS receiver.\n", ps.line)

		return true
	}

	ps.misc.gpsnmea_port = t

	t = split("", false)
	if t != "" {
		var n, _ = strconv.Atoi(t)
		ps.misc.gpsnmea_speed = n
	} else {
		ps.misc.gpsnmea_speed = 4800 // The standard at one time.
	}
	return false
}

// handleGPSD handles the GPSD keyword.
func handleGPSD(ps *parseState) bool {
	/*
	 * GPSD		- Use GPSD server.
	 *
	 * GPSD [ host [ port ] ]
	 */

	/*
	   TODO KG

	   	#if __WIN32__

	   		    text_color_set(DW_COLOR_ERROR);
	   		    dw_printf ("Config file, line %d: The GPSD interface is not available for Windows.\n", ps.line);
	   		    continue;

	   	#elif ENABLE_GPSD
	*/
	dw_printf("Warning: GPSD support currently disabled pending a rewrite of the integration.\n")

	ps.misc.gpsd_host = "localhost"
	ps.misc.gpsd_port = DEFAULT_GPSD_PORT

	var t = split("", false)
	if t != "" {
		ps.misc.gpsd_host = t

		t = split("", false)
		if t != "" {
			var n, _ = strconv.Atoi(t)
			if (n >= MIN_IP_PORT_NUMBER && n <= MAX_IP_PORT_NUMBER) || n == 0 {
				ps.misc.gpsd_port = n
			} else {
				ps.misc.gpsd_port = DEFAULT_GPSD_PORT

				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Invalid port number for GPSD Socket Interface. Using default of %d.\n",
					ps.line, ps.misc.gpsd_port)
			}
		}
	}
	/*
	   	TODO KG

	   #else

	   	text_color_set(DW_COLOR_ERROR);
	   	dw_printf ("Config file, line %d: The GPSD interface has not been enabled.\n", ps.line);
	   	dw_printf ("Install gpsd and libgps-dev packages then rebuild direwolf.\n");
	   	continue;

	   #endif
	*/
	return false
}

// handleWAYPOINT handles the WAYPOINT keyword.
func handleWAYPOINT(ps *parseState) bool {
	/*
	 * WAYPOINT		- Generate WPL and AIS NMEA sentences for display on map.
	 *
	 * WAYPOINT  serial-device [ formats ]
	 * WAYPOINT  host:udpport [ formats ]
	 *
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing output device for WAYPOINT on line %d.\n", ps.line)

		return true
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid UDP port number %d for sending waypoints.\n", ps.line, port)
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: Invalid output format '%c' for WAYPOINT on line %d.\n", c, ps.line)
		}
	}
	return false
}

// handleLOGDIR handles the LOGDIR keyword.
func handleLOGDIR(ps *parseState) bool {
	/*
	 * LOGDIR	- Directory name for automatically named daily log files.  Use "." for current working directory.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing directory name for LOGDIR on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.log_path != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: LOGDIR on line %d is replacing an earlier LOGDIR or LOGFILE.\n", ps.line)
		}

		ps.misc.log_daily_names = true
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: LOGDIR on line %d should have directory path and nothing more.\n", ps.line)
	}
	return false
}

// handleLOGFILE handles the LOGFILE keyword.
func handleLOGFILE(ps *parseState) bool {
	/*
	 * LOGFILE	- Log file name, including any directory part.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Missing file name for LOGFILE on line %d.\n", ps.line)

		return true
	} else {
		if ps.misc.log_path != "" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: LOGFILE on line %d is replacing an earlier LOGDIR or LOGFILE.\n", ps.line)
		}

		ps.misc.log_daily_names = false
		ps.misc.log_path = t
	}

	t = split("", false)
	if t != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: LOGFILE on line %d should have file name and nothing more.\n", ps.line)
	}
	return false
}

// handleBEACON handles the BEACON keyword.
func handleBEACON(ps *parseState) bool {
	/*
	 * BEACON channel delay every message
	 *
	 * Original handcrafted style.  Removed in version 1.0.
	 */
	text_color_set(DW_COLOR_ERROR)
	dw_printf("Config file, line %d: Old style 'BEACON' has been replaced with new commands.\n", ps.line)
	dw_printf("Use PBEACON, OBEACON, TBEACON, or CBEACON instead.\n")
	return false
}

// handleXBEACON handles the XBEACON keyword.
func handleXBEACON(ps *parseState) bool {
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

		if beacon_options(ps.text[len("xBEACON")+1:], &(ps.misc.beacon[ps.misc.num_beacons]), ps.line, ps.audio) == nil {
			ps.misc.num_beacons++
		}
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file: Maximum number of beacons exceeded on line %d.\n", ps.line)

		return true
	}
	return false
}

// handleSMARTBEACON handles the SMARTBEACON keyword.
func handleSMARTBEACON(ps *parseState) bool {
	/*
	 * SMARTBEACONING [ fast_speed fast_rate slow_speed slow_rate turn_time turn_angle turn_slope ]
	 *
	 * Parameters must be all or nothing.
	 */
	dw_printf("SMARTBEACONING support currently disabled due to mid-stage porting complexity - line %d skipped.\n", ps.line)

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
	return false
}

// handleFRACK handles the FRACK keyword.
func handleFRACK(ps *parseState) bool {
	/*
	 * ==================== AX.25 connected mode ====================
	 */

	/*
	 * FRACK  n 		- Number of seconds to wait for ack to transmission.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for FRACK.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_T1V_FRACK_MIN && n <= AX25_T1V_FRACK_MAX {
		ps.misc.frack = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid FRACK time. Using default %d.\n", ps.line, ps.misc.frack)
	}
	return false
}

// handleRETRY handles the RETRY keyword.
func handleRETRY(ps *parseState) bool {
	/*
	 * RETRY  n 		- Number of times to retry before giving up.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for RETRY.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N2_RETRY_MIN && n <= AX25_N2_RETRY_MAX {
		ps.misc.retry = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid RETRY number. Using default %d.\n", ps.line, ps.misc.retry)
	}
	return false
}

// handlePACLEN handles the PACLEN keyword.
func handlePACLEN(ps *parseState) bool {
	/*
	 * PACLEN  n 		- Maximum number of bytes in information part.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for PACLEN.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_N1_PACLEN_MIN && n <= AX25_N1_PACLEN_MAX {
		ps.misc.paclen = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid PACLEN value. Using default %d.\n", ps.line, ps.misc.paclen)
	}
	return false
}

// handleMAXFRAME handles the MAXFRAME keyword.
func handleMAXFRAME(ps *parseState) bool {
	/*
	 * MAXFRAME  n 		- Max frames to send before ACK.  mod 8 "Window" size.
	 *
	 * Window size would make more sense but everyone else calls it MAXFRAME.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for MAXFRAME.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_BASIC_MIN && n <= AX25_K_MAXFRAME_BASIC_MAX {
		ps.misc.maxframe_basic = n
	} else {
		ps.misc.maxframe_basic = AX25_K_MAXFRAME_BASIC_DEFAULT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid MAXFRAME value outside range of %d to %d. Using default %d.\n",
			ps.line, AX25_K_MAXFRAME_BASIC_MIN, AX25_K_MAXFRAME_BASIC_MAX, ps.misc.maxframe_basic)
	}
	return false
}

// handleEMAXFRAME handles the EMAXFRAME keyword.
func handleEMAXFRAME(ps *parseState) bool {
	/*
	 * EMAXFRAME  n 		- Max frames to send before ACK.  mod 128 "Window" size.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for EMAXFRAME.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= AX25_K_MAXFRAME_EXTENDED_MIN && n <= AX25_K_MAXFRAME_EXTENDED_MAX {
		ps.misc.maxframe_extended = n
	} else {
		ps.misc.maxframe_extended = AX25_K_MAXFRAME_EXTENDED_DEFAULT

		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid EMAXFRAME value outside of range %d to %d. Using default %d.\n",
			ps.line, AX25_K_MAXFRAME_EXTENDED_MIN, AX25_K_MAXFRAME_EXTENDED_MAX, ps.misc.maxframe_extended)
	}
	return false
}

// handleMAXV22 handles the MAXV22 keyword.
func handleMAXV22(ps *parseState) bool {
	/*
	 * MAXV22  n 		- Max number of SABME sent before trying SABM.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing value for MAXV22.\n", ps.line)

		return true
	}

	var n, _ = strconv.Atoi(t)
	if n >= 0 && n <= AX25_N2_RETRY_MAX {
		ps.misc.maxv22 = n
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Invalid MAXV22 number. Will use half of RETRY.\n", ps.line)
	}
	return false
}

// handleV20 handles the V20 keyword.
func handleV20(ps *parseState) bool {
	/*
	 * V20  address [ address ... ] 	- Stations known to support only AX.25 v2.0.
	 *					  When connecting to these, skip SABME and go right to SABM.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing address(es) for V20.\n", ps.line)

		return true
	}

	for t != "" {
		var strictness = 2
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, strictness)

		if ok {
			ps.misc.v20_addrs = append(ps.misc.v20_addrs, t)
			ps.misc.v20_count++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid station address for V20 command.\n", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}
	return false
}

// handleNOXID handles the NOXID keyword.
func handleNOXID(ps *parseState) bool {
	/*
	 * NOXID  address [ address ... ] 	- Stations known not to understand XID.
	 *					  After connecting to these (with v2.2 obviously), don't try using XID command.
	 *					  AX.25 for Linux is the one known case so far.
	 *					  Possible to have multiple and they are cumulative.
	 */
	var t = split("", false)
	if t == "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Line %d: Missing address(es) for NOXID.\n", ps.line)

		return true
	}

	for t != "" {
		var strictness = 2
		var _, _, _, ok = ax25_parse_addr(AX25_DESTINATION, t, strictness)

		if ok {
			ps.misc.noxid_addrs = append(ps.misc.noxid_addrs, t)
			ps.misc.noxid_count++
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Line %d: Invalid station address for NOXID command.\n", ps.line)

			// continue processing any others following.
		}

		t = split("", false)
	}
	return false
}

/*
 * Parse the PBEACON or OBEACON options.
 */

// FIXME: provide error messages when non applicable option is used for particular beacon type.
// e.g.  IBEACON DELAY=1 EVERY=1 SENDTO=IG OVERLAY=R SYMBOL="igate" LAT=37^44.46N LONG=122^27.19W COMMENT="N1KOL-1 IGATE"
// Just ignores overlay, symbol, lat, long, and comment.

func beacon_options(cmd string, b *beacon_s, line int, p_audio_config *audio_s) error { //nolint:unparam
	b.sendto_type = SENDTO_XMIT
	b.sendto_chan = 0
	b.delay = 60
	b.slot = G_UNKNOWN
	b.every = 600
	//b.delay = 6;		// temp test.
	//b.every = 3600;
	b.lat = G_UNKNOWN
	b.lon = G_UNKNOWN
	b.ambiguity = 0
	b.alt_m = G_UNKNOWN
	b.symtab = '/'
	b.symbol = '-' /* house */
	b.freq = G_UNKNOWN
	b.tone = G_UNKNOWN
	b.offset = G_UNKNOWN
	b.source = ""
	b.dest = ""

	var zone string
	var temp_symbol string
	var easting float64 = G_UNKNOWN
	var northing float64 = G_UNKNOWN

	for {
		var t = split("", false)
		if t == "" {
			break
		}

		var keyword, value, found = strings.Cut(t, "=")
		if !found {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file: No = found in, %s, on line %d.\n", t, line)

			return errors.New("TODO")
		}

		// QUICK TEMP EXPERIMENT, maybe permanent new feature.
		// Recognize \xnn as hexadecimal value.  Handy for UTF-8 in comment.
		// Maybe recognize the <0xnn> form that we print.
		//
		// # Convert between languages here:  https://translate.google.com/  then
		// # Convert to UTF-8 bytes here: https://codebeautify.org/utf8-converter
		//
		// pbeacon delay=0:05 every=0:30 sendto=R0 lat=12.5N long=69.97W  comment="\xe3\x82\xa2\xe3\x83\x9e\xe3\x83\x81\xe3\x83\xa5\xe3\x82\xa2\xe7\x84\xa1\xe7\xb7\x9a   \xce\xa1\xce\xb1\xce\xb4\xce\xb9\xce\xbf\xce\xb5\xcf\x81\xce\xb1\xcf\x83\xce\xb9\xcf\x84\xce\xb5\xcf\x87\xce\xbd\xce\xb9\xcf\x83\xce\xbc\xcf\x8c\xcf\x82"

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
			b.delay = parse_interval(value, line)
		} else if strings.EqualFold(keyword, "SLOT") {
			var n = parse_interval(value, line)
			if n < 1 || n > 3600 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: Beacon time slot, %d, must be in range of 1 to 3600 seconds.\n", line, n)

				continue
			}

			b.slot = n
		} else if strings.EqualFold(keyword, "EVERY") {
			b.every = parse_interval(value, line)
		} else if strings.EqualFold(keyword, "SENDTO") {
			if value[0] == 'i' || value[0] == 'I' {
				b.sendto_type = SENDTO_IGATE
				b.sendto_chan = 0
			} else if value[0] == 'r' || value[0] == 'R' {
				var n, _ = strconv.Atoi(value[1:])
				if (n < 0 || n >= MAX_TOTAL_CHANS || p_audio_config.chan_medium[n] == MEDIUM_NONE) && p_audio_config.chan_medium[n] != MEDIUM_IGATE {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file, line %d: Simulated receive on channel %d is not valid.\n", line, n)

					continue
				}

				b.sendto_type = SENDTO_RECV
				b.sendto_chan = n
			} else if value[0] == 't' || value[0] == 'T' || value[0] == 'x' || value[0] == 'X' {
				var n, _ = strconv.Atoi(value[1:])
				if (n < 0 || n >= MAX_TOTAL_CHANS || p_audio_config.chan_medium[n] == MEDIUM_NONE) && p_audio_config.chan_medium[n] != MEDIUM_IGATE {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file, line %d: Send to channel %d is not valid.\n", line, n)

					continue
				}

				b.sendto_type = SENDTO_XMIT
				b.sendto_chan = n
			} else {
				var n, _ = strconv.Atoi(value)
				if (n < 0 || n >= MAX_TOTAL_CHANS || p_audio_config.chan_medium[n] == MEDIUM_NONE) && p_audio_config.chan_medium[n] != MEDIUM_IGATE {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Config file, line %d: Send to channel %d is not valid.\n", line, n)

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
			if check_via_path(value) >= 0 {
				b.via = value
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: invalid via path.\n", line)
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
			b.lat = parse_ll(value, LAT, line)
		} else if strings.EqualFold(keyword, "LONG") || strings.EqualFold(keyword, "LON") {
			b.lon = parse_ll(value, LON, line)
		} else if strings.EqualFold(keyword, "AMBIGUITY") || strings.EqualFold(keyword, "AMBIG") {
			var n, _ = strconv.Atoi(value)
			if n >= 0 && n <= 4 {
				b.ambiguity = n
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: Location ambiguity, on line %d, must be in range of 0 to 4.\n", line)
			}
		} else if strings.EqualFold(keyword, "ALT") || strings.EqualFold(keyword, "ALTITUDE") {
			// Parse something like "10 metres" or "10" or "10metres"
			var unitIndex = strings.IndexFunc(value, func(r rune) bool {
				return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			})

			if unitIndex != -1 { // Did we find a unit string?
				var unit = value[unitIndex:]

				var value = value[:unitIndex]
				value = strings.TrimSpace(value)

				var meters float64 = 0

				for _, u := range units {
					if strings.EqualFold(u.name, unit) {
						meters = u.meters
					}
				}

				if meters == 0 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Line %d: Unrecognized unit '%s' for altitude.  Using meter.\n", line, unit)
					dw_printf("Try using singular form.  e.g.  ft or foot rather than feet.\n")
					var f, _ = strconv.ParseFloat(value, 64)
					b.alt_m = f
				} else {
					// valid unit
					var f, _ = strconv.ParseFloat(value, 64)
					b.alt_m = f * meters
				}
			} else {
				// no unit specified
				var f, _ = strconv.ParseFloat(value, 64)
				b.alt_m = f
			}
		} else if strings.EqualFold(keyword, "ZONE") {
			zone = value
		} else if strings.EqualFold(keyword, "EAST") || strings.EqualFold(keyword, "EASTING") {
			var f, _ = strconv.ParseFloat(value, 64)
			easting = f
		} else if strings.EqualFold(keyword, "NORTH") || strings.EqualFold(keyword, "NORTHING") {
			var f, _ = strconv.ParseFloat(value, 64)
			northing = f
		} else if strings.EqualFold(keyword, "SYMBOL") {
			/* Defer processing in case overlay appears later. */
			temp_symbol = value
		} else if strings.EqualFold(keyword, "OVERLAY") {
			if len(value) == 1 && (unicode.IsUpper(rune(value[0])) || unicode.IsDigit(rune(value[0]))) {
				b.symtab = value[0]
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: Overlay must be one character in range of 0-9 or A-Z, upper case only, on line %d.\n", line)
			}
		} else if strings.EqualFold(keyword, "POWER") {
			var n, _ = strconv.ParseFloat(value, 64)
			b.power = n
		} else if strings.EqualFold(keyword, "HEIGHT") { // This is in feet.
			var n, _ = strconv.ParseFloat(value, 64)
			b.height = n
			// TODO: ability to add units suffix, e.g.  10m
		} else if strings.EqualFold(keyword, "GAIN") {
			var n, _ = strconv.ParseFloat(value, 64)
			b.gain = n
		} else if strings.EqualFold(keyword, "DIR") || strings.EqualFold(keyword, "DIRECTION") {
			b.dir = value
		} else if strings.EqualFold(keyword, "FREQ") {
			var f, _ = strconv.ParseFloat(value, 64)
			b.freq = f
		} else if strings.EqualFold(keyword, "TONE") {
			var f, _ = strconv.ParseFloat(value, 64)
			b.tone = f
		} else if strings.EqualFold(keyword, "OFFSET") || strings.EqualFold(keyword, "OFF") {
			var f, _ = strconv.ParseFloat(value, 64)
			b.offset = f
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
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Invalid option keyword, %s.\n", line, keyword)

			return errors.New("TODO")
		}
	}

	if b.custom_info != "" && b.custom_infocmd != "" {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Can't use both INFO and INFOCMD at the same time.\n", line)
	}

	if b.compress && b.ambiguity != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Config file, line %d: Position ambiguity can't be used with compressed location format.\n", line)

		b.ambiguity = 0
	}

	/*
	 * Convert UTM coordinates to lat / long.
	 */
	if len(zone) > 0 || easting != G_UNKNOWN || northing != G_UNKNOWN {
		if len(zone) > 0 && easting != G_UNKNOWN && northing != G_UNKNOWN {
			var _, _hemi, lzone = parse_utm_zone(zone)

			var hemi = HemisphereRuneToCoordconvHemisphere(_hemi)

			var utm = coordconv.UTMCoord{
				Zone:       lzone,
				Hemisphere: hemi,
				Easting:    float64(easting),
				Northing:   float64(northing),
			}

			var geo, geoErr = coordconv.DefaultUTMConverter.ConvertToGeodetic(utm)
			if geoErr == nil {
				b.lat = R2D(float64(geo.Lat))
				b.lon = R2D(float64(geo.Lng))
			} else {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Line %d: Invalid UTM location: \n%s\n", line, geoErr)
			}
		} else {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: When any of ZONE, EASTING, NORTHING specified, they must all be specified.\n", line)
		}
	}

	/*
	 * Process symbol now that we have any later overlay.
	 *
	 * FIXME: Someone who used this was surprised to end up with Solar Powser  (S-).
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
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file, line %d: Could not find symbol matching %s.\n", line, temp_symbol)
			}
		}
	}

	/* Check is here because could be using default channel when SENDTO= is not specified. */

	if b.sendto_type == SENDTO_XMIT {
		if (b.sendto_chan < 0 || b.sendto_chan >= MAX_TOTAL_CHANS || p_audio_config.chan_medium[b.sendto_chan] == MEDIUM_NONE) && p_audio_config.chan_medium[b.sendto_chan] != MEDIUM_IGATE {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Config file, line %d: Send to channel %d is not valid.\n", line, b.sendto_chan)

			return errors.New("TODO")
		}

		if p_audio_config.chan_medium[b.sendto_chan] == MEDIUM_IGATE { // Prevent subscript out of bounds.
			// Will be using call from chan 0 later.
			if IsNoCall(p_audio_config.mycall[0]) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: MYCALL must be set for channel %d before beaconing is allowed.\n", 0)

				return errors.New("TODO")
			}
		} else {
			if IsNoCall(p_audio_config.mycall[b.sendto_chan]) {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Config file: MYCALL must be set for channel %d before beaconing is allowed.\n", b.sendto_chan)

				return errors.New("TODO")
			}
		}
	}

	return nil
}

func IsNoCall(callsign string) bool {
	return callsign == "" || strings.EqualFold(callsign, "NOCALL") || strings.EqualFold(callsign, "N0CALL")
}
