package direwolf

import (
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/tzneal/coordconv"
)

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
