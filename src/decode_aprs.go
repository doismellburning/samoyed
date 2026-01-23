package direwolf

// TODO:  Better error messages for examples here: http://lists.tapr.org/pipermail/aprssig_lists.tapr.org/2023-July/date.html

/*------------------------------------------------------------------
 *
 * Purpose:	Decode the information part of APRS frame.
 *
 * Description: Present the packet contents in human readable format.
 *		This is a fairly complete implementation with error messages
 *		pointing out various specification violations.
 *
 * Assumptions:	ax25_from_frame() has been called to
 *		separate the header and information.
 *
 *------------------------------------------------------------------*/

// #include <stdio.h>
// #include <time.h>
// #include <assert.h>
// #include <stdlib.h>	/* for atof */
// #include <string.h>	/* for strtok */
// #include <math.h>	/* for pow */
// #include <ctype.h>	/* for isdigit */
// #include <fcntl.h>
// #include "regex.h"
import "C"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"
)

type packet_type_e int

const (
	packet_type_none packet_type_e = iota
	packet_type_position
	packet_type_weather
	packet_type_object
	packet_type_item
	packet_type_message
	packet_type_query
	packet_type_capabilities
	packet_type_status
	packet_type_telemetry
	packet_type_userdefined
	packet_type_nws
)

type message_subtype_e int

const (
	message_subtype_invalid message_subtype_e = iota
	message_subtype_message
	message_subtype_ack
	message_subtype_rej
	message_subtype_bulletin
	message_subtype_nws
	message_subtype_telem_parm
	message_subtype_telem_unit
	message_subtype_telem_eqns
	message_subtype_telem_bits
	message_subtype_directed_query
)

type decode_aprs_t struct {
	g_quiet C.int /* Suppress error messages when decoding. */

	g_src [AX25_MAX_ADDR_LEN]C.char // In the case of a packet encapsulated by a 3rd party
	// header, this is the encapsulated source.

	g_dest [AX25_MAX_ADDR_LEN]C.char

	g_data_type_desc [100]C.char /* APRS data type description.  Telemetry descriptions get pretty long. */

	g_symbol_table C.char /* The Symbol Table Identifier character selects one */
	/* of the two Symbol Tables, or it may be used as */
	/* single-character (alpha or numeric) overlay, as follows: */

	/*	/ 	Primary Symbol Table (mostly stations) */

	/* 	\ 	Alternate Symbol Table (mostly Objects) */

	/*	0-9 	Numeric overlay. Symbol from Alternate Symbol */
	/*		Table (uncompressed lat/long data format) */

	/*	a-j	Numeric overlay. Symbol from Alternate */
	/*		Symbol Table (compressed lat/long data */
	/*		format only). i.e. a-j maps to 0-9 */

	/*	A-Z	Alpha overlay. Symbol from Alternate Symbol Table */

	g_symbol_code C.char /* Where the Symbol Table Identifier is 0-9 or A-Z (or a-j */
	/* with compressed position data only), the symbol comes from */
	/* the Alternate Symbol Table, and is overlaid with the */
	/* identifier (as a single digit or a capital letter). */

	g_aprstt_loc [APRSTT_LOC_DESC_LEN]C.char /* APRStt location from !DAO! */

	g_lat C.double
	g_lon C.double /* Location, degrees.  Negative for South or West. */
	/* Set to G_UNKNOWN if missing or error. */

	g_maidenhead [12]C.char /* 4 or 6 (or 8?) character maidenhead locator. */

	g_name [12]C.char /* Object or item name. Max. 9 characters. */

	g_addressee [12]C.char /* Addressee for a "message."  Max. 9 characters. */
	/* Also for Directed Station Query which is a */
	/* special case of message. */

	// This is so pfilter.c:filt_t does not need to duplicate the same work.

	g_has_thirdparty_header C.int
	g_packet_type           packet_type_e

	g_message_subtype message_subtype_e /* Various cases of the overloaded "message." */

	g_message_number [12]C.char /* Message number.  Should be 1 - 5 alphanumeric characters if used. */
	/* Addendum 1.1 has new format {mm} or {mm}aa with only two */
	/* characters for message number and an ack riding piggyback. */

	g_speed_mph C.float /* Speed in MPH.  */
	/* The APRS transmission uses knots so watch out for */
	/* conversions when sending and receiving APRS packets. */

	g_course C.float /* 0 = North, 90 = East, etc. */

	g_power C.int /* Transmitter power in watts. */

	g_height C.int /* Antenna height above average terrain, feet. */
	// TODO:  rename to g_height_ft

	g_gain C.int /* Antenna gain in dBi. */

	g_directivity [12]C.char /* Direction of max signal strength */

	g_range C.float /* Precomputed radio range in miles. */

	g_altitude_ft C.float /* Feet above median sea level.  */
	/* I used feet here because the APRS specification */
	/* has units of feet for altitude.  Meters would be */
	/* more natural to the other 96% of the world. */

	g_mfr [80]C.char /* Manufacturer or application. */

	g_mic_e_status [32]C.char /* MIC-E message. */

	g_freq C.double /* Frequency, MHz */

	g_tone C.float /* CTCSS tone, Hz, one fractional digit */

	g_dcs C.int /* Digital coded squelch, print as 3 octal digits. */

	g_offset C.int /* Transmit offset, kHz */

	g_query_type [12]C.char /* General Query: APRS, IGATE, WX, ... */
	/* Addressee is NOT set. */

	/* Directed Station Query: exactly 5 characters. */
	/* APRSD, APRST, PING?, ... */
	/* Addressee is set. */

	g_footprint_lat    C.double /* A general query may contain a foot print. */
	g_footprint_lon    C.double /* Set all to G_UNKNOWN if not used. */
	g_footprint_radius C.float  /* Radius in miles. */

	g_query_callsign [12]C.char /* Directed query may contain callsign.  */
	/* e.g. tell me all objects from that callsign. */

	g_weather [500]C.char /* Weather.  Can get quite long. Rethink max size. */

	g_telemetry [256]C.char /* Telemetry data.  Rethink max size. */

	g_comment [256]C.char /* Comment. */

}

/*------------------------------------------------------------------
 *
 * Function:	decode_aprs
 *
 * Purpose:	Split APRS packet into separate properties that it contains.
 *
 * Inputs:	pp	- APRS packet object.
 *
 *		quiet	- Suppress error messages.
 *
 *		third_party_src - Specify when parsing a third party header.
 *			(decode_aprs is called recursively.)
 *			This is mostly found when an IGate transmits a message
 *			that came via APRS-IS.
 *			nil when not third party payload.
 *
 * Outputs:	A.	g_symbol_table, g_symbol_code,
 *			g_lat, g_lon,
 *			g_speed_mph, g_course, g_altitude_ft,
 *			g_comment
 *			... and many others...
 *
 * Major Revisions: 1.1	Reorganized so parts are returned in a structure.
 *			Print function is now called separately.
 *
 *------------------------------------------------------------------*/

func decode_aprs(A *decode_aprs_t, pp *packet_t, quiet C.int, third_party_src *C.char) {

	//dw_printf ("DEBUG decode_aprs quiet=%d, third_party=%p\n", quiet, third_party_src);

	var _pinfo *C.uchar
	var info_len = ax25_get_info(pp, &_pinfo)

	//dw_printf ("DEBUG decode_aprs info=\"%s\"\n", pinfo);

	A.g_quiet = quiet

	var pinfo = C.GoBytes(unsafe.Pointer(_pinfo), info_len)

	if unicode.IsPrint(rune(pinfo[0])) {
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("ERROR!!!  Unknown APRS Data Type Indicator \"%c\"", pinfo[0])))
	} else {
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("ERROR!!!  Unknown APRS Data Type Indicator: unprintable 0x%02x", pinfo[0])))
	}

	A.g_symbol_table = '/' /* Default to primary table. */
	A.g_symbol_code = ' '  /* What should we have for default symbol? */

	A.g_lat = G_UNKNOWN
	A.g_lon = G_UNKNOWN

	A.g_speed_mph = G_UNKNOWN
	A.g_course = G_UNKNOWN

	A.g_power = G_UNKNOWN
	A.g_height = G_UNKNOWN
	A.g_gain = G_UNKNOWN

	A.g_range = G_UNKNOWN
	A.g_altitude_ft = G_UNKNOWN
	A.g_freq = G_UNKNOWN
	A.g_tone = G_UNKNOWN
	A.g_dcs = G_UNKNOWN
	A.g_offset = G_UNKNOWN

	A.g_footprint_lat = G_UNKNOWN
	A.g_footprint_lon = G_UNKNOWN
	A.g_footprint_radius = G_UNKNOWN

	// Check for RFONLY or NOGATE in the destination field.
	// Actual cases observed.
	// W1KU-4>APDW15,W1IMD,WIDE1,KQ1L-8,N3LLO-3,WIDE2*:}EB1EBT-9>NOGATE,TCPIP,W1KU-4*::DF1AKR-9 :73{4
	// NE1CU-10>RFONLY,KB1AEV-15,N3LLO-3,WIDE2*:}W1HS-11>APMI06,TCPIP,NE1CU-10*:T#050,190,039,008,095,20403,00000000

	var _atemp [AX25_MAX_ADDR_LEN]C.char
	ax25_get_addr_no_ssid(pp, AX25_DESTINATION, &_atemp[0])
	var atemp = C.GoString(&_atemp[0])

	if quiet == 0 {
		if atemp == "RFONLY" || atemp == "NOGATE" {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("RFONLY and NOGATE must not appear in the destination address field.\n")
			dw_printf("They should appear only at the end of the digi via path.\n")
		}
	}

	// Complain if obsolete WIDE or RELAY is found in via path.

	for i := C.int(0); i < ax25_get_num_repeaters(pp); i++ {
		ax25_get_addr_no_ssid(pp, AX25_REPEATER_1+i, &_atemp[0])
		if quiet == 0 {
			if atemp == "RELAY" || atemp == "WIDE" || atemp == "TRACE" {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("RELAY, TRACE, and WIDE (not WIDEn) are obsolete.\n")
				dw_printf("Modern digipeaters will not recoginize these.\n")
			}
		}
	}

	// TODO: complain if unused WIDEn-0 is see in path.
	// There is a report of UIDIGI decrementing ssid 1 to 0 and not marking it used.
	// http://lists.tapr.org/pipermail/aprssig_lists.tapr.org/2022-May/049397.html

	// TODO: Complain if used digi is found after unused.  Should never happen.

	// If third-party header, try to decode just the payload.

	if pinfo[0] == '}' {

		//dw_printf ("DEBUG decode_aprs recursively process third party header\n");

		// This must not be strict because the addresses in third party payload doesn't
		// need to adhere to the AX.25 address format (i.e. 6 upper case alphanumeric.)
		// SSID can be 2 alphanumeric characters.
		// Addresses can include lower case, e.g. q construct.

		// e.g.  WR2X-2>APRS,WA1PLE-13*:}
		//		K1BOS-B>APOSB,TCPIP,WR2X-2*:@122015z4221.42ND07111.93W&/A=000000SharkRF openSPOT3 MMDVM446.025 MA/SW

		var pp_payload = ax25_from_text(C.CString(string(pinfo[1:])), 0)
		if pp_payload != nil {
			var payload_src = pinfo[1:]
			payload_src, _, _ = bytes.Cut(payload_src, []byte{'>'})
			A.g_has_thirdparty_header = 1
			decode_aprs(A, pp_payload, quiet, C.CString(string(payload_src))) // 1 means used recursively
			ax25_delete(pp_payload)
			return
		} else {
			C.strcpy(&A.g_data_type_desc[0], C.CString("Third Party Header: Unable to parse payload."))
			ax25_get_addr_with_ssid(pp, AX25_SOURCE, &A.g_src[0])
			ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &A.g_dest[0])
		}
	}

	/*
	 * Extract source and destination including the SSID.
	 */
	if third_party_src != nil {
		C.strcpy(&A.g_src[0], third_party_src)
	} else {
		ax25_get_addr_with_ssid(pp, AX25_SOURCE, &A.g_src[0])
	}
	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &A.g_dest[0])

	//dw_printf ("DEBUG decode_aprs source=%s, dest=%s\n", A.g_src, A.g_dest);

	/*
	 * Report error if the information part contains a nul character.
	 * There are two known cases where this can happen.
	 *
	 *  - The Kenwood TM-D710A sometimes sends packets like this:
	 *
	 * 	VA3AJ-9>T2QU6X,VE3WRC,WIDE1,K8UNS,WIDE2*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`nW<0x1f>oS8>/]"6M}driving fast=
	 * 	K4JH-9>S5UQ6X,WR4AGC-3*,WIDE1*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`jP}l"&>/]"47}QRV from the EV =
	 *
	 *     Notice that the data type indicator of "4" is not valid.  If we remove
	 *     4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>   we are left with a good MIC-E format.
	 *     This same thing has been observed from others and is intermittent.
	 *
	 *  - AGW Tracker can send UTF-16 if an option is selected.  This can introduce nul bytes.
	 *    This is wrong, it should be using UTF-8.
	 */

	if (A.g_quiet == 0) && bytes.Contains(pinfo, []byte{0}) {

		text_color_set(DW_COLOR_ERROR)
		dw_printf("'nul' character found in Information part.  This should never happen with APRS.\n")
		dw_printf("If this is meant to be APRS, %s is transmitting with defective software.\n", C.GoString(&A.g_src[0]))

		if bytes.HasPrefix(pinfo, []byte("4P")) {
			dw_printf("The TM-D710 will do this intermittently.  A firmware upgrade is needed to fix it.\n")
		}
	}

	/*
	 * Device/Application is in the destination field for most packet types.
	 * MIC-E format has part of location in the destination field.
	 */

	switch pinfo[0] { /* "DTI" data type identifier. */

	case '\'': /* Old Mic-E Data */
		fallthrough
	case '`': /* Current Mic-E Data */

	default:
		C.strcpy(&A.g_mfr[0], C.CString(deviceid_decode_dest(C.GoString(&A.g_dest[0]))))
	}

	switch pinfo[0] { /* "DTI" data type identifier. */

	case '!': /* Position without timestamp (no APRS messaging). */
		/* or Ultimeter 2000 WX Station */
		fallthrough

	case '=': /* Position without timestamp (with APRS messaging). */

		if bytes.HasPrefix(pinfo, []byte("!!")) {
			aprs_ultimeter(A, pinfo) // TODO: produce obsolete error.
		} else {
			aprs_ll_pos(A, pinfo)
		}
		A.g_packet_type = packet_type_position

	//case '#':		/* Peet Bros U-II Weather station */		// TODO: produce obsolete error.
	//case '*':		/* Peet Bros U-II Weather station */
	//break;

	case '$': /* Raw GPS data or Ultimeter 2000 */

		if bytes.HasPrefix(pinfo, []byte("$ULTW")) {
			aprs_ultimeter(A, pinfo) // TODO: produce obsolete error.
			A.g_packet_type = packet_type_weather
		} else {
			aprs_raw_nmea(A, pinfo)
			A.g_packet_type = packet_type_position
		}

	case '\'': /* Old Mic-E Data (but Current data for TM-D700) */
		fallthrough
	case '`': /* Current Mic-E Data (not used in TM-D700) */

		aprs_mic_e(A, pp, pinfo)
		A.g_packet_type = packet_type_position

	case ')': /* Item. */

		aprs_item(A, pinfo)
		A.g_packet_type = packet_type_item

	case '/': /* Position with timestamp (no APRS messaging) */
		fallthrough
	case '@': /* Position with timestamp (with APRS messaging) */

		aprs_ll_pos_time(A, pinfo)
		A.g_packet_type = packet_type_position

	case ':': /* "Message" (special APRS meaning): for one person, a group, or a bulletin. */
		/* Directed Station Query */
		/* Telemetry metadata. */

		aprs_message(A, pinfo, quiet > 0)

		switch A.g_message_subtype {
		case message_subtype_message, message_subtype_ack, message_subtype_rej:
			A.g_packet_type = packet_type_message
		case message_subtype_nws:
			A.g_packet_type = packet_type_nws
		case message_subtype_telem_parm, message_subtype_telem_unit, message_subtype_telem_eqns, message_subtype_telem_bits:
			A.g_packet_type = packet_type_telemetry
		case message_subtype_directed_query:
			A.g_packet_type = packet_type_query
		default:
			// Also case message_subtype_bulletin:
		}

	case ';': /* Object */
		aprs_object(A, pinfo)
		A.g_packet_type = packet_type_object

	case '<': /* Station Capabilities */
		aprs_station_capabilities(A, pinfo)
		A.g_packet_type = packet_type_capabilities

	case '>': /* Status Report */
		aprs_status_report(A, pinfo)
		A.g_packet_type = packet_type_status

	case '?': /* General Query */
		aprs_general_query(A, pinfo, quiet)
		A.g_packet_type = packet_type_query

	case 'T': /* Telemetry */
		aprs_telemetry(A, pinfo, quiet)
		A.g_packet_type = packet_type_telemetry

	case '_': /* Positionless Weather Report */
		aprs_positionless_weather_report(A, pinfo)
		A.g_packet_type = packet_type_weather

	case '{': /* user defined data */
		aprs_user_defined(A, pinfo)
		A.g_packet_type = packet_type_userdefined

	case 't': /* Raw touch tone data - NOT PART OF STANDARD */
		/* Used to convey raw touch tone sequences to */
		/* to an application that might want to interpret them. */
		/* Might move into user defined data, above. */

		aprs_raw_touch_tone(A, pinfo)
		// no packet type for t/ filter

	case 'm': /* Morse Code data - NOT PART OF STANDARD */
		/* Used by APRStt gateway to put audible responses */
		/* into the transmit queue.  Could potentially find */
		/* other uses such as CW ID for station. */
		/* Might move into user defined data, above. */

		aprs_morse_code(A, pinfo)
		// no packet type for t/ filter

	//case '}':		/* third party header */

	// was already caught earlier.

	//case '\r':		/* CR or LF? */
	//case '\n':

	//break;

	default:
	}

	/*
	 * Priority order for determining the symbol is:
	 *	- Information part, where appropriate.  Already done above.
	 *	- Destination field starting with GPS, SPC, or SYM.
	 *	- Source SSID - Confusing to most people.  Even I forgot about it when
	 *		someone questioned where the symbol came from.  It's in the APRS
	 *		protocol spec, end of Chapter 20.
	 */

	if A.g_symbol_table == ' ' || A.g_symbol_code == ' ' {

		// A symbol on a "message" makes no sense and confuses people.
		// Third party too.  Set from the payload.
		// Maybe eliminate for a couple others.

		//dw_printf ("DEBUG decode_aprs@end1 third_party=%d, symbol_table=%c, symbol_code=%c, *pinfo=%c\n", third_party, A.g_symbol_table, A.g_symbol_code, *pinfo);

		if pinfo[0] != ':' && pinfo[0] != '}' {
			var symtab, symbol = symbols_from_dest_or_src(pinfo[0], C.GoString(&A.g_src[0]), C.GoString(&A.g_dest[0]))
			A.g_symbol_table = C.char(symtab)
			A.g_symbol_code = C.char(symbol)
		}

		//dw_printf ("DEBUG decode_aprs@end2 third_party=%d, symbol_table=%c, symbol_code=%c, *pinfo=%c\n", third_party, A.g_symbol_table, A.g_symbol_code, *pinfo);
	}

} /* end decode_aprs */

