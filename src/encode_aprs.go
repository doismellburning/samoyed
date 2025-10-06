package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Construct APRS packets from components.
 *
 * Description:
 *
 * References:	APRS Protocol Reference.
 *
 *		Frequency spec.
 *		http://www.aprs.org/info/freqspec.txt
 *
 *---------------------------------------------------------------*/

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
// #include <assert.h>
// #include "encode_aprs.h"
// #include "latlong.h"
// #include "textcolor.h"
import "C"

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unsafe"
)

/*------------------------------------------------------------------
 *
 * Name:        norm_position
 *
 * Purpose:     Fill in the human-readable latitude, longitude,
 * 		symbol part which is common to multiple data formats.
 *
 * Inputs: 	symtab	- Symbol table id or overlay.
 *		symbol	- Symbol id.
 *    		dlat	- Latitude.
 *		dlong	- Longitude.
 *		ambiguity - Blank out least significant digits.
 *
 * Returns:	presult	- Stored here.
 *
 *----------------------------------------------------------------*/

/* Position & symbol fields common to several message formats. */

func normal_position_string(p *position_t) string {
	return fmt.Sprintf("%s%c%s%c", string(p.lat[:]), p.sym_table_id, string(p.lon[:]), p.symbol_code)
}

func normal_position(symtab C.char, symbol C.char, dlat C.double, dlong C.double, ambiguity C.int) *position_t {
	var presult = new(position_t)

	var stemp [16]C.char
	C.latitude_to_str(dlat, ambiguity, &stemp[0])

	C.memcpy(unsafe.Pointer(&presult.lat[0]), unsafe.Pointer(&stemp[0]), C.ulong(len(presult.lat)))

	if symtab != '/' && symtab != '\\' && !unicode.IsDigit(rune(symtab)) && !unicode.IsUpper(rune(symtab)) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Symbol table identifier is not one of / \\ 0-9 A-Z\n")
	}
	presult.sym_table_id = byte(symtab)

	C.longitude_to_str(dlong, ambiguity, &stemp[0])
	C.memcpy(unsafe.Pointer(&presult.lon[0]), unsafe.Pointer(&stemp[0]), C.ulong(len(presult.lon)))

	if symbol < '!' || symbol > '~' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Symbol code is not in range of ! to ~\n")
	}
	presult.symbol_code = byte(symbol)

	return presult
}

/*------------------------------------------------------------------
 *
 * Name:        compressed_position
 *
 * Purpose:     Fill in the compressed latitude, longitude,
 *		symbol part which is common to multiple data formats.
 *
 * Inputs: 	symtab	- Symbol table id or overlay.
 *		symbol	- Symbol id.
 *    		dlat	- Latitude.
 *		dlong	- Longitude.
 *
 * 	 	power	- Watts.
 *		height	- Feet.
 *		gain	- dBi.
 *
 * 		course	- Degrees, 0 - 360 (360 equiv. to 0).
 *			  Use G_UNKNOWN for none or unknown.
 *		speed	- knots.
 *
 *
 * Returns:	presult	- Stored here.
 *
 * Description:	The cst field can have only one of
 *
 *		course/speed	- takes priority (this implementation)
 *		radio range	- calculated from PHG
 *		altitude	- not implemented yet.
 *
 *		Some conversion must be performed for course from
 *		the API definition to what is sent over the air.
 *
 *----------------------------------------------------------------*/

/* Compressed position & symbol fields common to several message formats. */

func compressed_position_string(p *compressed_position_t) string {
	return fmt.Sprintf("%c%s%s%c%c%c%c", p.sym_table_id, string(p.y[:]), string(p.x[:]), p.symbol_code, p.c, p.s, p.t)
}

