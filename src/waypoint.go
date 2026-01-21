package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Send NMEA waypoint sentences to GPS display or mapping application.
 *
 *---------------------------------------------------------------*/

// #include <stdio.h>
// #include <stdlib.h>
// #include <ctype.h>
// #include <errno.h>
// #include <sys/types.h>
// #include <sys/socket.h>
// #include <netinet/in.h>
// #include <netdb.h>		// gethostbyname
// #include <assert.h>
// #include <string.h>
// #include <time.h>
import "C"

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/term"
)

var s_waypoint_serial_port_fd *term.Term
var s_waypoint_udp_sock net.Conn
var s_waypoint_formats C.int = 0 // which formats should we generate?
var s_waypoint_debug = 0         // Print information flowing to attached device.

func waypoint_set_debug(n int) {
	s_waypoint_debug = n
}

/*-------------------------------------------------------------------
 *
 * Name:	waypoint_init
 *
 * Purpose:	Initialization for waypoint output port.
 *
 * Inputs:	mc			- Pointer to configuration options.
 *
 *		  ->waypoint_serial_port	- Name of serial port.  COM1, /dev/ttyS0, etc.
 *
 *		  ->waypoint_udp_hostname	- Destination host when using UDP.
 *
 *		  ->waypoint_udp_portnum	- UDP port number.
 *
 *		  (currently none)	- speed, baud.  Default 4800 if not set
 *
 *
 *		  ->waypoint_formats	- Set of formats enabled.
 *					  If none set, default to generic & Kenwood here.
 *
 * Global output: s_waypoint_serial_port_fd
 *		  s_waypoint_udp_sock
 *
 * Description:	First to see if this is shared with GPS input.
 *		If not, open serial port.
 *		In version 1.6 UDP is added.  It is possible to use both.
 *
 * Restriction:	MUST be done after GPS init because we might be sharing the
 *		same serial port device.
 *
 *---------------------------------------------------------------*/

func waypoint_init(mc *misc_config_s) {
	/* TODO KG
	#if DEBUG
		text_color_set (DW_COLOR_DEBUG);
		dw_printf ("waypoint_init() serial device=%s formats=%02x\n", mc.waypoint_serial_port, mc.waypoint_formats);
		dw_printf ("waypoint_init() destination hostname=%s UDP port=%d\n", mc.waypoint_udp_hostname, mc.waypoint_udp_portnum);
	#endif
	*/

	s_waypoint_udp_sock = nil
	if mc.waypoint_udp_portnum > 0 {
		var addr = net.JoinHostPort(mc.waypoint_udp_hostname, strconv.Itoa(int(mc.waypoint_udp_portnum)))
		var conn, err = net.Dial("udp", addr)

		if err != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Couldn't create socket for waypoint send to %s: %s\n", addr, err)
		} else {
			s_waypoint_udp_sock = conn
		}
	}

	/*
	 * TODO:
	 * Are we sharing with GPS input?
	 * First try to get fd if they have same device name.
	 * If that fails, do own serial port open.
	 */
	if mc.waypoint_serial_port != "" {
		s_waypoint_serial_port_fd = dwgpsnmea_get_fd(C.CString(mc.waypoint_serial_port), 4800)

		if s_waypoint_serial_port_fd == nil {
			s_waypoint_serial_port_fd = serial_port_open(mc.waypoint_serial_port, 4800)
		} else {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Note: Sharing same port for GPS input and waypoint output.\n")
		}

		if s_waypoint_serial_port_fd == nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Unable to open serial port %s for waypoint output.\n", mc.waypoint_serial_port)
		}
	}

	// Set default formats if user did not specify any.

	s_waypoint_formats = mc.waypoint_formats
	if s_waypoint_formats == 0 {
		s_waypoint_formats = WPL_FORMAT_NMEA_GENERIC | WPL_FORMAT_KENWOOD
	}
	if s_waypoint_formats&WPL_FORMAT_GARMIN > 0 {
		s_waypoint_formats |= WPL_FORMAT_NMEA_GENERIC /* See explanation below. */
	}

	/* TODO KG
	#if DEBUG
		text_color_set (DW_COLOR_DEBUG);
		dw_printf ("end of waypoint_init: s_waypoint_serial_port_fd = %d\n", s_waypoint_serial_port_fd);
		dw_printf ("end of waypoint_init: s_waypoint_udp_sock_fd = %d\n", s_waypoint_udp_sock_fd);
	#endif
	*/
}