func decode_aprs_print(A *decode_aprs_t) {

	/*
	 * First line has:
	 * - packet type
	 * - object name
	 * - symbol
	 * - manufacturer/application
	 * - mic-e status
	 * - power/height/gain, range
	 */
	var stemp = C.GoString(&A.g_data_type_desc[0])

	//dw_printf ("DEBUG decode_aprs_print stemp1=%s\n", stemp);

	if C.strlen(&A.g_name[0]) > 0 {
		stemp += ", \""
		stemp += C.GoString(&A.g_name[0])
		stemp += "\""
	}

	//dw_printf ("DEBUG decode_aprs_print stemp2=%s\n", stemp);

	//dw_printf ("DEBUG decode_aprs_print symbol_code=%c=0x%02x\n", A.g_symbol_code, A.g_symbol_code);

	if A.g_symbol_code != ' ' {
		var symbol_description = symbols_get_description(byte(A.g_symbol_table), byte(A.g_symbol_code))

		//dw_printf ("DEBUG decode_aprs_print symbol_description_description=%s\n", symbol_description);

		stemp += ", "
		stemp += symbol_description
	}

	//dw_printf ("DEBUG decode_aprs_print stemp3=%s mfr=%s\n", stemp, A.g_mfr);

	if C.strlen(&A.g_mfr[0]) > 0 {
		if C.strcmp(&A.g_dest[0], C.CString("APRS")) == 0 ||
			C.strcmp(&A.g_dest[0], C.CString("BEACON")) == 0 ||
			C.strcmp(&A.g_dest[0], C.CString("ID")) == 0 {
			stemp += "\nUse of \""
			stemp += C.GoString(&A.g_dest[0])
			stemp += "\" in the destination field is obsolete."
			stemp += "  You can help to improve the quality of APRS signals."
			stemp += "\nTell the sender ("
			stemp += C.GoString(&A.g_src[0])
			stemp += ") to use the proper product identifier from"
			stemp += " https://github.com/aprsorg/aprs-deviceid "
		} else {
			stemp += ", "
			stemp += C.GoString(&A.g_mfr[0])
		}
	}

	//dw_printf ("DEBUG decode_aprs_print stemp4=%s\n", stemp);

	if C.strlen(&A.g_mic_e_status[0]) > 0 {
		stemp += ", "
		stemp += C.GoString(&A.g_mic_e_status[0])
	}

	//dw_printf ("DEBUG decode_aprs_print stemp5=%s\n", stemp);

	if A.g_power > 0 {
		/* Protocol spec doesn't mention whether this is dBd or dBi.  */
		/* Clarified later. */
		/* http://eng.usna.navy.mil/~bruninga/aprs/aprs11.html */
		/* "The Antenna Gain in the PHG format on page 28 is in dBi." */

		stemp += fmt.Sprintf(", %d W height(HAAT)=%dft=%.0fm %ddBi %s", A.g_power, A.g_height, DW_FEET_TO_METERS(float64(A.g_height)), A.g_gain, C.GoString(&A.g_directivity[0]))
	}

	if A.g_range > 0 {
		stemp += fmt.Sprintf(", range=%.1f", A.g_range)
	}

	if strings.HasPrefix(stemp, "ERROR") {
		text_color_set(DW_COLOR_ERROR)
	} else {
		text_color_set(DW_COLOR_DECODED)
	}
	dw_printf("%s\n", stemp)

	/*
	 * Second line has:
	 * - Latitude
	 * - Longitude
	 * - speed
	 * - direction
	 * - altitude
	 * - frequency
	 */

	/*
	 * Convert Maidenhead locator to latitude and longitude.
	 *
	 * Any example was checked for each hemihemisphere using
	 * http://www.amsat.org/cgi-bin/gridconv
	 */

	if C.strlen(&A.g_maidenhead[0]) > 0 {

		if A.g_lat == G_UNKNOWN && A.g_lon == G_UNKNOWN {
			var lat, lon, err = ll_from_grid_square(C.GoString(&A.g_maidenhead[0]))
			if err == nil {
				A.g_lat = C.double(lat)
				A.g_lon = C.double(lon)
			}
		}

		dw_printf("Grid square = %s, ", C.GoString(&A.g_maidenhead[0]))
	}

	stemp = ""

	if A.g_lat != G_UNKNOWN || A.g_lon != G_UNKNOWN {

		var s_lat, s_lon string
		// Have location but it is possible one part is invalid.

		if A.g_lat != G_UNKNOWN {
			var absll C.double
			var news rune

			if A.g_lat >= 0 {
				absll = A.g_lat
				news = 'N'
			} else {
				absll = -A.g_lat
				news = 'S'
			}
			var deg = int(absll)
			var _min = (absll - C.double(deg)) * 60.0
			s_lat = fmt.Sprintf("%c %02d°%07.4f", news, deg, _min)
		} else {
			s_lat = "Invalid Latitude"
		}

		if A.g_lon != G_UNKNOWN {
			var absll C.double
			var news rune

			if A.g_lon >= 0 {
				absll = A.g_lon
				news = 'E'
			} else {
				absll = -A.g_lon
				news = 'W'
			}
			var deg = int(absll)
			var _min = (absll - C.double(deg)) * 60.0
			s_lon = fmt.Sprintf("%c %03d°%07.4f", news, deg, _min)
		} else {
			s_lon = "Invalid Longitude"
		}

		stemp = fmt.Sprintf("%s, %s", s_lat, s_lon)
	}

	if C.strlen(&A.g_aprstt_loc[0]) > 0 {
		if len(stemp) > 0 {
			stemp += ", "
		}
		stemp += C.GoString(&A.g_aprstt_loc[0])
	}

	if A.g_speed_mph != G_UNKNOWN {
		if len(stemp) > 0 {
			stemp += ", "
		}
		stemp += fmt.Sprintf("%.0f km/h (%.0f MPH)", DW_MILES_TO_KM(float64(A.g_speed_mph)), A.g_speed_mph)
	}

	if A.g_course != G_UNKNOWN {
		if len(stemp) > 0 {
			stemp += ", "
		}
		stemp += fmt.Sprintf("course %.0f", A.g_course)
	}

	if A.g_altitude_ft != G_UNKNOWN {
		if len(stemp) > 0 {
			stemp += ", "
		}
		stemp += fmt.Sprintf("alt %.0f m (%.0f ft)", DW_FEET_TO_METERS(float64(A.g_altitude_ft)), A.g_altitude_ft)
	}

	if A.g_freq != G_UNKNOWN {
		stemp += fmt.Sprintf(", %.3f MHz", A.g_freq)
	}

	if A.g_offset != G_UNKNOWN {
		if A.g_offset%1000 == 0 {
			stemp += fmt.Sprintf(", %+dM", A.g_offset/1000)
		} else {
			stemp += fmt.Sprintf(", %+dk", A.g_offset)
		}
	}

	if A.g_tone != G_UNKNOWN {
		if A.g_tone == 0 {
			stemp += ", no PL"
		} else {
			stemp += fmt.Sprintf(", PL %.1f", A.g_tone)
		}
	}

	if A.g_dcs != G_UNKNOWN {
		stemp += fmt.Sprintf(", DCS %03o", A.g_dcs)
	}

	if len(stemp) > 0 {
		text_color_set(DW_COLOR_DECODED)
		dw_printf("%s\n", stemp)
	}

	/*
	 * Finally, any weather and/or comment.
	 *
	 * Non-printable characters are changed to safe hexadecimal representations.
	 * For example, carriage return is displayed as <0x0d>.
	 *
	 * Drop annoying trailing CR LF.  Anyone who cares can see it in the raw datA->
	 */

	var n = C.strlen(&A.g_weather[0])
	if n >= 1 && A.g_weather[n-1] == '\n' {
		A.g_weather[n-1] = 0
		n--
	}
	if n >= 1 && A.g_weather[n-1] == '\r' {
		A.g_weather[n-1] = 0
		n--
	}
	if n > 0 {
		ax25_safe_print(&A.g_weather[0], -1, 0)
		dw_printf("\n")
	}

	if C.strlen(&A.g_telemetry[0]) > 0 {
		ax25_safe_print(&A.g_telemetry[0], -1, 0)
		dw_printf("\n")
	}

	n = C.strlen(&A.g_comment[0])
	if n >= 1 && A.g_comment[n-1] == '\n' {
		A.g_comment[n-1] = 0
		n--
	}
	if n >= 1 && A.g_comment[n-1] == '\r' {
		A.g_comment[n-1] = 0
		n--
	}
	if n > 0 {
		ax25_safe_print(&A.g_comment[0], -1, 0)
		dw_printf("\n")

		/*
		 * Point out incorrect attempts a degree symbol.
		 * 0xb0 is degree in ISO Latin1.
		 * To be part of a valid UTF-8 sequence, it would need to be preceded by 11xxxxxx or 10xxxxxx.
		 * 0xf8 is degree in Microsoft code page 437.
		 * To be part of a valid UTF-8 sequence, it would need to be followed by 10xxxxxx.
		 */

		// For values 00-7F, ASCII, Unicode, and ISO Latin-1 are all the same.
		// ISO Latin-1 adds 80-FF range with a few common symbols, such as degree, and
		// letters, with diacritical marks, for many European languages.
		// Unicode range 80-FF is called "Latin-1 Supplement."  Exactly the same as ISO Latin-1.
		// For UTF-8, an additional byte is inserted.
		//	Unicode		UTF-8
		//	-------		-----
		//	8x		C2 8x		Insert C2, keep original
		//	9x		C2 9x		"
		//	Ax		C2 Ax		"
		//	Bx		C2 Bx		"
		//	Cx		C3 8x		Insert C3, subtract 40 from original
		//	Dx		C3 9x		"
		//	Ex		C3 Ax		"
		//	Fx		C3 Bx		"
		//
		// Can we use this knowledge to provide guidance on other ISO Latin-1 characters besides degree?
		// Should we?
		// Reference:   https://www.fileformat.info/info/unicode/utf8test.htm

		if A.g_quiet == 0 {

			for j := C.size_t(0); j < n; j++ {
				if byte(A.g_comment[j]) == 0xb0 && (j == 0 || (byte(A.g_comment[j-1])&0x80) == 0) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Character code 0xb0 is probably an attempt at a degree symbol.\n")
					dw_printf("The correct encoding is 0xc2 0xb0 in UTF-8.\n")
				}
			}
			for j := C.size_t(0); j < n; j++ {
				if byte(A.g_comment[j]) == 0xf8 && (j == n-1 || (byte(A.g_comment[j+1])&0xc0) != 0xc0) {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Character code 0xf8 is probably an attempt at a degree symbol.\n")
					dw_printf("The correct encoding is 0xc2 0xb0 in UTF-8.\n")
				}
			}
		}
	}
}

/*------------------------------------------------------------------
 *
 * Function:	aprs_ll_pos
 *
 * Purpose:	Decode "Lat/Long Position Report - without Timestamp"
 *
 *		Reports without a timestamp can be regarded as real-time.
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_lat, A.g_lon, A.g_symbol_table, A.g_symbol_code, A.g_speed_mph, A.g_course, A.g_altitude_ft.
 *
 * Description:	Type identifier '=' has APRS messaging.
 *		Type identifier '!' does not have APRS messaging.
 *
 *		The location can be in either compressed or human-readable form.
 *
 *		When the symbol code is '_' this is a weather report.
 *
 * Examples:	!4309.95NS07307.13W#PHG3320 W2,NY2 Mt Equinox VT k2lm@arrl.net
 *		!4237.14NS07120.83W#
 * 		=4246.40N/07115.15W# {UIV32}
 *
 *		TODO: (?) Special case, DF report when sym table id = '/' and symbol code = '\'.
 *
 * 		=4903.50N/07201.75W\088/036/270/729
 *
 *------------------------------------------------------------------*/

