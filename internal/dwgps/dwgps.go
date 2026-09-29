// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dwgps reads the station's location from a GPS receiver, over a
// serial port or from gpsd.
package dwgps

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface for obtaining location from GPS.
 *
 * Description:	This is a wrapper for two different implementations:
 *
 *		(1) Read NMEA sentences from a serial port (or USB
 *		    that looks line one).  Available for all platforms.
 *
 *		(2) Read from gpsd, over its JSON-over-TCP protocol.
 *		    Requires a gpsd daemon to be running and reachable;
 *		    no separate library dependency is needed.
 *
 *
 * API:		NewGPS		Connect to data stream at start up time.
 *
 *		NewGPSNMEA	Same, for just a serial port.
 *
 *		GPS.Read	Return most recent location to application.
 *
 *		dwgps_print	Print contents of structure for debugging.
 *
 *		GPS.Term	Shutdown on exit.
 *
 *
 * from below:	GPS.SetData	Called from other two implementations to
 *				save data until it is needed.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

/*
 * Values for fix, equivalent to values from libgps.
 *	-2 = not initialized.
 *	-1 = error communicating with GPS receiver.
 *	0 = nothing heard yet.
 *	1 = had signal but lost it.
 *	2 = 2D.
 *	3 = 3D.
 *
 * Values that have not been reported are Nothing.
 *
 */

// GPSFix is how good a position GPSInfo holds: one of the DWFIX_* values.
type GPSFix int

const (
	DWFIX_NOT_INIT GPSFix = -2
	DWFIX_ERROR    GPSFix = -1
	DWFIX_NOT_SEEN GPSFix = 0
	DWFIX_NO_FIX   GPSFix = 1
	DWFIX_2D       GPSFix = 2
	DWFIX_3D       GPSFix = 3
)

// GPSInfo is the most recent position report from a GPS receiver.  Its
// zero value is "nothing heard yet", so a freshly declared one needs no
// clearing.
type GPSInfo struct {
	Timestamp  time.Time            /* When last updated.  System time. */
	Fix        GPSFix               /* Quality of position fix. */
	Lat        maybe.Maybe[float64] /* Latitude.  Valid if fix >= 2. */
	Lon        maybe.Maybe[float64] /* Longitude. Valid if fix >= 2. */
	SpeedKnots maybe.Maybe[float64] /* libgps uses meters/sec but we use GPS usual knots. */
	Track      maybe.Maybe[float64] /* What is difference between track and course? */
	Altitude   maybe.Maybe[float64] /* meters above mean sea level. Valid if fix == 3. */
}

// Config says where NewGPS finds its GPS receivers.  Leave a receiver's
// fields at their zero values to not use it.
type Config struct {
	NMEAPort  string /* Serial port name for reading NMEA sentences from GPS. e.g. COM22, /dev/ttyACM0 */
	NMEASpeed int    /* Speed for above, baud.  0 leaves the port's speed as it is. */
	GPSDHost  string /* Host for gpsd server. e.g. localhost, 192.168.1.2 */
	GPSDPort  int    /* Port number for gpsd server. */
}

// GPS holds the most recent position report from whichever GPS receivers
// NewGPS started.  The reader goroutines deposit it with SetData as it
// arrives and Read hands a copy to the application; mu keeps the fields of
// one report together.
//
// A nil *GPS is one that was never started: Read reports DWFIX_NOT_INIT, as
// a GPS with no receiver configured does, and Term does nothing.  So code that
// can run before startup has got this far needs no guard.
type GPS struct {
	debug int /* >= 1 show results from Read.  Set once by NewGPS. */

	mu   sync.Mutex
	info GPSInfo

	nmea gpsnmeaPort // The GPSNMEA receiver's serial port, if one was opened.
	gpsd gpsdClient  // The connection to gpsd, if one was made.
}