/*-------------------------------------------------------------------
 *
 * Name:        append_checksum
 *
 * Purpose:     Append checksum to the sentence.
 *
 * In/out:	sentence	- NMEA sentence beginning with '$'.
 *
 * Description:	Checksum is exclusive of characters except leading '$'.
 *		We append '*' and an upper case two hexadecimal value.
 *
 *		Don't add CR/LF at this point.
 *
 *--------------------------------------------------------------------*/

func append_checksum(sentence []byte) []byte {
	Assert(sentence[0] == '$')

	var cs = 0
	for _, p := range sentence[1:] {
		cs ^= int(p)
	}

	var checksum = fmt.Sprintf("*%02X", cs&0xff)

	return append(sentence, checksum...)
} /* end append_checksum */

/*-------------------------------------------------------------------
 *
 * Name:        nema_send_waypoint
 *
 * Purpose:     Convert APRS position or object into NMEA waypoint sentence
 *		for use by a GPS display or other mapping application.
 *
 * Inputs:	wname_in	- Name of waypoint.  Max of 9 characters.
 *		dlat		- Latitude.
 *		dlong		- Longitude.
 *		symtab		- Symbol table or overlay character.
 *		symbol		- Symbol code.
 *		alt		- Altitude in meters or G_UNKNOWN.
 *		course		- Course in degrees or G_UNKNOWN for unknown.
 *		speed		- Speed in knots or G_UNKNOWN.
 *		comment_in	- Description or message.
 *
 *
 * Description:	Currently we send multiple styles.  Maybe someday there might
 * 		be an option to send a selected subset.
 *
 *			$GPWPL		- NMEA generic with only location and name.
 *			$PGRMW		- Garmin, adds altitude, symbol, and comment
 *					  to previously named waypoint.
 *			$PMGNWPL	- Magellan, more complete for stationary objects.
 *			$PKWDWPL	- Kenwood with APRS style symbol but missing comment.
 *
*
 * AvMap G5 notes:
 *
 *		https://sites.google.com/site/kd7kuj/home/files?pli=1
 *		https://sites.google.com/site/kd7kuj/home/files/AvMapMessaging040810.pdf?attredirects=0&d=1
 *
 *		It sends $GPGGA & $GPRMC with location.
 *		It understands generic $GPWPL and Kenwood $PKWDWPL.
 *
 *		There are some proprietary $PAVP* used only for messaging.
 *		Messaging would be a separate project.
 *
 *--------------------------------------------------------------------*/