func aprs_ll_pos(A *decode_aprs_t, info []byte) {

	type aprs_ll_pos_s struct {
		DTI byte /* ! or = */
		Pos position_t
	}
	var p aprs_ll_pos_s

	type aprs_compressed_pos_s struct {
		DTI  byte /* ! or = */
		CPos compressed_position_t
	}
	var q aprs_compressed_pos_s

	C.strcpy(&A.g_data_type_desc[0], C.CString("Position"))

	var ll_bytes, _ = binary.Decode(info, binary.NativeEndian, &p)
	var compressed_bytes, _ = binary.Decode(info, binary.NativeEndian, &q)

	if unicode.IsDigit(rune(p.Pos.Lat[0])) { /* Human-readable location. */
		decode_position(A, &(p.Pos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* In this case, we expect 7 byte "data extension" */
			/* for the wind direction and speed. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report"))
			weather_data(A, info[ll_bytes:], true)
			/*
			   Here is an interesting case.
			   The protocol spec states that a position report with symbol _ is a special case
			   and the information part must contain wxnow.txt format weather data.
			   But, here we see it being generated like a normal position report.

			   N8VIM>BEACON,AB1OC-10*,WIDE2-1:!4240.85N/07133.99W_PHG72604/ Pepperell, MA. WX. 442.9+ PL100<0x0d>
			   Didn't find wind direction in form c999.
			   Didn't find wind speed in form s999.
			   Didn't find wind gust in form g999.
			   Didn't find temperature in form t999.
			   Weather Report, WEATHER Station (blue)
			   N 42 40.8500, W 071 33.9900
			   , "PHG72604/ Pepperell, MA. WX. 442.9+ PL100"

			   It seems, to me, that this is a violation of the protocol spec.
			   Then, immediately following, we have a positionless weather report in Ultimeter format.

			   N8VIM>APN391,AB1OC-10*,WIDE2-1:$ULTW006F00CA01421C52275800008A00000102FA000F04A6000B002A<0x0d><0x0a>
			   Ultimeter, Kantronics KPC-3 rom versions
			   wind 6.9 mph, direction 284, temperature 32.2, barometer 29.75, humidity 76

			   aprs.fi merges these two together.  Is that anywhere in the protocol spec or
			   just a heuristic added after noticing a pair of packets like this?
			*/

		} else {
			/* Regular position report. */

			data_extension_comment(A, info[ll_bytes:])
		}
	} else { /* Compressed location. */
		decode_compressed_position(A, &(q.CPos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* In this case, the wind direction and speed are in the */
			/* compressed data so we don't expect a 7 byte "data */
			/* extension" for them. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report"))
			weather_data(A, info[compressed_bytes:], false)
		} else {
			/* Regular position report. */

			process_comment(A, info[compressed_bytes:])
		}
	}

}

/*------------------------------------------------------------------
 *
 * Function:	aprs_ll_pos_time
 *
 * Purpose:	Decode "Lat/Long Position Report - with Timestamp"
 *
 *		Reports sent with a timestamp might contain very old information.
 *
 *		Otherwise, same as above.
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_lat, A.g_lon, A.g_symbol_table, A.g_symbol_code, A.g_speed_mph, A.g_course, A.g_altitude_ft.
 *
 * Description:	Type identifier '@' has APRS messaging.
 *		Type identifier '/' does not have APRS messaging.
 *
 *		The location can be in either compressed or human-readable form.
 *
 *		When the symbol code is '_' this is a weather report.
 *
 * Examples:	@041025z4232.32N/07058.81W_124/000g000t036r000p000P000b10229h65/wx rpt
 * 		@281621z4237.55N/07120.20W_017/002g006t022r000p000P000h85b10195.Dvs
 *		/092345z4903.50N/07201.75W>Test1234
 *
 * 		I think the symbol code of "_" indicates weather report.
 *
 *		(?) Special case, DF report when sym table id = '/' and symbol code = '\'.
 *
 *		@092345z4903.50N/07201.75W\088/036/270/729
 *		/092345z4903.50N/07201.75W\000/000/270/729
 *
 *------------------------------------------------------------------*/

func aprs_ll_pos_time(A *decode_aprs_t, info []byte) {

	type aprs_ll_pos_time_s struct {
		DTI       byte /* / or @ */
		Timestamp [7]byte
		Pos       position_t
	}
	var p aprs_ll_pos_time_s

	type aprs_compressed_pos_time_s struct {
		DTI       byte /* / or @ */
		Timestamp [7]byte
		CPos      compressed_position_t
	}
	var q aprs_compressed_pos_time_s

	C.strcpy(&A.g_data_type_desc[0], C.CString("Position with time"))

	var ts time.Time

	var llBytes, _ = binary.Decode(info, binary.NativeEndian, &p)
	var compressedBytes, _ = binary.Decode(info, binary.NativeEndian, &q)

	if unicode.IsDigit(rune(p.Pos.Lat[0])) { /* Human-readable location. */
		ts = get_timestamp(A, p.Timestamp)
		decode_position(A, &(p.Pos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* In this case, we expect 7 byte "data extension" */
			/* for the wind direction and speed. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report"))
			weather_data(A, info[llBytes:], true)
		} else {
			/* Regular position report. */

			data_extension_comment(A, info[llBytes:])
		}
	} else { /* Compressed location. */
		ts = get_timestamp(A, p.Timestamp)

		decode_compressed_position(A, &(q.CPos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* In this case, the wind direction and speed are in the */
			/* compressed data so we don't expect a 7 byte "data */
			/* extension" for them. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report"))
			weather_data(A, info[compressedBytes:], false)
		} else {
			/* Regular position report. */

			process_comment(A, info[compressedBytes:])
		}
	}

	_ = ts // suppress 'set but not used' warning. // TODO KG Why is this not used though??
}

/*------------------------------------------------------------------
 *
 * Function:	aprs_raw_nmea
 *
 * Purpose:	Decode "Raw NMEA Position Report"
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A. ...
 *
 * Description:	APRS recognizes raw ASCII data strings conforming to the NMEA 0183
 *		Version 2.0 specification, originating from navigation equipment such
 *		as GPS and LORAN receivers. It is recommended that APRS stations
 *		interpret at least the following NMEA Received Sentence types:
 *
 *		GGA Global Positioning System Fix Data
 *		GLL Geographic Position, Latitude/Longitude Data
 *		RMC Recommended Minimum Specific GPS/Transit Data
 *		VTG Velocity and Track Data
 *		WPL Way Point Location
 *
 *		We presently recognize only RMC and GGA.
 *
 * Examples:	$GPGGA,102705,5157.9762,N,00029.3256,W,1,04,2.0,75.7,M,47.6,M,,*62
 *		$GPGLL,2554.459,N,08020.187,W,154027.281,A
 *		$GPRMC,063909,A,3349.4302,N,11700.3721,W,43.022,89.3,291099,13.6,E*52
 *		$GPVTG,318.7,T,,M,35.1,N,65.0,K*69
 *
 *------------------------------------------------------------------*/

func aprs_raw_nmea(A *decode_aprs_t, info []byte) {
	if bytes.HasPrefix(info, []byte("$GPRMC,")) ||
		bytes.HasPrefix(info, []byte("$GNRMC,")) {
		var speed_knots C.float = G_UNKNOWN

		dwgpsnmea_gprmc(C.CString(string(info)), A.g_quiet, &(A.g_lat), &(A.g_lon), &speed_knots, &(A.g_course))
		A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(speed_knots)))
		C.strcpy(&A.g_data_type_desc[0], C.CString("Raw GPS data"))
	} else if bytes.HasPrefix(info, []byte("$GPGGA,")) ||
		bytes.HasPrefix(info, []byte("$GNGGA,")) {
		var alt_meters C.float = G_UNKNOWN
		var num_sat C.int = 0

		dwgpsnmea_gpgga(C.CString(string(info)), A.g_quiet, &(A.g_lat), &(A.g_lon), &alt_meters, &num_sat)
		A.g_altitude_ft = C.float(DW_METERS_TO_FEET(float64(alt_meters)))
		C.strcpy(&A.g_data_type_desc[0], C.CString("Raw GPS data"))
	}

	// TODO (low): add a few other sentence types.

} /* end aprs_raw_nmea */

/*------------------------------------------------------------------
 *
 * Function:	aprs_mic_e
 *
 * Purpose:	Decode MIC-E (e.g. Kenwood D7 & D700) packet.
 *		This format is an overzelous quest to make the packet as short as possible.
 *		It uses non-printable characters and hacks wrapped in kludges.
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:
 *
 * Description:
 *
 *		AX.25 Destination Address Field -
 *
 *		The 6-byte Destination Address field contains
 *		the following encoded information:
 *
 *			Byte 1: Lat digit 1, message bit A
 *			Byte 2: Lat digit 2, message bit B
 *			Byte 3: Lat digit 3, message bit C
 *			Byte 4: Lat digit 4, N/S lat indicator
 *			Byte 5: Lat digit 5, Longitude offset
 *			Byte 6: Lat digit 6, W/E Long indicator
 * *
 *		"Although the destination address appears to be quite unconventional, it is
 *		still a valid AX.25 address, consisting only of printable 7-bit ASCII values."
 *
 *		AX.25 Information Field - Starts with ' or `
 *
 *			Bytes 1,2,3: Longitude
 *			Bytes 4,5,6: Speed and Course
 *			Byte 6: Symbol code
 *			Byte 7: Symbol Table ID
 *
 *		The rest of it is a complicated comment field which can hold various information
 *		and must be interpreted in a particular order.  At this point we look for any
 *		prefix and/or suffix to identify the equipment type.
 *
 * 		References:	Mic-E TYPE CODES -- http://www.aprs.org/aprs12/mic-e-types.txt
 *				Mic-E TEST EXAMPLES -- http://www.aprs.org/aprs12/mic-e-examples.txt
 *
 *		Next, we have what Addedum 1.2 calls the "type byte."  This prefix can be
 *			space	Original MIC-E.
 *			>	Kenwood HT.
 *			]	Kenwood Mobile.
 *			none.
 *
 *		We also need to look at the last byte or two
 *		for a possible suffix to distinguish equipment types.  Examples:
 *			>......		is D7
 *			>......=	is D72
 *			>......^	is D74
 *
 *		For other brands, it gets worse.  There might a 2 character suffix.
 *		The prefix indicates whether messaging-capable.  Examples:
 *			`....._.%	Yaesu FTM-400DR
 *			`......_)	Yaesu FTM-100D
 *			`......_3	Yaesu FT5D
 *
 *			'......|3	Byonics TinyTrack3
 *			'......|4	Byonics TinyTrack4
 *
 *		Any prefix and suffix must be removed before further processsing.
 *
 *		Pick one: MIC-E Telemetry Data or "Status Text" (called a comment everywhere else).
 *
 *		If the character after the symbol table id is "," (comma) or 0x1d, we have telemetry.
 *		(Is this obsoleted by the base-91 telemetry?)
 *
 *			`	Two 2-character hexadecimal numbers. (Channels 1 & 3)
 *			'	Five 2-character hexadecimal numbers.
 *
 *		Anything left over is a comment which can contain various types of information.
 *
 *		If present, the MIC-E compressed altitude must be first.
 *		It is three base-91 characters followed by "}".
 *		Examples:    "4T}	"4T}	]"4T}
 *
 *		We can also have frequency specification  --  http://www.aprs.org/info/freqspec.txt
 *
 * Warning:	Some Kenwood radios add CR at the end, in apparent violation of the spec.
 *		Watch out so it doesn't get included when looking for equipment type suffix.
 *
 *		Mic-E TEST EXAMPLES -- http://www.aprs.org/aprs12/mic-e-examples.txt
 *
 * Examples:	Observed on the air.
 *
 *		KB1HNZ-9>TSSP5P,W1IMD,WIDE1,KQ1L-8,N3LLO-3,WIDE2*:`b6,l}#>/]"48}449.225MHz<0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff><0xff>=<0x0d>
 *
 *		`       b6,    l}#   >/     ]         "48}    449.225MHz   ......    =       <0x0d>
 *		mic-e  long.   cs    sym    prefix    alt.    freq         comment   suffix   must-ignore
 *		                            Kenwood                                  D710
 *---------------
 *
 *		N1JDU-9>ECCU8Y,W1MHL*,WIDE2-1:'cZ<0x7f>l#H>/]Go fly a kite!<0x0d>
 *
 *		'      cZ<0x7f>   l#H     >/     ]         .....                 <0x0d>
 *		mic-e  long.      cs      sym    prefix    comment   no-suffix   must-ignore
 *		                                 Kenwood              D700
 *---------------
 *
 *		KC1HHO-7>T2PX5R,WA1PLE-4,WIDE1*,WIDE2-1:`c_snp(k/`"4B}official relay station NTS_(<0x0d>
 *
 *		`       c_s     np(  k/     `       "4B}      .......   _(       <0x0d>
 *		mic-e  long.    cs   sym    prefix   alt      comment   suffix   must-ignore
 *		                                                         FT2D
 *---------------
 *
 *		N1CMD-12>T3PQ1Y,KA1GJU-3,WIDE1,WA1PLE-4*:`cP#l!Fk/'"7H}|!%&-']|!w`&!|3
 *
 *		`      cP#      l!F   k/     '       "7H}    |!%&-']|         !w`&!   |3
 *		mic-e  long.    cs   sym    prefix   alt     base91telemetry  DAO     suffix
 *		                                                                      TinyTrack3
 *---------------
 *
 *		 W1STJ-3>T2UR4X,WA1PLE-4,WIDE1*,WIDE2-1:`c@&l#.-/`"5,}146.685MHz T100 -060 146.520 Simplex or Voice Alert_%<0x0d>
 *
 *		`      c@&     l#.   -/     `        "5,}    146.685MHz T100 -060     ..............  _%       <0x0d>
 *		mic-e  long.    cs   sym    prefix   alt     frequency-specification     comment     suffix   must-ignore
 *		                                                                                    FTM-400DR
 *---------------
 *
 *
 *
 *
 * TODO:	Destination SSID can contain generic digipeater path.  (?)
 *
 * Bugs:	Doesn't handle ambiguous position.  "space" treated as zero.
 *		Invalid data results in a message but latitude is not set to unknown.
 *
 *------------------------------------------------------------------*/

/* a few test cases

# example from http://www.aprs.org/aprs12/mic-e-examples.txt produces 4 errors.
# TODO:  Analyze all the bits someday and possibly report problem with document.

N0CALL>ABCDEF:'abc123R/text

# Let's use an actual valid location and concentrate on the manufacturers
# as listed in http://www.aprs.org/aprs12/mic-e-types.txt

N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Jeff Mobile_%

N1ZZN-9>T2SP0W:`c_Vm6hk/ "49}Originl Mic-E (leading space)

N1ZZN-9>T2SP0W:`c_Vm6hk/>"49}TH-D7A walkie Talkie
N1ZZN-9>T2SP0W:`c_Vm6hk/>"49}TH-D72 walkie Talkie=
W6GPS>S4PT3R:`p(1oR0K\>TH-D74A^
N1ZZN-9>T2SP0W:`c_Vm6hk/]"49}TM-D700 MObile Radio
N1ZZN-9>T2SP0W:`c_Vm6hk/]"49}TM-D710 Mobile Radio=

# Note: next line has trailing space character after _

N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Yaesu VX-8_
N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Yaesu FTM-350_"
N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Yaesu VX-8G_#
N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Yaesu FT1D_$
N1ZZN-9>T2SP0W:`c_Vm6hk/`"49}Yaesu FTM-400DR_%

N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}Byonics TinyTrack3|3
N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}Byonics TinyTrack4|4

# The next group starts with metacharacter "T" which can be any of space > ] ` '
# But space is for original Mic-E, # > and ] are for Kenwood,
# so ` ' would probably be less ambiguous choices but any appear to be valid.

N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}Hamhud\9
N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}Argent/9
N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}HinzTec anyfrog^9
N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}APOZxx www.KissOZ.dk Tracker. OZ1EKD and OZ7HVO*9
N1ZZN-9>T2SP0W:'c_Vm6hk/`"49}OTHER~9


# TODO:  Why is manufacturer unknown?  Should we explicitly say unknown?

[0] VE2VL-9>TU3V0P,VE2PCQ-3,WIDE1,W1UWS-1,UNCAN,WIDE2*:`eB?l")v/"3y}
MIC-E, VAN, En Route

[0] VE2VL-9>TU3U5Q,VE2PCQ-3,WIDE1,W1UWS-1,N1NCI-3,WIDE2*:`eBgl"$v/"42}73 de Julien, Tinytrak 3
MIC-E, VAN, En Route

[0] W1ERB-9>T1SW8P,KB1AEV-15,N1NCI-3,WIDE2*:`dI8l!#j/"3m}
MIC-E, JEEP, In Service

[0] W1ERB-9>T1SW8Q,KB1AEV-15,N1NCI-3,WIDE2*:`dI6l{^j/"4+}IntheJeep..try146.79(PVRA)
"146.79" in comment looks like a frequency in non-standard format.
For most systems to recognize it, use exactly this form "146.790MHz" at beginning of comment.
MIC-E, JEEP, In Service

*/

func mic_e_digit(A *decode_aprs_t, c C.char, mask int, std_msg *int, cust_msg *int) int {

	if c >= '0' && c <= '9' {
		return int(c - '0')
	}

	if c >= 'A' && c <= 'J' {
		*cust_msg |= mask
		return int(c - 'A')
	}

	if c >= 'P' && c <= 'Y' {
		*std_msg |= mask
		return int(c - 'P')
	}

	/* K, L, Z should be converted to space. */
	/* others are invalid. */
	/* But caller expects only values 0 - 9. */

	if c == 'K' {
		*cust_msg |= mask
		return (0)
	}

	if c == 'L' {
		return (0)
	}

	if c == 'Z' {
		*std_msg |= mask
		return (0)
	}

	if A.g_quiet == 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid character \"%c\" in MIC-E destination/latitude.\n", c)
	}

	return (0)
}

func aprs_mic_e(A *decode_aprs_t, pp *packet_t, info []byte) {
	type aprs_mic_e_s struct {
		DTI         byte    /* ' or ` */
		Lon         [3]byte /* "d+28", "m+28", "h+28" */
		SpeedCourse [3]byte
		SymbolCode  byte
		SymTableId  byte
	}

	C.strcpy(&A.g_data_type_desc[0], C.CString("MIC-E"))

	var sizeof_struct_aprs_mic_e_s = 9
	if len(info) < sizeof_struct_aprs_mic_e_s {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("MIC-E format must have at least %d characters in the information part.\n", sizeof_struct_aprs_mic_e_s)
		}
		return
	}

	var p aprs_mic_e_s
	binary.Decode(info, binary.NativeEndian, &p)

	/* Destination is really latitude of form ddmmhh. */
	/* Message codes are buried in the first 3 digits. */

	var dest [12]C.char
	ax25_get_addr_with_ssid(pp, AX25_DESTINATION, &dest[0])

	var std_msg = 0
	var cust_msg = 0
	A.g_lat = C.double(mic_e_digit(A, dest[0], 4, &std_msg, &cust_msg)*10+
		mic_e_digit(A, dest[1], 2, &std_msg, &cust_msg)) +
		C.double(mic_e_digit(A, dest[2], 1, &std_msg, &cust_msg)*1000+
			mic_e_digit(A, dest[3], 0, &std_msg, &cust_msg)*100+
			mic_e_digit(A, dest[4], 0, &std_msg, &cust_msg)*10+
			mic_e_digit(A, dest[5], 0, &std_msg, &cust_msg))/6000.0

	/* 4th character of destination indicates north / south. */

	if (dest[3] >= '0' && dest[3] <= '9') || dest[3] == 'L' {
		/* South */
		A.g_lat = (-A.g_lat)
	} else if dest[3] >= 'P' && dest[3] <= 'Z' {
		/* North */
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid MIC-E N/S encoding in 4th character of destination.\n")
		}
	}

	/* Longitude is mostly packed into 3 bytes of message but */
	/* has a couple bits of information in the destination. */

	var offset bool
	if (dest[4] >= '0' && dest[4] <= '9') || dest[4] == 'L' {
		offset = false
	} else if dest[4] >= 'P' && dest[4] <= 'Z' {
		offset = true
	} else {
		offset = false
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid MIC-E Longitude Offset in 5th character of destination.\n")
		}
	}

	/* First character of information field is longitude in degrees. */
	/* It is possible for the unprintable DEL character to occur here. */

	/* 5th character of destination indicates longitude offset of +100. */
	/* Not quite that simple :-( */

	var ch = p.Lon[0]

	if offset && ch >= 118 && ch <= 127 {
		A.g_lon = C.double(ch - 118) /* 0 - 9 degrees */
	} else if !offset && ch >= 38 && ch <= 127 {
		A.g_lon = C.double(ch-38) + 10 /* 10 - 99 degrees */
	} else if offset && ch >= 108 && ch <= 117 {
		A.g_lon = C.double(ch-108) + 100 /* 100 - 109 degrees */
	} else if offset && ch >= 38 && ch <= 107 {
		A.g_lon = C.double(ch-38) + 110 /* 110 - 179 degrees */
	} else {
		A.g_lon = G_UNKNOWN
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character 0x%02x for MIC-E Longitude Degrees.\n", ch)
		}
	}

	/* Second character of information field is A.g_longitude minutes. */
	/* These are all printable characters. */

	/*
	 * More than once I've see the TH-D72A put <0x1a> here and flip between north and south.
	 *
	 * WB2OSZ>TRSW1R,WIDE1-1,WIDE2-2:`c0ol!O[/>=<0x0d>
	 * N 42 37.1200, W 071 20.8300, 0 MPH, course 151
	 *
	 * WB2OSZ>TRS7QR,WIDE1-1,WIDE2-2:`v<0x1a>n<0x1c>"P[/>=<0x0d>
	 * Invalid character 0x1a for MIC-E Longitude Minutes.
	 * S 42 37.1200, Invalid Longitude, 0 MPH, course 252
	 *
	 * This was direct over the air with no opportunity for a digipeater
	 * or anything else to corrupt the message.
	 */

	if A.g_lon != G_UNKNOWN {
		ch = p.Lon[1]

		if ch >= 88 && ch <= 97 {
			A.g_lon += C.double(ch-88) / 60.0 /* 0 - 9 minutes*/
		} else if ch >= 38 && ch <= 87 {
			A.g_lon += C.double((ch-38)+10) / 60.0 /* 10 - 59 minutes */
		} else {
			A.g_lon = G_UNKNOWN
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid character 0x%02x for MIC-E Longitude Minutes.\n", ch)
			}
		}

		/* Third character of information field is longitude hundredths of minutes. */
		/* There are 100 possible values, from 0 to 99. */
		/* Note that the range includes 4 unprintable control characters and DEL. */

		if A.g_lon != G_UNKNOWN {
			ch = p.Lon[2]

			if ch >= 28 && ch <= 127 {
				A.g_lon += C.double((ch-28)+0) / 6000.0 /* 0 - 99 hundredths of minutes*/
			} else {
				A.g_lon = G_UNKNOWN
				if A.g_quiet == 0 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Invalid character 0x%02x for MIC-E Longitude hundredths of Minutes.\n", ch)
				}
			}
		}
	}

	/* 6th character of destination indicates east / west. */

	/*
	 * Example of apparently invalid encoding.  6th character missing.
	 *
	 * [0] KB1HOZ-9>TTRW5,KQ1L-2,WIDE1,KQ1L-8,UNCAN,WIDE2*:`aFo"]|k/]"4m}<0x0d>
	 * Invalid character "Invalid MIC-E E/W encoding in 6th character of destination.
	 * MIC-E, truck, Kenwood TM-D700, Off Duty
	 * N 44 27.5000, E 069 42.8300, 76 MPH, course 196, alt 282 ft
	 */

	if (dest[5] >= '0' && dest[5] <= '9') || dest[5] == 'L' {
		/* East */
	} else if dest[5] >= 'P' && dest[5] <= 'Z' {
		/* West */
		if A.g_lon != G_UNKNOWN {
			A.g_lon = (-A.g_lon)
		}
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid MIC-E E/W encoding in 6th character of destination.\n")
		}
	}

	/* Symbol table and codes like everyone else. */

	A.g_symbol_table = C.char(p.SymTableId)
	A.g_symbol_code = C.char(p.SymbolCode)

	if A.g_symbol_table != '/' && A.g_symbol_table != '\\' && !unicode.IsUpper(rune(A.g_symbol_table)) && !unicode.IsDigit(rune(A.g_symbol_table)) {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid symbol table code not one of / \\ A-Z 0-9\n")
		}
		A.g_symbol_table = '/'
	}

	/* Message type from two 3-bit codes. */

	var std_text = []string{"Emergency", "Priority", "Special", "Committed", "Returning", "In Service", "En Route", "Off Duty"}
	var cust_text = []string{"Emergency", "Custom-6", "Custom-5", "Custom-4", "Custom-3", "Custom-2", "Custom-1", "Custom-0"}

	if std_msg == 0 && cust_msg == 0 {
		C.strcpy(&A.g_mic_e_status[0], C.CString("Emergency"))
	} else if std_msg == 0 && cust_msg != 0 {
		C.strcpy(&A.g_mic_e_status[0], C.CString(cust_text[cust_msg]))
	} else if std_msg != 0 && cust_msg == 0 {
		C.strcpy(&A.g_mic_e_status[0], C.CString(std_text[std_msg]))
	} else {
		C.strcpy(&A.g_mic_e_status[0], C.CString("Unknown MIC-E Message Type"))
	}

	/* Speed and course from next 3 bytes. */

	var n = int((p.SpeedCourse[0]-28)*10) + int((p.SpeedCourse[1]-28)/10)
	if n >= 800 {
		n -= 800
	}

	A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(n)))

	n = int((p.SpeedCourse[1]-28)%10)*100 + int(p.SpeedCourse[2]-28)
	if n >= 400 {
		n -= 400
	}

	/* Result is 0 for unknown and 1 - 360 where 360 is north. */
	/* Convert to 0 - 360 and reserved value for unknown. */

	switch n {
	case 0:
		A.g_course = G_UNKNOWN
	case 360:
		A.g_course = 0
	default:
		A.g_course = C.float(n)
	}

	// The rest is a comment which can have other information cryptically embedded.
	// Remove any trailing CR, which I would argue, violates the protocol spec.
	// It is essential to keep trailing spaces.  e.g. VX-8 device id suffix is "_ "

	if len(info) <= sizeof_struct_aprs_mic_e_s {
		// Too short for a comment.  We are finished.
		C.strcpy(&A.g_mfr[0], C.CString("UNKNOWN vendor/model"))
		return
	}

	var mcomment = info[sizeof_struct_aprs_mic_e_s:]

	Assert(len(mcomment) > 0)

	if mcomment[len(mcomment)-1] == '\r' {
		mcomment = mcomment[:len(mcomment)-1]
		if len(mcomment) == 0 {
			// Nothing left after removing trailing CR.
			C.strcpy(&A.g_mfr[0], C.CString("UNKNOWN vendor/model"))
			return
		}
	}

	/* Now try to pick out manufacturer and other optional items. */
	/* The telemetry field, in the original spec, is no longer used. */

	// Comment with vendor/model removed.
	var trimmed, device = deviceid_decode_mice(string(mcomment))
	C.strcpy(&A.g_mfr[0], C.CString(device))

	// Possible altitude at beginning of remaining comment.
	// Three base 91 characters followed by }

	if len(trimmed) >= 4 &&
		isdigit91(trimmed[0]) &&
		isdigit91(trimmed[1]) &&
		isdigit91(trimmed[2]) &&
		trimmed[3] == '}' {

		A.g_altitude_ft = C.float(DW_METERS_TO_FEET(float64(float64(trimmed[0])-33)*91*91 + (float64(trimmed[1])-33)*91 + (float64(trimmed[2]) - 33) - 10000))

		process_comment(A, []byte(trimmed)[4:])
		return
	}

	process_comment(A, []byte(trimmed))

} // end aprs_mic_e

