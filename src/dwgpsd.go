//nolint:gochecknoglobals
package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Interface to location data from a gpsd daemon.
 *
 * Description:	gpsd multiplexes access to a GPS receiver and speaks a
 *		simple JSON-over-TCP protocol, so this talks to it with
 *		just "net" and "encoding/json" - no cgo, no libgps.
 *
 * Reference:	https://gpsd.io/gpsd_json.html
 *
 *---------------------------------------------------------------*/

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

/* Knots per meter/second. */

const MPS_TO_KNOTS = 1.9438444924406

/*
 * How long to wait for the TCP connection to gpsd before giving up. Without a
 * limit, an unreachable host that drops packets rather than refusing the
 * connection (a firewall, typically) would hang startup indefinitely, rather
 * than reporting the problem and letting everything else carry on.
 */

const GPSD_CONNECT_TIMEOUT = 10 * time.Second

// errNotTPV means the report was valid JSON but not a "class":"TPV" one, so there's nothing to apply.
var errNotTPV = errors.New("gpsd report is not a TPV")

// gpsdClient holds the state for the connection to the gpsd daemon.
//
// conn is touched by both dwgpsd_term (caller's goroutine) and
// read_gpsd_thread (reader goroutine), so it's guarded by mu.
type gpsdClient struct {
	mu   sync.Mutex
	conn net.Conn
}

var s_gpsd = new(gpsdClient)

func (c *gpsdClient) setConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
}

// clearConnIfCurrent nils out conn, but only if it still points at the connection
// the caller read, so a stale reader thread can't clobber a newer connection.
func (c *gpsdClient) clearConnIfCurrent(conn net.Conn) {
	c.mu.Lock()

	if c.conn == conn {
		c.conn = nil
	}

	c.mu.Unlock()
}

// closeAndClear detaches the connection and closes it. Close can block, and the
// reader goroutine wants the mutex to report the connection it has just lost, so
// the close happens outside the critical section.
func (c *gpsdClient) closeAndClear() {
	c.mu.Lock()
	var conn = c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsd_init
 *
 * Purpose:    	Initialize the GPSD interface.
 *
 * Inputs:	gps		Where to deposit location reports.
 *
 *		pconfig		Configuration settings.  This includes
 *				host name/address and port for gpsd.
 *
 *		debug	- If >= 1, print results when GPS.Read is called.
 *				(In dwgps.go.)
 *
 *			  If >= 2, location updates are also printed.
 *				(In this file.)
 *
 * Returns:	1 = success
 *		0 = nothing to do  (no host specified in config)
 *		-1 = failure
 *
 * Description:	- Establish TCP connection with gpsd.
 *		- Enable streaming of JSON reports.
 *		- Start up thread to process incoming data.
 *		  It reads from the daemon and deposits into
 *		  shared region via GPS.setData.
 *
 * 		The application calls GPS.Read to get the most
 *		recent information.
 *
 *--------------------------------------------------------------------*/

func dwgpsd_init(ctx context.Context, gps *GPS, pconfig *misc_config_s, debug int) int {
	if debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("dwgpsd_init()\n")
	}

	if pconfig.gpsd_host == "" {
		/* Nothing to do.  Leave initial fix value for not init. */
		return 0
	}

	var addr = net.JoinHostPort(pconfig.gpsd_host, strconv.Itoa(pconfig.gpsd_port))

	var dialCtx, cancelDial = context.WithTimeout(ctx, GPSD_CONNECT_TIMEOUT)
	defer cancelDial()

	var conn, connErr = new(net.Dialer).DialContext(dialCtx, "tcp", addr)
	if connErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to connect to GPSD stream at %s.\n", addr)
		dw_printf("%v\n", connErr)

		return -1
	}

	/* Ask gpsd to start streaming reports as JSON. */

	var _, writeErr = conn.Write([]byte("?WATCH={\"enable\":true,\"json\":true}\n"))
	if writeErr != nil {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Unable to start GPSD watch at %s.\n", addr)
		dw_printf("%v\n", writeErr)

		conn.Close()

		return -1
	}

	s_gpsd.setConn(conn)

	go read_gpsd_thread(ctx, gps, conn, debug)

	/* success */

	return 1
}

/*-------------------------------------------------------------------
 *
 * Name:        read_gpsd_thread
 *
 * Purpose:     Read information from GPSD, as it becomes available, and
 *		store it for later retrieval by GPS.Read.
 *
 * Inputs:	gps	- Where to deposit location reports.
 *
 *		conn	- Connection to gpsd daemon.
 *
 *		debug	- As for dwgpsd_init.  Given by value rather than
 *			  read from s_gpsd, whose debug the next dwgpsd_init
 *			  writes while this may still be running.
 *
 * Description:	This reads newline delimited JSON objects from gpsd and
 *		picks out the "TPV" (Time-Position-Velocity) reports.
 *		Other classes (VERSION, DEVICES, WATCH, SKY, ...) are ignored.
 *
 *--------------------------------------------------------------------*/

