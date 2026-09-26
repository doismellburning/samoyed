package direwolf

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
 *		GPS.Read	Return most recent location to application.
 *
 *		dwgps_print	Print contents of structure for debugging.
 *
 *		GPS.Term	Shutdown on exit.
 *
 *
 * from below:	GPS.setData	Called from other two implementations to
 *				save data until it is needed.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
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

// GPS holds the most recent position report from whichever GPS receivers
// NewGPS started.  The reader goroutines deposit it with setData as it
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

func NewGPS(ctx context.Context, pconfig *misc_config_s, debug int) *GPS {
	var g = new(GPS)
	g.debug = debug
	g.info.Fix = DWFIX_NOT_INIT // The reader goroutines replace it with DWFIX_NOT_SEEN once they are running.

	dwgpsnmea_init(ctx, g, pconfig, debug)

	dwgpsd_init(ctx, g, pconfig, debug)

	SLEEP_MS(500) /* So receive thread(s) can clear the */
	/* not init status before it gets checked. */

	return g
} /* end NewGPS */

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

func (g *GPS) Read(gpsinfo *GPSInfo) GPSFix {
	if g == nil {
		var none GPSInfo
		none.Fix = DWFIX_NOT_INIT
		*gpsinfo = none

		return gpsinfo.Fix
	}

	g.mu.Lock()

	*gpsinfo = g.info

	g.mu.Unlock()

	if g.debug >= 1 {
		text_color_set(DW_COLOR_DEBUG)
		dwgps_print("gps_read: ", gpsinfo)
	}

	// TODO: Should we check timestamp and complain if very stale?
	// or should we leave that up to the caller?

	return (gpsinfo.Fix)
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_print
 *
 * Purpose:     Print gps information for debugging.
 *
 * Inputs:	msg		- Message for prefix on line.
 *		gpsinfo		- Structure with latitude, longitude, etc.
 *
 * Description:	Caller is responsible for setting text color.
 *
 *--------------------------------------------------------------------*/

func dwgps_print(msg string, gpsinfo *GPSInfo) {
	dw_printf("%stime=%s fix=%d lat=%s lon=%s trk=%s spd=%s alt=%s\n",
		msg,
		gpsinfo.Timestamp.Format(time.RFC3339), gpsinfo.Fix,
		formatMaybeFloat("%.6f", gpsinfo.Lat), formatMaybeFloat("%.6f", gpsinfo.Lon),
		formatMaybeFloat("%.0f", gpsinfo.Track), formatMaybeFloat("%.1f", gpsinfo.SpeedKnots),
		formatMaybeFloat("%.0f", gpsinfo.Altitude))
} /* end dwgps_print */

// formatMaybeFloat renders m with the given verb, or as "unknown" for Nothing.
func formatMaybeFloat(format string, m maybe.Maybe[float64]) string {
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

func (g *GPS) Term() {
	if g == nil {
		return
	}

	dwgpsnmea_term()

	g.gpsd.closeAndClear() // Shut down the GPSD interface.
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

func (g *GPS) setData(gpsinfo *GPSInfo) {
	/* Debug print is handled by the two callers so */
	/* we can distinguish the source. */
	g.mu.Lock()

	g.info = *gpsinfo

	g.mu.Unlock()
} /* end setData */

/* end dwgps.c */