/*------------------------------------------------------------------
 *
 * Function:	aprs_message
 *
 * Purpose:	Decode "Message Format."
 *		The word message is used loosely all over the place, but it has a very specific meaning here.
 *
 * Inputs:	info 	- Information field.  Be careful not to modify it here!
 *		quiet	- suppress error messages.
 *
 * Outputs:	A.g_data_type_desc		Text description for screen display.
 *
 *		A.g_addressee		To whom is it addressed.
 *					Could be a specific station, alias, bulletin, etc.
 *					For telemetry metadata is is about this station,
 *					not being sent to it.
 *
 *		A.g_message_subtype	Subtype so caller might avoid replicating
 *					all the code to distinguish them.
 *
 *		A.g_message_number	Message number if any.  Required for ack/rej.
 *
 * Description:	An APRS message is a text string with a specified addressee.
 *
 *		It's a lot more complicated with different types of addressees
 *		and replies with acknowledgement or rejection.
 *
 *		Is it an elegant generalization to lump all of these special cases
 *		together or was it a big mistake that will cause confusion and incorrect
 *		implementations?  The decision to put telemetry metadata here is baffling.
 *
 *
 * Cases:	:BLNxxxxxx: ...			Bulletin.
 *		:NWSxxxxxx: ...			National Weather Service Bulletin.
 *							http://www.aprs.org/APRS-docs/WX.TXT
 *		:SKYxxxxxx: ...			Need reference.
 *		:CWAxxxxxx: ...			Need reference.
 *		:BOMxxxxxx: ...			Australian version.
 *
 *		:xxxxxxxxx:PARM.		Telemetry metadata, parameter name
 *		:xxxxxxxxx:UNIT.		Telemetry metadata, unit/label
 *		:xxxxxxxxx:EQNS.		Telemetry metadata, Equation Coefficients
 *		:xxxxxxxxx:BITS.		Telemetry metadata, Bit Sense/Project Name
 *		:xxxxxxxxx:?			Directed Station Query
 *		:xxxxxxxxx:ackNNNN		Message acknowledged (received)
 *		:xxxxxxxxx:rejNNNNN		Message rejected (unable to accept)
 *
 *		:xxxxxxxxx: ...			Message with no message number.
 *						(Text may not contain the { character because
 *						 it indicates beginning of optional message number.)
 *		:xxxxxxxxx: ... {NNNNN		Message with message number, 1 to 5 alphanumeric.
 *		:xxxxxxxxx: ... {mm}		Message with new style message number.
 *		:xxxxxxxxx: ... {mm}aa		Message with new style message number and ack.
 *
 *
 * Reference:	http://www.aprs.org/txt/messages101.txt
 *		http://www.aprs.org/aprs11/replyacks.txt	<-- New (1999) adding ack to outgoing message.
 *
 *------------------------------------------------------------------*/

func aprs_message(A *decode_aprs_t, info []byte, quiet bool) {

	type aprs_message_s struct {
		DTI       byte /* : */
		Addressee [9]byte
		Colon     byte /* : */
		// No actual message field because it may not be full - pull it out of what's left of info
		/* message   [256 - 1 - 9 - 1]byte */
		/* Officially up to 67 characters for message text. */
		/* Relaxing seemingly arbitrary restriction here; it doesn't need to fit on a punched card. */
		/* Wouldn't surprise me if others did not pay attention to the limit. */
		/* Optional '{' followed by 1-5 alphanumeric characters for message number */

		/* If the first character is '?' it is a Directed Station Query. */
	}

	var p aprs_message_s
	var headerBytes, _ = binary.Decode(info, binary.NativeEndian, &p)
	var message = info[headerBytes:]

	C.strcpy(&A.g_data_type_desc[0], C.CString("APRS Message"))
	A.g_message_subtype = message_subtype_message /* until found otherwise */

	if len(info) < 11 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("APRS Message must have a minimum of 11 characters for : 9 character addressee :\n")
		}
		A.g_message_subtype = message_subtype_invalid
		return
	}

	if p.Colon != ':' {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("APRS Message must begin with ':' 9 character addressee ':'\n")
			dw_printf("Spaces must be added to shorter addressee to make 9 characters.\n")
		}
		A.g_message_subtype = message_subtype_invalid
		return
	}

	var addressee = p.Addressee[0:9] // copy exactly 9 bytes.

	// TODO KG Trim nulls?

	/* Trim trailing spaces. */
	addressee = bytes.TrimRight(addressee, " ")

	// Anytone AT-D878UV 2 plus would pad out station name to 6 characters
	// before appending the SSID.  e.g.  "AE7MK -5 "

	// Test cases.  First is valid.  Others should produce errors:
	//
	// cbeacon sendto=r0  delay=0:10  info=":AE7MK-5  :test0"
	// cbeacon sendto=r0  delay=0:15  info=":AE7MK-5:test1"
	// cbeacon sendto=r0  delay=0:20  info=":AE7MK -5 :test2"
	// cbeacon sendto=r0  delay=0:25  info=":AE7   -5 :test3"

	var bad_addressee_re = regexp.MustCompile("[A-Z0-9]+ +-[0-9]")

	if bad_addressee_re.Match(addressee) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Malformed addressee with space between station name and SSID.\n")
		dw_printf("Please tell message sender this is invalid.\n")
	}

	C.strcpy(&A.g_addressee[0], C.CString(string(addressee)))

	/*
	 * Addressee starting with BLN or NWS is a bulletin.
	 */
	if len(addressee) >= 3 && bytes.HasPrefix(addressee, []byte("BLN")) {

		// Interpret 3 cases of identifiers.
		// BLN9	"general bulletin" has a single digit.
		// BLNX	"announcement" has a single uppercase letter.
		// BLN9xxxxx	"group bulletin" has single digit group id and group name up to 5 characters.

		if len(addressee) == 4 && unicode.IsDigit(rune(addressee[3])) {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("General Bulletin with identifier \"%s\"", addressee[3:])))
		} else if len(addressee) == 4 && unicode.IsUpper(rune(addressee[3])) {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Announcement with identifier \"%s\"", addressee[3:])))
		}
		if len(addressee) >= 5 && unicode.IsDigit(rune(addressee[3])) {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Group Bulletin with identifier \"%c\", group name \"%s\"", addressee[3], addressee[4:])))
		} else {
			// Not one of the official formats.
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Bulletin with identifier \"%s\"", addressee[3:])))
		}
		A.g_message_subtype = message_subtype_bulletin
		C.strcpy(&A.g_comment[0], C.CString(string(message)))
	} else if len(addressee) >= 3 && bytes.HasPrefix(addressee, []byte("NWS")) {

		// Weather bulletins have addressee starting with NWS, SKY, CWA, or BOM.
		// The protocol spec and http://www.aprs.org/APRS-docs/WX.TXT state that
		// the 3 letter prefix must be followed by a dash.
		// However, https://www.aprs-is.net/WX/ also lists the underscore
		// alternative for the compressed format.  Xastir implements this.

		if len(addressee) >= 4 && addressee[3] == '-' {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Weather bulletin with identifier \"%s\"", addressee[4:])))
		} else if len(addressee) >= 4 && addressee[3] == '_' {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Compressed Weather bulletin with identifier \"%s\"", addressee[4:])))
		} else {
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Weather bulletin is missing - or _ after %.3s", addressee)))
		}
		A.g_message_subtype = message_subtype_nws
		C.strcpy(&A.g_comment[0], C.CString(string(message)))
	} else if len(addressee) >= 3 && (bytes.HasPrefix(addressee, []byte("SKY")) || bytes.HasPrefix(addressee, []byte("CWA")) || bytes.HasPrefix(addressee, []byte("BOM"))) {
		// SKY... or CWA...   https://www.aprs-is.net/WX/
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Weather bulletin with identifier \"%s\"", addressee[4:])))
		A.g_message_subtype = message_subtype_nws
		C.strcpy(&A.g_comment[0], C.CString(string(message)))
	} else if bytes.HasPrefix(message, []byte("PARM.")) {

		/*
		 * Special message formats contain telemetry metadata.
		 * It applies to the addressee, not the sender.
		 * Makes no sense to me that it would not apply to sender instead.
		 * Wouldn't the sender be describing his own data?
		 *
		 * I also don't understand the reasoning for putting this in a "message."
		 * Telemetry data always starts with "#" after the "T" data type indicator.
		 * Why not use other characters after the "T" for metadata?
		 */

		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Telemetry Parameter Name for \"%s\"", addressee)))
		A.g_message_subtype = message_subtype_telem_parm
		telemetry_name_message(string(addressee), string(message[5:]))
	} else if bytes.HasPrefix(message, []byte("UNIT.")) {
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Telemetry Unit/Label for \"%s\"", addressee)))
		A.g_message_subtype = message_subtype_telem_unit
		telemetry_unit_label_message(string(addressee), string(message[5:]))
	} else if bytes.HasPrefix(message, []byte("EQNS.")) {
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Telemetry Equation Coefficients for \"%s\"", addressee)))
		A.g_message_subtype = message_subtype_telem_eqns
		telemetry_coefficents_message(string(addressee), string(message[5:]), bool2Cint(quiet))
	} else if bytes.HasPrefix(message, []byte("BITS.")) {
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("Telemetry Bit Sense/Project Name for \"%s\"", addressee)))
		A.g_message_subtype = message_subtype_telem_bits
		telemetry_bit_sense_message(string(addressee), string(message[5:]), bool2Cint(quiet))
	} else if message[0] == '?' {

		/*
		 * If first character of message is "?" it is a query directed toward a specific station.
		 */

		C.strcpy(&A.g_data_type_desc[0], C.CString("Directed Station Query"))
		A.g_message_subtype = message_subtype_directed_query

		aprs_directed_station_query(A, addressee, message[1:], quiet)
	} else if bytes.EqualFold(message[:3], []byte("ack")) {

		/* ack or rej?  Message number is required for these. */

		if !bytes.HasPrefix(message, []byte("ack")) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ERROR: \"%s\" must be lower case \"ack\"\n", message)
		} else {
			C.strcpy(&A.g_message_number[0], C.CString(string(message[3:])))
			if C.strlen(&A.g_message_number[0]) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("ERROR: Message number is missing after \"ack\".\n")
			}
		}

		// Xastir puts a carriage return on the end.
		var p = C.strchr(&A.g_message_number[0], '\r')
		if p != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("The APRS protocol specification says nothing about a possible carriage return after the\n")
			dw_printf("message id.  Adding CR might prevent proper interoperability with with other applications.\n")
			*p = 0
		}

		if C.strlen(&A.g_message_number[0]) >= 3 && A.g_message_number[2] == '}' {
			A.g_message_number[2] = 0
		}
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("\"%s\" ACKnowledged message number \"%s\" from \"%s\"", C.GoString(&A.g_src[0]), C.GoString(&A.g_message_number[0]), addressee)))
		A.g_message_subtype = message_subtype_ack
	} else if bytes.EqualFold(message[:3], []byte("rej")) {
		if !bytes.HasPrefix(message, []byte("rej")) {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("ERROR: \"%s\" must be lower case \"rej\"\n", message)
		} else {
			C.strcpy(&A.g_message_number[0], C.CString(string(message[3:])))
			if C.strlen(&A.g_message_number[0]) == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("ERROR: Message number is missing after \"rej\".\n")
			}
		}

		// Xastir puts a carriage return on the end.
		var p = C.strchr(&A.g_message_number[0], '\r')
		if p != nil {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("The APRS protocol specification says nothing about a possible carriage return after the\n")
			dw_printf("message id.  Adding CR might prevent proper interoperability with with other applications.\n")
			*p = 0
		}

		if C.strlen(&A.g_message_number[0]) >= 3 && A.g_message_number[2] == '}' {
			A.g_message_number[2] = 0
		}
		C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("\"%s\" REJected message number \"%s\" from \"%s\"", C.GoString(&A.g_src[0]), C.GoString(&A.g_message_number[0]), addressee)))
		A.g_message_subtype = message_subtype_ack
	} else {

		// Message to a particular station or a bulletin.
		// message number is optional here.
		// Test cases.  Wrap in third party too.
		// A>B::WA1XYX-15:Howdy y'all
		// A>B::WA1XYX-15:Howdy y'all{12345
		// A>B::WA1XYX-15:Howdy y'all{12}
		// A>B::WA1XYX-15:Howdy y'all{12}34
		// A>B::WA1XYX-15:Howdy y'all{toolong
		// X>Y:}A>B::WA1XYX-15:Howdy y'all
		// X>Y:}A>B::WA1XYX-15:Howdy y'all{12345
		// X>Y:}A>B::WA1XYX-15:Howdy y'all{12}
		// X>Y:}A>B::WA1XYX-15:Howdy y'all{12}34
		// X>Y:}A>B::WA1XYX-15:Howdy y'all{toolong

		// Normal messaage case.  Look for message number.
		var _, after, found = bytes.Cut(message, []byte{'{'})
		if found {
			C.strcpy(&A.g_message_number[0], C.CString(string(after)))

			// Xastir puts a carriage return on the end.
			var p = C.strchr(&A.g_message_number[0], '\r')
			if p != nil {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("The APRS protocol specification says nothing about a possible carriage return after the\n")
				dw_printf("message id.  Adding CR might prevent proper interoperability with with other applications.\n")
				*p = 0
			}

			var mlen = C.strlen(&A.g_message_number[0])
			if mlen < 1 || mlen > 5 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Message number \"%s\" has length outside range of 1 to 5.\n", C.GoString(&A.g_message_number[0]))
			}

			// TODO: Complain if not alphanumeric.

			var ack [8]C.char

			if mlen >= 3 && A.g_message_number[2] == '}' {
				//  New (1999) style.
				A.g_message_number[2] = 0
				C.strcpy(&ack[0], &A.g_message_number[3])
			}

			if C.strlen(&ack[0]) > 0 {
				// With ACK.  Message number should be 2 characters.
				C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("APRS Message, number \"%s\", from \"%s\" to \"%s\", with ACK for \"%s\"", C.GoString(&A.g_message_number[0]), C.GoString(&A.g_src[0]), addressee, C.GoString(&ack[0]))))
			} else {
				// Message number can be 1-5 characters.
				C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("APRS Message, number \"%s\", from \"%s\" to \"%s\"", C.GoString(&A.g_message_number[0]), C.GoString(&A.g_src[0]), addressee)))
			}
		} else {
			// No message number.
			C.strcpy(&A.g_data_type_desc[0], C.CString(fmt.Sprintf("APRS Message, with no number, from \"%s\" to \"%s\"", C.GoString(&A.g_src[0]), addressee)))
		}

		A.g_message_subtype = message_subtype_message

		/* No location so don't use  process_comment () */

		C.strcpy(&A.g_comment[0], C.CString(string(message)))
		// Remove message number when displaying message text.
		var pno = C.strchr(&A.g_comment[0], '{')
		if pno != nil {
			*pno = 0
		}
	}

}

/*------------------------------------------------------------------
 *
 * Function:	aprs_object
 *
 * Purpose:	Decode "Object Report Format"
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_object_name, A.g_lat, A.g_lon, A.g_symbol_table, A.g_symbol_code, A.g_speed_mph, A.g_course, A.g_altitude_ft.
 *
 * Description:	Message has a 9 character object name which could be quite different than
 *		the source station.
 *
 *		This can also be a weather report when the symbol id is '_'.
 *
 * Examples:	;WA2PNU   *050457z4051.72N/07325.53W]BBS & FlexNet 145.070 MHz
 *
 *		;ActonEOC *070352z4229.20N/07125.95WoFire, EMS, Police, Heli-pad, Dial 911
 *
 *		;IRLPC494@*012112zI9*n*<ONV0   446325-146IDLE<CR>
 *
 *------------------------------------------------------------------*/

func aprs_object(A *decode_aprs_t, info []byte) {

	type aprs_object_s struct {
		DTI          byte /* ; */
		Name         [9]byte
		LiveOrKilled byte /* * for live or _ for killed */
		Timestamp    [7]byte
		Pos          position_t
	}
	var p aprs_object_s

	type aprs_compressed_object_s struct {
		DTI          byte /* ; */
		Name         [9]byte
		LiveOrKilled byte /* * for live or _ for killed */
		Timestamp    [7]byte
		CPos         compressed_position_t
	}
	var q aprs_compressed_object_s

	var objectPosBytes, _ = binary.Decode(info, binary.NativeEndian, &p)
	var objectCompressedPosBytes, _ = binary.Decode(info, binary.NativeEndian, &q)

	//Assert (sizeof(A.g_name) > sizeof(p.name));

	C.memcpy(unsafe.Pointer(&A.g_name[0]), C.CBytes(p.Name[:]), C.size_t(len(p.Name))) // copy exactly 9 bytes.

	/* Trim trailing spaces. */
	var i = C.int(C.strlen(&A.g_name[0])) - 1
	for i >= 0 && A.g_name[i] == ' ' {
		A.g_name[i] = 0
		i--
	}

	switch p.LiveOrKilled {
	case '*':
		C.strcpy(&A.g_data_type_desc[0], C.CString("Object"))
	case '_':
		C.strcpy(&A.g_data_type_desc[0], C.CString("Killed Object"))
	default:
		C.strcpy(&A.g_data_type_desc[0], C.CString("Object - invalid live/killed"))
	}

	var ts = get_timestamp(A, p.Timestamp)
	_ = ts // TODO KG Why is ts unused??

	if unicode.IsDigit(rune(p.Pos.Lat[0])) { /* Human-readable location. */
		decode_position(A, &(p.Pos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* In this case, we expect 7 byte "data extension" */
			/* for the wind direction and speed. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report with Object"))
			weather_data(A, info[objectPosBytes:], true)
		} else {
			/* Regular object. */

			data_extension_comment(A, info[objectPosBytes:])
		}
	} else { /* Compressed location. */
		decode_compressed_position(A, &(q.CPos))

		if A.g_symbol_code == '_' {
			/* Symbol code indidates it is a weather report. */
			/* The spec doesn't explicitly mention the combination */
			/* of weather report and object with compressed */
			/* position. */

			C.strcpy(&A.g_data_type_desc[0], C.CString("Weather Report with Object"))
			weather_data(A, info[objectCompressedPosBytes:], false)
		} else {
			/* Regular position report. */

			process_comment(A, info[objectCompressedPosBytes:])
		}
	}
} /* end aprs_object */

/*------------------------------------------------------------------
 *
 * Function:	aprs_item
 *
 * Purpose:	Decode "Item Report Format"
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_object_name, A.g_lat, A.g_lon, A.g_symbol_table, A.g_symbol_code, A.g_speed_mph, A.g_course, A.g_altitude_ft.
 *
 * Description:	An "item" is very much like an "object" except
 *
 *		-- It doesn't have a time.
 *		-- Name is a VARIABLE length 3 to 9 instead of fixed 9.
 *		-- "live" indicator is ! rather than *
 *
 * Examples:
 *
 *------------------------------------------------------------------*/

func aprs_item(A *decode_aprs_t, info []byte) {

	/*
		Structure:

		{
			DTI byte
			Name []byte // Can't decode into this because variable length
			LiveOrKilled byte
			Pos position_t | compressed_position_t
			Comment []byte
		}
	*/

	// Chomp info

	Assert(info[0] == ')')
	info = info[1:] // Drop the DTI ')'

	// Name is variable length, should be 3-9 bytes

	var name []byte
	for {
		var b = info[0]

		if b == '!' || b == '_' { // We've hit the live/killed indicator
			break
		} else {
			name = append(name, b)
			info = info[1:]
		}
	}

	Assert(3 <= len(name) && len(name) <= 9)

	C.strcpy(&A.g_name[0], C.CString(string(name)))

	var liveOrKilled = info[0]
	info = info[1:]

	switch liveOrKilled {
	case '!':
		C.strcpy(&A.g_data_type_desc[0], C.CString("Item"))
	case '_':
		C.strcpy(&A.g_data_type_desc[0], C.CString("Killed Item"))
	default:
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Item name not followed by ! or _.\n")
		}
		C.strcpy(&A.g_data_type_desc[0], C.CString("Object - invalid live/killed"))
	}

	var p position_t
	var q compressed_position_t

	var positionBytes, _ = binary.Decode(info, binary.NativeEndian, &p)
	var compressedPositionBytes, _ = binary.Decode(info, binary.NativeEndian, &q)

	if unicode.IsDigit(rune(p.Lat[0])) { // Human-readable location.
		decode_position(A, &p)

		data_extension_comment(A, info[positionBytes:])
	} else { // Compressed location.
		decode_compressed_position(A, &q)

		process_comment(A, info[compressedPositionBytes:])
	}
}