func read_gpsd_thread(ctx context.Context, gps *GPS, conn net.Conn, debug int) {
	if debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dw_printf("read_gpsd_thread (%+v)\n", conn)
	}

	var info = new(dwgps_info_t) /* Zero value is DWFIX_NOT_SEEN, nothing else known. */

	if debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dwgps_print("GPSD: ", info)
	}

	gps.setData(info)

	// Scan blocks until gpsd says something, which it need not ever do, so
	// closing the connection is what gets this goroutine back when we are
	// asked to stop.
	defer closeOnDone(ctx, conn)()

	var scanner = bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		var report, err = parse_gpsd_tpv(scanner.Bytes())
		if err != nil || report == nil {
			continue
		}

		apply_gpsd_tpv(info, report)

		info.timestamp = time.Now()

		if debug >= 2 {
			text_color_set(DW_COLOR_DEBUG)
			dwgps_print("GPSD: ", info)
		}

		gps.setData(info)
	}

	if ctx.Err() != nil {
		return // We closed the connection ourselves on the way out.
	}

	/* Lost connection to gpsd, e.g. it was stopped or the network dropped. */

	text_color_set(DW_COLOR_ERROR)
	dw_printf("------------------------------------------\n")
	dw_printf("GPSD: Lost communication with gpsd server.\n")
	dw_printf("------------------------------------------\n")

	info.fix = DWFIX_ERROR

	if debug >= 2 {
		text_color_set(DW_COLOR_DEBUG)
		dwgps_print("GPSD: ", info)
	}

	gps.setData(info)

	s_gpsd.clearConnIfCurrent(conn)

	conn.Close()
}

/*-------------------------------------------------------------------
 *
 * Name:        gpsdTPV / parse_gpsd_tpv
 *
 * Purpose:     Parse a "class":"TPV" report from gpsd.
 *
 * Description:	Fields are pointers so we can tell "absent" from "zero",
 *		which is the same distinction maybe.Maybe makes once the
 *		report reaches dwgps_info_t.
 *
 *		altMSL is the current field name for altitude above mean
 *		sea level; older gpsd versions (< 3.20) called it "alt".
 *
 *--------------------------------------------------------------------*/

type gpsdTPV struct {
	Class  string   `json:"class"`
	Mode   int      `json:"mode"`
	Lat    *float64 `json:"lat"`
	Lon    *float64 `json:"lon"`
	Track  *float64 `json:"track"`
	Speed  *float64 `json:"speed"`
	AltMSL *float64 `json:"altMSL"` //nolint:tagliatelle // Field name is dictated by the gpsd JSON protocol.
	Alt    *float64 `json:"alt"`
}

func parse_gpsd_tpv(line []byte) (*gpsdTPV, error) {
	var classOnly struct {
		Class string `json:"class"`
	}

	var classErr = json.Unmarshal(line, &classOnly)
	if classErr != nil {
		return nil, classErr
	}

	if classOnly.Class != "TPV" {
		return nil, errNotTPV
	}

	var report = new(gpsdTPV)

	var reportErr = json.Unmarshal(line, report)
	if reportErr != nil {
		return nil, reportErr
	}

	return report, nil
}

func apply_gpsd_tpv(info *dwgps_info_t, report *gpsdTPV) {
	var newFix dwfix_t

	switch {
	case report.Mode >= 3:
		newFix = DWFIX_3D
	case report.Mode == 2:
		newFix = DWFIX_2D
	default:
		newFix = DWFIX_NO_FIX
	}

	if newFix != info.fix {
		text_color_set(DW_COLOR_INFO)

		switch newFix {
		case DWFIX_NO_FIX:
			dw_printf("GPSD: Location fix has been lost.\n")
		case DWFIX_2D:
			dw_printf("GPSD: Location fix is now 2D.\n")
		case DWFIX_3D:
			dw_printf("GPSD: Location fix is now 3D.\n")
		default:
		}
	}

	info.fix = newFix

	if newFix < DWFIX_2D {
		/* Keep the last known location; it's better than totally lost. */
		return
	}

	info.dlat = maybe.FromPointer(report.Lat).Or(info.dlat)
	info.dlon = maybe.FromPointer(report.Lon).Or(info.dlon)

	/*
	 * gpsd doesn't repeat every field on every TPV report - one derived from
	 * $GPRMC alone, for example, won't carry altitude even though mode is
	 * still 3D from an earlier $GPGGA-derived report. So a missing field here
	 * just means "unchanged", not "unknown"; keep whatever we saw last
	 * instead of clobbering it. A field that has never been reported at all
	 * is Nothing.
	 */

	info.track = maybe.FromPointer(report.Track).Or(info.track)

	var knots = maybe.Fmap(func(mps float64) float64 { return mps * MPS_TO_KNOTS }, maybe.FromPointer(report.Speed))
	info.speed_knots = knots.Or(info.speed_knots)

	if newFix >= DWFIX_3D {
		info.altitude = maybe.FromPointer(report.AltMSL).
			Or(maybe.FromPointer(report.Alt)).
			Or(info.altitude)
	}
	/* Otherwise keep last known altitude when we downgrade from 3D to 2D fix. */
	/* Caller knows altitude is outdated if info.fix == DWFIX_2D. */
}

/*-------------------------------------------------------------------
 *
 * Name:        dwgpsd_term
 *
 * Purpose:    	Shut down GPSD interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func dwgpsd_term() {
	s_gpsd.closeAndClear()
}
