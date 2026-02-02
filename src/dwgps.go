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
 *		(2) Read from gpsd.  Not available for Windows.
 *		    Including this is optional because it depends
 *		    on another external software component.
 *
 *
 * API:		dwgps_init	Connect to data stream at start up time.
 *
 *		dwgps_read	Return most recent location to application.
 *
 *		dwgps_print	Print contents of structure for debugging.
 *
 *		dwgps_term	Shutdown on exit.
 *
 *
 * from below:	dwgps_set_data	Called from other two implementations to
 *				save data until it is needed.
 *
 *---------------------------------------------------------------*/

import (
	"sync"
	"time"
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
 * Undefined float & double values are set to G_UNKNOWN.
 *
 */

type dwfix_t int

const (
	DWFIX_NOT_INIT dwfix_t = -2
	DWFIX_ERROR    dwfix_t = -1
	DWFIX_NOT_SEEN dwfix_t = 0
	DWFIX_NO_FIX   dwfix_t = 1
	DWFIX_2D       dwfix_t = 2
	DWFIX_3D       dwfix_t = 3
)

type dwgps_info_t struct {
	timestamp   time.Time /* When last updated.  System time. */
	fix         dwfix_t   /* Quality of position fix. */
	dlat        float64   /* Latitude.  Valid if fix >= 2. */
	dlon        float64   /* Longitude. Valid if fix >= 2. */
	speed_knots float64   /* libgps uses meters/sec but we use GPS usual knots. */
	track       float64   /* What is difference between track and course? */
	altitude    float64   /* meters above mean sea level. Valid if fix == 3. */
}

var s_dwgps_debug = 0 /* Enable debug output. */
/* >= 2 show updates from GPS. */
/* >= 1 show results from dwgps_read. */

/*
 * The GPS reader threads deposit current data here when it becomes available.
 * dwgps_read returns it to the requesting application.
 *
 * A critical region to avoid inconsistency between fields.
 */

var s_dwgps_info = new(dwgps_info_t)

var s_gps_mutex sync.Mutex

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_init
 *
 * Purpose:    	Initialize the GPS interface.
 *
 * Inputs:	pconfig		Configuration settings.  This might include
 *				serial port name for direct connect and host
 *				name or address for network connection.
 *
 *		debug	- If >= 1, print results when dwgps_read is called.
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

func dwgps_init(pconfig *misc_config_s, debug int) {

	dwgps_clear(s_dwgps_info) // Init the global

	s_dwgps_debug = debug

	dwgpsnmea_init(pconfig, debug)

	/* TODO KG
	#if ENABLE_GPSD
	dwgpsd_init(pconfig, debug)
	*/

	SLEEP_MS(500) /* So receive thread(s) can clear the */
	/* not init status before it gets checked. */

} /* end dwgps_init */

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_clear
 *
 * Purpose:    	Clear the gps info structure.
 *
 *--------------------------------------------------------------------*/

func dwgps_clear(gpsinfo *dwgps_info_t) {
	gpsinfo.timestamp = time.Time{}
	gpsinfo.fix = DWFIX_NOT_SEEN
	gpsinfo.dlat = G_UNKNOWN
	gpsinfo.dlon = G_UNKNOWN
	gpsinfo.speed_knots = G_UNKNOWN
	gpsinfo.track = G_UNKNOWN
	gpsinfo.altitude = G_UNKNOWN
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_read
 *
 * Purpose:     Return most recent location data available.
 *
 * Outputs:	gpsinfo		- Structure with latitude, longitude, etc.
 *
 * Returns:	Position fix quality.  Same as in structure.
 *
 *
 *--------------------------------------------------------------------*/

func dwgps_read(gpsinfo *dwgps_info_t) dwfix_t {

	s_gps_mutex.Lock()

	*gpsinfo = *s_dwgps_info

	s_gps_mutex.Unlock()

	if s_dwgps_debug >= 1 {
		text_color_set(DW_COLOR_DEBUG)
		dwgps_print("gps_read: ", gpsinfo)
	}

	// TODO: Should we check timestamp and complain if very stale?
	// or should we leave that up to the caller?

	return (s_dwgps_info.fix)
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

func dwgps_print(msg string, gpsinfo *dwgps_info_t) {

	dw_printf("%stime=%s fix=%d lat=%.6f lon=%.6f trk=%.0f spd=%.1f alt=%.0f\n",
		msg,
		gpsinfo.timestamp.Format(time.RFC3339), gpsinfo.fix,
		gpsinfo.dlat, gpsinfo.dlon,
		gpsinfo.track, gpsinfo.speed_knots,
		gpsinfo.altitude)

} /* end dwgps_set_data */

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_term
 *
 * Purpose:    	Shut down GPS interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func dwgps_term() {

	dwgpsnmea_term()

	/* TODO KG
	#if ENABLE_GPSD
	dwgpsd_term()
	*/

} /* end dwgps_term */

/*-------------------------------------------------------------------
 *
 * Name:        dwgps_set_data
 *
 * Purpose:     Called by the GPS interfaces when new data is available.
 *
 * Inputs:	gpsinfo		- Structure with latitude, longitude, etc.
 *
 *--------------------------------------------------------------------*/

func dwgps_set_data(gpsinfo *dwgps_info_t) {

	/* Debug print is handled by the two callers so */
	/* we can distinguish the source. */

	s_gps_mutex.Lock()

	*s_dwgps_info = *gpsinfo

	s_gps_mutex.Unlock()

} /* end dwgps_set_data */

/* end dwgps.c */