/*------------------------------------------------------------------
 *
 * Function:	aprs_station_capabilities
 *
 * Purpose:	Decode "Station Capabilities"
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	???
 *
 * Description:	Each capability is a TOKEN or TOKEN=VALUE pair.
 *
 *
 * Example:	<IGATE,MSG_CNT=3,LOC_CNT=49<CR>
 *
 * Bugs:	Not implemented yet.  Treat whole thing as comment.
 *
 *------------------------------------------------------------------*/

func aprs_station_capabilities(A *decode_aprs_t, info []byte) {

	C.strcpy(&A.g_data_type_desc[0], C.CString("Station Capabilities"))

	// 	process_comment() not applicable here because it
	//	extracts information found in certain formats.

	C.strcpy(&A.g_comment[0], C.CString(string(info[1:])))

} /* end aprs_station_capabilities */

/*------------------------------------------------------------------
 *
 * Function:	aprs_status_report
 *
 * Purpose:	Decode "Status Report"
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	???
 *
 * Description:	There are 3 different formats:
 *
 *		(1)	'>'
 *			7 char - timestamp, DHM z format
 *			0-55 char - status text
 *
 *		(3)	'>'
 *			4 or 6 char - Maidenhead Locator
 *			2 char - symbol table & code
 *			' ' character
 *			0-53 char - status text
 *
 *		(2)	'>'
 *			0-62 char - status text
 *
 *
 *		In all cases, Beam heading and ERP can be at the
 *		very end by using '^' and two other characters.
 *
 *
 * Examples from specification:
 *
 *
 *		>Net Control Center without timestamp.
 *		>092345zNet Control Center with timestamp.
 *		>IO91SX/G
 *		>IO91/G
 *		>IO91SX/- My house 		(Note the space at the start of the status text).
 *		>IO91SX/- ^B7 			Meteor Scatter beam heading = 110 degrees, ERP = 490 watts.
 *
 *------------------------------------------------------------------*/

func aprs_status_report(A *decode_aprs_t, info []byte) {
	type aprs_status_time_s struct {
		DTI   byte    /* > */
		ZTime [7]byte /* Time stamp ddhhmmz */
	}
	var pt aprs_status_time_s

	type aprs_status_m4_s struct {
		DTI        byte    /* > */
		Mhead4     [4]byte /* 4 character Maidenhead locator. */
		SymTableId byte
		SymbolCode byte
		Space      byte /* Should be space after symbol code. */
	}
	var pm4 aprs_status_m4_s

	type aprs_status_m6_s struct {
		DTI        byte    /* > */
		Mhead6     [6]byte /* 6 character Maidenhead locator. */
		SymTableId byte
		SymbolCode byte
		Space      byte /* Should be space after symbol code. */
	}
	var pm6 aprs_status_m6_s

	type aprs_status_s struct {
		DTI byte /* > */
	}
	var ps aprs_status_s

	C.strcpy(&A.g_data_type_desc[0], C.CString("Status Report"))

	var ptBytes, _ = binary.Decode(info, binary.NativeEndian, &pt)
	var pm4Bytes, _ = binary.Decode(info, binary.NativeEndian, &pm4)
	var pm6Bytes, _ = binary.Decode(info, binary.NativeEndian, &pm6)
	var psBytes, _ = binary.Decode(info, binary.NativeEndian, &ps)

	/*
	 * Do we have format with time?
	 */
	if unicode.IsDigit(rune(pt.ZTime[0])) &&
		unicode.IsDigit(rune(pt.ZTime[1])) &&
		unicode.IsDigit(rune(pt.ZTime[2])) &&
		unicode.IsDigit(rune(pt.ZTime[3])) &&
		unicode.IsDigit(rune(pt.ZTime[4])) &&
		unicode.IsDigit(rune(pt.ZTime[5])) &&
		pt.ZTime[6] == 'z' {

		// 	process_comment() not applicable here because it
		//	extracts information found in certain formats.

		C.strcpy(&A.g_comment[0], C.CString(string(info[ptBytes:])))
	} else if get_maidenhead(A, pm6.Mhead6[:]) == 6 {

		/*
		 * Do we have format with 6 character Maidenhead locator?
		 */

		C.memset(unsafe.Pointer(&A.g_maidenhead[0]), 0, C.size_t(len(A.g_maidenhead)))
		C.memcpy(unsafe.Pointer(&A.g_maidenhead[0]), C.CBytes(pm6.Mhead6[:]), C.size_t(len(pm6.Mhead6)))

		A.g_symbol_table = C.char(pm6.SymTableId)
		A.g_symbol_code = C.char(pm6.SymbolCode)

		if A.g_symbol_table != '/' && A.g_symbol_table != '\\' && !unicode.IsUpper(rune(A.g_symbol_table)) && !unicode.IsDigit(rune(A.g_symbol_table)) {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid symbol table code '%c' not one of / \\ A-Z 0-9\n", A.g_symbol_table)
			}
			A.g_symbol_table = '/'
		}

		if pm6.Space != ' ' && pm6.Space != 0 {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Error: Found '%c' instead of space required after symbol code.\n", pm6.Space)
			}
		}

		// 	process_comment() not applicable here because it
		//	extracts information found in certain formats.

		C.strcpy(&A.g_comment[0], C.CString(string(info[pm6Bytes:])))
	} else if get_maidenhead(A, pm4.Mhead4[:]) == 4 {

		/*
		 * Do we have format with 4 character Maidenhead locator?
		 */

		C.memset(unsafe.Pointer(&A.g_maidenhead[0]), 0, C.size_t(len(A.g_maidenhead)))
		C.memcpy(unsafe.Pointer(&A.g_maidenhead[0]), C.CBytes(pm4.Mhead4[:]), C.size_t(len(pm4.Mhead4)))

		A.g_symbol_table = C.char(pm4.SymTableId)
		A.g_symbol_code = C.char(pm4.SymbolCode)

		if A.g_symbol_table != '/' && A.g_symbol_table != '\\' && !unicode.IsUpper(rune(A.g_symbol_table)) && !unicode.IsDigit(rune(A.g_symbol_table)) {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid symbol table code '%c' not one of / \\ A-Z 0-9\n", A.g_symbol_table)
			}
			A.g_symbol_table = '/'
		}

		if pm4.Space != ' ' && pm4.Space != 0 {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Error: Found '%c' instead of space required after symbol code.\n", pm4.Space)
			}
		}

		// 	process_comment() not applicable here because it
		//	extracts information found in certain formats.

		C.strcpy(&A.g_comment[0], C.CString(string(info[pm4Bytes:])))
	} else {

		/*
		 * Whole thing is status text.
		 */
		C.strcpy(&A.g_comment[0], C.CString(string(info[psBytes:])))
	}

	/*
	 * Last 3 characters can represent beam heading and ERP.
	 */

	if C.strlen(&A.g_comment[0]) >= 3 {
		var _hp = &A.g_comment[C.strlen(&A.g_comment[0])-3]
		var hp = C.GoString(_hp)

		if hp[0] == '^' {

			var h = hp[1]
			var p = hp[2]
			var beam = -1
			var erp = -1

			if h >= '0' && h <= '9' {
				beam = int(h-'0') * 10
			} else if h >= 'A' && h <= 'Z' {
				beam = int(h-'A')*10 + 100
			}

			if p >= '1' && p <= 'K' {
				erp = int(p-'0') * int(p-'0') * 10
			}

			// TODO (low):  put result somewhere.
			// could use A.g_directivity and need new variable for erp.

			*_hp = 0
			_ = beam
			_ = erp
		}
	}

} /* end aprs_status_report */

/*------------------------------------------------------------------
 *
 * Function:	aprs_general_query
 *
 * Purpose:	Decode "General Query" for all stations.
 *
 * Inputs:	info 	- Information field.  First character should be "?".
 *		quiet	- suppress error messages.
 *
 * Outputs:	A	- Decoded packet structure
 *				A.g_query_type
 *				A.g_query_lat		(optional)
 *				A.g_query_lon		(optional)
 *				A.g_query_radius	(optional)
 *
 * Description:	Formats are:
 *
 *			?query?
 *			?query?lat,long,radius
 *
 *		'query' is one of APRS, IGATE, WX, ...
 *		optional footprint, in degrees and miles radius, means only
 *			those in the specified circle should respond.
 *
 * Examples from specification, Chapter 15:
 *
 *		?APRS?
 *		?APRS? 34.02,-117.15,0200
 *		?IGATE?
 *
 *------------------------------------------------------------------*/

/*
https://groups.io/g/direwolf/topic/95961245#7357

What APRS queries should DireWolf respond to? Well, it should be configurable whether it responds to queries at all, in case some other application is using DireWolf as a dumb TNC (KISS or AGWPE style) and wants to handle the queries itself.

Assuming query responding is enabled, the following broadcast queries should be supported (if the corresponding data is configured in DireWolf):

?APRS (I am an APRS station)
?IGATE (I am operating as a I-gate)
?WX (I am providing local weather data in my beacon)

*/

func aprs_general_query(A *decode_aprs_t, info []byte, quiet C.int) {

	C.strcpy(&A.g_data_type_desc[0], C.CString("General Query"))

	/*
	 * There should be another "?" after the query type.
	 */
	var before, after, found = bytes.Cut(info[1:], []byte{'?'})
	if !found {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("General Query must have ? after the query type.\n")
		}
		return
	}

	C.strcpy(&A.g_query_type[0], C.CString(string(before)))

	// TODO: remove debug

	text_color_set(DW_COLOR_DEBUG)
	dw_printf("DEBUG: General Query type = \"%s\"\n", C.GoString(&A.g_query_type[0]))

	if len(after) == 0 {
		return
	}

	/*
	 * Try to extract footprint.
	 * Spec says positive coordinate would be preceded by space
	 * and radius must be exactly 4 digits.  We are more forgiving.
	 */
	after = bytes.TrimSpace(after)
	var parts = bytes.Split(after, []byte{','})
	if len(parts) != 3 {
		var lat, latErr = strconv.ParseFloat(string(parts[0]), 64)

		if latErr != nil || lat < -90 || lat > 90 {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid latitude for General Query footprint.\n")
			}
			return
		}

		var lon, lonErr = strconv.ParseFloat(string(parts[1]), 64)

		if lonErr != nil || lon < -180 || lon > 180 {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid longitude for General Query footprint.\n")
			}
			return
		}

		var radius, radiusErr = strconv.ParseFloat(string(parts[1]), 64)

		if radiusErr != nil || radius <= 0 || radius > 9999 {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Invalid radius for General Query footprint.\n")
			}
			return
		}
		// TODO: remove debug

		text_color_set(DW_COLOR_DEBUG)
		dw_printf("DEBUG: General Query footprint = %.6f %.6f %.2f\n", lat, lon, radius)

		A.g_footprint_lat = C.double(lat)
		A.g_footprint_lon = C.double(lon)
		A.g_footprint_radius = C.float(radius)
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't parse latitude,longitude,radius for General Query footprint.\n")
		}
		return
	}

} /* end aprs_general_query */

/*------------------------------------------------------------------
 *
 * Function:	aprs_directed_station_query
 *
 * Purpose:	Decode "Directed Station Query" aimed at specific station.
 *		This is actually a special format of the more general "message."
 *
 * Inputs:	addressee	- To whom it is directed.
 *				  Redundant because it is already in A.addressee.
 *
 *		query	 	- What's left over after ":addressee:?" in info part.
 *
 *		quiet		- suppress error messages.
 *
 * Outputs:	A	- Decoded packet structure
 *				A.g_query_type
 *				A.g_query_callsign	(optional)
 *
 * Description:	The caller has already removed the :addressee:? part so we are left
 *		with a query type of exactly 5 characters and optional "callsign
 *		of heard station."
 *
 * Examples from specification, Chapter 15.   Our "query" argument.
 *
 *		:KH2Z     :?APRSD		APRSD
 *		:KH2Z     :?APRSHVN0QBF     	APRSHVN0QBF
 *		:KH2Z     :?APRST		APRST
 *		:KH2Z     :?PING?		PING?
 *
 *		"PING?" contains "?" only to pad it out to exactly 5 characters.
 *
 *------------------------------------------------------------------*/

/*
https://groups.io/g/direwolf/topic/95961245#7357

The following directed queries (sent as bodies of APRS text messages) would also be useful (if corresponding data configured):

?APRSP (force my current beacon)
?APRST and ?PING (trace my path to requestor)
?APRSD (all stations directly heard [no digipeat hops] by local station)
?APRSO (any Objects/Items originated by this station)
?APRSH (how often or how many times the specified 3rd station was heard by the queried station)
?APRSS (immediately send the Status message if configured) (can DireWolf do Status messages?)

Lynn KJ4ERJ and I have implemented a non-standard query which might be useful:

?VER (send the human-readable software version of the queried station)

Hope this is useful. It's just my $.02.

Andrew, KA2DDO
author of YAAC
*/

func aprs_directed_station_query(A *decode_aprs_t, addressee []byte, query []byte, quiet bool) {
	//char query_type[20];		/* Does the query type always need to be exactly 5 characters? */
	/* If not, how would we know where the extra optional information starts? */

	//char callsign[AX25_MAX_ADDR_LEN];

	//if (strlen(query) < 5) ...

} /* end aprs_directed_station_query */

/*------------------------------------------------------------------
 *
 * Function:	aprs_Telemetry
 *
 * Purpose:	Decode "Telemetry"
 *
 * Inputs:	info 	- Information field.
 *		quiet	- suppress error messages.
 *
 * Outputs:	A.g_telemetry
 *		A.g_comment
 *
 * Description:	TBD.
 *
 * Examples from specification:
 *
 *
 *		TBD
 *
 *------------------------------------------------------------------*/

func aprs_telemetry(A *decode_aprs_t, info []byte, quiet C.int) {

	C.strcpy(&A.g_data_type_desc[0], C.CString("Telemetry"))

	telemetry_data_original(C.GoString(&A.g_src[0]), string(info), quiet, &A.g_telemetry[0], C.size_t(len(A.g_telemetry)), &A.g_comment[0], C.size_t(len(A.g_comment)))

} /* end aprs_telemetry */

/*------------------------------------------------------------------
 *
 * Function:	aprs_user_defined
 *
 * Purpose:	Decode user defined data.
 *
 * Inputs:	info 	- Information field.
 *
 * Description:	APRS Protocol Specification, Chapter 18
 *		User IDs allocated here:  http://www.aprs.org/aprs11/expfmts.txt
 *
 *------------------------------------------------------------------*/

func aprs_user_defined(A *decode_aprs_t, info []byte) {
	if bytes.HasPrefix(info, []byte("{tt")) || // Historical.
		bytes.HasPrefix(info, []byte("{DT")) { // Official after registering {D*
		aprs_raw_touch_tone(A, info)
	} else if bytes.HasPrefix(info, []byte("{mc")) || // Historical.
		bytes.HasPrefix(info, []byte("{DM")) { // Official after registering {D*
		aprs_morse_code(A, info)
	} else if info[0] == '{' && info[1] == USER_DEF_USER_ID && info[2] == USER_DEF_TYPE_AIS {
		var lat, lon C.double
		var knots, course, alt_meters C.float

		ais_parse(C.CString(string(info[3:])), 0, &A.g_data_type_desc[0], C.int(len(A.g_data_type_desc)), &A.g_name[0], C.int(len(A.g_name)),
			&lat, &lon, &knots, &course, &alt_meters, &(A.g_symbol_table), &(A.g_symbol_code),
			&A.g_comment[0], C.int(len(A.g_comment)))

		A.g_lat = lat
		A.g_lon = lon
		A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(knots)))
		A.g_course = course
		A.g_altitude_ft = C.float(DW_METERS_TO_FEET(float64(alt_meters)))
		C.strcpy(&A.g_mfr[0], C.CString(""))
	} else if bytes.HasPrefix(info, []byte("{{")) {
		C.strcpy(&A.g_data_type_desc[0], C.CString("User-Defined Experimental"))
	} else {
		C.strcpy(&A.g_data_type_desc[0], C.CString("User-Defined Data"))
	}

} /* end aprs_user_defined */

/*------------------------------------------------------------------
 *
 * Function:	aprs_raw_touch_tone
 *
 * Purpose:	Decode raw touch tone datA.
 *
 * Inputs:	info 	- Information field.
 *
 * Description:	Touch tone data is converted to a packet format
 *		so it can be conveyed to an application for processing.
 *
 * 		This is not part of the APRS standard.
 *
 *------------------------------------------------------------------*/

func aprs_raw_touch_tone(A *decode_aprs_t, info []byte) {

	C.strcpy(&A.g_data_type_desc[0], C.CString("Raw Touch Tone Data"))

	/* Just copy the info field without the message type. */

	if info[0] == '{' {
		C.strcpy(&A.g_comment[0], C.CString(string(info[3:])))
	} else {
		C.strcpy(&A.g_comment[0], C.CString(string(info[1:])))
	}

} /* end aprs_raw_touch_tone */

/*------------------------------------------------------------------
 *
 * Function:	aprs_morse_code
 *
 * Purpose:	Convey message in packet format to be transmitted as
 *		Morse Code.
 *
 * Inputs:	info 	- Information field.
 *
 * Description:	This is not part of the APRS standard.
 *
 *------------------------------------------------------------------*/

func aprs_morse_code(A *decode_aprs_t, info []byte) {

	C.strcpy(&A.g_data_type_desc[0], C.CString("Morse Code Data"))

	/* Just copy the info field without the message type. */

	if info[0] == '{' {
		C.strcpy(&A.g_comment[0], C.CString(string(info[3:])))
	} else {
		C.strcpy(&A.g_comment[0], C.CString(string(info[1:])))
	}

} /* end aprs_morse_code */

/*------------------------------------------------------------------
 *
 * Function:	aprs_ll_pos_time
 *
 * Purpose:	Decode weather report without a position.
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_symbol_table, A.g_symbol_code.
 *
 * Description:	Type identifier '_' is a weather report without a position.
 *
 *------------------------------------------------------------------*/

func aprs_positionless_weather_report(A *decode_aprs_t, info []byte) {

	type aprs_positionless_weather_s struct {
		dti        byte    /* _ */
		time_stamp [8]byte /* MDHM format */
		comment    [99]byte
	}
	var p aprs_positionless_weather_s

	C.strcpy(&A.g_data_type_desc[0], C.CString("Positionless Weather Report"))

	//time_t ts = 0;

	binary.Decode(info, binary.NativeEndian, &p)

	// not yet implemented for 8 character format // ts = get_timestamp (A, p.time_stamp);

	weather_data(A, p.comment[:], false)
}

/*------------------------------------------------------------------
 *
 * Function:	weather_data
 *
 * Purpose:	Decode weather data in position or object report.
 *
 * Inputs:	info 	- Pointer to first byte after location
 *			  and symbol code.
 *
 *		wind_prefix 	- Expecting leading wind info
 *				  for human-readable location.
 *				  (Currently ignored.  We are very
 *				  forgiving in what is accepted.)
 * TODO: call this context instead and have 3 enumerated values.
 *
 * Global In:	A.g_course	- Wind info for compressed location.
 *		A.g_speed_mph
 *
 * Outputs:	A.g_weather
 *
 * Description:	Extract weather details and format into a comment.
 *
 *		For human-readable locations, we expect wind direction
 *		and speed in a format like this:  999/999.
 *		For compressed location, this has already been
 * 		processed and put in A.g_course and A.g_speed_mph.
 *		Otherwise, for positionless weather data, the
 *		wind is in the form c999s999.
 *
 * References:	APRS Weather specification comments.
 *		http://aprs.org/aprs11/spec-wx.txt
 *
 *		Weather updates to the spec.
 *		http://aprs.org/aprs12/weather-new.txt
 *
 * Examples:
 *
 *	_10090556c220s004g005t077r000p000P000h50b09900wRSW
 *	!4903.50N/07201.75W_220/004g005t077r000p000P000h50b09900wRSW
 *	!4903.50N/07201.75W_220/004g005t077r000p000P000h50b.....wRSW
 *	@092345z4903.50N/07201.75W_220/004g005t-07r000p000P000h50b09900wRSW
 *	=/5L!!<*e7_7P[g005t077r000p000P000h50b09900wRSW
 *	@092345z/5L!!<*e7_7P[g005t077r000p000P000h50b09900wRSW
 *	;BRENDA   *092345z4903.50N/07201.75W_220/004g005b0990
 *
 *------------------------------------------------------------------*/

