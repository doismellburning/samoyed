// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwgps

// Parsing NMEA sentences, apart from reading them from a receiver (see
// dwgpsnmea.go), so it builds where there are no serial ports - APRS decoding
// needs it for packets that carry raw NMEA.

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

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
			logrus.WithField("sentence", sent).Warn("Missing GPS checksum")
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
			logrus.WithField("sentence", sent).Warn(errorMsg)
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
	Lat    maybe.Maybe[float64]
	Lon    maybe.Maybe[float64]
	Knots  maybe.Maybe[float64]
	Course maybe.Maybe[float64]
	Fix    GPSFix
}

func ParseGPRMC(sentence string, quiet bool) *GPRMCResult {
	var result = new(GPRMCResult)

	// TODO Default to Error, because that's what most returns are? On the other hand it's good to be explicit...
	result.Fix = DWFIX_NO_FIX

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
			logrus.WithField("sentence", sentence).Warn("No status in GPRMC sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		var lat, latErr = latlong.LatitudeFromNMEA(plat, pns[0])
		if latErr != nil {
			if !quiet {
				logrus.WithError(latErr).WithField("sentence", sentence).Warn("Can't get latitude from GPRMC sentence")
			}

			result.Fix = DWFIX_ERROR

			return result
		}

		result.Lat = maybe.Just(lat)
	} else {
		if !quiet {
			logrus.WithField("sentence", sentence).Warn("Can't get latitude from GPRMC sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		var lon, lonErr = latlong.LongitudeFromNMEA(plon, pew[0])
		if lonErr != nil {
			if !quiet {
				logrus.WithError(lonErr).WithField("sentence", sentence).Warn("Can't get longitude from GPRMC sentence")
			}

			result.Fix = DWFIX_ERROR

			return result
		}

		result.Lon = maybe.Just(lon)
	} else {
		if !quiet {
			logrus.WithField("sentence", sentence).Warn("Can't get longitude from GPRMC sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	/* Speed over ground is a magnitude, so a negative one is as unusable as
	 * a field that isn't a number at all - and so is an infinity, which
	 * ParseFloat is happy to return for "inf". */
	var knots, knotsErr = strconv.ParseFloat(pknots, 64)
	if knotsErr == nil && knots >= 0 && !math.IsInf(knots, 0) {
		result.Knots = maybe.Just(knots)
	} else {
		if !quiet {
			logrus.WithField("speed", pknots).Warn("Can't get speed from GPRMC sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	/* Track made good is degrees true, so anything outside a circle is not a
	 * course, and is left absent like the empty field a stationary receiver
	 * sends. */
	var course, courseErr = strconv.ParseFloat(pcourse, 64)
	if courseErr == nil && course >= 0 && course <= 360 {
		result.Course = maybe.Just(course)
	}

	//text_color_set (DW_COLOR_INFO);
	//dw_printf("%.6f %.6f %.1f %.0f\n", *odlat, *odlon, *oknots, *ocourse);

	result.Fix = DWFIX_2D

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
	Lat maybe.Maybe[float64]
	Lon maybe.Maybe[float64]
	Alt maybe.Maybe[float64]
	Sat maybe.Maybe[int]
	Fix GPSFix
}

func ParseGPGGA(sentence string, quiet bool) *GPGGAResult {
	var result = new(GPGGAResult)

	result.Fix = DWFIX_NO_FIX

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
			logrus.WithField("sentence", sentence).Warn("No fix in GPGGA sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plat) > 0 && len(pns) > 0 {
		var lat, latErr = latlong.LatitudeFromNMEA(plat, pns[0])
		if latErr != nil {
			if !quiet {
				logrus.WithError(latErr).WithField("sentence", sentence).Warn("Can't get latitude from GPGGA sentence")
			}

			result.Fix = DWFIX_ERROR

			return result
		}

		result.Lat = maybe.Just(lat)
	} else {
		if !quiet {
			logrus.WithField("sentence", sentence).Warn("Can't get latitude from GPGGA sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}

	if len(plon) > 0 && len(pew) > 0 {
		var lon, lonErr = latlong.LongitudeFromNMEA(plon, pew[0])
		if lonErr != nil {
			if !quiet {
				logrus.WithError(lonErr).WithField("sentence", sentence).Warn("Can't get longitude from GPGGA sentence")
			}

			result.Fix = DWFIX_ERROR

			return result
		}

		result.Lon = maybe.Just(lon)
	} else {
		if !quiet {
			logrus.WithField("sentence", sentence).Warn("Can't get longitude from GPGGA sentence")
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
			/* ParseFloat accepts "NaN" and "inf", neither of which survives
			 * the conversion to the integer feet of an /A= field. */
			var altitude, altitudeErr = strconv.ParseFloat(paltitude, 64)
			if altitudeErr == nil && !math.IsNaN(altitude) && !math.IsInf(altitude, 0) {
				result.Alt = maybe.Just(altitude)
				result.Fix = DWFIX_3D
			} else {
				if !quiet {
					logrus.WithField("altitude", paltitude).Warn("Can't get altitude from GPGGA sentence")
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
			logrus.WithField("sentence", sentence).Warn("Can't get altitude from GPGGA sentence")
		}

		result.Fix = DWFIX_ERROR

		return result
	}
} /* end ParseGPGGA */