func compressed_position(symtab C.char, symbol C.char, dlat C.double, dlong C.double,
	power C.int, height C.int, gain C.int,
	course C.int, speed C.int) *compressed_position_t {

	var presult = new(compressed_position_t)

	if symtab != '/' && symtab != '\\' && !unicode.IsDigit(rune(symtab)) && !unicode.IsUpper(rune(symtab)) {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Symbol table identifier is not one of / \\ 0-9 A-Z\n")
	}

	/*
	 * In compressed format, the characters a-j are used for a numeric overlay.
	 * This allows the receiver to distinguish between compressed and normal formats.
	 */
	if unicode.IsDigit(rune(symtab)) {
		symtab = symtab - '0' + 'a'
	}
	presult.sym_table_id = byte(symtab)

	C.latitude_to_comp_str(dlat, (*C.char)(unsafe.Pointer(&presult.y[0])))
	C.longitude_to_comp_str(dlong, (*C.char)(unsafe.Pointer(&presult.x[0])))

	if symbol < '!' || symbol > '~' {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Symbol code is not in range of ! to ~\n")
	}
	presult.symbol_code = byte(symbol)

	/*
	 * The cst field is complicated.
	 *
	 * When c is ' ', the cst field is not used.
	 *
	 * When the t byte has a certain pattern, c & s represent altitude.
	 *
	 * Otherwise, c & s can be either course/speed or radio range.
	 *
	 * When c is in range of '!' to 'z',
	 *
	 * 	('!' - 33) * 4 = 0 degrees.
	 *	...
	 *	('z' - 33) * 4 = 356 degrees.
	 *
	 * In this case, s represents speed ...
	 *
	 * When c is '{', s is range ...
	 */

	if speed > 0 {
		var c C.int

		if course != G_UNKNOWN {
			c = (course + 2) / 4
			if c < 0 {
				c += 90
			}
			if c >= 90 {
				c -= 90
			}
		} else {
			c = 0
		}
		presult.c = byte(c + '!')

		var s = math.Round(math.Log(float64(speed)+1.0) / math.Log(1.08))
		presult.s = byte(s + '!')

		presult.t = 0x26 + '!' /* current, other tracker. */
	} else if power > 0 || height > 0 || gain > 0 {
		presult.c = '{' /* radio range. */

		if power == 0 {
			power = 10
		}
		if height == 0 {
			height = 20
		}
		if gain == 0 {
			gain = 3
		}

		// from protocol reference page 29.
		var _range = math.Sqrt(2.0 * float64(height) * math.Sqrt((float64(power)/10.0)*(float64(gain)/2.0)))

		var s = math.Round(math.Log(_range/2.) / math.Log(1.08))
		if s < 0 {
			s = 0
		}
		if s > 93 {
			s = 93
		}

		presult.s = byte(s + '!')

		presult.t = 0x26 + '!' /* current, other tracker. */
	} else {
		presult.c = ' ' /* cst field not used. */
		presult.s = ' '
		presult.t = '!' /* avoid space. */
	}

	return presult
}

/*------------------------------------------------------------------
 *
 * Name:        phg_data_extension
 *
 * Purpose:     Fill in parts of the power/height/gain data extension.
 *
 * Inputs: 	power	- Watts.
 *		height	- Feet.
 *		gain	- dB.  Protocol spec doesn't mention whether it is dBi or dBd.
 *				This says dBi:
 *				http://www.tapr.org/pipermail/aprssig/2008-September/027034.html

 *		dir	- Directivity: N, NE, etc., omni.
 *
 * Returns:	presult	- Stored here.
 *
 *----------------------------------------------------------------*/

// TODO (bug):  Doesn't check for G_UNKNOWN.
// could have a case where some, but not all, values were specified.
// Callers originally checked for any not zero.
// now they check for any > 0.

type phg_t struct {
	P C.char
	H C.char
	G C.char
	p C.char
	h C.char
	g C.char
	d C.char
}