func getwdata(wpp []byte, id C.char, dlen C.int) (C.float, []byte, bool) {

	Assert(dlen >= 2 && dlen <= 6)

	if C.char(wpp[0]) != id {
		return G_UNKNOWN, wpp, false
	}

	var field = wpp[1 : dlen+1]

	// All spaces or dots means unknown value
	if bytes.Count(field, []byte{'.'}) == len(field) || bytes.Count(field, []byte{' '}) == len(field) {
		return G_UNKNOWN, wpp[dlen+1:], true
	}

	var f, floatErr = strconv.ParseFloat(string(field), 64)

	if floatErr != nil {
		return G_UNKNOWN, wpp, false
	}

	return C.float(f), wpp[dlen+1:], true
}

func weather_data(A *decode_aprs_t, wdata []byte, wind_prefix bool) {

	var wp = wdata
	var found bool

	if wp[3] == '/' {
		var n int
		var count, _ = fmt.Sscanf(string(wp[:3]), "%3d", &n) // TODO KG I *think* this works right but I'd be lying if I said I trusted it... TODO Test better
		if count > 0 {
			// Data Extension format.
			// Fine point:  Officially, should be values of 001-360.
			// "000" or "..." or "   " means unknown.
			// In practice we see do see "000" here.
			A.g_course = C.float(n)
		}
		count, _ = fmt.Sscanf(string(wp[4:7]), "%3d", &n)
		if count > 0 {
			A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(n))) /* yes, in knots */
		}
		wp = wp[7:]
	} else if A.g_speed_mph == G_UNKNOWN {
		A.g_course, wp, found = getwdata(wp, 'c', 3)
		if !found {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Didn't find wind direction in form c999.\n")
			}
		}
		A.g_speed_mph, wp, found = getwdata(wp, 's', 3) /* MPH here */
		if !found {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Didn't find wind speed in form s999.\n")
			}
		}
	}

	// At this point, we should have the wind direction and speed
	// from one of three methods.

	if A.g_speed_mph != G_UNKNOWN {
		C.strcpy(&A.g_weather[0], C.CString(fmt.Sprintf("wind %.1f mph", A.g_speed_mph)))
		if A.g_course != G_UNKNOWN {
			C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", direction %.0f", A.g_course)))
		}
	}

	/* We don't want this to show up on the location line. */
	A.g_speed_mph = G_UNKNOWN
	A.g_course = G_UNKNOWN

	/*
	 * After the mandatory wind direction and speed (in 1 of 3 formats), the
	 * next two must be in fixed positions:
	 * - gust (peak in mph last 5 minutes)
	 * - temperature, degrees F, can be negative e.g. -01
	 */
	var fval C.float

	fval, wp, found = getwdata(wp, 'g', 3)
	if found {
		if fval != G_UNKNOWN {
			C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", gust %.0f", fval)))
		}
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Didn't find wind gust in form g999.\n")
		}
	}

	fval, wp, found = getwdata(wp, 't', 3)
	if found {
		if fval != G_UNKNOWN {
			C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", temperature %.0f", fval)))
		}
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Didn't find temperature in form t999.\n")
		}
	}

	/*
	 * Now pick out other optional fields in any order.
	 */
	for {
		// TODO KG Rebuild this by peeking at wp[0]

		fval, wp, found = getwdata(wp, 'r', 3)
		if found {
			/* r = rainfall, 1/100 inch, last hour */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", rain %.2f in last hour", fval/100.)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'p', 3)
		if found {
			/* p = rainfall, 1/100 inch, last 24 hours */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", rain %.2f in last 24 hours", fval/100.)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'P', 3)
		if found {
			/* P = rainfall, 1/100 inch, since midnight */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", rain %.2f since midnight", fval/100.)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'h', 2)
		if found {
			/* h = humidity %, 00 means 100%  */

			if fval != G_UNKNOWN {
				if fval == 0 {
					fval = 100
				}
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", humidity %.0f", fval)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'b', 5)
		if found {
			/* b = barometric presure (tenths millibars / tenths of hPascal)  */
			/* Here, display as inches of mercury. */

			if fval != G_UNKNOWN {
				fval = C.float(DW_MBAR_TO_INHG(float64(fval) * 0.1))
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", barometer %.2f", fval)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'L', 3)
		if found {
			/* L = Luminosity, watts/ sq meter, 000-999  */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", %.0f watts/m^2", fval)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'l', 3)
		if found {
			/* l = Luminosity, watts/ sq meter, 1000-1999  */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", %.0f watts/m^2", fval+1000)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 's', 3)
		if found {

			/* s = Snowfall in last 24 hours, inches  */
			/* Data can have decimal point so we don't have to worry about scaling. */
			/* 's' is also used by wind speed but that must be in a fixed */
			/* position in the message so there is no confusion. */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", %.1f snow in 24 hours", fval)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 's', 3)
		if found {
			/* # = Raw rain counter  */

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", raw rain counter %.f", fval)))
			}
			continue
		}

		fval, wp, found = getwdata(wp, 'X', 3)
		if found {
			/* X = Nuclear Radiation.  */
			/* Encoded as two significant digits and order of magnitude */
			/* like resistor color code. */

			// TODO: decode this properly

			if fval != G_UNKNOWN {
				C.strcat(&A.g_weather[0], C.CString(fmt.Sprintf(", nuclear Radiation %.f", fval)))
			}
			continue
		}

		// TODO: add new flood level, battery voltage, etc.
		break
	}

	/*
	 * We should be left over with:
	 * - one character for software.
	 * - two to four characters for weather station type.
	 * Examples: tU2k, wRSW
	 *
	 * But few people follow the protocol spec here.  Instead more often we see things like:
	 *  sunny/WX
	 *  / {UIV32N}
	 */

	C.strcat(&A.g_weather[0], C.CString(", \""))
	C.strcat(&A.g_weather[0], C.CString(string(wp)))
	/*
	 * Drop any CR / LF character at the end.
	 */
	var n = C.strlen(&A.g_weather[0])
	if n >= 1 && A.g_weather[n-1] == '\n' {
		A.g_weather[n-1] = 0
	}

	n = C.strlen(&A.g_weather[0])
	if n >= 1 && A.g_weather[n-1] == '\r' {
		A.g_weather[n-1] = 0
	}

	C.strcat(&A.g_weather[0], C.CString("\""))
} /* end weather_data */

/*------------------------------------------------------------------
 *
 * Function:	aprs_ultimeter
 *
 * Purpose:	Decode Peet Brothers ULTIMETER Weather Station Info.
 *
 * Inputs:	info 	- Information field.
 *
 * Outputs:	A.g_weather
 *
 * Description:	http://www.peetbros.com/shop/custom.aspx?recid=7
 *
 * 		There are two different data formats in use.
 *		One begins with $ULTW and is called "Packet Mode."  Example:
 *
 *		$ULTW009400DC00E21B8027730008890200010309001E02100000004C<CR><LF>
 *
 *		The other begins with !! and is called "logging mode."  Example:
 *
 *		!!000000A600B50000----------------001C01D500000017<CR><LF>
 *
 *
 * Bugs:	Implementation is incomplete.
 *		The example shown in the APRS protocol spec has a couple "----"
 *		fields in the $ULTW message.  This should be rewritten to handle
 *		each field separately to deal with missing pieces.
 *
 *------------------------------------------------------------------*/

func aprs_ultimeter(A *decode_aprs_t, info []byte) {

	// Header = $ULTW
	// Data Fields
	var h_windpeak C.short  // 1. Wind Speed Peak over last 5 min. (0.1 kph)
	var h_wdir C.short      // 2. Wind Direction of Wind Speed Peak (0-255)
	var h_otemp C.short     // 3. Current Outdoor Temp (0.1 deg F)
	var h_totrain C.short   // 4. Rain Long Term Total (0.01 in.)
	var h_baro C.short      // 5. Current Barometer (0.1 mbar)
	var h_barodelta C.short // 6. Barometer Delta Value(0.1 mbar)
	var h_barocorrl C.short // 7. Barometer Corr. Factor(LSW)
	var h_barocorrm C.short // 8. Barometer Corr. Factor(MSW)
	var h_ohumid C.short    // 9. Current Outdoor Humidity (0.1%)
	var h_date C.short      // 10. Date (day of year)
	var h_time C.short      // 11. Time (minute of day)
	var h_raintoday C.short // 12. Today's Rain Total (0.01 inches)*
	var h_windave C.short   // 13. 5 Minute Wind Speed Average (0.1kph)*
	// Carriage Return & Line Feed
	// *Some instruments may not include field 13, some may
	// not include 12 or 13.
	// Total size: 44, 48 or 52 characters (hex digits) +
	// header, carriage return and line feed.

	C.strcpy(&A.g_data_type_desc[0], C.CString("Ultimeter"))

	if info[0] == '$' {
		var n, _ = fmt.Sscanf(string(info[5:]), "%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx%4hx",
			&h_windpeak,
			&h_wdir,
			&h_otemp,
			&h_totrain,
			&h_baro,
			&h_barodelta,
			&h_barocorrl,
			&h_barocorrm,
			&h_ohumid,
			&h_date,
			&h_time,
			&h_raintoday, // not on some models.
			&h_windave)   // not on some models.

		if n >= 11 && n <= 13 {

			var windpeak, wdir, otemp, baro, ohumid C.float

			windpeak = C.float(DW_KM_TO_MILES(float64(h_windpeak) * 0.1))
			wdir = C.float(h_wdir&0xff) * 360. / 256.
			otemp = C.float(h_otemp) * 0.1
			baro = C.float(DW_MBAR_TO_INHG(float64(h_baro) * 0.1))
			ohumid = C.float(h_ohumid) * 0.1

			C.strcpy(&A.g_weather[0], C.CString(fmt.Sprintf("wind %.1f mph, direction %.0f, temperature %.1f, barometer %.2f, humidity %.0f",
				windpeak, wdir, otemp, baro, ohumid)))
		}
	}

	// Header = !!
	// Data Fields
	// 1. Wind Speed (0.1 kph)
	// 2. Wind Direction (0-255)
	// 3. Outdoor Temp (0.1 deg F)
	// 4. Rain* Long Term Total (0.01 inches)
	// 5. Barometer (0.1 mbar) 	[ can be ---- ]
	// 6. Indoor Temp (0.1 deg F) 	[ can be ---- ]
	// 7. Outdoor Humidity (0.1%) 	[ can be ---- ]
	// 8. Indoor Humidity (0.1%) 	[ can be ---- ]
	// 9. Date (day of year)
	// 10. Time (minute of day)
	// 11. Today's Rain Total (0.01 inches)*
	// 12. 1 Minute Wind Speed Average (0.1kph)*
	// Carriage Return & Line Feed
	//
	// *Some instruments may not include field 12, some may not include 11 or 12.
	// Total size: 40, 44 or 48 characters (hex digits) + header, carriage return and line feed

	if info[0] == '!' {
		var n, _ = fmt.Sscanf(string(info[2:]), "%4hx%4hx%4hx%4hx",
			&h_windpeak,
			&h_wdir,
			&h_otemp,
			&h_totrain)

		if n == 4 {

			var windpeak, wdir, otemp C.float

			windpeak = C.float(DW_KM_TO_MILES(float64(h_windpeak) * 0.1))
			wdir = C.float(h_wdir&0xff) * 360. / 256.
			otemp = C.float(h_otemp) * 0.1

			C.strcpy(&A.g_weather[0], C.CString(fmt.Sprintf("wind %.1f mph, direction %.0f, temperature %.1f\n",
				windpeak, wdir, otemp)))
		}

	}

} /* end aprs_ultimeter */

/*------------------------------------------------------------------
 *
 * Function:	decode_position
 *
 * Purpose:	Decode the position & symbol information common to many message formats.
 *
 * Inputs:	ppos 	- Pointer to position & symbol fields.
 *
 * Returns:	A.g_lat
 *		A.g_lon
 *		A.g_symbol_table
 *		A.g_symbol_code
 *
 * Description:	This provides resolution of about 60 feet.
 *		This can be improved by using !DAO! in the comment.
 *
 *------------------------------------------------------------------*/

func decode_position(A *decode_aprs_t, ppos *position_t) {

	A.g_lat = get_latitude_8(ppos.Lat, A.g_quiet > 0)
	A.g_lon = get_longitude_9(ppos.Lon, A.g_quiet > 0)

	A.g_symbol_table = C.char(ppos.SymTableId)
	A.g_symbol_code = C.char(ppos.SymbolCode)
}

/*------------------------------------------------------------------
 *
 * Function:	decode_compressed_position
 *
 * Purpose:	Decode the compressed position & symbol information common to many message formats.
 *
 * Inputs:	ppos 	- Pointer to compressed position & symbol fields.
 *
 * Returns:	A.g_lat
 *		A.g_lon
 *		A.g_symbol_table
 *		A.g_symbol_code
 *
 *		One of the following:
 *			A.g_course & A.g_speeed
 *			A.g_altitude_ft
 *			A.g_range
 *
 * Description:	The compressed position provides resolution of around ???
 *		This also includes course/speed or altitude.
 *
 *		It contains 13 bytes of the format:
 *
 *			symbol table	/, \, or overlay A-Z, a-j is mapped into 0-9
 *
 *			yyyy		Latitude, base 91.
 *
 *			xxxx		Longitude, base 91.
 *
 *			symbol code
 *
 *			cs		Course/Speed or altitude.
 *
 *			t		Various "type" info.
 *
 *------------------------------------------------------------------*/

func decode_compressed_position(A *decode_aprs_t, pcpos *compressed_position_t) {
	if isdigit91(pcpos.Y[0]) && isdigit91(pcpos.Y[1]) && isdigit91(pcpos.Y[2]) && isdigit91(pcpos.Y[3]) {
		A.g_lat = 90 - C.double((pcpos.Y[0]-33)*91*91*91+(pcpos.Y[1]-33)*91*91+(pcpos.Y[2]-33)*91+(pcpos.Y[3]-33))/380926.0
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in compressed latitude.  Must be in range of '!' to '{'.\n")
		}
		A.g_lat = G_UNKNOWN
	}

	if isdigit91(pcpos.X[0]) && isdigit91(pcpos.X[1]) && isdigit91(pcpos.X[2]) && isdigit91(pcpos.X[3]) {
		A.g_lon = -180 + C.double((pcpos.X[0]-33)*91*91*91+(pcpos.X[1]-33)*91*91+(pcpos.X[2]-33)*91+(pcpos.X[3]-33))/190463.0
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in compressed longitude.  Must be in range of '!' to '{'.\n")
		}
		A.g_lon = G_UNKNOWN
	}

	if pcpos.SymTableId == '/' || pcpos.SymTableId == '\\' || unicode.IsUpper(rune(pcpos.SymTableId)) {
		/* primary or alternate or alternate with upper case overlay. */
		A.g_symbol_table = C.char(pcpos.SymTableId)
	} else if pcpos.SymTableId >= 'a' && pcpos.SymTableId <= 'j' {
		/* Lower case a-j are used to represent overlay characters 0-9 */
		/* because a digit here would mean normal (non-compressed) location. */
		A.g_symbol_table = C.char(pcpos.SymTableId) - 'a' + '0'
	} else {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid symbol table id for compressed position.\n")
		}
		A.g_symbol_table = '/'
	}

	A.g_symbol_code = C.char(pcpos.SymbolCode)

	if pcpos.C == ' ' {
		/* ignore other two bytes */
	} else if ((pcpos.T - 33) & 0x18) == 0x10 {
		A.g_altitude_ft = C.float(math.Pow(1.002, float64(pcpos.C-33)*91+float64(pcpos.S-33)))
	} else if pcpos.C == '{' {
		A.g_range = 2.0 * C.float(math.Pow(1.08, float64(pcpos.S-33)))
	} else if pcpos.C >= '!' && pcpos.C <= 'z' {
		/* For a weather station, this is wind information. */
		A.g_course = C.float(pcpos.C-33) * 4
		A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(math.Pow(1.08, float64(pcpos.S-33)) - 1.0))
	}

}

/*------------------------------------------------------------------
 *
 * Function:	get_latitude_8
 *
 * Purpose:	Convert 8 byte latitude encoding to degrees.
 *
 * Inputs:	plat 	- Pointer to first byte.
 *
 * Returns:	Double precision value in degrees.  Negative for South.
 *
 * Description:	Latitude is expressed as a fixed 8-character field, in degrees
 *		and decimal minutes (to two decimal places), followed by the
 *		letter N for north or S for south.
 *		The protocol spec specifies upper case but I've seen lower
 *		case so this will accept either one.
 *		Latitude degrees are in the range 00 to 90. Latitude minutes
 *		are expressed as whole minutes and hundredths of a minute,
 *		separated by a decimal point.
 *		For example:
 *		4903.50N is 49 degrees 3 minutes 30 seconds north.
 *		In generic format examples, the latitude is shown as the 8-character
 *		string ddmm.hhN (i.e. degrees, minutes and hundredths of a minute north).
 *
 * Bug:		We don't properly deal with position ambiguity where trailing
 *		digits might be replaced by spaces.  We simply treat them like zeros.
 *
 * Errors:	Return G_UNKNOWN for any type of error.
 *
 *		Should probably print an error message.
 *
 *------------------------------------------------------------------*/

func get_latitude_8(p [8]byte, quiet bool) C.double {
	type lat_s struct {
		Deg  [2]byte
		Minn [2]byte
		Dot  byte
		HMin [2]byte
		NS   byte
	}

	var plat lat_s
	binary.Decode(p[:], binary.NativeEndian, &plat)

	var result C.double = 0

	if unicode.IsDigit(rune(plat.Deg[0])) {
		result += C.double(plat.Deg[0]-'0') * 10
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-9 for tens of degrees.\n", plat.Deg[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plat.Deg[1])) {
		result += C.double(plat.Deg[1]-'0') * 1
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-9 for degrees.\n", plat.Deg[1])
		}
		return (G_UNKNOWN)
	}

	if plat.Minn[0] >= '0' && plat.Minn[0] <= '5' {
		result += C.double(plat.Minn[0]-'0') * (10. / 60.)
	} else if plat.Minn[0] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-5 for tens of minutes.\n", plat.Minn[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plat.Minn[1])) {
		result += C.double(plat.Minn[1]-'0') * (1. / 60.)
	} else if plat.Minn[1] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-9 for minutes.\n", plat.Minn[1])
		}
		return (G_UNKNOWN)
	}

	if plat.Dot != '.' {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Unexpected character \"%c\" found where period expected in latitude.\n", plat.Dot)
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plat.HMin[0])) {
		result += C.double(plat.HMin[0]-'0') * (0.1 / 60.)
	} else if plat.HMin[0] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-9 for tenths of minutes.\n", plat.HMin[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plat.HMin[1])) {
		result += C.double(plat.HMin[1]-'0') * (0.01 / 60.)
	} else if plat.HMin[1] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in latitude.  Found '%c' when expecting 0-9 for hundredths of minutes.\n", plat.HMin[1])
		}
		return (G_UNKNOWN)
	}

	// The spec requires upper case for hemisphere.  Accept lower case but warn.

	switch plat.NS {
	case 'N':
		return (result)
	case 'n':
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Warning: Lower case n found for latitude hemisphere.  Specification requires upper case N or S.\n")
		}
		return (result)
	case 'S':
		return (-result)
	case 's':
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Warning: Lower case s found for latitude hemisphere.  Specification requires upper case N or S.\n")
		}
		return (-result)
	default:
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error: '%c' found for latitude hemisphere.  Specification requires upper case N or S.\n", plat.NS)
		}
		return (G_UNKNOWN)
	}
}

