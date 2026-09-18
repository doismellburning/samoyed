// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package dwgps

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
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/textcolor"
	"github.com/pkg/term"
)

var s_debug = 0 /* Enable debug output. */
/* See nmeaInit description for values. */

var s_save_configp *Config

/*-------------------------------------------------------------------
 *
 * Name:        nmeaInit
 *
 * Purpose:    	Open serial port for the GPS receiver.
 *
 * Inputs:	pconfig		Where to find the GPS.  This includes the
 *				serial port name for direct connect.
 *
 *		debug	- If >= 1, print results when Read is called.
 *				(In different file.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In this file.)
 *				Why not do it in setData() ?
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
 *			  s_dwgps_info, in dwgps.go.
 *
 * 		The application calls Read to get the most recent information.
 *
 *--------------------------------------------------------------------*/

/* Make this static and available to all functions so term function can access it. */

var s_gpsnmea_port_fd *term.Term

func nmeaInit(pconfig *Config, debug int) int {
	//Info info;
	//int e;
	s_debug = debug
	s_save_configp = pconfig

	if s_debug >= 2 {
		textcolor.Set(textcolor.Debug)
		textcolor.Printf("nmeaInit()\n")
	}

	if pconfig.NMEAPort == "" || pconfig.OpenSerialPort == nil {
		/* Nothing to do.  Leave initial fix value for not init. */
		return (0)
	}

	/*
	 * Open serial port connection.
	 */

	s_gpsnmea_port_fd = pconfig.OpenSerialPort(pconfig.NMEAPort, pconfig.NMEASpeed)

	if s_gpsnmea_port_fd != nil {
		go read_gpsnmea_thread(s_gpsnmea_port_fd)
	} else {
		textcolor.Set(textcolor.Error)
		textcolor.Printf("Could not open serial port %s for GPS receiver.\n", pconfig.NMEAPort)

		return (-1)
	}

	/* success */

	return (1)
} /* end nmeaInit */

// SharedNMEAPort is the serial port the GPS is being read from, if that is the
// same device, at the same speed, as the caller wants - so waypoint output can
// share a port with GPS input rather than trying to open it a second time.  It
// is nil if there is no such port, including before Init has run.

func SharedNMEAPort(wp_port_name string, speed int) *term.Term {
	if s_save_configp == nil {
		return nil
	}

	if s_save_configp.NMEAPort == wp_port_name && speed == s_save_configp.NMEASpeed {
		return (s_gpsnmea_port_fd)
	}

	return nil
}

/*-------------------------------------------------------------------
 *
 * Name:        read_gpsnmea_thread
 *
 * Purpose:     Read information from GPS, as it becomes available, and
 *		store it for later retrieval by Read.
 *
 * Inputs:	fd	- The serial port the GPS is connected to.  It is an
 *			  io.Reader rather than the port itself because
 *			  reading is all this does with it - closing on error
 *			  is done through s_gpsnmea_port_fd, below.
 *
 * Description:	This version reads from serial port and parses the
 *		NMEA sentences.
 *
 *--------------------------------------------------------------------*/

const TIMEOUT = 5