func waypoint_send_sentence(name_in string, dlat float64, dlong float64, symtab rune, symbol byte,
	alt float64, course float64, speed float64, comment_in string) {
	/* TODO KG
	#if DEBUG
		text_color_set (DW_COLOR_DEBUG);
		dw_printf ("waypoint_send_sentence (\"%s\", \"%c%c\")\n", name_in, symtab, symbol);
	#endif
	*/

	// Don't waste time if no destinations specified.

	if s_waypoint_serial_port_fd == nil && s_waypoint_udp_sock == nil {
		return
	}

	/*
	 * We need to remove any , or * from name, symbol, or comment because they are field delimiters.
	 * Follow precedent of Geosat AvMap $PAVPMSG sentence and make the following substitutions:
	 *
	 *		,  ->  |
	 *		*  ->  ~
	 *
	 * The system on the other end would need to change them back after extracting the
	 * fields delimited by , or *.
	 * We will deal with the symbol in the Kenwood section.
	 * Needs to be left intact for other icon/symbol conversions.
	 */

	var wname = name_in
	wname = strings.ReplaceAll(wname, ",", "|")
	wname = strings.ReplaceAll(wname, "*", "~")

	var wcomment = comment_in
	wcomment = strings.ReplaceAll(wcomment, ",", "|")
	wcomment = strings.ReplaceAll(wcomment, "*", "~")

	/*
	 * Convert numeric values to character form.
	 * G_UNKNOWN value will result in an empty string.
	 */

	var slat, slat_ns = latitude_to_nmea(dlat)
	var slong, slong_ew = longitude_to_nmea(dlong)

	var salt string
	if alt != G_UNKNOWN {
		salt = fmt.Sprintf("%.1f", alt)
	}

	var sspeed string
	if speed != G_UNKNOWN {
		sspeed = fmt.Sprintf("%.1f", speed)
	}

	var scourse string
	if course != G_UNKNOWN {
		scourse = fmt.Sprintf("%.1f", course)
	}

	/*
	 *	NMEA Generic.
	 *
	 *	Has only location and name.  Rather disappointing.
	 *
	 *	$GPWPL,ddmm.mmmm,ns,dddmm.mmmm,ew,wname*99
	 *
	 *	Where,
	 *	 	ddmm.mmmm,ns	is latitude
	 *		dddmm.mmmm,ew	is longitude
	 *		wname		is the waypoint name
	 *		*99		is checksum
	 */

	if s_waypoint_formats&WPL_FORMAT_NMEA_GENERIC > 0 {
		var sentence = fmt.Sprintf("$GPWPL,%s,%s,%s,%s,%s", slat, slat_ns, slong, slong_ew, wname)
		var full_sentence = append_checksum([]byte(sentence))
		send_sentence(full_sentence)
	}

	/*
	 *	Garmin
	 *
	 *	https://www8.garmin.com/support/pdf/NMEA_0183.pdf
	 *
	 *	No location!  Adds altitude, symbol, and comment to existing waypoint.
	 *	So, we should always send the NMEA generic waypoint before this one.
	 *	The init function should take care of that.
	 *
	 *	$PGRMW,wname,alt,symbol,comment*99
	 *
	 *	Where,
	 *
	 *		wname		is waypoint name.  Must match existing waypoint.
	 *		alt		is altitude in meters.
	 *		symbol		is symbol code.  Hexadecimal up to FFFF.
	 *					See Garmin Device Interface Specification
	 *					001-0063-00 for values of "symbol_type."
	 *		comment		is comment for the waypoint.
	 *		*99		is checksum
	 */

	if s_waypoint_formats&WPL_FORMAT_GARMIN > 0 {
		var i = int(symbol - ' ')
		var grm_sym int /* Garmin symbol code. */

		if i >= 0 && (i < len(grm_primary_symtab) || i < len(grm_alternate_symtab)) {
			if symtab == '/' {
				grm_sym = grm_primary_symtab[i]
			} else {
				grm_sym = grm_alternate_symtab[i]
			}
		} else {
			grm_sym = sym_default
		}

		var sentence = fmt.Sprintf("$PGRMW,%s,%s,%04X,%s", wname, salt, grm_sym, wcomment)
		var full_sentence = append_checksum([]byte(sentence))
		send_sentence(full_sentence)
	}

	/*
	 *	Magellan
	 *
	 *	http://www.gpsinformation.org/mag-proto-2-11.pdf	Rev 2.11, Mar 2003, P/N 21-00091-000
	 *	http://gpsinformation.net/mag-proto.htm			Rev 1.0,  Aug 1999, P/N 21-00091-000
	 *
	 *
	 *	$PMGNWPL,ddmm.mmmm,ns,dddmm.mmmm,ew,alt,unit,wname,comment,icon,xx*99
	 *
	 *	Where,
	 *	 	ddmm.mmmm,ns	is latitude
	 *		dddmm.mmmm,ew	is longitude
	 *		alt		is altitude
	 *		unit		is M for meters or F for feet
	 *		wname		is the waypoint name
	 *		comment		is message or comment
	 *		icon		is one or two letters for icon code
	 *		xx		is waypoint type which is optional, not well
	 *					defined, and not used in their example
	 *					so we won't use it.
	 *		*99		is checksum
	 *
	 * Possible enhancement:  If the "object report" has the kill option set, use $PMGNDWP
	 * to delete that specific waypoint.
	 */

	if s_waypoint_formats&WPL_FORMAT_MAGELLAN > 0 {
		var i = int(symbol - ' ')
		var sicon string /* Magellan icon string.  Currently 1 or 2 characters. */

		if i >= 0 && (i < len(mgn_primary_symtab) || i < len(mgn_alternate_symtab)) {
			if symtab == '/' {
				sicon = mgn_primary_symtab[i]
			} else {
				sicon = mgn_alternate_symtab[i]
			}
		} else {
			sicon = MGN_default
		}

		var sentence = fmt.Sprintf("$PMGNWPL,%s,%s,%s,%s,%s,M,%s,%s,%s", slat, slat_ns, slong, slong_ew, salt, wname, wcomment, sicon)
		var full_sentence = append_checksum([]byte(sentence))
		send_sentence(full_sentence)
	}

	/*
	 *	Kenwood
	 *
	 *
	 *	$PKWDWPL,hhmmss,v,ddmm.mm,ns,dddmm.mm,ew,speed,course,ddmmyy,alt,wname,ts*99
	 *
	 *	Where,
	 *		hhmmss		is time in UTC from the clock in the transceiver.
	 *
	 *					This will be bogus if the clock was not set properly.
	 *					It does not use the timestamp from a position
	 *					report which could be useful.
	 *
	 *		GPS Status	A = active, V = void.
	 *					It looks like this might be modeled after the GPS status values
	 *					we see in $GPRMC.  i.e. Does the transceiver know its location?
	 *					I don't see how that information would be relevant in this context.
	 *					I've observed this under various conditions (No GPS, GPS with/without
	 *					fix) and it has always been "V."
	 *					(There is some information out there indicating this field
	 *					can contain "I" for invalid but I don't think that is accurate.)
	 *
	 *	 	ddmm.mm,ns	is latitude. N or S.
	 *		dddmm.mm,ew	is longitude.  E or W.
	 *
	 *					The D710 produces two fractional digits for minutes.
	 *					This is the same resolution most often used
	 *					in APRS packets.  Any additional resolution offered by
	 *					the compressed format or the DAO option is not conveyed here.
	 *					We will provide greater resolution.
	 *
	 *		speed		is speed over ground, knots.
	 *		course		is course over ground, degrees.
	 *
	 *					Empty if not available.
	 *
	 *		ddmmyy		is date.  See comments for time.
	 *
	 *		alt		is altitude, meters above mean sea level.
	 *
	 *					Empty if no altitude is available.
	 *
	 *		wname		is the waypoint name.  For an Object Report, the id is the object name.
	 *					For a position report, it is the call of the sending station.
	 *
	 *					An Object name can contain any printable characters.
	 *					What if object name contains , or * characters?
	 *					Those are field delimiter characters and it would be unfortunate
	 *					if they appeared in a NMEA sentence data field.
	 *
	 *					If there is a comma in the name, such as "test,5" the D710A displays
	 *					it fine but we end up with an extra field.
	 *
	 *						$PKWDWPL,150803,V,4237.14,N,07120.83,W,,,190316,,test,5,/'*30
	 *
	 *					If the name contains an asterisk, it doesn't show up on the
	 *					display and no waypoint sentence is generated.
	 *					We will substitute these two characters following the AvMap precedent.
	 *
	 *						$PKWDWPL,204714,V,4237.1400,N,07120.8300,W,,,200316,,test|5,/'*61
	 *						$PKWDWPL,204719,V,4237.1400,N,07120.8300,W,,,200316,,test~6,/'*6D
	 *
	 *		ts		are the table and symbol.
	 *
	 *					What happens if the symbol is comma or asterisk?
	 *						, Boy Scouts / Girl Scouts
	 *						* SnowMobile / Snow
	 *
	 *					the D710A just pushes them thru without checking.
	 *					These would not be parsed properly:
	 *
	 *						$PKWDWPL,150753,V,4237.14,N,07120.83,W,,,190316,,test3,/,*1B
	 *						$PKWDWPL,150758,V,4237.14,N,07120.83,W,,,190316,,test4,/ **3B
	 *
	 *					We perform the usual substitution and the other end would
	 *					need to change them back after extracting from NMEA sentence.
	 *
	 *						$PKWDWPL,204704,V,4237.1400,N,07120.8300,W,,,200316,,test3,/|*41
	 *						$PKWDWPL,204709,V,4237.1400,N,07120.8300,W,,,200316,,test4,/~*49
	 *
	 *
	 *		*99		is checksum
	 *
	 *	Oddly, there is no place for comment.
	 */

	if s_waypoint_formats&WPL_FORMAT_KENWOOD > 0 {
		var now = time.Now()
		var stime = now.Format("150405") // "%H%M%S"
		var sdate = now.Format("020106") // "%d%m%y"

		// A symbol code of , or * would not be good because
		// they are field delimiters for NMEA sentences.

		// The AvMap G5 to Kenwood protocol description performs a substitution
		// for these characters that appear in message text.
		//		,	->	|
		//		*	->	~

		// Those two are listed as "TNC Stream Switch" and are not used for symbols.
		// It might be reasonable assumption that this same substitution might be
		// used for the symbol code.

		var ken_sym byte /* APRS symbol with , or * substituted. */
		switch symbol {
		case ',':
			ken_sym = '|'
		case '*':
			ken_sym = '~'
		default:
			ken_sym = symbol
		}

		var sentence = fmt.Sprintf("$PKWDWPL,%s,V,%s,%s,%s,%s,%s,%s,%s,%s,%s,%c%c",
			stime, slat, slat_ns, slong, slong_ew,
			sspeed, scourse, sdate, salt, wname, symtab, ken_sym)
		var full_sentence = append_checksum([]byte(sentence))
		send_sentence(full_sentence)
	}

	/*
	 *	One application recognizes these.  Not implemented at this time.
	 *
	 *	$GPTLL,01,ddmm.mmmm,ns,dddmm.mmmm,ew,tname,000000.00,T,R*99
	 *
	 *	Where,
	 *		ddmm.mmmm,ns	is latitude
	 *		dddmm.mmmm,ew	is longitude
	 *		tname		is the target name
	 *		000000.00	is timestamp ???
	 *		T		is target status (S for need help)
	 *		R		is reference target ???
	 *		*99		is checksum
	 *
	 *
	 *	$GPTXT,01,01,tname,message*99
	 *
	 *	Where,
	 *
	 *		01		is total number of messages in transmission
	 *		01		is message number in this transmission
	 *		tname		is target name.  Should match name in WPL or TTL.
	 *		message		is the message.
	 *		*99		is checksum
	 *
	 */
} /* end waypoint_send_sentence */

