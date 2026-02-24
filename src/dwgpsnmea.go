package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	process NMEA sentences from a GPS receiver.
 *
 * Description:	This version is available for all operating systems.
 *
 *
 * TODO:	GPS is no longer the only game in town.
 *		"GNSS" is often seen as a more general term to include
 *		other similar systems.  Some receivers will receive
 *		multiple types at the same time and combine them
 *		for greater accuracy and reliability.
 *
 *		We can now see NMEA sentences with other "Talker IDs."
 *
 *			$GPxxx = GPS
 *			$GLxxx = GLONASS
 *			$GAxxx = Galileo
 *			$GBxxx = BeiDou
 *			$GNxxx = Any combination
 *
 *---------------------------------------------------------------*/

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/term"
)

// TODO KG var s_debug = 0 /* Enable debug output. */
/* See dwgpsnmea_init description for values. */

var s_save_configp *misc_config_s

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsnmea_init
 *
 * Purpose:    	Open serial port for the GPS receiver.
 *
 * Inputs:	pconfig		Configuration settings.  This includes
 *				serial port name for direct connect.
 *
 *		debug	- If >= 1, print results when dwgps_read is called.
 *				(In different file.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In this file.)
 *				Why not do it in dwgps_set_data() ?
 *				Here, we can prefix it with GPSNMEA to
 *				distinguish it from GPSD.
 *
 *			  If >= 3, Also the NMEA sentences.
 *				(In this file.)
 *
 * Returns:	1 = success
 *		0 = nothing to do  (no serial port specified in config)
 *		-1 = failure
 *
 * Description:	When talking directly to GPS receiver  (any operating system):
 *
 *			- Open the appropriate serial port.
 *			- Start up thread to process incoming data.
 *			  It reads from the serial port and deposits into
 *			  dwgps_info, above.
 *
 * 		The application calls dwgps_read to get the most recent information.
 *
 *--------------------------------------------------------------------*/

/* Make this static and available to all functions so term function can access it. */

var s_gpsnmea_port_fd *term.Term

func dwgpsnmea_init(pconfig *misc_config_s, debug int) int {
	//dwgps_info_t info;
	//int e;
	s_debug = debug
	s_save_configp = pconfig

	if s_debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("dwgpsnmea_init()\n")
	}

	if pconfig.gpsnmea_port == "" {
		/* Nothing to do.  Leave initial fix value for not init. */
		return (0)
	}

	/*
	 * Open serial port connection.
	 */

	s_gpsnmea_port_fd = serial_port_open(pconfig.gpsnmea_port, pconfig.gpsnmea_speed)

	if s_gpsnmea_port_fd != nil {
		go read_gpsnmea_thread(s_gpsnmea_port_fd)
	} else {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Could not open serial port %s for GPS receiver.\n", pconfig.gpsnmea_port)

		return (-1)
	}

	/* success */

	return (1)
} /* end dwgpsnmea_init */

/* Return fd to share if waypoint wants same device. */

func dwgpsnmea_get_fd(wp_port_name string, speed int) *term.Term {
	if s_save_configp.gpsnmea_port == wp_port_name && speed == s_save_configp.gpsnmea_speed {
		return (s_gpsnmea_port_fd)
	}

	return nil
}

/*-------------------------------------------------------------------
 *
 * Name:        read_gpsnmea_thread
 *
 * Purpose:     Read information from GPS, as it becomes available, and
 *		store it for later retrieval by dwgps_read.
 *
 * Inputs:	fd	- File descriptor for serial port.
 *
 * Description:	This version reads from serial port and parses the
 *		NMEA sentences.
 *
 *--------------------------------------------------------------------*/

const TIMEOUT = 5