func read_gpsnmea_thread(fd io.Reader) {
	// Maximum length of message from GPS receiver is 82 according to some people.
	// Make buffer considerably larger to be safe.
	const NMEA_MAX_LEN = 160

	if s_debug >= 2 {
		textcolor.Set(textcolor.Debug)
		textcolor.Printf("read_gpsnmea_thread (%+v)\n", fd)
	}

	var info = new(Info) /* Zero value is FixNotSeen, nothing else known. */

	if s_debug >= 2 {
		textcolor.Set(textcolor.Debug)
		Print("GPSNMEA: ", info)
	}

	setData(info)

	var reader = bufio.NewReader(fd)

	var gps_msg string

	for {
		var ch, err = reader.ReadByte()
		if err != nil {
			/* This might happen if a USB  device is unplugged. */
			/* I can't imagine anything that would cause it with */
			/* a normal serial port. */
			textcolor.Set(textcolor.Error)
			textcolor.Printf("----------------------------------------------\n")
			textcolor.Printf("GPSNMEA: Lost communication with GPS receiver.\n")
			textcolor.Printf("----------------------------------------------\n")

			info.Fix = FixError

			if s_debug >= 2 {
				textcolor.Set(textcolor.Debug)
				Print("GPSNMEA: ", info)
			}

			setData(info)

			if s_gpsnmea_port_fd != nil {
				s_gpsnmea_port_fd.Close()
			}

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
					textcolor.Set(textcolor.Debug)
					textcolor.Printf("%s\n", gps_msg)
				}

				/* Process sentence. */
				// TODO: More general: Ignore the second letter rather than recognizing only GP... and GN...

				if strings.HasPrefix(gps_msg, "$GPRMC") || strings.HasPrefix(gps_msg, "$GNRMC") {
					// Here we just tuck away the course and speed.
					// Fix and location will be updated by GxGGA.
					var f = ParseGPRMC(gps_msg, false)

					if f.Fix == FixError {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						textcolor.Set(textcolor.Error)
						textcolor.Printf("GPSNMEA: Error parsing $GPRMC sentence.\n")
						textcolor.Printf("%s\n", gps_msg)
					} else {
						info.SpeedKnots = f.Knots.Or(info.SpeedKnots)
						info.Track = f.Course.Or(info.Track)
					}
				} else if strings.HasPrefix(gps_msg, "$GPGGA") || strings.HasPrefix(gps_msg, "$GNGGA") {
					var f = ParseGPGGA(gps_msg, false)

					if f.Fix == FixError {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						textcolor.Set(textcolor.Error)
						textcolor.Printf("GPSNMEA: Error parsing $GPGGA sentence.\n")
						textcolor.Printf("%s\n", gps_msg)
					} else {
						info.Lat = f.Lat.Or(info.Lat)
						info.Lon = f.Lon.Or(info.Lon)
						info.Altitude = f.Alt.Or(info.Altitude)

						if f.Fix != info.Fix { // Print change in location fix.
							textcolor.Set(textcolor.Info)

							switch f.Fix {
							case FixNoFix:
								textcolor.Printf("GPSNMEA: Location fix has been lost.\n")
							case Fix2D:
								textcolor.Printf("GPSNMEA: Location fix is now 2D.\n")
							case Fix3D:
								textcolor.Printf("GPSNMEA: Location fix is now 3D.\n")
							default:
							}

							info.Fix = f.Fix
						}

						info.Timestamp = time.Now()

						if s_debug >= 2 {
							textcolor.Set(textcolor.Debug)
							Print("GPSNMEA: ", info)
						}

						setData(info)
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
		if !quiet {
			textcolor.Set(textcolor.Info)
			textcolor.Printf("Missing GPS checksum.\n")
		}

		return "", errors.New("missing GPS checksum")
	}

	var calculatedChecksum int64
	for _, r := range msg[1:] {
		calculatedChecksum ^= int64(r)
	}

	var checksum, _ = strconv.ParseInt(checksumStr, 16, 0)

	if calculatedChecksum != checksum {
		var errorMsg = fmt.Sprintf("GPS checksum error. Expected %02x but found %s", calculatedChecksum, checksumStr)

		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("%s.\n", errorMsg)
		}

		return "", errors.New(errorMsg)
	}

	return msg, nil
}

/*-------------------------------------------------------------------
 *
 * Name:        ParseGPRMC
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
 * Returns:	FixError	Parse error.
 *		FixNoFix	GPS is there but Position unknown.  Could be temporary.
 *		Fix2D	Valid position.   We don't know if it is really 2D or 3D.
 *
 * Examples:	$GPRMC,001431.00,V,,,,,,,121015,,,N*7C
 *		$GPRMC,212404.000,V,4237.1505,N,07120.8602,W,,,150614,,*0B
 *		$GPRMC,000029.020,V,,,,,,,080810,,,N*45
 *		$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F
 *
 *--------------------------------------------------------------------*/

type GPRMCResult struct {
	Lat    maybe.Maybe[float64]
	Lon    maybe.Maybe[float64]
	Knots  maybe.Maybe[float64]
	Course maybe.Maybe[float64]
	Fix    Fix
}

func ParseGPRMC(sentence string, quiet bool) *GPRMCResult {
	var result = new(GPRMCResult)

	// TODO Default to Error, because that's what most returns are? On the other hand it's good to be explicit...
	result.Fix = FixNoFix

	sentence, err := remove_checksum(sentence, quiet)
	if err != nil {
		result.Fix = FixError

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
			result.Fix = FixNoFix

			return result /* Not "Active." Don't parse. */
		}
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("No status in GPRMC sentence.\n")
		}

		result.Fix = FixError

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		var lat, latErr = LatitudeFromNMEA(plat, pns[0])
		if latErr != nil {
			if !quiet {
				textcolor.Set(textcolor.Error)
				textcolor.Printf("Can't get latitude from GPRMC sentence: %v\n", latErr)
			}

			result.Fix = FixError

			return result
		}

		result.Lat = maybe.Just(lat)
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get latitude from GPRMC sentence.\n")
		}

		result.Fix = FixError

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		var lon, lonErr = LongitudeFromNMEA(plon, pew[0])
		if lonErr != nil {
			if !quiet {
				textcolor.Set(textcolor.Error)
				textcolor.Printf("Can't get longitude from GPRMC sentence: %v\n", lonErr)
			}

			result.Fix = FixError

			return result
		}

		result.Lon = maybe.Just(lon)
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get longitude from GPRMC sentence.\n")
		}

		result.Fix = FixError

		return result
	}

	var knots, knotsErr = strconv.ParseFloat(pknots, 64)
	if knotsErr == nil {
		result.Knots = unlessUnknown(knots)
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get speed from GPRMC sentence: %s\n", knotsErr)
		}

		result.Fix = FixError

		return result
	}

	var course, courseErr = strconv.ParseFloat(pcourse, 64)
	if courseErr == nil {
		result.Course = unlessUnknown(course)
	}
	/* When stationary, this field might be empty, and Course stays Nothing. */
	/* A parsed value can still be the G_UNKNOWN sentinel, hence unlessUnknown. */

	//textcolor.Set(textcolor.Info)
	//textcolor.Printf("%.6f %.6f %.1f %.0f\n", *odlat, *odlon, *oknots, *ocourse);

	result.Fix = Fix2D

	return result
} /* end ParseGPRMC */