func phg_data_extension(power C.int, height C.int, gain C.int, _dir *C.char) string {
	var p = math.Round(math.Sqrt(float64(power))) + '0'
	if p < '0' {
		p = '0'
	} else if p > '9' {
		p = '9'
	}

	var h = math.Round(math.Log2(float64(height)/10.0)) + '0'
	if h < '0' {
		h = '0'
	}
	/* Result can go beyond '9'. */

	var g = float64(gain + '0')
	if g < '0' {
		g = '0'
	} else if g > '9' {
		g = '0'
	}

	var d = '0'
	var dir = C.GoString(_dir)
	if dir != "" {
		if strings.EqualFold(dir, "NE") {
			d = '1'
		} else if strings.EqualFold(dir, "E") {
			d = '2'
		} else if strings.EqualFold(dir, "SE") {
			d = '3'
		} else if strings.EqualFold(dir, "S") {
			d = '4'
		} else if strings.EqualFold(dir, "SW") {
			d = '5'
		} else if strings.EqualFold(dir, "W") {
			d = '6'
		} else if strings.EqualFold(dir, "NW") {
			d = '7'
		} else if strings.EqualFold(dir, "N") {
			d = '8'
		}
	}

	return fmt.Sprintf("PHG%c%c%c%c", C.char(p), C.char(h), C.char(g), d)
}

/*------------------------------------------------------------------
 *
 * Name:        cse_spd_data_extension
 *
 * Purpose:     Fill in parts of the course & speed data extension.
 *
 * Inputs: 	course	- Degrees, 0 - 360 (360 equiv. to 0).
 *			  Use G_UNKNOWN for none or unknown.
 *
 *		speed	- knots.
 *
 * Returns:	presult	- Stored here.
 *
 * Description: Over the air we use:
 *			0 	for unknown or not relevant.
 *			1 - 360	for valid course.  (360 for north)
 *
 *----------------------------------------------------------------*/

func cse_spd_data_extension(course C.int, speed C.int) string {

	var cse C.int
	if course != G_UNKNOWN {
		cse = course
		for cse < 1 {
			cse += 360
		}
		for cse > 360 {
			cse -= 360
		}
		// Should now be in range of 1 - 360. */
		// Original value of 0 for north is transmitted as 360. */
	} else {
		cse = 0
	}

	var spd = speed
	if spd < 0 {
		spd = 0 // would include G_UNKNOWN
	}
	if spd > 999 {
		spd = 999
	}

	return fmt.Sprintf("%03d/%03d", cse, spd)
}

/*------------------------------------------------------------------
 *
 * Name:        frequency_spec
 *
 * Purpose:     Put frequency specification in beginning of comment field.
 *
 * Inputs: 	freq	- MHz.
 *		tone	- Hz.
 *		offset	- MHz.
 *
 * Returns:     Result
 *
 * Description:	There are several valid variations.
 *
 *		The frequency could be missing here if it is in the
 *		object name.  In this case we could have tone & offset.
 *
 *		Offset must always be preceded by tone.
 *
 *		Resulting formats are all fixed width and have a trailing space:
 *
 *			"999.999MHz "
 *			"T999 "
 *			"+999 "			(10 kHz units)
 *
 * Reference:	http://www.aprs.org/info/freqspec.txt
 *
 *----------------------------------------------------------------*/

func frequency_spec(freq C.float, tone C.float, offset C.float) string {
	var result string

	if freq > 0 {
		/* TODO: Should use letters for > 999.999. */
		/* For now, just be sure we have proper field width. */

		if freq > 999.999 {
			freq = 999.999
		}

		result += fmt.Sprintf("%07.3fMHz ", freq)
	}

	if tone != G_UNKNOWN {
		if tone == 0 {
			result += "Toff "
		} else {
			result += fmt.Sprintf("T%03d ", int(tone))
		}
	}

	if offset != G_UNKNOWN {
		result += fmt.Sprintf("%+04d ", int(math.Round(float64(offset)*100)))
	}

	return result
}