func read_gpsnmea_thread(fd *term.Term) {
	// Maximum length of message from GPS receiver is 82 according to some people.
	// Make buffer considerably larger to be safe.
	const NMEA_MAX_LEN = 160

	if s_debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("read_gpsnmea_thread (%+v)\n", fd)
	}

	var info = new(dwgps_info_t)
	dwgps_clear(info)
	info.fix = DWFIX_NOT_SEEN /* clear not init state. */

	if s_debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dwgps_print("GPSNMEA: ", info)
	}

	dwgps_set_data(info)

	var gps_msg string

	for {
		var ch, err = serial_port_get1(fd)
		if err != nil {
			/* This might happen if a USB  device is unplugged. */
			/* I can't imagine anything that would cause it with */
			/* a normal serial port. */
			text_color_set(DW_COLOR_ERROR)
			dw_printf("----------------------------------------------\n")
			dw_printf("GPSNMEA: Lost communication with GPS receiver.\n")
			dw_printf("----------------------------------------------\n")

			info.fix = DWFIX_ERROR

			if s_debug >= 2 {
				text_color_set(DW_COLOR_DEBUG)
				dwgps_print("GPSNMEA: ", info)
			}

			dwgps_set_data(info)

			serial_port_close(s_gpsnmea_port_fd)
			s_gpsnmea_port_fd = nil

			// TODO: If the open() was in this thread, we could wait a while and
			// try to open again.  That would allow recovery if the USB GPS device
			// is unplugged and plugged in again.
			break /* terminate thread. */
		}

		switch ch {
		case '$':
			// Start of new sentence.
			gps_msg = string(ch)
		case '\r', '\n':
			if len(gps_msg) >= 6 && gps_msg[0] == '$' {
				if s_debug >= 3 {
					text_color_set(DW_COLOR_DEBUG)
					dw_printf("%s\n", gps_msg)
				}

				/* Process sentence. */
				// TODO: More general: Ignore the second letter rather than recognizing only GP... and GN...

				if strings.HasPrefix(gps_msg, "$GPRMC") || strings.HasPrefix(gps_msg, "$GNRMC") {
					// Here we just tuck away the course and speed.
					// Fix and location will be updated by GxGGA.
					var f = dwgpsnmea_gprmc(gps_msg, false)

					if f.Fix == DWFIX_ERROR {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						text_color_set(DW_COLOR_ERROR)
						dw_printf("GPSNMEA: Error parsing $GPRMC sentence.\n")
						dw_printf("%s\n", gps_msg)
					} else {
						if f.Knots != G_UNKNOWN {
							info.speed_knots = f.Knots
						}

						if f.Course != G_UNKNOWN {
							info.track = f.Course
						}
					}
				} else if strings.HasPrefix(gps_msg, "$GPGGA") || strings.HasPrefix(gps_msg, "$GNGGA") {
					var f = dwgpsnmea_gpgga(gps_msg, false)

					if f.Fix == DWFIX_ERROR {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						text_color_set(DW_COLOR_ERROR)
						dw_printf("GPSNMEA: Error parsing $GPGGA sentence.\n")
						dw_printf("%s\n", gps_msg)
					} else {
						if f.Lat != G_UNKNOWN {
							info.dlat = f.Lat
						}

						if f.Lon != G_UNKNOWN {
							info.dlon = f.Lon
						}

						if f.Alt != G_UNKNOWN {
							info.altitude = f.Alt
						}

						if f.Fix != info.fix { // Print change in location fix.
							text_color_set(DW_COLOR_INFO)

							switch f.Fix {
							case DWFIX_NO_FIX:
								dw_printf("GPSNMEA: Location fix has been lost.\n")
							case DWFIX_2D:
								dw_printf("GPSNMEA: Location fix is now 2D.\n")
							case DWFIX_3D:
								dw_printf("GPSNMEA: Location fix is now 3D.\n")
							default:
							}

							info.fix = f.Fix
						}

						info.timestamp = time.Now()

						if s_debug >= 2 {
							text_color_set(DW_COLOR_DEBUG)
							dwgps_print("GPSNMEA: ", info)
						}

						dwgps_set_data(info)
					}
				}
			}

			gps_msg = ""
		default:
			if len(gps_msg) < NMEA_MAX_LEN-1 {
				gps_msg += string(ch)
			}
		}
	} /* while (1) */
} /* end read_gpsnmea_thread */

/*-------------------------------------------------------------------
 *
 * Name:	remove_checksum
 *
 * Purpose:	Validate checksum and remove before further processing.
 *
 * Inputs:	sentence
 *		quiet		suppress printing of error messages.
 *
 * Outputs:	sentence	modified in place.
 *
 * Returns:	0 = good checksum.
 *		-1 = error.  missing or wrong.
 *
 *--------------------------------------------------------------------*/

func remove_checksum(sent string, quiet bool) (string, error) {
	var msg, checksumStr, found = strings.Cut(sent, "*")
	if !found {
		var errorMsg = "Missing GPS checksum"

		if !quiet {
			text_color_set(DW_COLOR_INFO)
			dw_printf("%s.\n", errorMsg)
		}

		return "", errors.New(errorMsg)
	}

	var calculatedChecksum int64
	for _, r := range msg[1:] {
		calculatedChecksum ^= int64(r)
	}

	var checksum, _ = strconv.ParseInt(checksumStr, 16, 0)

	if calculatedChecksum != checksum {
		var errorMsg = fmt.Sprintf("GPS checksum error. Expected %02x but found %s", calculatedChecksum, checksumStr)

		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("%s.\n", errorMsg)
		}

		return "", errors.New(errorMsg)
	}

	return msg, nil
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsnmea_gprmc
 *
 * Purpose:    	Parse $GPRMC sentence and extract interesting parts.
 *
 * Inputs:	sentence	NMEA sentence.
 *
 *		quiet		suppress printing of error messages.
 *
 * Outputs:	odlat		latitude
 *		odlon		longitude
 *		oknots		speed
 *		ocourse		direction of travel.
 *
 *					Left undefined if not valid.
 *
 * Note:	RMC does not contain altitude.
 *
 * Returns:	DWFIX_ERROR	Parse error.
 *		DWFIX_NO_FIX	GPS is there but Position unknown.  Could be temporary.
 *		DWFIX_2D	Valid position.   We don't know if it is really 2D or 3D.
 *
 * Examples:	$GPRMC,001431.00,V,,,,,,,121015,,,N*7C
 *		$GPRMC,212404.000,V,4237.1505,N,07120.8602,W,,,150614,,*0B
 *		$GPRMC,000029.020,V,,,,,,,080810,,,N*45
 *		$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F
 *
 *--------------------------------------------------------------------*/

