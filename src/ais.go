package direwolf

/********************************************************************************
 *
 * Purpose:	Functions for processing received AIS transmissions and
 *		converting to NMEA sentence representation.
 *
 * References:	AIVDM/AIVDO protocol decoding by Eric S. Raymond
 *		https://gpsd.gitlab.io/gpsd/AIVDM.html
 *
 *		Sample recording with about 100 messages.  Test with "atest -B AIS xxx.wav"
 *		https://github.com/freerange/ais-on-sdr/wiki/example-data/long-beach-160-messages.wav
 *
 *		Useful on-line decoder for AIS NMEA sentences.
 *		https://www.aggsoft.com/ais-decoder.htm
 *
 * Future?	Add an interface to feed AIS data into aprs.fi.
 *		https://aprs.fi/page/ais_feeding
 *
 *******************************************************************************/

// #include "direwolf.h"
// #include <stdio.h>
// #include <stdlib.h>
// #include <assert.h>
// #include <ctype.h>
// #include <string.h>
import "C"

import (
	"fmt"
	"strconv"
	"strings"
)

// Lengths, in bits, for the AIS message types.

type AISTypeSize struct {
	Min int
	Max int
}

var ValidAISLengths = []AISTypeSize{
	{-1, -1},    // 0	not used
	{168, 168},  // 1
	{168, 168},  // 2
	{168, 168},  // 3
	{168, 168},  // 4
	{424, 424},  // 5
	{72, 1008},  // 6	multipurpose
	{72, 168},   // 7	increments of 32 bits
	{168, 1008}, // 8	multipurpose
	{168, 168},  // 9
	{72, 72},    // 10
	{168, 168},  // 11
	{72, 1008},  // 12
	{72, 168},   // 13	increments of 32 bits
	{40, 1008},  // 14
	{88, 160},   // 15
	{96, 114},   // 16	96 or 114, not range
	{80, 816},   // 17
	{168, 168},  // 18
	{312, 312},  // 19
	{72, 160},   // 20
	{272, 360},  // 21
	{168, 168},  // 22
	{160, 160},  // 23
	{160, 168},  // 24
	{40, 168},   // 25
	{60, 1064},  // 26
	{96, 168},   // 27	96 or 168, not range
}

/*-------------------------------------------------------------------
 *
 * Functions to get and set element of a bit vector.
 *
 *--------------------------------------------------------------------*/

var mask = []byte{0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01}

func get_bit(base []byte, offset uint) bool {
	return ((base[offset>>3] & mask[offset&0x7]) != 0)
}

func set_bit(base []byte, offset uint, val bool) {
	if val {
		base[offset>>3] |= mask[offset&0x7]
	} else {
		base[offset>>3] &= ^mask[offset&0x7]
	}
}

/*-------------------------------------------------------------------
 *
 * Extract a variable length field from a bit vector.
 *
 *--------------------------------------------------------------------*/

func get_field(base []byte, start uint, length uint) int {
	var result = 0
	for k := uint(0); k < length; k++ {
		result <<= 1
		if get_bit(base, start+k) {
			result |= 1
		}
	}
	return (result)
}

func set_field(base []byte, start uint, length uint, val int) { // TODO KG int32?
	for k := uint(0); k < length; k++ {
		set_bit(base, start+k, (val>>(length-1-k))&1 != 0)
	}
}

func get_field_signed(base []byte, start uint, length uint) int32 {
	var result = int32(get_field(base, start, length))
	// Sign extend.
	result <<= (32 - length)
	result >>= (32 - length)
	return (result)
}

func get_field_lat(base []byte, start uint, length uint) C.double {
	// Latitude of 0x3412140 (91 deg) means not available.
	// Message type 27 uses lower resolution, 17 bits rather than 27.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field_signed(base, start, length)
	if length == 17 {
		if n == 91*600 {
			return G_UNKNOWN
		} else {
			return C.double(n) / 600.0
		}
	} else {
		if n == 91*600000 {
			return G_UNKNOWN
		} else {
			return C.double(n) / 600000.0
		}
	}
}