/*------------------------------------------------------------------
 *
 * Function:	get_longitude_9
 *
 * Purpose:	Convert 9 byte longitude encoding to degrees.
 *
 * Inputs:	plat 	- Pointer to first byte.
 *
 * Returns:	Double precision value in degrees.  Negative for West.
 *
 * Description:	Longitude is expressed as a fixed 9-character field, in degrees and
 *		decimal minutes (to two decimal places), followed by the letter E
 *		for east or W for west.
 *		Longitude degrees are in the range 000 to 180. Longitude minutes are
 *		expressed as whole minutes and hundredths of a minute, separated by a
 *		decimal point.
 *		For example:
 *		07201.75W is 72 degrees 1 minute 45 seconds west.
 *		In generic format examples, the longitude is shown as the 9-character
 *		string dddmm.hhW (i.e. degrees, minutes and hundredths of a minute west).
 *
 * Bug:		We don't properly deal with position ambiguity where trailing
 *		digits might be replaced by spaces.  We simply treat them like zeros.
 *
 * Errors:	Return G_UNKNOWN for any type of error.
 *
 * Example:
 *
 *------------------------------------------------------------------*/

func get_longitude_9(p [9]byte, quiet bool) C.double {
	type lat_s struct {
		Deg  [3]byte
		Minn [2]byte
		Dot  byte
		HMin [2]byte
		EW   byte
	}

	var plon lat_s // TODO KG lon_s, no?
	binary.Decode(p[:], binary.NativeEndian, &plon)

	var result C.double = 0

	if plon.Deg[0] == '0' || plon.Deg[0] == '1' {
		result += C.double((plon.Deg[0])-'0') * 100
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0 or 1 for hundreds of degrees.\n", plon.Deg[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plon.Deg[1])) {
		result += C.double((plon.Deg[1])-'0') * 10
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-9 for tens of degrees.\n", plon.Deg[1])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plon.Deg[2])) {
		result += C.double((plon.Deg[2])-'0') * 1
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-9 for degrees.\n", plon.Deg[2])
		}
		return (G_UNKNOWN)
	}

	if plon.Minn[0] >= '0' && plon.Minn[0] <= '5' {
		result += C.double((plon.Minn[0])-'0') * (10. / 60.)
	} else if plon.Minn[0] == ' ' {
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-5 for tens of minutes.\n", plon.Minn[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plon.Minn[1])) {
		result += C.double((plon.Minn[1])-'0') * (1. / 60.)
	} else if plon.Minn[1] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-9 for minutes.\n", plon.Minn[1])
		}
		return (G_UNKNOWN)
	}

	if plon.Dot != '.' {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Unexpected character \"%c\" found where period expected in longitude.\n", plon.Dot)
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plon.HMin[0])) {
		result += C.double((plon.HMin[0])-'0') * (0.1 / 60.)
	} else if plon.HMin[0] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-9 for tenths of minutes.\n", plon.HMin[0])
		}
		return (G_UNKNOWN)
	}

	if unicode.IsDigit(rune(plon.HMin[1])) {
		result += C.double((plon.HMin[1])-'0') * (0.01 / 60.)
	} else if plon.HMin[1] == ' ' {

	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Invalid character in longitude.  Found '%c' when expecting 0-9 for hundredths of minutes.\n", plon.HMin[1])
		}
		return (G_UNKNOWN)
	}

	// The spec requires upper case for hemisphere.  Accept lower case but warn.

	switch plon.EW {
	case 'E':
		return (result)
	case 'e':
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Warning: Lower case e found for longitude hemisphere.  Specification requires upper case E or W.\n")
		}
		return (result)
	case 'W':
		return (-result)
	case 'w':
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Warning: Lower case w found for longitude hemisphere.  Specification requires upper case E or W.\n")
		}
		return (-result)
	default:
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Error: '%c' found for longitude hemisphere.  Specification requires upper case E or W.\n", plon.EW)
		}
		return (G_UNKNOWN)
	}
}

/*------------------------------------------------------------------
 *
 * Function:	get_timestamp
 *
 * Purpose:	Convert 7 byte timestamp to unix time value.
 *
 * Inputs:	p 	- Pointer to first byte.
 *
 * Returns:	time_t data type. (UTC)  Zero if error.
 *
 * Description:
 *
 *		Day/Hours/Minutes (DHM) format is a fixed 7-character field, consisting of
 *		a 6-digit day/time group followed by a single time indicator character (z or
 *		/). The day/time group consists of a two-digit day-of-the-month (01-31) and
 *		a four-digit time in hours and minutes.
 *		Times can be expressed in zulu (UTC/GMT) or local time. For example:
 *
 *		  092345z is 2345 hours zulu time on the 9th day of the month.
 *		  092345/ is 2345 hours local time on the 9th day of the month.
 *
 *		It is recommended that future APRS implementations only transmit zulu
 *		format on the air.
 *
 *		Note: The time in Status Reports may only be in zulu format.
 *
 *		Hours/Minutes/Seconds (HMS) format is a fixed 7-character field,
 *		consisting of a 6-digit time in hours, minutes and seconds, followed by the h
 *		time-indicator character. For example:
 *
 *		  234517h is 23 hours 45 minutes and 17 seconds zulu.
 *
 *		Note: This format may not be used in Status Reports.
 *
 *		Month/Day/Hours/Minutes (MDHM) format is a fixed 8-character field,
 *		consisting of the month (01-12) and day-of-the-month (01-31), followed by
 *		the time in hours and minutes zulu. For example:
 *
 *		  10092345 is 23 hours 45 minutes zulu on October 9th.
 *
 *		This format is only used in reports from stand-alone "positionless" weather
 *		stations (i.e. reports that do not contain station position information).
 *
 *
 * Bugs:	Local time not implemented yet.
 *		8 character form not implemented yet.
 *
 *		Boundary conditions are not handled properly.
 *		For example, suppose it is 00:00:03 on January 1.
 *		We receive a timestamp of 23:59:58 (which was December 31).
 *		If we simply replace the time, and leave the current date alone,
 *		the result is about a day into the future.
 *
 *
 * Example:
 *
 *------------------------------------------------------------------*/

func get_timestamp(A *decode_aprs_t, p [7]byte) time.Time {
	type dhm_s struct {
		Day     [2]byte
		Hours   [2]byte
		Minutes [2]byte
		TIC     byte /* Time indicator character. */
		/* z = UTC. */
		/* / = local - not implemented yet. */
	}
	var pdhm dhm_s

	type hms_s struct {
		Hours   [2]byte
		Minutes [2]byte
		Seconds [2]byte
		TIC     byte /* Time indicator character. */
		/* h = UTC. */
	}
	var phms hms_s

	if !(unicode.IsDigit(rune(p[0])) &&
		unicode.IsDigit(rune(p[1])) &&
		unicode.IsDigit(rune(p[2])) &&
		unicode.IsDigit(rune(p[3])) &&
		unicode.IsDigit(rune(p[4])) &&
		unicode.IsDigit(rune(p[5])) &&
		(p[6] == 'z' || p[6] == '/' || p[6] == 'h')) { //nolnit:staticcheck
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Timestamp must be 6 digits followed by z, h, or /.\n")
		return time.Time{}
	}

	var now = time.Now()

	binary.Decode(p[:], binary.NativeEndian, &pdhm)
	binary.Decode(p[:], binary.NativeEndian, &phms)

	if pdhm.TIC == 'z' || pdhm.TIC == '/' { /* Wrong! */
		var day = int(pdhm.Day[0]-'0')*10 + int(pdhm.Day[1]-'0')
		//text_color_set(DW_COLOR_DECODED);
		//dw_printf("Changing day from %d to %d\n", ptm.tm_mday, j);

		var hour = int(pdhm.Hours[0]-'0')*10 + int(pdhm.Hours[1]-'0')
		//dw_printf("Changing hours from %d to %d\n", ptm.tm_hour, j);

		var minute = int(pdhm.Minutes[0]-'0')*10 + int(pdhm.Minutes[1]-'0')
		//dw_printf("Changing minutes from %d to %d\n", ptm.tm_min, j);

		return time.Date(now.Year(), now.Month(), day, hour, minute, 0, 0, time.UTC)

	} else if phms.TIC == 'h' {
		var hour = int(phms.Hours[0]-'0')*10 + int(phms.Hours[1]-'0')
		//text_color_set(DW_COLOR_DECODED);
		//dw_printf("Changing hours from %d to %d\n", ptm.tm_hour, j);

		var minute = int(phms.Minutes[0]-'0')*10 + int(phms.Minutes[1]-'0')
		//dw_printf("Changing minutes from %d to %d\n", ptm.tm_min, j);

		var second = int(phms.Seconds[0]-'0')*10 + int(phms.Seconds[1]-'0')
		//dw_printf("%sChanging seconds from %d to %d\n", ptm.tm_sec, j);

		return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, second, 0, time.UTC)
	}

	return time.Time{}
}

/*------------------------------------------------------------------
 *
 * Function:	get_maidenhead
 *
 * Purpose:	See if we have a maidenhead locator.
 *
 * Inputs:	p 	- Byte slice
 *
 * Returns:	0 = not found.
 *		4 = possible 4 character locator found.
 *		6 = possible 6 character locator found.
 *
 *		It is not stored anywhere or processed.
 *
 * Description:
 *
 *		The maidenhead locator system is sometimes used as a more compact,
 *		and less precise, alternative to numeric latitude and longitude.
 *
 *		It is composed of:
 *			a pair of letters in range A to R.
 *			a pair of digits in range of 0 to 9.
 *			an optional pair of letters in range of A to X.
 *
 *		The spec says:
 *				"All letters must be transmitted in upper case.
 *				Letters may be received in upper case or lower case."
 *
 *		Typically the second set of letters is written in lower case.
 *		An earlier version incorrectly produced an error if lower case found.
 *
 *
 * Examples from APRS spec:
 *
 *		IO91SX
 *		IO91
 *
 *
 *------------------------------------------------------------------*/

func get_maidenhead(A *decode_aprs_t, p []byte) C.int {

	if unicode.ToUpper(rune(p[0])) >= 'A' && unicode.ToUpper(rune(p[0])) <= 'R' &&
		unicode.ToUpper(rune(p[1])) >= 'A' && unicode.ToUpper(rune(p[1])) <= 'R' &&
		unicode.IsDigit(rune(p[2])) && unicode.IsDigit(rune(p[3])) {

		/* We have 4 characters matching the rule. */

		if unicode.ToUpper(rune(p[4])) >= 'A' && unicode.ToUpper(rune(p[4])) <= 'X' &&
			unicode.ToUpper(rune(p[5])) >= 'A' && unicode.ToUpper(rune(p[5])) <= 'X' {

			/* We have 6 characters matching the rule. */
			return 6
		}

		return 4
	}

	return 0
}

/*------------------------------------------------------------------
 *
 * Function:	data_extension_comment
 *
 * Purpose:	A fixed length 7-byte field may follow APRS position datA.
 *
 * Inputs:	pdext	- Pointer to optional data extension and comment.
 *
 * Returns:	true if a data extension was found.
 *
 * Outputs:	One or more of the following, depending the data found:
 *
 *			A.g_course
 *			A.g_speed_mph
 *			A.g_power
 *			A.g_height
 *			A.g_gain
 *			A.g_directivity
 *			A.g_range
 *
 *		Anything left over will be put in
 *
 *			A.g_comment
 *
 * Description:
 *
 *
 *
 *------------------------------------------------------------------*/

// TODO KG rename?
var dir []string = []string{"omni", "NE", "E", "SE", "S", "SW", "W", "NW", "N"}

func data_extension_comment(A *decode_aprs_t, pdext []byte) C.int {

	if C.strlen(C.CString(string(pdext))) < 7 {
		C.strcpy(&A.g_comment[0], C.CString(string(pdext)))
		return 0
	}

	/* Tyy/Cxx - Area object descriptor. */

	if pdext[0] == 'T' &&
		pdext[3] == '/' &&
		pdext[4] == 'C' {
		/* not decoded at this time */
		process_comment(A, pdext[7:])
		return 1
	}

	/* CSE/SPD */
	/* For a weather station (symbol code _) this is wind. */
	/* For others, it would be course and speed. */

	if pdext[3] == '/' {
		var n int
		var count, _ = fmt.Sscanf(string(pdext), "%3d", &n)
		if count > 0 {
			A.g_course = C.float(n)
		}
		count, _ = fmt.Sscanf(string(pdext[4:]), "%3d", &n)
		if count > 0 {
			A.g_speed_mph = C.float(DW_KNOTS_TO_MPH(float64(n)))
		}

		/* Bearing and Number/Range/Quality? */

		if pdext[7] == '/' && pdext[11] == '/' {
			process_comment(A, pdext[7+8:])
		} else {
			process_comment(A, pdext[7:])
		}
		return 1
	}

	/* check for Station power, height, gain. */

	if bytes.HasPrefix(pdext, []byte("PHG")) {
		A.g_power = C.int(pdext[3]-'0') * C.int(pdext[3]-'0')
		A.g_height = C.int(1<<(pdext[4]-'0')) * 10
		A.g_gain = C.int(pdext[5] - '0')
		if pdext[6] >= '0' && pdext[6] <= '8' {
			C.strcpy(&A.g_directivity[0], C.CString(dir[pdext[6]-'0']))
		}

		// TODO: look for another 0-9 A-Z followed by a /
		// http://www.aprs.org/aprs12/probes.txt

		process_comment(A, pdext[7:])
		return 1
	}

	/* check for precalculated radio range. */

	if bytes.HasPrefix(pdext, []byte("RNG")) {
		var n int
		var count, _ = fmt.Sscanf(string(pdext[3:]), "%4d", &n)
		if count > 0 {
			A.g_range = C.float(n)
		}
		process_comment(A, pdext[7:])
		return 1
	}

	/* DF signal strength,  */

	if bytes.HasPrefix(pdext, []byte("DFS")) {
		//A.g_strength = pdext[3] - '0';
		A.g_height = C.int(1<<(pdext[4]-'0')) * 10
		A.g_gain = C.int(pdext[5] - '0')
		if pdext[6] >= '0' && pdext[6] <= '8' {
			C.strcpy(&A.g_directivity[0], C.CString(dir[pdext[6]-'0']))
		}

		process_comment(A, pdext[7:])
		return 1
	}

	process_comment(A, pdext)
	return 0
}

/*------------------------------------------------------------------
 *
 * Function:	process_comment
 *
 * Purpose:	Extract optional items from the comment.
 *
 * Inputs:	commentData - byte slice of the remainder of the information field.
 *
 *		clen		- Length of comment or -1 to take it all.
 *
 * Outputs:	A.g_telemetry	- Base 91 telemetry |ss1122|
 *		A.g_altitude_ft - from /A=123456 or /A=-12345
 *		A.g_lat	- Might be adjusted from !DAO!
 *		A.g_lon	- Might be adjusted from !DAO!
 *		A.g_aprstt_loc	- Private extension to !DAO!
 *		A.g_freq
 *		A.g_tone
 *		A.g_offset
 *		A.g_comment	- Anything left over after extracting above.
 *
 * Description:	After processing fixed and possible optional parts
 *		of the message, everything left over is a comment.
 *
 *		Except!!!
 *
 *		There are could be some other pieces of data, with
 *		particular formats, buried in there.
 *		Pull out those special items and put everything
 *		else into A.g_comment.
 *
 * References:	http://www.aprs.org/info/freqspec.txt
 *
 *			999.999MHz T100 +060	Voice frequency.
 *
 *		http://www.aprs.org/datum.txt
 *
 *			!DAO!			APRS precision and Datum option.
 *
 *		Protocol reference, end of chapter 6.
 *
 *			/A=123456		Altitude
 *			/A=-12345		Enhancement - There are many places on the earth's
 *						surface but the APRS spec has no provision for negative
 *						numbers.  I propose having 5 digits for a consistent
 *						field width.  6 would be excessive.
 *
 * What can appear in a comment?
 *
 *		Chapter 5 of the APRS spec ( http://www.aprs.org/doc/APRS101.PDF ) says:
 *
 *			"The comment may contain any printable ASCII characters (except | and ~,
 *			which are reserved for TNC channel switching)."
 *
 *		"Printable" would exclude character values less than space (00100000), e.g.
 *		tab, carriage return, line feed, nul.  Sometimes we see carriage return
 *		(00001010) at the end of APRS packets.   This would be in violation of the
 *		specification.
 *
 *		The base 91 telemetry format (http://he.fi/doc/aprs-base91-comment-telemetry.txt ),
 *		which is not part of the APRS spec, uses the | character in the comment to delimit encoded
 *		telemetry data.   This would be in violation of the original spec.
 *
 *		The APRS Spec Addendum 1.2 Proposals ( http://www.aprs.org/aprs12/datum.txt)
 *		adds use of UTF-8 (https://en.wikipedia.org/wiki/UTF-8 )for the free form text in
 *		messages and comments. It can't be used in the fixed width fields.
 *
 *		Non-ASCII characters are represented by multi-byte sequences.  All bytes in these
 *		multi-byte sequences have the most significant bit set to 1.  Using UTF-8 would not
 *		add any nul (00000000) bytes to the stream.
 *
 *		There are two known cases where we can have a nul character value.
 *
 *		* The Kenwood TM-D710A sometimes sends packets like this:
 *
 *			VA3AJ-9>T2QU6X,VE3WRC,WIDE1,K8UNS,WIDE2*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`nW<0x1f>oS8>/]"6M}driving fast=
 *			K4JH-9>S5UQ6X,WR4AGC-3*,WIDE1*:4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>`jP}l"&>/]"47}QRV from the EV =
 *
 *		  Notice that the data type indicator of "4" is not valid.  If we remove
 *		  4P<0x00><0x0f>4T<0x00><0x0f>4X<0x00><0x0f>4\<0x00>   we are left with a good MIC-E format.
 *		  This same thing has been observed from others and is intermittent.
 *
 *		* AGW Tracker can send UTF-16 if an option is selected.  This can introduce nul bytes.
 *		  This is wrong.  It should be using UTF-8 and I'm not going to accommodate it here.
 *
 *
 *		The digipeater and IGate functions should pass along anything exactly the
 *		we received it, even if it is invalid.  If different implementations try to fix it up
 *		somehow, like changing unprintable characters to spaces, we will only make things
 *		worse and thwart the duplicate detection.
 *
 *------------------------------------------------------------------*/

/* CTCSS tones in various formats to avoid conversions every time. */

const NUM_CTCSS = 50

var i_ctcss = [NUM_CTCSS]int{
	67, 69, 71, 74, 77, 79, 82, 85, 88, 91,
	94, 97, 100, 103, 107, 110, 114, 118, 123, 127,
	131, 136, 141, 146, 151, 156, 159, 162, 165, 167,
	171, 173, 177, 179, 183, 186, 189, 192, 196, 199,
	203, 206, 210, 218, 225, 229, 233, 241, 250, 254}

var f_ctcss = [NUM_CTCSS]C.float{
	67.0, 69.3, 71.9, 74.4, 77.0, 79.7, 82.5, 85.4, 88.5, 91.5,
	94.8, 97.4, 100.0, 103.5, 107.2, 110.9, 114.8, 118.8, 123.0, 127.3,
	131.8, 136.5, 141.3, 146.2, 151.4, 156.7, 159.8, 162.2, 165.5, 167.9,
	171.3, 173.8, 177.3, 179.9, 183.5, 186.2, 189.9, 192.8, 196.6, 199.5,
	203.5, 206.5, 210.7, 218.1, 225.7, 229.1, 233.6, 241.8, 250.3, 254.1}

