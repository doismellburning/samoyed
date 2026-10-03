// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !js

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
	"context"
	"strings"
	"sync"
	"time"

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

func dwgpsnmea_init(ctx context.Context, gps *GPS, pconfig *Config, debug int) int {
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