func get_field_lon(base []byte, start uint, length uint) C.double {
	// Longitude of 0x6791AC0 (181 deg) means not available.
	// Message type 27 uses lower resolution, 18 bits rather than 28.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field_signed(base, start, length)
	if length == 18 {
		if n == 181*600 {
			return G_UNKNOWN
		} else {
			return C.double(n) / 600.0
		}
	} else {
		if n == 181*600000 {
			return G_UNKNOWN
		} else {
			return C.double(n) / 600000.0
		}
	}
}

func get_field_speed(base []byte, start uint, length uint) C.float {
	// Raw 1023 means not available.
	// Multiply by 0.1 to get knots.
	// For aircraft it is knots, not deciknots.

	// Message type 27 uses lower resolution, 6 bits rather than 10.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field(base, start, length)
	if length == 6 {
		if n == 63 {
			return G_UNKNOWN
		} else {
			return C.float(n)
		}
	} else {
		if n == 1023 {
			return G_UNKNOWN
		} else {
			return C.float(n) * 0.1
		}
	}
}

func get_field_course(base []byte, start uint, length uint) C.float {
	// Raw 3600 means not available.
	// Multiply by 0.1 to get degrees
	// Message type 27 uses lower resolution, 9 bits rather than 12.
	// It encodes degrees rather than normal degrees/10.

	var n = get_field(base, start, length)
	if length == 9 {
		if n == 360 {
			return G_UNKNOWN
		} else {
			return C.float(n)
		}
	} else {
		if n == 3600 {
			return G_UNKNOWN
		} else {
			return C.float(n) * 0.1
		}
	}
}

func get_field_ascii(base []byte, start uint, length uint) int {
	Assert(length == 6)
	var ch = get_field(base, start, length)
	if ch < 32 {
		ch += 64
	}
	return (ch)
}

func get_field_string(base []byte, start uint, length uint) string {
	Assert(length%6 == 0)
	var result string
	var nc = length / 6 // Number of characters.
	// Caller better provide space for at least this +1.
	// No bounds checking here.
	for i := uint(0); i < nc; i++ {
		result += string(rune(get_field_ascii(base, start+i*6, 6)))
	}
	// Officially it should be terminated/padded with @ but we also see trailing spaces.
	result = strings.TrimRight(result, "@ ")

	return result
}

/*-------------------------------------------------------------------
 *
 * Convert between 6 bit values and printable characters used in
 * in the AIS NMEA sentences.
 *
 *--------------------------------------------------------------------*/

// Characters '0' thru 'W'  become values 0 thru 39.
// Characters '`' thru 'w'  become values 40 thru 63.

func char_to_sextet(ch byte) int {
	if ch >= '0' && ch <= 'W' {
		return int(ch - '0')
	} else if ch >= '`' && ch <= 'w' {
		return int(ch - '`' + 40)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid character \"%c\" found in AIS NMEA sentence payload.\n", ch)
		return (0)
	}
}

// Values 0 thru 39 become characters '0' thru 'W'.
// Values 40 thru 63 become characters '`' thru 'w'.
// This is known as "Payload Armoring."

func sextet_to_char(val int) byte {
	if val >= 0 && val <= 39 {
		return byte('0' + val)
	} else if val >= 40 && val <= 63 {
		return byte('`' + val - 40)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Invalid 6 bit value %d from AIS HDLC payload.\n", val)
		return '0'
	}
}

/*-------------------------------------------------------------------
 *
 * Convert AIS binary block (from HDLC frame) to NMEA sentence.
 *
 * In:	AIS binary block
 * Out:	NMEA sentence.
 *
 *--------------------------------------------------------------------*/