var s_ctcss = [NUM_CTCSS]string{
	"67.0", "69.3", "71.9", "74.4", "77.0", "79.7", "82.5", "85.4", "88.5", "91.5",
	"94.8", "97.4", "100.0", "103.5", "107.2", "110.9", "114.8", "118.8", "123.0", "127.3",
	"131.8", "136.5", "141.3", "146.2", "151.4", "156.7", "159.8", "162.2", "165.5", "167.9",
	"171.3", "173.8", "177.3", "179.9", "183.5", "186.2", "189.9", "192.8", "196.6", "199.5",
	"203.5", "206.5", "210.7", "218.1", "225.7", "229.1", "233.6", "241.8", "250.3", "254.1"}

func cutBytes(b []byte, from int, to int) []byte {
	var result = make([]byte, len(b)-(to-from))
	copy(result, b[:from])
	copy(result[from:], b[to:])
	return result
}

// #define sign(x) (((x)>=0)?1:(-1))
func aprsSign(x C.double) C.double {
	if x >= 0 {
		return 1
	} else {
		return -1
	}
}

func process_comment(A *decode_aprs_t, commentData []byte) {

	/*
	 * Frequency must be at the at the beginning.
	 * Others can be anywhere in the comment.
	 */

	//e = regcomp (&freq_re, "^[0-9A-O][0-9][0-9]\\.[0-9][0-9][0-9 ]MHz( [TCDtcd][0-9][0-9][0-9]| Toff)?( [+-][0-9][0-9][0-9])?");

	// Freq optionally preceded by space or /.
	// Third fractional digit can be space instead.
	// "MHz" should be exactly that capitalization.
	// Print warning later it not.
	var std_freq_re = regexp.MustCompile("^[/ ]?([0-9A-O][0-9][0-9]\\.[0-9][0-9][0-9 ])([Mm][Hh][Zz])") /* Frequency in standard format. */

	// If no tone, we might gobble up / after any data extension,
	// We could also have a space but it's not required.
	// I don't understand the difference between T and C so treat the same for now.
	// We can also have "off" instead of number to explicitly mean none.

	var std_tone_re = regexp.MustCompile("^[/ ]?([TtCc][012][0-9][0-9])")                                                    /* Tone in standard format. */
	var std_toff_re = regexp.MustCompile("^[/ ]?[TtCc][Oo][Ff][Ff]")                                                         /* Explicitly no tone. */
	var std_dcs_re = regexp.MustCompile("^[/ ]?[Dd]([0-7][0-7][0-7])")                                                       /* Digital codes squelch in standard format. */
	var std_offset_re = regexp.MustCompile("^[/ ]?([+-][0-9][0-9][0-9])")                                                    /* Xmit freq offset in standard format. */
	var std_range_re = regexp.MustCompile("^[/ ]?[Rr]([0-9][0-9])([mk])")                                                    /* Range in standard format. */
	var dao_re = regexp.MustCompile("!([A-Z][0-9 ][0-9 ]|[a-z][!-{ ][!-{ ]|T[0-9 B][0-9 ])!")                                /* DAO */
	var alt_re = regexp.MustCompile("/A=[0-9-][0-9][0-9][0-9][0-9][0-9]")                                                    /* /A= altitude */
	var bad_freq_re = regexp.MustCompile("[0-9][0-9][0-9]\\.[0-9][0-9][0-9]?")                                               /* Likely frequency, not standard format */
	var bad_tone_re = regexp.MustCompile("(^|[^0-9.])([6789][0-9]\\.[0-9]|[12][0-9][0-9]\\.[0-9]|67|77|100|123)($|[^0-9.])") /* Likely tone, not standard format */
	var base91_tel_re = regexp.MustCompile("\\|(([!-{][!-{]){2,7})\\|")                                                      /* Base 91 compressed telemetry data. */

	commentData = bytes.TrimRight(commentData, "\x00") // Drop any trailing nulls
	var clen = len(commentData)

	/*
	 * Watch out for buffer overflow.
	 * KG6AZZ reports that there is a local digipeater that seems to
	 * malfunction occasionally.  It corrupts the packet, as it is
	 * digipeated, causing the comment to be hundreds of characters long.
	 */

	if clen > len(A.g_comment)-1 {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Comment is extremely long, %d characters.\n", clen)
			dw_printf("Please report this, along with surrounding lines, so we can find the cause.\n")
		}
	}

	var atof = func(b []byte) C.float {
		var f, _ = strconv.ParseFloat(string(b), 64)
		return C.float(f)
	}

	/*
	 * Look for frequency in the standard format at start of comment.
	 * If that fails, try to obtain from object name.
	 */

	if match := std_freq_re.FindSubmatchIndex(commentData); match != nil {
		var sftemp = commentData[match[2]:match[3]]
		var smtemp = commentData[match[4]:match[5]]

		//dw_printf("matches= %d - %d, %d - %d, %d - %d\n", (int)(match[0].rm_so), (int)(match[0].rm_eo),
		//						    (int)(match[1].rm_so), (int)(match[1].rm_eo),
		//						    (int)(match[2].rm_so), (int)(match[2].rm_eo) );

		switch sftemp[0] {
		case 'A':
			A.g_freq = 1200 + C.double(atof(sftemp[1:]))
		case 'B':
			A.g_freq = 2300 + C.double(atof(sftemp[1:]))
		case 'C':
			A.g_freq = 2400 + C.double(atof(sftemp[1:]))
		case 'D':
			A.g_freq = 3400 + C.double(atof(sftemp[1:]))
		case 'E':
			A.g_freq = 5600 + C.double(atof(sftemp[1:]))
		case 'F':
			A.g_freq = 5700 + C.double(atof(sftemp[1:]))
		case 'G':
			A.g_freq = 5800 + C.double(atof(sftemp[1:]))
		case 'H':
			A.g_freq = 10100 + C.double(atof(sftemp[1:]))
		case 'I':
			A.g_freq = 10200 + C.double(atof(sftemp[1:]))
		case 'J':
			A.g_freq = 10300 + C.double(atof(sftemp[1:]))
		case 'K':
			A.g_freq = 10400 + C.double(atof(sftemp[1:]))
		case 'L':
			A.g_freq = 10500 + C.double(atof(sftemp[1:]))
		case 'M':
			A.g_freq = 24000 + C.double(atof(sftemp[1:]))
		case 'N':
			A.g_freq = 24100 + C.double(atof(sftemp[1:]))
		case 'O':
			A.g_freq = 24200 + C.double(atof(sftemp[1:]))
		default:
			A.g_freq = C.double(atof(sftemp))
		}

		if bytes.HasPrefix(smtemp, []byte("MHz")) {
			if A.g_quiet == 0 {
				text_color_set(DW_COLOR_ERROR)
				dw_printf("Warning: \"%s\" has non-standard capitalization and might not be recognized by some systems.\n", smtemp)
				dw_printf("For best compatibility, it should be exactly like this: \"MHz\"  (upper,upper,lower case)\n")
			}
		}

		commentData = cutBytes(commentData, match[0], match[1])
	} else if C.strlen(&A.g_name[0]) > 0 {

		// Try to extract sensible number from object/item name.

		var x = atof(C.GoBytes(unsafe.Pointer(&A.g_name[0]), C.int(len(A.g_name))))

		if (x >= 144 && x <= 148) ||
			(x >= 222 && x <= 225) ||
			(x >= 420 && x <= 450) ||
			(x >= 902 && x <= 928) {
			A.g_freq = C.double(x)
		}
	}

	/*
	 * Next, look for tone, DCS code, and range.
	 * Examples always have them in same order but it's not clear
	 * whether any order is allowed after possible frequency.
	 *
	 * TODO: Convert integer tone to original value for display.
	 * TODO: samples in zfreq-test3.txt
	 */

	var keep_going = true
	for keep_going {
		if match := std_tone_re.FindSubmatchIndex(commentData); match != nil {
			var _sttemp = commentData[match[2]:match[3]] /* includes leading letter */
			var sttemp = C.GoBytes(unsafe.Pointer(&_sttemp[0]), C.int(len(_sttemp)))

			// Try to convert from integer to proper value.

			var f, _ = strconv.Atoi(string(sttemp[1:]))
			for i := 0; i < NUM_CTCSS; i++ {
				if f == i_ctcss[i] {
					A.g_tone = f_ctcss[i]
					break
				}
			}
			if A.g_tone == G_UNKNOWN {
				if A.g_quiet == 0 {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Bad CTCSS/PL specification: \"%s\"\n", sttemp)
					dw_printf("Integer does not correspond to standard tone.\n")
				}
			}

			commentData = cutBytes(commentData, match[0], match[1])
		} else if match := std_toff_re.FindSubmatchIndex(commentData); match != nil {

			dw_printf("NO tone\n")
			A.g_tone = 0

			commentData = cutBytes(commentData, match[0], match[1])
		} else if match := std_dcs_re.FindSubmatchIndex(commentData); match != nil {

			var sttemp = commentData[match[2]:match[3]]

			var offset, _ = strconv.ParseUint(string(sttemp), 8, 64)

			A.g_dcs = C.int(offset)

			commentData = cutBytes(commentData, match[0], match[1])
		} else if match := std_offset_re.FindSubmatchIndex(commentData); match != nil {

			var sttemp = commentData[match[2]:match[3]]

			var offset, _ = strconv.Atoi(string(sttemp))

			A.g_offset = 10 * C.int(offset)

			commentData = cutBytes(commentData, match[0], match[1])
		} else if match := std_range_re.FindSubmatchIndex(commentData); match != nil {

			var sttemp = commentData[match[2]:match[3]] /* should be two digits */
			var sutemp = commentData[match[4]:match[5]] /* m for miles or k for km */

			var r, _ = strconv.Atoi(string(sttemp))

			if string(sutemp) == "m" {
				A.g_range = C.float(r)
			} else {
				A.g_range = C.float(DW_KM_TO_MILES(float64(r)))
			}

			commentData = cutBytes(commentData, match[0], match[1])
		} else {
			keep_going = false
		}
	}

	/*
	 * Telemetry data, in base 91 compressed format appears as 2 to 7 pairs
	 * of base 91 digits, ! thru {, surrounded by | at start and end.
	 */

	if match := base91_tel_re.FindSubmatchIndex(commentData); match != nil {

		//dw_printf("compressed telemetry start=%d, end=%d\n", (int)(match[0].rm_so), (int)(match[0].rm_eo));

		var tdata = commentData[match[2]:match[3]] /* Should be even number of 4 to 14 characters. */

		//dw_printf("compressed telemetry data = \"%s\"\n", tdata);

		telemetry_data_base91(C.GoString(&A.g_src[0]), string(tdata), &A.g_telemetry[0], C.size_t(len(A.g_telemetry)))

		commentData = cutBytes(commentData, match[0], match[1])
	}

	/*
	 * Latitude and Longitude in the form DD MM.HH has a resolution of about 60 feet.
	 * The !DAO! option allows another digit or almost two for greater resolution.
	 *
	 * This would not make sense to use this with a compressed location which
	 * already has much greater resolution.
	 *
	 * It surprised me to see this in a MIC-E message.
	 * MIC-E has resolution of .01 minute so it would make sense to have it as an option.
	 * We also find an example in  http://www.aprs.org/aprs12/mic-e-examples.txt
	 *	'abc123R/'123}FFF.FFFMHztext.../A=123456...!DAO! Mv
	 */

	if match := dao_re.FindSubmatchIndex(commentData); match != nil {

		var d = commentData[match[0]+1]
		var a = commentData[match[0]+2]
		var o = commentData[match[0]+3]

		//dw_printf("DAO start=%d, end=%d\n", (int)(match[0].rm_so), (int)(match[0].rm_eo));

		/*
		 * Private extension for APRStt
		 */

		if d == 'T' {
			if a == ' ' && o == ' ' {
				C.strcpy(&A.g_aprstt_loc[0], C.CString("APRStt corral location"))
			} else if unicode.IsDigit(rune(a)) && o == ' ' {
				C.strcpy(&A.g_aprstt_loc[0], C.CString(fmt.Sprintf("APRStt location %c of 10", a)))
			} else if unicode.IsDigit(rune(a)) && unicode.IsDigit(rune(o)) {
				C.strcpy(&A.g_aprstt_loc[0], C.CString(fmt.Sprintf("APRStt location %c%c of 100", a, o)))
			} else if a == 'B' && unicode.IsDigit(rune(o)) {
				C.strcpy(&A.g_aprstt_loc[0], C.CString(fmt.Sprintf("APRStt location %c%c...", a, o)))
			}
		} else if unicode.IsUpper(rune(d)) {
			/*
			 * This adds one extra digit to each.  Dao adds extra digit like:
			 *
			 *		Lat:	 DD MM.HHa
			 *		Lon:	DDD HH.HHo
			 */
			if unicode.IsDigit(rune(a)) {
				A.g_lat += C.double(a-'0') / 60000.0 * aprsSign(A.g_lat)
			}
			if unicode.IsDigit(rune(o)) {
				A.g_lon += C.double(o-'0') / 60000.0 * aprsSign(A.g_lon)
			}
		} else if unicode.IsLower(rune(d)) {
			/*
			 * This adds almost two extra digits to each like this:
			 *
			 *		Lat:	 DD MM.HHxx
			 *		Lon:	DDD HH.HHxx
			 *
			 * The original character range '!' to '{' is first converted
			 * to an integer in range of 0 to 90.  It is multiplied by 1.1
			 * to stretch the numeric range to be 0 to 99.
			 */

			/*
			 * Here are a couple situations where it is seen.
			 *
			 *	W8SAT-1>T2UV0P:`qC<0x1f>l!Xu\'"69}WMNI EDS Response Unit #1|+/%0'n|!w:X!|3
			 *
			 * Let's break that down into pieces.
			 *
			 *	W8SAT-1>T2UV0P:`qC<0x1f>l!Xu\'"69}		MIC-E format
			 *							N 42 56.0000, W 085 39.0300,
			 *							0 MPH, course 160, alt 709 ft
			 *	WMNI EDS Response Unit #1			comment
			 *	|+/%0'n|					base 91 telemetry
			 *	!w:X!						DAO
			 *	|3						Tiny Track 3
			 *
			 * Comment earlier points out that MIC-E format has resolution of 0.01 minute,
			 * same as non-compressed format, so the DAO does work out, after thinking
			 * about it for a while.
			 * We also find a MIC-E example with !DAO! here:  http://www.aprs.org/aprs12/mic-e-examples.txt
			 *
			 * Another one:
			 *
			 *	KS4FUN-12>3X0PRU,W6CX-3,BKELEY,WIDE2*:`2^=l!<0x1c>+/'"48}MT-RTG|%B%p'a|!wqR!|3
			 *
			 *							MIC-E, Red Cross, Special
			 *							N 38 00.2588, W 122 06.3354
			 *							0 MPH, course 100, alt 108 ft
			 *	MT-RTG						comment
			 *	|%B%p'a|					Seq=397, A1=443, A2=610
			 *	!wqR!						DAO
			 *	|3						Byonics TinyTrack3
			 *
			 */

			/*
			 * The spec appears to be wrong.  It says '}' is the maximum value when it should be '{'.
			 */

			if isdigit91(a) {
				A.g_lat += C.double(a-B91_MIN) * 1.1 / 600000.0 * aprsSign(A.g_lat)
			}
			if isdigit91(o) {
				A.g_lon += C.double(o-B91_MIN) * 1.1 / 600000.0 * aprsSign(A.g_lon)
			}
		}

		commentData = cutBytes(commentData, match[0], match[1])
	}

	/*
	 * Altitude in feet.  /A=123456 or /A=-12345
	 */

	if match := alt_re.FindSubmatchIndex(commentData); match != nil {

		//dw_printf("start=%d, end=%d\n", (int)(match[0].rm_so), (int)(match[0].rm_eo));

		var temp = commentData[match[0]:match[1]]

		var altitude, _ = strconv.Atoi(string(temp[3:]))
		A.g_altitude_ft = C.float(altitude)

		commentData = cutBytes(commentData, match[0], match[1])
	}

	//dw_printf("Final comment='%s'\n", A.g_comment);

	/*
	 * Finally look for something that looks like frequency or CTCSS tone
	 * in the remaining comment.  Point this out and suggest the
	 * standardized format.
	 * Don't complain if we have already found a valid value.
	 */
	if match := bad_freq_re.FindSubmatchIndex(commentData); match != nil && A.g_freq == G_UNKNOWN {

		var bad = commentData[match[0]:match[1]]

		var x, _ = strconv.ParseFloat(string(bad), 64)

		if (x >= 144 && x <= 148) ||
			(x >= 222 && x <= 225) ||
			(x >= 420 && x <= 450) ||
			(x >= 902 && x <= 928) {

			if A.g_quiet == 0 {
				var good = fmt.Sprintf("%07.3fMHz", x)
				text_color_set(DW_COLOR_ERROR)
				dw_printf("\"%s\" in comment looks like a frequency in non-standard format.\n", bad)
				dw_printf("For most systems to recognize it, use exactly this form \"%s\" at beginning of comment.\n", good)
			}
			if A.g_freq == G_UNKNOWN {
				A.g_freq = C.double(x)
			}
		}
	}

	if match := bad_tone_re.FindSubmatchIndex(commentData); match != nil && A.g_tone == G_UNKNOWN {

		var bad1 = commentData[match[4]:match[5]] /* original 99.9 or 999.9 format or one of 67 77 100 123 */
		var bad2 = string(bad1)                   /* 99.9 or 999.9 format.  ".0" appended for special cases. */
		if bad2 == "67" || bad2 == "77" || bad2 == "100" || bad2 == "123" {
			bad2 += ".0"
		}

		// TODO:  Why wasn't freq/PL recognized here?
		// Should we recognize some cases of single decimal place as frequency?

		//DECODED[194] N8VIM audio level = 27   [NONE]
		//[0] N8VIM>BEACON,WIDE2-2:!4240.85N/07133.99W_PHG72604/ Pepperell, MA-> WX. 442.9+ PL100<0x0d>
		//Didn't find wind direction in form c999.
		//Didn't find wind speed in form s999.
		//Didn't find wind gust in form g999.
		//Didn't find temperature in form t999.
		//Weather Report, WEATHER Station (blue)
		//N 42 40.8500, W 071 33.9900
		//, "PHG72604/ Pepperell, MA-> WX. 442.9+ PL100"

		for i := 0; i < NUM_CTCSS; i++ {
			if s_ctcss[i] == bad2 {

				if A.g_quiet == 0 {
					var good = fmt.Sprintf("T%03d", i_ctcss[i])
					text_color_set(DW_COLOR_ERROR)
					dw_printf("\"%s\" in comment looks like it might be a CTCSS tone in non-standard format.\n", bad1)
					dw_printf("For most systems to recognize it, use exactly this form \"%s\" at near beginning of comment, after any frequency.\n", good)
				}
				if A.g_tone == G_UNKNOWN {
					var tone, _ = strconv.ParseFloat(bad2, 64)
					A.g_tone = C.float(tone)
				}
				break
			}
		}
	}

	if (A.g_offset == 6000 || A.g_offset == -6000) && A.g_freq >= 144 && A.g_freq <= 148 {
		if A.g_quiet == 0 {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("A transmit offset of 6 MHz on the 2 meter band doesn't seem right.\n")
			dw_printf("Each unit is 10 kHz so you should probably be using \"-060\" or \"+060\"\n")
		}
	}

	/*
	 * TODO: samples in zfreq-test4.txt
	 */

	// Finally copy what's left of commentData into g_comment
	C.strcpy(&A.g_comment[0], C.CString(string(commentData)))
}

/* end process_comment */

/* end decode_aprs.c */