type GPRMCResult struct {
	Lat    float64
	Lon    float64
	Knots  float64
	Course float64
	Fix    dwfix_t
}

func dwgpsnmea_gprmc(sentence string, quiet bool) *GPRMCResult {
	var result = &GPRMCResult{
		Lat:    G_UNKNOWN,
		Lon:    G_UNKNOWN,
		Knots:  G_UNKNOWN,
		Course: G_UNKNOWN,
		Fix:    DWFIX_NO_FIX, // TODO Default to Error, because that's what most returns are? On the other hand it's good to be explicit...
	}

	sentence, err := remove_checksum(sentence, quiet)
	if err != nil {
		result.Fix = DWFIX_ERROR
		return result
	}

	ptype, sentence, _ := strings.Cut(sentence, ",")   /* Should be $GPRMC */
	ptime, sentence, _ := strings.Cut(sentence, ",")   /* Time, hhmmss[.sss] */
	pstatus, sentence, _ := strings.Cut(sentence, ",") /* Status, A=Active (valid position), V=Void */
	plat, sentence, _ := strings.Cut(sentence, ",")    /* Latitude */
	pns, sentence, _ := strings.Cut(sentence, ",")     /* North/South */
	plon, sentence, _ := strings.Cut(sentence, ",")    /* Longitude */
	pew, sentence, _ := strings.Cut(sentence, ",")     /* East/West */
	pknots, sentence, _ := strings.Cut(sentence, ",")  /* Speed over ground, knots. */
	pcourse, sentence, _ := strings.Cut(sentence, ",") /* True course, degrees. */
	pdate, sentence, _ := strings.Cut(sentence, ",")   /* Date, ddmmyy */
	/* Magnetic variation */
	/* In version 3.00, mode is added: A D E N (see below) */
	/* Checksum */

	/* Suppress the 'set but not used' warnings. */
	/* Alternatively, we might use __attribute__((unused)) */

	_ = ptype
	_ = ptime
	_ = pdate
	_ = sentence

	if pstatus != "" && len(pstatus) == 1 {
		if pstatus != "A" {
			result.Fix = DWFIX_NO_FIX
			return result /* Not "Active." Don't parse. */
		}
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("No status in GPRMC sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		result.Lat = latitude_from_nmea(plat, pns[0])
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get latitude from GPRMC sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		result.Lon = longitude_from_nmea(plon, pew[0])
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get longitude from GPRMC sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	var knots, knotsErr = strconv.ParseFloat(pknots, 64)
	if knotsErr == nil {
		result.Knots = knots
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get speed from GPRMC sentence: %s\n", knotsErr)
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	var course, courseErr = strconv.ParseFloat(pcourse, 64)
	if courseErr == nil {
		result.Course = course
	} else {
		/* When stationary, this field might be empty. */
		result.Course = G_UNKNOWN
	}

	//text_color_set (DW_COLOR_INFO);
	//dw_printf("%.6f %.6f %.1f %.0f\n", *odlat, *odlon, *oknots, *ocourse);

	result.Fix = DWFIX_2D

	return result
} /* end dwgpsnmea_gprmc */

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsnmea_gpgga
 *
 * Purpose:    	Parse $GPGGA sentence and extract interesting parts.
 *
 * Inputs:	sentence	NMEA sentence.
 *
 *		quiet		suppress printing of error messages.
 *
 * Outputs:	odlat		latitude
 *		odlon		longitude
 *		oalt		altitude in meters
 *		onsat		number of satellites.
 *
 *					Left undefined if not valid.
 *
 * Note:	GGA has altitude but not course and speed so we need to use both.
 *
 * Returns:	DWFIX_ERROR	Parse error.
 *		DWFIX_NO_FIX	GPS is there but Position unknown.  Could be temporary.
 *		DWFIX_2D	Valid position.   We don't know if it is really 2D or 3D.
 *				Take more cautious value so we don't try using altitude.
 *		DWFIX_3D	Valid 3D position.
 *
 * Examples:	$GPGGA,001429.00,,,,,0,00,99.99,,,,,,*68
 *		$GPGGA,212407.000,4237.1505,N,07120.8602,W,0,00,,,M,,M,,*58
 *		$GPGGA,000409.392,,,,,0,00,,,M,0.0,M,,0000*53
 *		$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B
 *
 *--------------------------------------------------------------------*/

type GPGGAResult struct {
	Lat float64
	Lon float64
	Alt float64
	Sat int
	Fix dwfix_t
}

func dwgpsnmea_gpgga(sentence string, quiet bool) *GPGGAResult {
	var result = &GPGGAResult{
		Lat: G_UNKNOWN,
		Lon: G_UNKNOWN,
		Alt: G_UNKNOWN,
		Sat: G_UNKNOWN,
		Fix: DWFIX_NO_FIX,
	}

	sentence, err := remove_checksum(sentence, quiet)
	if err != nil {
		result.Fix = DWFIX_ERROR
		return result
	}

	ptype, sentence, _ := strings.Cut(sentence, ",")                 /* Should be $GPGGA */
	ptime, sentence, _ := strings.Cut(sentence, ",")                 /* Time, hhmmss[.sss] */
	plat, sentence, _ := strings.Cut(sentence, ",")                  /* Latitude */
	pns, sentence, _ := strings.Cut(sentence, ",")                   /* North/South */
	plon, sentence, _ := strings.Cut(sentence, ",")                  /* Longitude */
	pew, sentence, _ := strings.Cut(sentence, ",")                   /* East/West */
	pfix, sentence, _ := strings.Cut(sentence, ",")                  /* 0=invalid, 1=GPS fix, 2=DGPS fix */
	pnum_sat, sentence, _ := strings.Cut(sentence, ",")              /* Number of satellites */
	phdop, sentence, _ := strings.Cut(sentence, ",")                 /* Horiz. Dilution of Precision */
	paltitude, sentence, altitudeFound := strings.Cut(sentence, ",") /* Altitude, above mean sea level */
	palt_u, sentence, _ := strings.Cut(sentence, ",")                /* Units for Altitude, typically M for meters. */
	pheight, sentence, _ := strings.Cut(sentence, ",")               /* Height above ellipsoid */
	pheight_u, sentence, _ := strings.Cut(sentence, ",")             /* Units for height, typically M for meters. */
	psince, sentence, _ := strings.Cut(sentence, ",")                /* Time since last DGPS update. */
	pdsta, sentence, _ := strings.Cut(sentence, ",")                 /* DGPS reference station id. */

	/* Suppress the 'set but not used' warnings. */
	/* Alternatively, we might use __attribute__((unused)) */

	_ = ptype
	_ = ptime
	_ = pnum_sat
	_ = phdop
	_ = palt_u
	_ = pheight
	_ = pheight_u
	_ = psince
	_ = pdsta
	_ = sentence

	if len(pfix) == 1 {
		if pfix == "0" {
			result.Fix = DWFIX_NO_FIX /* No Fix. Don't parse the rest. */
			return result
		}
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("No fix in GPGGA sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		result.Lat = latitude_from_nmea(plat, pns[0])
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get latitude from GPGGA sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		result.Lon = longitude_from_nmea(plon, pew[0])
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get longitude from GPGGA sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	// TODO: num sat...  Why would we care?

	/*
	 * We can distinguish between 2D & 3D fix by presence
	 * of altitude or an empty field.
	 */

	if altitudeFound {
		if len(paltitude) > 0 {
			var altitude, altitudeErr = strconv.ParseFloat(paltitude, 64)
			if altitudeErr == nil {
				result.Alt = altitude
				result.Fix = DWFIX_3D
			} else {
				if !quiet {
					text_color_set(DW_COLOR_ERROR)
					dw_printf("Can't get altitude from GPGGA sentence: %s\n", altitudeErr)
				}

				result.Fix = DWFIX_ERROR

				return result
			}
		} else {
			result.Fix = DWFIX_2D
		}

		return result
	} else {
		if !quiet {
			text_color_set(DW_COLOR_ERROR)
			dw_printf("Can't get altitude from GPGGA sentence.\n")
		}

		result.Fix = DWFIX_ERROR

		return result
	}
} /* end dwgpsnmea_gpgga */

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsnmea_term
 *
 * Purpose:    	Shut down GPS interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func dwgpsnmea_term() {

	// Should probably kill reader thread before closing device to avoid
	// message about read error.

	// serial_port_close (s_gpsnmea_port_fd);

} /* end dwgps_term */

/* end dwgpsnmea.c */