func ais_to_nmea(ais []byte) []byte {

	var payload []byte
	// Number of resulting characters for payload.
	var ns = uint(len(ais)*8+5) / 6
	for k := uint(0); k < ns; k++ {
		payload = append(payload, sextet_to_char(get_field(ais, k*6, 6)))
	}

	var nmea = []byte("!AIVDM,1,1,,A,")
	nmea = append(nmea, payload...)

	// If the number of bytes in is not a multiple of 3, this does not
	// produce a whole number of characters out. Extra padding bits were
	// added to get the last character.  Include this number so the
	// decoding application can drop this number of bits from the end.
	// At least, I think that is the way it should work.
	// The examples all have 0.
	var pad_bytes = fmt.Sprintf(",%d", int(ns)*6-len(ais)*8)
	nmea = append(nmea, []byte(pad_bytes)...)

	// Finally the NMEA style checksum.
	var cs byte = 0
	for _, p := range nmea[1:] {
		cs ^= p
	}

	var checksum = fmt.Sprintf("*%02X", cs&0x7f)
	nmea = append(nmea, []byte(checksum)...)

	return nmea
}

/*-------------------------------------------------------------------
 *
 * Name:        ais_parse
 *
 * Purpose:    	Parse AIS sentence and extract interesting parts.
 *
 * Inputs:	sentence	NMEA sentence.
 *
 *		quiet		Suppress printing of error messages.
 *
 * Outputs:	descr		Description of AIS message type.
 *		mssi		9 digit identifier.
 *		odlat		latitude.
 *		odlon		longitude.
 *		ofknots		speed, knots.
 *		ofcourse	direction of travel.
 *		ofalt_m		altitude, meters.
 *		symtab		APRS symbol table.
 *		symbol		APRS symbol code.
 *
 * Returns:	0 for success, -1 for error.
 *
 *--------------------------------------------------------------------*/

// Maximum NMEA sentence length is 82, including CR/LF.
// Make buffer considerably larger to be safe.
const NMEA_MAX_LEN = 240

