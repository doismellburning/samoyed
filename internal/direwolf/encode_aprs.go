package direwolf

// Construct APRS packets from components.
//
// References: APRS Protocol Reference, and the frequency spec at
// http://www.aprs.org/info/freqspec.txt

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

// checkSymbol complains about a symbol table identifier or symbol code that
// isn't valid APRS.  The position is still encoded, as Dire Wolf always did.
func checkSymbol(symtab byte, symbol byte) {
	if symtab != '/' && symtab != '\\' && !unicode.IsDigit(rune(symtab)) && !unicode.IsUpper(rune(symtab)) {
		logrus.WithField("symtab", string(symtab)).Error("Symbol table identifier is not one of / \\ 0-9 A-Z")
	}

	if symbol < '!' || symbol > '~' {
		logrus.WithField("symbol", string(symbol)).Error("Symbol code is not in range of ! to ~")
	}
}

// normal_position_string renders a position from normal_position.
func normal_position_string(p *position_t) string {
	return fmt.Sprintf("%s%c%s%c", string(p.Lat[:]), p.SymTableId, string(p.Lon[:]), p.SymbolCode)
}

// normal_position fills in the human-readable latitude, longitude and symbol
// part which is common to multiple data formats.
//
// symtab is the symbol table id or overlay, symbol the symbol id, and
// ambiguity the number of least significant digits to blank out.
func normal_position(symtab byte, symbol byte, dlat float64, dlong float64, ambiguity int) *position_t {
	var pos = new(position_t)

	checkSymbol(symtab, symbol)

	copy(pos.Lat[:], latlong.LatitudeToString(dlat, ambiguity))

	pos.SymTableId = symtab

	copy(pos.Lon[:], latlong.LongitudeToString(dlong, ambiguity))

	pos.SymbolCode = symbol

	return pos
}

// compressed_position_string renders a position from compressed_position.
func compressed_position_string(p *compressed_position_t) string {
	return fmt.Sprintf("%c%s%s%c%c%c%c", p.SymTableId, string(p.Y[:]), string(p.X[:]), p.SymbolCode, p.C, p.S, p.T)
}

// compressed_position fills in the compressed latitude, longitude and symbol
// part which is common to multiple data formats.
//
// power is in watts, height in feet, gain in dBi, course in degrees (0 - 360,
// 360 equivalent to 0) and speed in knots.
//
// The cst field can have only one of
//
//   - course/speed - takes priority (this implementation)
//   - radio range - calculated from PHG
//   - altitude - not implemented yet.
//
// Some conversion must be performed for course from the API definition to
// what is sent over the air.
func compressed_position(symtab byte, symbol byte, dlat float64, dlong float64,
	power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int],
	course maybe.Maybe[int], speed maybe.Maybe[int]) *compressed_position_t {
	var pos = new(compressed_position_t)

	checkSymbol(symtab, symbol)

	// In compressed format, the characters a-j are used for a numeric overlay.
	// This allows the receiver to distinguish between compressed and normal formats.
	if unicode.IsDigit(rune(symtab)) {
		symtab = symtab - '0' + 'a'
	}

	pos.SymTableId = symtab

	copy(pos.Y[:], latlong.LatitudeToCompressedString(dlat))
	copy(pos.X[:], latlong.LongitudeToCompressedString(dlong))

	pos.SymbolCode = symbol

	// The cst field is complicated.
	//
	// When c is ' ', the cst field is not used.
	//
	// When the t byte has a certain pattern, c & s represent altitude.
	//
	// Otherwise, c & s can be either course/speed or radio range.
	//
	// When c is in range of '!' to 'z',
	//
	//	('!' - 33) * 4 = 0 degrees.
	//	...
	//	('z' - 33) * 4 = 356 degrees.
	//
	// In this case, s represents speed ...
	//
	// When c is '{', s is range ...

	// Only one of power, height and gain needs to have been given.  An absent
	// one counts as zero, which the radio range calculation below replaces
	// with its own default.
	var p = maybe.FromMaybe(0, power)
	var h = maybe.FromMaybe(0, height)
	var g = maybe.FromMaybe(0, gain)
	var knots = maybe.FromMaybe(0, speed)

	if knots > 0 {
		var c int

		if degrees, known := course.Get(); known {
			// Into 0 - 359 first, as Go's division truncates toward zero.
			c = (((degrees%360)+360)%360 + 2) / 4 % 90
		}

		pos.C = byte(c + '!')

		var s = min(math.Round(math.Log(float64(knots)+1.0)/math.Log(1.08)), 93)
		pos.S = byte(s + '!')

		pos.T = 0x26 + '!' // current, other tracker.
	} else if p > 0 || h > 0 || g > 0 {
		pos.C = '{' // radio range.

		if p == 0 {
			p = 10
		}

		if h == 0 {
			h = 20
		}

		if g == 0 {
			g = 3
		}

		// from protocol reference page 29.
		var rangeMiles = math.Sqrt(2.0 * float64(h) * math.Sqrt((float64(p)/10.0)*(float64(g)/2.0)))

		var s = min(max(math.Round(math.Log(rangeMiles/2.)/math.Log(1.08)), 0), 93)

		pos.S = byte(s + '!')

		pos.T = 0x26 + '!' // current, other tracker.
	} else {
		pos.C = ' ' // cst field not used.
		pos.S = ' '
		pos.T = '!' // avoid space.
	}

	return pos
}