/*------------------------------------------------------------------
 *
 * Name:        encode_position
 *
 * Purpose:     Construct info part for position report format.
 *
 * Inputs:      messaging - This determines whether the data type indicator
 *			   is set to '!' (false) or '=' (true).
 *		compressed - Send in compressed form?
 *		lat	- Latitude.
 *		lon	- Longitude.
 *		ambiguity - Number of digits to omit from location.
 *		alt_ft	- Altitude in feet.
 *		symtab	- Symbol table id or overlay.
 *		symbol	- Symbol id.
 *
 * 	 	power	- Watts.
 *		height	- Feet.
 *		gain	- dB.  Not clear if it is dBi or dBd.
 *		dir	- Directivity: N, NE, etc., omni.
 *
 *		course	- Degrees, 0 - 360 (360 equiv. to 0).
 *			  Use G_UNKNOWN for none or unknown.
 *		speed	- knots.		// TODO:  should distinguish unknown(not revevant) vs. known zero.
 *
 * 	 	freq	- MHz.
 *		tone	- Hz.
 *		offset	- MHz.
 *
 *		comment	- Additional comment text.
 *
 * Returns:	result	- Should be at least ??? bytes.
 *				Could get into hundreds of characters
 *				because it includes the comment.
 *
 * Description:	There can be a single optional "data extension"
 *		following the position so there is a choice
 *		between:
 *			Power/height/gain/directivity or
 *			Course/speed.
 *
 *		After that,
 *
 *----------------------------------------------------------------*/

type aprs_ll_pos_t struct {
	dti C.char /* ! or = */
	pos position_t
	/* Comment up to 43 characters. */
	/* Start of comment could be data extension(s). */
}

type aprs_compressed_pos_t struct {
	dti  C.char /* ! or = */
	cpos compressed_position_t
	/* Comment up to 40 characters. */
	/* No data extension allowed for compressed location. */
}

func encode_position(messaging C.int, compressed C.int, lat C.double, lon C.double, ambiguity C.int, alt_ft C.int,
	symtab C.char, symbol C.char,
	power C.int, height C.int, gain C.int, dir *C.char,
	course C.int, speed C.int,
	freq C.float, tone C.float, offset C.float,
	comment *C.char) string {
	var result string

	if compressed > 0 {
		// Thought:
		// https://groups.io/g/direwolf/topic/92718535#6886
		// When speed is zero, we could put the altitude in the compressed
		// position rather than having /A=999999.
		// However, the resolution would be decreased and that could be important
		// when hiking in hilly terrain.  It would also be confusing to
		// flip back and forth between two different representations.

		var dti = '!'
		if messaging > 0 {
			dti = '='
		}
		var c = compressed_position(symtab, symbol, lat, lon,
			power, height, gain,
			course, speed)

		result = string(dti) + compressed_position_string(c)
	} else {
		var dti = '!'
		if messaging > 0 {
			dti = '='
		}
		var n = normal_position(symtab, symbol, lat, lon, ambiguity)
		result = string(dti) + normal_position_string(n)

		/* Optional data extension. (singular) */
		/* Can't have both course/speed and PHG.  Former gets priority. */

		if course != G_UNKNOWN || speed > 0 {
			var cse = cse_spd_data_extension(course, speed)
			result += cse
		} else if power > 0 || height > 0 || gain > 0 {
			var phg = phg_data_extension(power, height, gain, dir)
			result += phg
		}
	}

	/* Optional frequency spec. */

	if freq != 0 || tone != 0 || offset != 0 {
		var fs = frequency_spec(freq, tone, offset)
		result += fs
	}

	/* Altitude.  Can be anywhere in comment. */
	// Officially, altitude must be six digits.
	// What about all the places on the earth's surface that are below sea level?
	// https://en.wikipedia.org/wiki/List_of_places_on_land_with_elevations_below_sea_level

	// The MIC-E format allows negative altitudes; not allowing it for /A=123456 seems to be an oversight.
	// Most modern applications recognize the form /A=-12345 with minus and five digits.
	// This maintains the same total field width and the range is more than adequate.

	if alt_ft != G_UNKNOWN {
		/* Not clear if altitude can be negative. */
		/* Be sure it will be converted to 6 digits. */
		// if (alt_ft < 0) alt_ft = 0;
		if alt_ft < -99999 {
			alt_ft = -99999
		}
		if alt_ft > 999999 {
			alt_ft = 999999
		}
		result += fmt.Sprintf("/A=%06d", alt_ft) // /A=123456 ot /A=-12345
	}

	/* Finally, comment text. */

	if comment != nil {
		result += C.GoString(comment)
	}

	return result
} /* end encode_position */