func ais_parse(sentence *C.char, _quiet C.int, descr *C.char, descr_size C.int, mssi *C.char, mssi_size C.int, odlat *C.double, odlon *C.double,
	ofknots *C.float, ofcourse *C.float, ofalt_m *C.float, symtab *C.char, symbol *C.char, comment *C.char, comment_size C.int) C.int {

	var quiet = _quiet != 0

	C.strcpy(mssi, C.CString("?"))
	*odlat = G_UNKNOWN
	*odlon = G_UNKNOWN
	*ofknots = G_UNKNOWN
	*ofcourse = G_UNKNOWN
	*ofalt_m = G_UNKNOWN

	var stemp = C.GoString(sentence)

	// Verify and remove checksum.

	var calculatedChecksum byte = 0

	for _, p := range stemp[1:] {
		if p == '*' {
			break
		}
		calculatedChecksum ^= byte(p)
	}

	var data, checksumStr, found = strings.Cut(stemp, "*")

	if !found {
		if !quiet {
			text_color_set(DW_COLOR_INFO)
			dw_printf("Missing AIS sentence checksum.\n")
		}
		return (-1)
	}

	var _checksum, _ = strconv.ParseInt(checksumStr, 16, 0)
	var checksum = byte(_checksum)

	if calculatedChecksum != checksum {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("AIS sentence checksum error. Expected %02x but found %s.\n", calculatedChecksum, checksumStr)
		}
		return (-1)
	}

	// Extract the comma separated fields.

	talker, data, _ := strings.Cut(data, ",")     /* Expecting !AIVDM */
	frag_count, data, _ := strings.Cut(data, ",") /* ignored */
	frag_num, data, _ := strings.Cut(data, ",")   /* ignored */
	msg_id, data, _ := strings.Cut(data, ",")     /* ignored */
	radio_chan, data, _ := strings.Cut(data, ",") /* ignored */
	payload, data, _ := strings.Cut(data, ",")    /* Encoded as 6 bits per character. */
	fill_bits, data, _ := strings.Cut(data, ",")  /* Number of bits to discard. */

	/* Suppress the 'set but not used' compiler warnings. */

	_ = data
	_ = talker
	_ = frag_count
	_ = frag_num
	_ = msg_id
	_ = radio_chan

	if len(payload) == 0 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Payload is missing from AIS sentence.\n")
		}
		return (-1)
	}

	// Convert character representation to bit vector.

	var ais = make([]byte, 256)

	for i, b := range payload {
		set_field(ais, uint(i)*6, 6, char_to_sextet(byte(b)))
	}

	// Verify number of filler bits.

	var plen = len(payload)
	var nfill, _ = strconv.Atoi(fill_bits)
	var nbytes = (plen * 6) / 8

	if nfill != plen*6-nbytes*8 {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Number of filler bits is %d when %d is expected.\n",
				nfill, plen*6-nbytes*8)
		}
	}

	// Extract the fields of interest from a few message types.
	// Don't get too carried away.

	var aisType = get_field(ais, 0, 6)

	if aisType >= 1 && aisType <= 27 {
		C.strcpy(mssi, C.CString(fmt.Sprintf("%09d", get_field(ais, 8, 30))))
	}

	switch aisType {

	case 1, 2, 3: // Position Report Class A

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Position Report Class A", aisType)))
		*symtab = '/'
		*symbol = 's' // Power boat (ship) side view
		*odlon = get_field_lon(ais, 61, 28)
		*odlat = get_field_lat(ais, 89, 27)
		*ofknots = get_field_speed(ais, 50, 10)
		*ofcourse = get_field_course(ais, 116, 12)
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	case 4: // Base Station Report

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Base Station Report", aisType)))
		*symtab = '\\'
		*symbol = 'L' // Lighthouse
		//year = get_field(ais, 38, 14);
		//month = get_field(ais, 52, 4);
		//day = get_field(ais, 56, 5);
		//hour = get_field(ais, 61, 5);
		//minute = get_field(ais, 66, 6);
		//second = get_field(ais, 72, 6);
		*odlon = get_field_lon(ais, 79, 28)
		*odlat = get_field_lat(ais, 107, 27)
		// Is this suitable or not?  Doesn't hurt, I suppose.
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	case 5: // Static and Voyage Related Data

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Static and Voyage Related Data", aisType)))
		*symtab = '/'
		*symbol = 's' // Power boat (ship) side view
		{
			var callsign = get_field_string(ais, 70, 42)
			var shipname = get_field_string(ais, 112, 120)
			var destination = get_field_string(ais, 302, 120)
			save_ship_data(C.GoString(mssi), shipname, callsign, destination)
			C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))
		}

	case 9: // Standard SAR Aircraft Position Report

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: SAR Aircraft Position Report", aisType)))
		*symtab = '/'
		*symbol = '\''                             // Small AIRCRAFT
		*ofalt_m = C.float(get_field(ais, 38, 12)) // meters, 4095 means not available
		*odlon = get_field_lon(ais, 61, 28)
		*odlat = get_field_lat(ais, 89, 27)
		*ofknots = get_field_speed(ais, 50, 10) // plane is knots, not knots/10
		if *ofknots != G_UNKNOWN {
			*ofknots *= 10.0
		}
		*ofcourse = get_field_course(ais, 116, 12)
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	case 18: // Standard Class B CS Position Report
		// As an oversimplification, Class A is commercial, B is recreational.

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Standard Class B CS Position Report", aisType)))
		*symtab = '/'
		*symbol = 'Y' // YACHT (sail)
		*odlon = get_field_lon(ais, 57, 28)
		*odlat = get_field_lat(ais, 85, 27)
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	case 19: // Extended Class B CS Position Report

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Extended Class B CS Position Report", aisType)))
		*symtab = '/'
		*symbol = 'Y' // YACHT (sail)
		*odlon = get_field_lon(ais, 57, 28)
		*odlat = get_field_lat(ais, 85, 27)
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	case 27: // Long Range AIS Broadcast message

		C.strcpy(descr, C.CString(fmt.Sprintf("AIS %d: Long Range AIS Broadcast message", aisType)))
		*symtab = '\\'
		*symbol = 's'                       // OVERLAY SHIP/boat (top view)
		*odlon = get_field_lon(ais, 44, 18) // Note: minutes/10 rather than usual /10000.
		*odlat = get_field_lat(ais, 62, 17)
		*ofknots = get_field_speed(ais, 79, 6)   // Note: knots, not deciknots.
		*ofcourse = get_field_course(ais, 85, 9) // Note: degrees, not decidegrees.
		C.strcpy(comment, C.CString(get_ship_data(C.GoString(mssi))))

	default:
		C.strcpy(descr, C.CString(fmt.Sprintf("AIS message type %d", aisType)))
	}

	return (0)

} /* end ais_parse */

