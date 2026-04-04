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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
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

	var token string
	var in_quotes = false

	var parsedLen int

outerLoop:
	for parsedLen = 0; parsedLen < len(splitCmd); parsedLen++ {
		var c = splitCmd[parsedLen]
		switch c {
		case '"':
			if in_quotes {
				if parsedLen+1 < len(splitCmd) && splitCmd[parsedLen+1] == '"' {
					token += string(c)
					parsedLen++
				} else {
					in_quotes = false
				}
			} else {
				in_quotes = true
			}
		case ' ':
			if in_quotes || rest_of_line {
				token += string(c)
			} else {
				break outerLoop
			}
		default:
			token += string(c)
		}
	}

	splitCmd = splitCmd[parsedLen:]

	// dw_printf("split out: '%s'\n", token);

	return token
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

	for adevice := 0; adevice < MAX_ADEVS; adevice++ {
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
	for channel := 0; channel < MAX_TOTAL_CHANS; channel++ {
		p_audio_config.chan_medium[channel] = MEDIUM_NONE /* One or both channels will be */
		/* set to radio when corresponding */
		/* audio device is defined. */
	}

	// MAX_RADIO_CHANS for achan[]
	// Maybe achan should be renamed to radiochan to make it clearer.
	for channel := 0; channel < MAX_RADIO_CHANS; channel++ {
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

		for ot := 0; ot < NUM_OCTYPES; ot++ {
			p_audio_config.achan[channel].octrl[ot].ptt_method = PTT_METHOD_NONE
			p_audio_config.achan[channel].octrl[ot].ptt_device = ""
			p_audio_config.achan[channel].octrl[ot].ptt_line = PTT_LINE_NONE
			p_audio_config.achan[channel].octrl[ot].ptt_line2 = PTT_LINE_NONE
			p_audio_config.achan[channel].octrl[ot].out_gpio_num = 0
			p_audio_config.achan[channel].octrl[ot].ptt_lpt_bit = 0
		}

		for it := 0; it < NUM_ICTYPES; it++ {
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

	for m := 0; m < TT_ERROR_MAXP1; m++ {
		p_tt_config.response[m].method = "MORSE"
		p_tt_config.response[m].mtext = "?"
	}

	p_tt_config.response[TT_ERROR_OK].mtext = "R"

	p_misc_config.agwpe_port = DEFAULT_AGWPE_PORT

	for i := 0; i < MAX_KISS_TCP_PORTS; i++ {
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

	for i := 0; i < MAX_TOTAL_CHANS; i++ {
		for j := 0; j < MAX_TOTAL_CHANS; j++ {
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
		for j := 0; j < MAX_TOTAL_CHANS; j++ {
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

func IsNoCall(callsign string) bool {
	return callsign == "" || strings.EqualFold(callsign, "NOCALL") || strings.EqualFold(callsign, "N0CALL")
}