/*-------------------------------------------------------------------
 *
 * Name:        ParseGPGGA
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
 * Returns:	FixError	Parse error.
 *		FixNoFix	GPS is there but Position unknown.  Could be temporary.
 *		Fix2D	Valid position.   We don't know if it is really 2D or 3D.
 *				Take more cautious value so we don't try using altitude.
 *		Fix3D	Valid 3D position.
 *
 * Examples:	$GPGGA,001429.00,,,,,0,00,99.99,,,,,,*68
 *		$GPGGA,212407.000,4237.1505,N,07120.8602,W,0,00,,,M,,M,,*58
 *		$GPGGA,000409.392,,,,,0,00,,,M,0.0,M,,0000*53
 *		$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B
 *
 *--------------------------------------------------------------------*/

type GPGGAResult struct {
	Lat maybe.Maybe[float64]
	Lon maybe.Maybe[float64]
	Alt maybe.Maybe[float64]
	Sat maybe.Maybe[int]
	Fix Fix
}

func ParseGPGGA(sentence string, quiet bool) *GPGGAResult {
	var result = new(GPGGAResult)

	result.Fix = FixNoFix

	sentence, err := remove_checksum(sentence, quiet)
	if err != nil {
		result.Fix = FixError

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
			result.Fix = FixNoFix /* No Fix. Don't parse the rest. */

			return result
		}
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("No fix in GPGGA sentence.\n")
		}

		result.Fix = FixError

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		var lat, latErr = LatitudeFromNMEA(plat, pns[0])
		if latErr != nil {
			if !quiet {
				textcolor.Set(textcolor.Error)
				textcolor.Printf("Can't get latitude from GPGGA sentence: %v\n", latErr)
			}

			result.Fix = FixError

			return result
		}

		result.Lat = maybe.Just(lat)
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get latitude from GPGGA sentence.\n")
		}

		result.Fix = FixError

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		var lon, lonErr = LongitudeFromNMEA(plon, pew[0])
		if lonErr != nil {
			if !quiet {
				textcolor.Set(textcolor.Error)
				textcolor.Printf("Can't get longitude from GPGGA sentence: %v\n", lonErr)
			}

			result.Fix = FixError

			return result
		}

		result.Lon = maybe.Just(lon)
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get longitude from GPGGA sentence.\n")
		}

		result.Fix = FixError

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
				result.Alt = unlessUnknown(altitude)
				result.Fix = Fix3D
			} else {
				if !quiet {
					textcolor.Set(textcolor.Error)
					textcolor.Printf("Can't get altitude from GPGGA sentence: %s\n", altitudeErr)
				}

				result.Fix = FixError

				return result
			}
		} else {
			result.Fix = Fix2D
		}

		return result
	} else {
		if !quiet {
			textcolor.Set(textcolor.Error)
			textcolor.Printf("Can't get altitude from GPGGA sentence.\n")
		}

		result.Fix = FixError

		return result
	}
} /* end ParseGPGGA */

/*-------------------------------------------------------------------
 *
 * Name:        nmeaTerm
 *
 * Purpose:    	Shut down GPS interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func nmeaTerm() {

	// Should probably kill reader thread before closing device to avoid
	// message about read error.

	// s_gpsnmea_port_fd.Close()

} /* end nmeaTerm */

/* end dwgpsnmea.c */