/*-------------------------------------------------------------------
 *
 * Name:        ais_check_length
 *
 * Purpose:    	Verify frame length against expected.
 *
 * Inputs:	aisType		Message type, 1 - 27.
 *
 *		length		Number of data octets in in frame.
 *
 * Returns:	-1		Invalid message type.
 *		0		Good length.
 *		1		Unexpected length.
 *
 *--------------------------------------------------------------------*/

func ais_check_length(aisType int, length int) int {
	if aisType >= 1 && aisType < len(ValidAISLengths) {
		var b = length * 8
		if b >= ValidAISLengths[aisType].Min && b <= ValidAISLengths[aisType].Max {
			return (0) // Good.
		} else {
			//text_color_set (DW_COLOR_ERROR);
			//dw_printf("AIS ERROR: type %d, has %d bits when %d to %d expected.\n",
			//	type, b, valid_len[aisType].min, valid_len[aisType].max);
			return (1) // Length out of range.
		}
	} else {
		//text_color_set (DW_COLOR_ERROR);
		//dw_printf("AIS ERROR: message type %d is invalid.\n", aisType);
		return (-1) // Invalid type.
	}

} // end ais_check_length

/*-------------------------------------------------------------------
 *
 * Name:        save_ship_data
 *
 * Purpose:    	Save shipname, etc., from "Static and Voyage Related Data"
 *		so it can be combined later with the position reports.
 *
 * Inputs:	mssi
 *		shipname
 *		callsign
 *		destination
 *
 *--------------------------------------------------------------------*/

type ship_data_s struct {
	pnext       *ship_data_s
	mssi        string
	shipname    string
	callsign    string
	destination string
}

// Just use a single linked list for now.
// If I get ambitious, I might use a hash table.
// I don't think we need a critical region because all channels
// should be serialized thru the receive queue.

var ships *ship_data_s

func save_ship_data(mssi string, shipname string, callsign string, destination string) {
	// Get list node, either existing or new.
	var p = ships
	for p != nil {
		if mssi == p.mssi {
			break
		}
		p = p.pnext
	}

	if p == nil {
		p = new(ship_data_s)
		p.pnext = ships
		ships = p
	}

	p.mssi = mssi
	p.shipname = shipname
	p.callsign = callsign
	p.destination = destination
}

/*-------------------------------------------------------------------
 *
 * Purpose:    	Get ship data for specified mssi.
 *
 * Inputs:	mssi
 *
 * Outputs:	comment	- If mssi is found, return in single string here,
 *			  suitable for the comment field.
 *
 *--------------------------------------------------------------------*/

func get_ship_data(mssi string) string {
	var p = ships
	for p != nil {
		if mssi == p.mssi {
			break
		}
		p = p.pnext
	}
	if p != nil {
		if len(p.destination) > 0 {
			return fmt.Sprintf("%s, %s, dest. %s", p.shipname, p.callsign, p.destination)
		} else {
			return fmt.Sprintf("%s, %s", p.shipname, p.callsign)
		}
	}

	return ""
}