/*-------------------------------------------------------------------
 *
 * Name:        NewGPS
 *
 * Purpose:    	Initialize the GPS interface.
 *
 * Inputs:	pconfig		Configuration settings.  This might include
 *				serial port name for direct connect and host
 *				name or address for network connection.
 *
 *		debug	- If >= 1, print results when Read is called.
 *				(In this file.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In other two related files.)
 *
 * Returns:	The GPS.  Its fix stays DWFIX_NOT_INIT if no receiver was
 *		configured, or none could be opened.
 *
 * Description:	Call corresponding functions for implementations.
 * 		Normally we would expect someone to use either GPSNMEA or
 *		GPSD but there is nothing to prevent use of both at the
 *		same time.
 *
 *--------------------------------------------------------------------*/

func NewGPS(ctx context.Context, pconfig *Config, debug int) *GPS {
	var g = new(GPS)
	g.debug = debug
	g.info.Fix = DWFIX_NOT_INIT // The reader goroutines replace it with DWFIX_NOT_SEEN once they are running.

	dwgpsnmea_init(ctx, g, pconfig, debug)

	dwgpsd_init(ctx, g, pconfig, debug)

	_ = dwutil.SleepCtx(ctx, 500*time.Millisecond) /* So receive thread(s) can clear the */
	/* not init status before it gets checked. */

	return g
} /* end NewGPS */

// NewGPSNMEA starts reading NMEA sentences from the GPS receiver on the
// serial port named port, leaving its speed as it is and not using gpsd.  It
// is NewGPS for a standalone tool with nothing else to configure.
func NewGPSNMEA(ctx context.Context, port string, debug int) *GPS {
	var config Config
	config.NMEAPort = port

	return NewGPS(ctx, &config, debug)
}

// Read returns the most recent location data available.  Its Fix says how
// far the rest of it can be trusted.
func (g *GPS) Read() GPSInfo {
	var gpsinfo GPSInfo

	if g == nil {
		gpsinfo.Fix = DWFIX_NOT_INIT

		return gpsinfo
	}

	g.mu.Lock()

	gpsinfo = g.info

	g.mu.Unlock()

	if g.debug >= 1 {
		dwgps_print("gps_read", &gpsinfo)
	}

	// TODO: Should we check timestamp and complain if very stale?
	// or should we leave that up to the caller?

	return gpsinfo
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_print
 *
 * Purpose:     Log gps information for debugging.
 *
 * Inputs:	source		- Where it came from, for the log entry.
 *		gpsinfo		- Structure with latitude, longitude, etc.
 *
 *--------------------------------------------------------------------*/

func dwgps_print(source string, gpsinfo *GPSInfo) {
	logrus.WithFields(logrus.Fields{
		"source": source,
		"time":   gpsinfo.Timestamp.Format(time.RFC3339),
		"fix":    gpsinfo.Fix,
		"lat":    maybe.Format("%.6f", "unknown", gpsinfo.Lat),
		"lon":    maybe.Format("%.6f", "unknown", gpsinfo.Lon),
		"trk":    maybe.Format("%.0f", "unknown", gpsinfo.Track),
		"spd":    maybe.Format("%.1f", "unknown", gpsinfo.SpeedKnots),
		"alt":    maybe.Format("%.0f", "unknown", gpsinfo.Altitude),
	}).Debug("GPS location")
} /* end dwgps_print */

/*-------------------------------------------------------------------
 *
 * Name:        Term
 *
 * Purpose:    	Shut down GPS interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func (g *GPS) Term() {
	if g == nil {
		return
	}

	dwgpsnmea_term()

	g.gpsd.closeAndClear() // Shut down the GPSD interface.
} /* end Term */

/*-------------------------------------------------------------------
 *
 * Name:        SetData
 *
 * Purpose:     Called by the GPS interfaces when new data is available.
 *		Also for tests elsewhere that need a GPS reporting a
 *		given location.
 *
 * Inputs:	gpsinfo		- Structure with latitude, longitude, etc.
 *
 *--------------------------------------------------------------------*/

func (g *GPS) SetData(gpsinfo *GPSInfo) {
	/* Debug print is handled by the two callers so */
	/* we can distinguish the source. */
	g.mu.Lock()

	g.info = *gpsinfo

	g.mu.Unlock()
} /* end SetData */

/* end dwgps.c */
