package direwolf

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/tzneal/coordconv"
)

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