// phg_data_extension returns the power/height/gain data extension.
//
// power is in watts and height in feet.  gain is in dB: the protocol spec
// doesn't mention whether it is dBi or dBd, but this says dBi:
// http://www.tapr.org/pipermail/aprssig/2008-September/027034.html
//
// dir is the directivity: N, NE, etc., or omni.
func phg_data_extension(power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int], dir string) string {
	// The callers only check that at least one of the three was specified, so
	// the others can still be absent.  Treat those as unspecified, which is
	// what the zero the callers originally checked for meant.
	var watts = maybe.FromMaybe(0, power)
	var feet = maybe.FromMaybe(0, height)
	var dBi = maybe.FromMaybe(0, gain)

	var p = min(max(math.Round(math.Sqrt(float64(watts)))+'0', '0'), '9')

	var h = max(math.Round(math.Log2(float64(feet)/10.0))+'0', '0')
	// Result can go beyond '9'.

	var g = min(max(dBi, 0), 9) + '0'

	// Anything else, e.g. omni, is 0.
	var d = '0'

	switch strings.ToUpper(dir) {
	case "NE":
		d = '1'
	case "E":
		d = '2'
	case "SE":
		d = '3'
	case "S":
		d = '4'
	case "SW":
		d = '5'
	case "W":
		d = '6'
	case "NW":
		d = '7'
	case "N":
		d = '8'
	}

	return fmt.Sprintf("PHG%c%c%c%c", byte(p), byte(h), byte(g), d)
}

// cse_spd_data_extension returns the course & speed data extension.
//
// course is in degrees, 0 - 360 (360 equivalent to 0), and speed in knots.
// Over the air we use 0 for an unknown or irrelevant course, and 1 - 360 for
// a valid one (360 for north).
func cse_spd_data_extension(course maybe.Maybe[int], speed maybe.Maybe[int]) string {
	var cse int
	if degrees, known := course.Get(); known {
		cse = degrees
		for cse < 1 {
			cse += 360
		}

		for cse > 360 {
			cse -= 360
		}
		// Should now be in range of 1 - 360.
		// Original value of 0 for north is transmitted as 360.
	}

	var spd = min(max(maybe.FromMaybe(0, speed), 0), 999)

	return fmt.Sprintf("%03d/%03d", cse, spd)
}

// dataExtension returns the optional data extension (singular) that may follow
// an uncompressed position, or "" for none.  Can't have both course/speed and
// PHG; the former gets priority.
func dataExtension(power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int], dir string,
	course maybe.Maybe[int], speed maybe.Maybe[int]) string {
	if course.IsJust() || maybe.FromMaybe(0, speed) > 0 {
		return cse_spd_data_extension(course, speed)
	}

	if maybe.FromMaybe(0, power) > 0 || maybe.FromMaybe(0, height) > 0 || maybe.FromMaybe(0, gain) > 0 {
		return phg_data_extension(power, height, gain, dir)
	}

	return ""
}

// frequency_spec returns the frequency specification for the beginning of the
// comment field, or "" if nothing was given.  freq is in MHz, tone in Hz and
// offset in MHz.
//
// There are several valid variations.  The frequency could be missing here if
// it is in the object name.  In this case we could have tone & offset.  Offset
// must always be preceded by tone.
//
// Resulting formats are all fixed width and have a trailing space:
//
//	"999.999MHz "
//	"T999 "
//	"+999 "		(10 kHz units)
//
// Reference: http://www.aprs.org/info/freqspec.txt
func frequency_spec(freq maybe.Maybe[float64], tone maybe.Maybe[float64], offset maybe.Maybe[float64]) string {
	var result string

	var megahertz = maybe.FromMaybe(0, freq)
	if megahertz > 0 {
		// TODO: Should use letters for > 999.999.
		// For now, just be sure we have proper field width.
		result += fmt.Sprintf("%07.3fMHz ", min(megahertz, 999.999))
	}

	if hertz, known := tone.Get(); known {
		if hertz == 0 {
			result += "Toff "
		} else {
			result += fmt.Sprintf("T%03d ", int(hertz))
		}
	}

	if megahertz, known := offset.Get(); known {
		result += fmt.Sprintf("%+04d ", int(math.Round(megahertz*100)))
	}

	return result
}