/*------------------------------------------------------------------
 *
 * Name:        encode_object
 *
 * Purpose:     Construct info part for object report format.
 *
 * Inputs:      name	- Name, up to 9 characters.
 *		compressed - Send in compressed form?
 *		thyme	- Time stamp or 0 for none.
 *		lat	- Latitude.
 *		lon	- Longitude.
 *		ambiguity - Number of digits to omit from location.
 *		symtab	- Symbol table id or overlay.
 *		symbol	- Symbol id.
 *
 * 	 	power	- Watts.
 *		height	- Feet.
 *		gain	- dB.  Not clear if it is dBi or dBd.
 *		dir	- Direction: N, NE, etc., omni.
 *
 *		course	- Degrees, 0 - 360 (360 equiv. to 0).
 *			  Use G_UNKNOWN for none or unknown.
 *		speed	- knots.
 *
 * 	 	freq	- MHz.
 *		tone	- Hz.
 *		offset	- MHz.
 *
 *		comment	- Additional comment text.
 *
 * Returns:	result	- Should be at least ??? characters.
 *				36 for fixed part,
 *				7 for optional extended data,
 *				~20 for freq, etc.,
 *				comment could be very long...
 *
 *----------------------------------------------------------------*/

type aprs_object_t struct {
	o struct {
		dti         rune /* ; */
		name        [9]rune
		live_killed rune /* * for live or _ for killed */
		time_stamp  [7]rune
	}
	u struct { // TODO KG Was union
		pos  position_t            /* Up to 43 char comment.  First 7 bytes could be data extension. */
		cpos compressed_position_t /* Up to 40 char comment.  No PHG data extension in this case. */
	}
}

func encode_object(name *C.char, compressed C.int, thyme C.time_t, lat C.double, lon C.double, ambiguity C.int,
	symtab C.char, symbol C.char,
	power C.int, height C.int, gain C.int, dir *C.char,
	course C.int, speed C.int,
	freq C.float, tone C.float, offset C.float, comment *C.char) string {

	var dti = ';'
	var liveKilled = '*'

	var timestamp string
	if thyme != 0 {
		var tm = time.Unix(int64(thyme), 0)
		timestamp = tm.Format("020304z")
	} else {
		timestamp = "111111z"
	}

	var result = fmt.Sprintf("%c%-9.9s%c%-7.7s", dti, C.GoString(name), liveKilled, timestamp)

	if compressed > 0 {
		result += compressed_position_string(compressed_position(symtab, symbol, lat, lon,
			power, height, gain,
			course, speed))
	} else {
		result += normal_position_string(normal_position(symtab, symbol, lat, lon, ambiguity))

		/* Optional data extension. (singular) */
		/* Can't have both course/speed and PHG.  Former gets priority. */

		if course != G_UNKNOWN || speed > 0 {
			result += cse_spd_data_extension(course, speed)
		} else if power > 0 || height > 0 || gain > 0 {
			result += phg_data_extension(power, height, gain, dir)
		}

	}

	/* Optional frequency spec. */

	if freq != 0 || tone != 0 || offset != 0 {
		result += frequency_spec(freq, tone, offset)
	}

	/* Finally, comment text. */

	if comment != nil {
		result += C.GoString(comment)
	}

	return result
} /* end encode_object */

/*------------------------------------------------------------------
 *
 * Name:        encode_message
 *
 * Purpose:     Construct info part for APRS "message" format.
 *
 * Inputs:      addressee	- Addressed to, up to 9 characters.
 *		text		- Text part of the message.
 *		id		- Identifier, 0 to 5 characters.
 *
 * Returns:	presult	- Stored here.
 *
 * Description:
 *
 *----------------------------------------------------------------*/

func encode_message(addressee *C.char, text *C.char, _id *C.char) string {
	var result = fmt.Sprintf(":%-9.9s:%s", C.GoString(addressee), C.GoString(text))

	var id = C.GoString(_id)
	if len(id) > 0 {
		result += "{" + id
	}

	return result
}
