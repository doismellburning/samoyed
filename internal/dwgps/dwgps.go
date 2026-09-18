// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dwgps obtains the station's location from a GPS receiver.
//
//nolint:gochecknoglobals
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
 * API:		Init		Connect to data stream at start up time.
 *
 *		Read		Return most recent location to application.
 *
 *		Print		Print contents of structure for debugging.
 *
 *		Term		Shutdown on exit.
 *
 *
 * from below:	setData		Called from other two implementations to
 *				save data until it is needed.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/textcolor"
	"github.com/pkg/term"
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

type Fix int

const (
	FixNotInit Fix = -2
	FixError   Fix = -1
	FixNotSeen Fix = 0
	FixNoFix   Fix = 1
	Fix2D      Fix = 2
	Fix3D      Fix = 3
)

// Info is the most recent position report from a GPS receiver.  Its zero value
// is "nothing heard yet", so a freshly declared one needs no clearing.
type Info struct {
	Timestamp  time.Time            /* When last updated.  System time. */
	Fix        Fix                  /* Quality of position fix. */
	Lat        maybe.Maybe[float64] /* Latitude.  Valid if Fix >= 2. */
	Lon        maybe.Maybe[float64] /* Longitude. Valid if Fix >= 2. */
	SpeedKnots maybe.Maybe[float64] /* libgps uses meters/sec but we use GPS usual knots. */
	Track      maybe.Maybe[float64] /* What is difference between track and course? */
	Altitude   maybe.Maybe[float64] /* meters above mean sea level. Valid if Fix == 3. */
}

// Config is where to find the GPS, taken from the application's configuration
// settings.  A serial port is opened through OpenSerialPort rather than by this
// package, which has no business knowing how the application talks to one.
type Config struct {
	NMEAPort  string /* Serial port name for reading NMEA sentences from GPS. e.g. COM22, /dev/ttyACM0 */
	NMEASpeed int    /* Speed for above, baud. */

	// OpenSerialPort opens NMEAPort, returning nil if it cannot.  A nil
	// OpenSerialPort means the same thing, so there is no serial GPS.
	OpenSerialPort func(devicename string, baud int) *term.Term

	GPSDHost string /* Host for gpsd server. e.g. localhost, 192.168.1.2 */
	GPSDPort int    /* Port number for gpsd server. */
}

var s_dwgps_debug = 0 /* Enable debug output. */
/* >= 2 show updates from GPS. */
/* >= 1 show results from Read. */

/*
 * The GPS reader threads deposit current data here when it becomes available.
 * Read returns it to the requesting application.
 *
 * A critical region to avoid inconsistency between fields.
 */

var s_dwgps_info = new(Info)

var s_gps_mutex sync.Mutex

/*-------------------------------------------------------------------
 *
 * Name:        Init
 *
 * Purpose:    	Initialize the GPS interface.
 *
 * Inputs:	config		Where to find the GPS.  This might include
 *				serial port name for direct connect and host
 *				name or address for network connection.
 *
 *		debug	- If >= 1, print results when Read is called.
 *				(In this file.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In other two related files.)
 *
 * Returns:	none
 *
 * Description:	Call corresponding functions for implementations.
 * 		Normally we would expect someone to use either GPSNMEA or
 *		GPSD but there is nothing to prevent use of both at the
 *		same time.
 *
 *--------------------------------------------------------------------*/

func Init(config *Config, debug int) {
	setData(new(Info)) // Init the global

	s_dwgps_debug = debug

	nmeaInit(config, debug)

	gpsdInit(config, debug)

	time.Sleep(500 * time.Millisecond) /* So receive thread(s) can clear the */
	/* not init status before it gets checked. */
} /* end Init */

/*-------------------------------------------------------------------
 *
 * Name:        Read
 *
 * Purpose:     Return most recent location data available.
 *
 * Outputs:	gpsinfo		- Structure with latitude, longitude, etc.
 *
 * Returns:	Position fix quality.  Same as in structure.
 *
 *
 *--------------------------------------------------------------------*/

func Read(gpsinfo *Info) Fix {
	s_gps_mutex.Lock()

	*gpsinfo = *s_dwgps_info

	s_gps_mutex.Unlock()

	if s_dwgps_debug >= 1 {
		textcolor.Set(textcolor.Debug)
		Print("gps_read: ", gpsinfo)
	}

	// TODO: Should we check timestamp and complain if very stale?
	// or should we leave that up to the caller?

	return (gpsinfo.Fix)
}

/*-------------------------------------------------------------------
 *
 * Name:        Print
 *
 * Purpose:     Print gps information for debugging.
 *
 * Inputs:	msg		- Message for prefix on line.
 *		gpsinfo		- Structure with latitude, longitude, etc.
 *
 * Description:	Caller is responsible for setting text color.
 *
 *--------------------------------------------------------------------*/

func Print(msg string, gpsinfo *Info) {
	textcolor.Printf("%stime=%s fix=%d lat=%s lon=%s trk=%s spd=%s alt=%s\n",
		msg,
		gpsinfo.Timestamp.Format(time.RFC3339), gpsinfo.Fix,
		FormatMaybeFloat("%.6f", gpsinfo.Lat), FormatMaybeFloat("%.6f", gpsinfo.Lon),
		FormatMaybeFloat("%.0f", gpsinfo.Track), FormatMaybeFloat("%.1f", gpsinfo.SpeedKnots),
		FormatMaybeFloat("%.0f", gpsinfo.Altitude))
} /* end Print */

// FormatMaybeFloat renders m with the given verb, or as "unknown" for Nothing.
func FormatMaybeFloat(format string, m maybe.Maybe[float64]) string {
	return maybe.Fold("unknown", func(value float64) string {
		return fmt.Sprintf(format, value)
	}, m)
}

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

func Term() {
	nmeaTerm()

	gpsdTerm()
} /* end Term */

/*-------------------------------------------------------------------
 *
 * Name:        setData
 *
 * Purpose:     Called by the GPS interfaces when new data is available.
 *
 * Inputs:	gpsinfo		- Structure with latitude, longitude, etc.
 *
 *--------------------------------------------------------------------*/

func setData(gpsinfo *Info) {
	/* Debug print is handled by the two callers so */
	/* we can distinguish the source. */
	s_gps_mutex.Lock()

	*s_dwgps_info = *gpsinfo

	s_gps_mutex.Unlock()
} /* end setData */

/* end dwgps.c */
