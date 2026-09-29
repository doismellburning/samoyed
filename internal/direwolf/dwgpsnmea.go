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
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/latlong"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/serialport"
	"github.com/pkg/term"
	"github.com/sirupsen/logrus"
)

// gpsnmeaPort is the serial port a GPS receiver is read from, as opened by
// dwgpsnmea_init.  The waypoint sender can share it (see GPS.SharedNMEAPort),
// and the reader goroutine closes it and clears fd if the receiver goes away,
// so fd is guarded by mu.
//
// Its zero value is a port that was never opened.
type gpsnmeaPort struct {
	mu    sync.Mutex
	name  string
	speed int
	fd    *term.Term
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsnmea_init
 *
 * Purpose:    	Open serial port for the GPS receiver.
 *
 * Inputs:	gps		Where to deposit location reports.
 *
 *		pconfig		Configuration settings.  This includes
 *				serial port name for direct connect.
 *
 *		debug	- If >= 1, print results when GPS.Read is called.
 *				(In different file.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In this file.)
 *				Why not do it in GPS.SetData() ?
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
 *			  the GPS it was given.
 *
 * 		The application calls GPS.Read to get the most recent information.
 *
 *--------------------------------------------------------------------*/

func dwgpsnmea_init(ctx context.Context, gps *GPS, pconfig *GPSConfig, debug int) int {
	if debug >= 2 {
		logrus.Debug("dwgpsnmea_init")
	}

	if pconfig.NMEAPort == "" {
		/* Nothing to do.  Leave initial fix value for not init. */
		return (0)
	}

	/*
	 * Open serial port connection.
	 */

	var fd = serialport.Open(pconfig.NMEAPort, pconfig.NMEASpeed)

	if fd != nil {
		gps.nmea.mu.Lock()
		gps.nmea.name = pconfig.NMEAPort
		gps.nmea.speed = pconfig.NMEASpeed
		gps.nmea.fd = fd
		gps.nmea.mu.Unlock()

		go read_gpsnmea_thread(ctx, gps, fd, debug)
	} else {
		logrus.WithField("port", pconfig.NMEAPort).Error("Could not open serial port for GPS receiver")

		return (-1)
	}

	/* success */

	return (1)
} /* end dwgpsnmea_init */

// SharedNMEAPort returns the GPS receiver's serial port, for the waypoint
// sender to share, if it is the one named, at the same speed, and still open.
// Otherwise, or for a nil GPS, it returns nil.
func (g *GPS) SharedNMEAPort(name string, speed int) *term.Term {
	if g == nil {
		return nil
	}

	g.nmea.mu.Lock()
	defer g.nmea.mu.Unlock()

	if g.nmea.fd != nil && g.nmea.name == name && g.nmea.speed == speed {
		return g.nmea.fd
	}

	return nil
}

// closeIfCurrent closes fd, and forgets it if it is still the port's.
func (p *gpsnmeaPort) closeIfCurrent(fd *term.Term) {
	p.mu.Lock()

	if p.fd == fd {
		p.fd = nil
	}

	p.mu.Unlock()

	serialport.Close(fd)
}

/*-------------------------------------------------------------------
 *
 * Name:        read_gpsnmea_thread
 *
 * Purpose:     Read information from GPS, as it becomes available, and
 *		store it for later retrieval by GPS.Read.
 *
 * Inputs:	gps	- Where to deposit location reports.
 *
 *		fd	- File descriptor for serial port.
 *
 *		debug	- As for dwgpsnmea_init.
 *
 * Description:	This version reads from serial port and parses the
 *		NMEA sentences.
 *
 *--------------------------------------------------------------------*/

const TIMEOUT = 5

func read_gpsnmea_thread(ctx context.Context, gps *GPS, fd *term.Term, debug int) {
	// Maximum length of message from GPS receiver is 82 according to some people.
	// Make buffer considerably larger to be safe.
	const NMEA_MAX_LEN = 160

	if debug >= 2 {
		logrus.Debug("read_gpsnmea_thread")
	}

	var info = new(GPSInfo) /* Zero value is DWFIX_NOT_SEEN, nothing else known. */

	if debug >= 2 {
		dwgps_print("GPSNMEA", info)
	}

	gps.SetData(info)

	var gps_msg string

	// As in KissSerial.get, a cancellation is noticed between sentences
	// rather than during one: the port is read directly rather than through
	// something the runtime can interrupt, so closing it would not get this
	// goroutine back - and this port can be shared with the waypoint sender
	// (see GPS.SharedNMEAPort), which closes it in its own teardown.
	for ctx.Err() == nil {
		var ch, err = serialport.Get1(fd)
		if err != nil {
			if ctx.Err() != nil {
				return // We closed it ourselves on the way out.
			}

			/* This might happen if a USB  device is unplugged. */
			/* I can't imagine anything that would cause it with */
			/* a normal serial port. */
			logrus.Error("GPSNMEA: Lost communication with GPS receiver")

			// Close the port before reporting the error, so that nobody
			// who has seen DWFIX_ERROR can still be handed it to share.
			gps.nmea.closeIfCurrent(fd)

			info.Fix = DWFIX_ERROR

			if debug >= 2 {
				dwgps_print("GPSNMEA", info)
			}

			gps.SetData(info)

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
				if debug >= 3 {
					logrus.WithField("sentence", gps_msg).Trace("GPSNMEA: Sentence")
				}

				/* Process sentence. */
				// TODO: More general: Ignore the second letter rather than recognizing only GP... and GN...

				if strings.HasPrefix(gps_msg, "$GPRMC") || strings.HasPrefix(gps_msg, "$GNRMC") {
					// Here we just tuck away the course and speed.
					// Fix and location will be updated by GxGGA.
					var f = ParseGPRMC(gps_msg, false)

					if f.Fix == DWFIX_ERROR {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						logrus.WithField("sentence", gps_msg).Warn("GPSNMEA: Error parsing $GPRMC sentence")
					} else {
						info.SpeedKnots = f.Knots.Or(info.SpeedKnots)
						info.Track = f.Course.Or(info.Track)
					}
				} else if strings.HasPrefix(gps_msg, "$GPGGA") || strings.HasPrefix(gps_msg, "$GNGGA") {
					var f = ParseGPGGA(gps_msg, false)

					if f.Fix == DWFIX_ERROR {
						/* Parse error.  Shouldn't happen.  Better luck next time. */
						logrus.WithField("sentence", gps_msg).Warn("GPSNMEA: Error parsing $GPGGA sentence")
					} else {
						info.Lat = f.Lat.Or(info.Lat)
						info.Lon = f.Lon.Or(info.Lon)
						info.Altitude = f.Alt.Or(info.Altitude)

						if f.Fix != info.Fix { // Print change in location fix.
							switch f.Fix {
							case DWFIX_NO_FIX:
								logrus.Info("GPSNMEA: Location fix has been lost")
							case DWFIX_2D:
								logrus.Info("GPSNMEA: Location fix is now 2D")
							case DWFIX_3D:
								logrus.Info("GPSNMEA: Location fix is now 3D")
							default:
							}

							info.Fix = f.Fix
						}

						info.Timestamp = time.Now()

						if debug >= 2 {
							dwgps_print("GPSNMEA", info)
						}

						gps.SetData(info)
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

	// serialport.Close (the port's fd);

} /* end dwgps_term */

/* end dwgpsnmea.c */
