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
// #include "textcolor.h"
// #include "ais.h"
import "C"

// Lengths, in bits, for the AIS message types.

/* FIXME KG 
static const struct {
	short min;
	short max;
} valid_len[NUM_TYPES+1] = {
	{ -1, -1 },		// 0	not used
	{ 168, 168 },		// 1
	{ 168, 168 },		// 2
	{ 168, 168 },		// 3
	{ 168, 168 },		// 4
	{ 424, 424 },		// 5
	{ 72, 1008 },		// 6	multipurpose
	{ 72, 168 },		// 7	increments of 32 bits
	{ 168, 1008 },		// 8	multipurpose
	{ 168, 168 },		// 9
	{ 72, 72 },		// 10
	{ 168, 168 },		// 11
	{ 72, 1008 },		// 12
	{ 72, 168 },		// 13	increments of 32 bits
	{ 40, 1008 },		// 14
	{ 88, 160 },		// 15
	{ 96, 114 },		// 16	96 or 114, not range
	{ 80, 816 },		// 17
	{ 168, 168 },		// 18
	{ 312, 312 },		// 19
	{ 72, 160 },		// 20
	{ 272, 360 },		// 21
	{ 168, 168 },		// 22
	{ 160, 160 },		// 23
	{ 160, 168 },		// 24
	{ 40, 168 },		// 25
	{ 60, 1064 },		// 26
	{ 96, 168 }		// 27	96 or 168, not range
};
*/



/*-------------------------------------------------------------------
 *
 * Functions to get and set element of a bit vector.
 *
 *--------------------------------------------------------------------*/

var mask = []byte{ 0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01 };

func get_bit (base byte,  offset byte) bool {
	return ( (base[offset >> 3] & mask[offset & 0x7]) != 0);
}

func set_bit (base *byte, offset byte, val bool) {
	if (val) {
	  base[offset >> 3] |= mask[offset & 0x7];
	} else {
	  base[offset >> 3] &= ~ mask[offset & 0x7];
	}
}


/*-------------------------------------------------------------------
 *
 * Extract a variable length field from a bit vector.
 *
 *--------------------------------------------------------------------*/

func get_field (base *byte, start int, length int) int {
	var result = 0;
	for k := 0; k < length; k++ {
	  result <<= 1;
	  result |= get_bit (base, start + k);
	}
	return (result);
}

func set_field (base *byte, start int, length int, val int) {
	for k := 0; k < length; k++ {
	  set_bit (base, start + k, (val >> (length - 1 - k) ) & 1);
	}
}


func get_field_signed (base *byte, start int, length int) int {
	var result = int( get_field(base, start, length))
	// Sign extend.
	result <<= (32 - length);
	result >>= (32 - length);
	return (result);
}

func get_field_lat (base *byte, start int, length int) C.double {
	// Latitude of 0x3412140 (91 deg) means not available.
	// Message type 27 uses lower resolution, 17 bits rather than 27.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field_signed(base, start, length);
	if (length == 17) {
		if (n == 91*600) {
			return  G_UNKNOWN 
		} else {
			return C.double(n) / 600.0;
		}
	} else {
	  if (n == 91*600000)  {
	  	return G_UNKNOWN 
	} else {
		return C.double(n) / 600000.0;
	}
	}
}

func get_field_lon (base *byte, start int, length int) C.double {
	// Longitude of 0x6791AC0 (181 deg) means not available.
	// Message type 27 uses lower resolution, 18 bits rather than 28.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field_signed(base, start, length);
	if (length == 18) {
	  if (n == 181*600) {
		 	return G_UNKNOWN 
		} else {
			return C.double(n) / 600.0;
		}
	} else {
	  if (n == 181*600000) {
		  return G_UNKNOWN 
	  } else {
		  return C.double(n) / 600000.0;
	  }
	}
}