/*-------------------------------------------------------------------
 *
 * Name:        nema_send_ais
 *
 * Purpose:     Send NMEA AIS sentence to GPS display or other mapping application.
 *
 * Inputs:	sentence	- should look something like this, with checksum, and no CR LF.
 *
 *			!AIVDM,1,1,,A,35NO=dPOiAJriVDH@94E84AJ0000,0*4B
 *
 *--------------------------------------------------------------------*/

func waypoint_send_ais(sentence []byte) {
	if s_waypoint_serial_port_fd == nil && s_waypoint_udp_sock == nil {
		return
	}

	if s_waypoint_formats&WPL_FORMAT_AIS > 0 {
		send_sentence(sentence)
	}
}

/*
 * Append CR LF and send it.
 */

func send_sentence(sentence []byte) {
	if s_waypoint_debug > 0 {
		text_color_set(DW_COLOR_XMIT)
		dw_printf("waypoint send sentence: \"%s\"\n", sentence)
	}

	var final = sentence
	final = append(final, '\r')
	final = append(final, '\n')

	var final_len = len(final)

	if s_waypoint_serial_port_fd != nil {
		serial_port_write(s_waypoint_serial_port_fd, final)
	}

	if s_waypoint_udp_sock != nil {
		var n, err = s_waypoint_udp_sock.Write(final)
		if n != final_len {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Failed to send waypoint via UDP, err=%s\n", err)
		}
	}
} /* send_sentence */

func waypoint_term() {
	if s_waypoint_serial_port_fd != nil {
		serial_port_close(s_waypoint_serial_port_fd)
		s_waypoint_serial_port_fd = nil
	}
	if s_waypoint_udp_sock != nil {
		s_waypoint_udp_sock.Close()
		s_waypoint_udp_sock = nil
	}
}