// encodeLocation returns the part of the info that position reports and
// objects share: the position, compressed or not, then the optional data
// extension (which only an uncompressed position has room for) and the
// optional frequency spec.
//
// power is in watts, height in feet, gain in dB (not clear if it is dBi or
// dBd) and dir the directivity: N, NE, etc., or omni.  course is in degrees,
// 0 - 360 (360 equivalent to 0), and speed in knots.  freq is in MHz, tone in
// Hz and offset in MHz.
func encodeLocation(compressed bool, lat float64, lon float64, ambiguity int,
	symtab byte, symbol byte,
	power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int], dir string,
	course maybe.Maybe[int], speed maybe.Maybe[int],
	freq maybe.Maybe[float64], tone maybe.Maybe[float64], offset maybe.Maybe[float64]) string {
	var result string

	if compressed {
		result = compressed_position_string(compressed_position(symtab, symbol, lat, lon,
			power, height, gain,
			course, speed))
	} else {
		result = normal_position_string(normal_position(symtab, symbol, lat, lon, ambiguity)) +
			dataExtension(power, height, gain, dir, course, speed)
	}

	return result + frequency_spec(freq, tone, offset)
}

// EncodePosition returns the info part for the position report format.  It
// could get into hundreds of characters because it includes the comment.
//
// messaging determines whether the data type indicator is '!' (false) or '='
// (true).  ambiguity is the number of digits to omit from the location,
// altFeet the altitude in feet, symtab the symbol table id or overlay and
// symbol the symbol id.  The rest are as for encodeLocation, then any
// additional comment text.
//
// There can be a single optional "data extension" following the position, so
// there is a choice between power/height/gain/directivity and course/speed.
// After that come the optional frequency spec, altitude and comment.
func EncodePosition(messaging bool, compressed bool, lat float64, lon float64, ambiguity int, altFeet maybe.Maybe[int],
	symtab byte, symbol byte,
	power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int], dir string,
	course maybe.Maybe[int], speed maybe.Maybe[int],
	freq maybe.Maybe[float64], tone maybe.Maybe[float64], offset maybe.Maybe[float64],
	comment string) string {
	var dti = '!'
	if messaging {
		dti = '='
	}

	// Thought:
	// https://groups.io/g/direwolf/topic/92718535#6886
	// When speed is zero, we could put the altitude in the compressed
	// position rather than having /A=999999.
	// However, the resolution would be decreased and that could be important
	// when hiking in hilly terrain.  It would also be confusing to
	// flip back and forth between two different representations.
	var result = string(dti) + encodeLocation(compressed, lat, lon, ambiguity, symtab, symbol,
		power, height, gain, dir, course, speed, freq, tone, offset)

	// Altitude.  Can be anywhere in comment.
	// Officially, altitude must be six digits.
	// What about all the places on the earth's surface that are below sea level?
	// https://en.wikipedia.org/wiki/List_of_places_on_land_with_elevations_below_sea_level

	// The MIC-E format allows negative altitudes; not allowing it for /A=123456 seems to be an oversight.
	// Most modern applications recognize the form /A=-12345 with minus and five digits.
	// This maintains the same total field width and the range is more than adequate.

	if feet, known := altFeet.Get(); known {
		// Not clear if altitude can be negative.
		// Be sure it will be converted to 6 digits.
		result += fmt.Sprintf("/A=%06d", min(max(feet, -99999), 999999)) // /A=123456 or /A=-12345
	}

	// Finally, comment text.
	result += comment

	return result
}

// encode_object returns the info part for the object report format: 36
// characters of fixed part, 7 for optional extended data, ~20 for freq, etc.,
// then the comment, which could be very long.
//
// name is up to 9 characters.  when is the time stamp, or the zero time for
// none.  The rest are as for EncodePosition.
func encode_object(name string, compressed bool, when time.Time, lat float64, lon float64, ambiguity int,
	symtab byte, symbol byte,
	power maybe.Maybe[int], height maybe.Maybe[int], gain maybe.Maybe[int], dir string,
	course maybe.Maybe[int], speed maybe.Maybe[int],
	freq maybe.Maybe[float64], tone maybe.Maybe[float64], offset maybe.Maybe[float64], comment string) string {
	var dti = ';'
	var liveKilled = '*'

	var timestamp string
	if !when.IsZero() {
		timestamp = when.UTC().Format("021504z")
	} else {
		timestamp = "111111z"
	}

	return fmt.Sprintf("%c%-9.9s%c%-7.7s", dti, name, liveKilled, timestamp) +
		encodeLocation(compressed, lat, lon, ambiguity, symtab, symbol,
			power, height, gain, dir, course, speed, freq, tone, offset) +
		comment
}

// encode_message returns the info part for the APRS "message" format.
// addressee is up to 9 characters, and id, the identifier, 0 to 5.
func encode_message(addressee string, text string, id string) string {
	var result = fmt.Sprintf(":%-9.9s:%s", addressee, text)

	if id != "" {
		result += "{" + id
	}

	return result
}