func get_field_speed (base *byte, start int, length int) C.float {
	// Raw 1023 means not available.
	// Multiply by 0.1 to get knots.
	// For aircraft it is knots, not deciknots.

	// Message type 27 uses lower resolution, 6 bits rather than 10.
	// It encodes minutes/10 rather than normal minutes/10000.

	var n = get_field(base, start, length);
	if (length == 6) {
	  if (n == 63)  {
return  G_UNKNOWN 
} else {
return  C.float(n)
}
	} else {
	  if (n == 1023)  {
return  G_UNKNOWN 
} else {
return  C.float(n) * 0.1
}
	}
}

func get_field_course (base *byte, start int, length int) C.float {
	// Raw 3600 means not available.
	// Multiply by 0.1 to get degrees
	// Message type 27 uses lower resolution, 9 bits rather than 12.
	// It encodes degrees rather than normal degrees/10.

	var n = get_field(base, start, length);
	if (length == 9) {
	  if (n == 360)  {
return  G_UNKNOWN 
} else {
return  C.float(n)
}
	} else {
	  if (n == 3600)  {
return  G_UNKNOWN 
} else {
return  C.float(n) * 0.1
}
	}
}

func get_field_ascii (base *byte, start int, length int) int {
	Assert (length == 6);
	var ch = get_field(base, start, length);
	if (ch < 32) {
		ch += 64;
	}
	return (ch);
}

func get_field_string (base *byte, start int, length int, result *C.char) {
	Assert (length % 6 == 0);
	var nc = length / 6;	// Number of characters.
				// Caller better provide space for at least this +1.
				// No bounds checking here.
				for i := 0; i < nc; i++ {
	  result[i] = get_field_ascii (base, start + i * 6, 6);
	}
	result[nc] = 0;
	// Officially it should be terminated/padded with @ but we also see trailing spaces.
	var p = strchr(result, '@');
	if (p != nil) {
		p = 0
	}
	for k := strlen(result) - 1; k >= 0 && result[k] == ' '; k-- {
	  result[k] = 0;
	}
}



/*-------------------------------------------------------------------
 *
 * Convert between 6 bit values and printable characters used in 
 * in the AIS NMEA sentences.
 *
 *--------------------------------------------------------------------*/

// Characters '0' thru 'W'  become values 0 thru 39.
// Characters '`' thru 'w'  become values 40 thru 63.

func char_to_sextet (ch byte) int {
	if (ch >= '0' && ch <= 'W') {
	  return (ch - '0');
	} else if (ch >= '`' && ch <= 'w') {
	  return (ch - '`' + 40);
	} else {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Invalid character \"%c\" found in AIS NMEA sentence payload.\n", ch);
	  return (0);
	}
}


// Values 0 thru 39 become characters '0' thru 'W'.
// Values 40 thru 63 become characters '`' thru 'w'.
// This is known as "Payload Armoring."

func sextet_to_char (val int) byte {
	if (val >= 0 && val <= 39) {
	  return ('0' + val);
	} else if (val >= 40 && val <= 63) {
	  return ('`' + val - 40);
	} else {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("Invalid 6 bit value %d from AIS HDLC payload.\n", val);
	  return ('0');
	}
}


/*-------------------------------------------------------------------
 *
 * Convert AIS binary block (from HDLC frame) to NMEA sentence. 
 *
 * In:	Pointer to AIS binary block and number of bytes.
 * Out:	NMEA sentence.  Provide size to avoid string overflow.
 *
 *--------------------------------------------------------------------*/

func ais_to_nmea (ais *C.char, ais_len C.int, nmea *C.char, nmea_size C.int) {

	var payload[256]C.char
	// Number of resulting characters for payload.
	var ns = (ais_len * 8 + 5) / 6;
	if (ns+1 > len(payload)) {
	  text_color_set(DW_COLOR_ERROR);
	  dw_printf ("AIS HDLC payload of %d bytes is too large.\n", ais_len);
	  ns = sizeof(payload) - 1;
	}
	for k := 0; k < ns; k++ {
	  payload[k] = sextet_to_char(get_field(ais, k*6, 6));
	}
	payload[ns] = 0;

	C.strlcpy (nmea, "!AIVDM,1,1,,A,", nmea_size);
	C.strlcat (nmea, payload, nmea_size);

	// If the number of bytes in is not a multiple of 3, this does not
	// produce a whole number of characters out. Extra padding bits were
	// added to get the last character.  Include this number so the
	// decoding application can drop this number of bits from the end.
	// At least, I think that is the way it should work.
	// The examples all have 0.
	var pad_bytes = fmt.Sprintf(",%d", ns * 6 - ais_len * 8);
	C.strlcat (nmea, C.CString(pad_bits), nmea_size);

	// Finally the NMEA style checksum.
	var cs = 0;
	for p := nmea + 1; *p != 0; p++ {
	  cs ^= *p;
	}

	var checksum = fmt.Sprintf("*%02X", cs & 0x7f);
	C.strlcat (nmea, C.CString(checksum), nmea_size);
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

func ais_parse (sentence *C.char, quiet C.int, descr *C.char, descr_size C.int, mssi *C.char, mssi_size C.int, odlat *C.double, odlon *C.double,
			ofknots *C.float, ofcourse *C.float, ofalt_m *C.float, symtab *C.char, symbol *C.char, comment *C.char, comment_size C.int) C.int {

	C.strlcpy (mssi, "?", mssi_size);
	*odlat = G_UNKNOWN;
	*odlon = G_UNKNOWN;
	*ofknots = G_UNKNOWN;
	*ofcourse = G_UNKNOWN;
	*ofalt_m = G_UNKNOWN;

	var stemp = C.GoBytes(unsafe.Pointer(sentence), C.strlen(sentence))

// Verify and remove checksum.

        var cs C.uchar = 0;
        var p *C.char

        for p = stemp+1; *p != '*' && *p != 0; p++ {
          cs ^= *p;
        }

        p = strchr (stemp, '*');
        if (p == nil) {
	  if ( ! quiet) {
	    text_color_set (DW_COLOR_INFO);
            dw_printf("Missing AIS sentence checksum.\n");
	  }
          return (-1);
        }
        if (cs != strtoul(p+1, nil, 16)) {
	  if ( ! quiet) {
	    text_color_set (DW_COLOR_ERROR);
            dw_printf("AIS sentence checksum error. Expected %02x but found %s.\n", cs, p+1);
	  }
          return (-1);
        }
        *p = 0;      // Remove the checksum.

// Extract the comma separated fields.

	var next *C.char

	var talker *C.char			/* Expecting !AIVDM */
	var frag_count *C.char		/* ignored */
	var frag_num *C.char			/* ignored */
	var msg_id *C.char			/* ignored */
	var radio_chan *C.char		/* ignored */
	var payload *C.char			/* Encoded as 6 bits per character. */
	var fill_bits *C.char		/* Number of bits to discard. */

	next = stemp;
	talker = strsep(&next, ",");
	frag_count = strsep(&next, ",");
	frag_num = strsep(&next, ",");	
	msg_id = strsep(&next, ",");
	radio_chan = strsep(&next, ",");
	payload = strsep(&next, ",");
	fill_bits = strsep(&next, ",");

	/* Suppress the 'set but not used' compiler warnings. */
	/* Alternatively, we might use __attribute__((unused)) */

	/* FIXME KG
	(void)(talker);
	(void)(frag_count);
	(void)(frag_num);
	(void)(msg_id);
	(void)(radio_chan);
	*/

	if (payload == nil || strlen(payload) == 0) {
	  if ( ! quiet) {
	    text_color_set (DW_COLOR_ERROR);
            dw_printf("Payload is missing from AIS sentence.\n");
	  }
	  return (-1);
	}

// Convert character representation to bit vector.
	
	var ais[256]C.uchar
	memset (ais, 0, sizeof(ais));

	var plen = strlen(payload);

	for k := 0; k < plen; k++ {
	  set_field (ais, k*6, 6, char_to_sextet(payload[k]));
	}

// Verify number of filler bits.

	var nfill = atoi(fill_bits);
	var nbytes = (plen * 6) / 8;

	if (nfill != plen * 6 - nbytes * 8) {
	  if ( ! quiet) {
	    text_color_set (DW_COLOR_ERROR);
            dw_printf("Number of filler bits is %d when %d is expected.\n",
			nfill, plen * 6 - nbytes * 8);
	  }
	}


// Extract the fields of interest from a few message types.
// Don't get too carried away.

	var aisType = get_field(ais, 0, 6);

	if (aisType >= 1 && aisType <= 27) {
	  snprintf (mssi, mssi_size, "%09d", get_field(ais, 8, 30));
	}
	switch (aisType) {

	  case 1:	// Position Report Class A
	  case 2:
	  case 3:

	    snprintf (descr, descr_size, "AIS %d: Position Report Class A", aisType);
	    *symtab = '/';
	    *symbol = 's';		// Power boat (ship) side view
	    *odlon = get_field_lon(ais, 61, 28);
	    *odlat = get_field_lat(ais, 89, 27);
	    *ofknots = get_field_speed(ais, 50, 10);
	    *ofcourse = get_field_course(ais, 116, 12);
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  case 4:	// Base Station Report

	    snprintf (descr, descr_size, "AIS %d: Base Station Report", aisType);
	    *symtab = '\\';
	    *symbol = 'L';		// Lighthouse
	    //year = get_field(ais, 38, 14);
	    //month = get_field(ais, 52, 4);
	    //day = get_field(ais, 56, 5);
	    //hour = get_field(ais, 61, 5);
	    //minute = get_field(ais, 66, 6);
	    //second = get_field(ais, 72, 6);
	    *odlon = get_field_lon(ais, 79, 28);
	    *odlat = get_field_lat(ais, 107, 27);
	    // Is this suitable or not?  Doesn't hurt, I suppose.
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  case 5:	// Static and Voyage Related Data

	    snprintf (descr, descr_size, "AIS %d: Static and Voyage Related Data", aisType);
	    *symtab = '/';
	    *symbol = 's';		// Power boat (ship) side view
	    {
	      var callsign[12]C.char
	      var shipname[24]C.char
	      var destination[24]C.char
	      get_field_string(ais, 70, 42, callsign);
	      get_field_string(ais, 112, 120, shipname);
	      get_field_string(ais, 302, 120, destination);
	      save_ship_data(mssi, shipname, callsign, destination);
	      get_ship_data(mssi, comment, comment_size);
	    }
	    break;


	  case 9:	// Standard SAR Aircraft Position Report

	    snprintf (descr, descr_size, "AIS %d: SAR Aircraft Position Report", aisType);
	    *symtab = '/';
	    *symbol = '\'';		// Small AIRCRAFT
	    *ofalt_m = get_field(ais, 38, 12);		// meters, 4095 means not available
	    *odlon = get_field_lon(ais, 61, 28);
	    *odlat = get_field_lat(ais, 89, 27);
	    *ofknots = get_field_speed(ais, 50, 10);	// plane is knots, not knots/10
	    if (*ofknots != G_UNKNOWN) {
			*ofknots = *ofknots * 10.0;
		}
	    *ofcourse = get_field_course(ais, 116, 12);
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  case 18:	// Standard Class B CS Position Report
			// As an oversimplification, Class A is commercial, B is recreational.

	    snprintf (descr, descr_size, "AIS %d: Standard Class B CS Position Report", aisType);
	    *symtab = '/';
	    *symbol = 'Y';		// YACHT (sail)
	    *odlon = get_field_lon(ais, 57, 28);
	    *odlat = get_field_lat(ais, 85, 27);
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  case 19:	// Extended Class B CS Position Report

	    snprintf (descr, descr_size, "AIS %d: Extended Class B CS Position Report", aisType);
	    *symtab = '/';
	    *symbol = 'Y';		// YACHT (sail)
	    *odlon = get_field_lon(ais, 57, 28);
	    *odlat = get_field_lat(ais, 85, 27);
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  case 27:	// Long Range AIS Broadcast message

	    snprintf (descr, descr_size, "AIS %d: Long Range AIS Broadcast message", aisType);
	    *symtab = '\\';
	    *symbol = 's';		// OVERLAY SHIP/boat (top view)
	    *odlon = get_field_lon(ais, 44, 18);	// Note: minutes/10 rather than usual /10000.
	    *odlat = get_field_lat(ais, 62, 17);
	    *ofknots = get_field_speed(ais, 79, 6);	// Note: knots, not deciknots.
	    *ofcourse = get_field_course(ais, 85, 9);	// Note: degrees, not decidegrees.
	    get_ship_data(mssi, comment, comment_size);
	    break;

	  default:
	    snprintf (descr, descr_size, "AIS message type %d", aisType);
	    break;
	}

	return (0);

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

func ais_check_length (aisType C.int, length C.int) C.int {
	if (aisType >= 1 && aisType <= NUM_TYPES) {
	  var b = length * 8;
	  if (b >= valid_len[aisType].min && b <= valid_len[aisType].max) {
	    return (0);		// Good.
	  } else {
	    //text_color_set (DW_COLOR_ERROR);
            //dw_printf("AIS ERROR: type %d, has %d bits when %d to %d expected.\n",
	    //	type, b, valid_len[aisType].min, valid_len[aisType].max);
	    return (1);		// Length out of range.
	  }
	} else {
	  //text_color_set (DW_COLOR_ERROR);
          //dw_printf("AIS ERROR: message type %d is invalid.\n", aisType);
	  return (-1);		// Invalid type.
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
	pnext *ship_data_s
	mssi[9+1]C.char
	shipname[20+1]C.char
	callsign[7+1]C.char
	destination[20+1]C.char
};

// Just use a single linked list for now.
// If I get ambitious, I might use a hash table.
// I don't think we need a critical region because all channels
// should be serialized thru the receive queue.

var ships *ship_data_s


func save_ship_data(mssi *C.char, shipname *C.char, callsign *C.char, destination *C.char) {
	// Get list node, either existing or new.
	var p = ships;
	for (p != nil) {
	  if (strcmp(mssi, p.mssi) == 0) {
	    break;
	  }
	  p = p.pnext;
	}

	if (p == nil) {
	  p = new(ship_data_s)
	  p.pnext = ships;
	  ships = p;
	}

	strlcpy (p.mssi, mssi, sizeof(p.mssi));
	strlcpy (p.shipname, shipname, sizeof(p.shipname));
	strlcpy (p.callsign, callsign, sizeof(p.callsign));
	strlcpy (p.destination, destination, sizeof(p.destination));
}

/*-------------------------------------------------------------------
 *
 * Name:        save_ship_data
 *
 * Purpose:    	Get ship data for specified mssi.
 *
 * Inputs:	mssi
 *
 * Outputs:	comment	- If mssi is found, return in single string here,
 *			  suitable for the comment field.
 *
 *--------------------------------------------------------------------*/

func get_ship_data(mssi *C.char, comment *C.char, comment_size C.int) {
	var p = ships;
	for (p != nil) {
	  if (strcmp(mssi, p.mssi) == 0) {
	    break;
	  }
	  p = p.pnext;
	}
	if (p != nil) {
	  if (strlen(p.destination) > 0) {
	    snprintf (comment, comment_size, "%s, %s, dest. %s", p.shipname, p.callsign, p.destination);
	  } else {
	    snprintf (comment, comment_size, "%s, %s", p.shipname, p.callsign);
	  }
	}
}


// end ais.c
