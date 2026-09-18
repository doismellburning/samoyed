// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//nolint:gochecknoglobals
package dwgps

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
	"github.com/doismellburning/samoyed/internal/textcolor"
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
// debug is set once in gpsdInit, before the reader goroutine is started, and
// only read afterwards, so it doesn't need mutex protection. conn is touched by
// both gpsdTerm (caller's goroutine) and read_gpsd_thread (reader goroutine),
// so it's guarded by mu.
type gpsdClient struct {
	debug int
	mu    sync.Mutex
	conn  net.Conn
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
 * Name:        gpsdInit
 *
 * Purpose:    	Initialize the GPSD interface.
 *
 * Inputs:	pconfig		Where to find the GPS.  This includes the
 *				host name/address and port for gpsd.
 *
 *		debug	- If >= 1, print results when Read is called.
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
 *		  shared region via setData.
 *
 * 		The application calls Read to get the most
 *		recent information.
 *
 *--------------------------------------------------------------------*/

func gpsdInit(pconfig *Config, debug int) int {
	s_gpsd.debug = debug

	if s_gpsd.debug >= 2 {
		textcolor.Set(textcolor.Debug)
		textcolor.Printf("gpsdInit()\n")
	}

	if pconfig.GPSDHost == "" {
		/* Nothing to do.  Leave initial fix value for not init. */
		return 0
	}

	var addr = net.JoinHostPort(pconfig.GPSDHost, strconv.Itoa(pconfig.GPSDPort))

	var dialCtx, cancelDial = context.WithTimeout(context.Background(), GPSD_CONNECT_TIMEOUT)
	defer cancelDial()

	var conn, connErr = new(net.Dialer).DialContext(dialCtx, "tcp", addr)
	if connErr != nil {
		textcolor.Set(textcolor.Error)
		textcolor.Printf("Unable to connect to GPSD stream at %s.\n", addr)
		textcolor.Printf("%v\n", connErr)

		return -1
	}

	/* Ask gpsd to start streaming reports as JSON. */

	var _, writeErr = conn.Write([]byte("?WATCH={\"enable\":true,\"json\":true}\n"))
	if writeErr != nil {
		textcolor.Set(textcolor.Error)
		textcolor.Printf("Unable to start GPSD watch at %s.\n", addr)
		textcolor.Printf("%v\n", writeErr)

		conn.Close()

		return -1
	}

	s_gpsd.setConn(conn)

	go read_gpsd_thread(conn)

	/* success */

	return 1
}

/*-------------------------------------------------------------------
 *
 * Name:        read_gpsd_thread
 *
 * Purpose:     Read information from GPSD, as it becomes available, and
 *		store it for later retrieval by Read.
 *
 * Inputs:	conn	- Connection to gpsd daemon.
 *
 * Description:	This reads newline delimited JSON objects from gpsd and
 *		picks out the "TPV" (Time-Position-Velocity) reports.
 *		Other classes (VERSION, DEVICES, WATCH, SKY, ...) are ignored.
 *
 *--------------------------------------------------------------------*/

func read_gpsd_thread(conn net.Conn) {
	if s_gpsd.debug >= 2 {
		textcolor.Set(textcolor.Debug)
		textcolor.Printf("read_gpsd_thread (%+v)\n", conn)
	}

	var info = new(Info) /* Zero value is FixNotSeen, nothing else known. */

	if s_gpsd.debug >= 2 {
		textcolor.Set(textcolor.Debug)
		Print("GPSD: ", info)
	}

	setData(info)

	var scanner = bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)

	for scanner.Scan() {
		var report, err = parse_gpsd_tpv(scanner.Bytes())
		if err != nil || report == nil {
			continue
		}

		apply_gpsd_tpv(info, report)

		info.Timestamp = time.Now()

		if s_gpsd.debug >= 2 {
			textcolor.Set(textcolor.Debug)
			Print("GPSD: ", info)
		}

		setData(info)
	}

	/* Lost connection to gpsd, e.g. it was stopped or the network dropped. */

	textcolor.Set(textcolor.Error)
	textcolor.Printf("------------------------------------------\n")
	textcolor.Printf("GPSD: Lost communication with gpsd server.\n")
	textcolor.Printf("------------------------------------------\n")

	info.Fix = FixError

	if s_gpsd.debug >= 2 {
		textcolor.Set(textcolor.Debug)
		Print("GPSD: ", info)
	}

	setData(info)

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
 *		report reaches Info.
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

func apply_gpsd_tpv(info *Info, report *gpsdTPV) {
	var newFix Fix

	switch {
	case report.Mode >= 3:
		newFix = Fix3D
	case report.Mode == 2:
		newFix = Fix2D
	default:
		newFix = FixNoFix
	}

	if newFix != info.Fix {
		textcolor.Set(textcolor.Info)

		switch newFix {
		case FixNoFix:
			textcolor.Printf("GPSD: Location fix has been lost.\n")
		case Fix2D:
			textcolor.Printf("GPSD: Location fix is now 2D.\n")
		case Fix3D:
			textcolor.Printf("GPSD: Location fix is now 3D.\n")
		default:
		}
	}

	info.Fix = newFix

	if newFix < Fix2D {
		/* Keep the last known location; it's better than totally lost. */
		return
	}

	info.Lat = maybe.FromPointer(report.Lat).Or(info.Lat)
	info.Lon = maybe.FromPointer(report.Lon).Or(info.Lon)

	/*
	 * gpsd doesn't repeat every field on every TPV report - one derived from
	 * $GPRMC alone, for example, won't carry altitude even though mode is
	 * still 3D from an earlier $GPGGA-derived report. So a missing field here
	 * just means "unchanged", not "unknown"; keep whatever we saw last
	 * instead of clobbering it. A field that has never been reported at all
	 * is Nothing.
	 */

	info.Track = maybe.FromPointer(report.Track).Or(info.Track)

	var knots = maybe.Fmap(func(mps float64) float64 { return mps * MPS_TO_KNOTS }, maybe.FromPointer(report.Speed))
	info.SpeedKnots = knots.Or(info.SpeedKnots)

	if newFix >= Fix3D {
		info.Altitude = maybe.FromPointer(report.AltMSL).
			Or(maybe.FromPointer(report.Alt)).
			Or(info.Altitude)
	}
	/* Otherwise keep last known altitude when we downgrade from 3D to 2D fix. */
	/* Caller knows altitude is outdated if info.Fix == Fix2D. */
}

/*-------------------------------------------------------------------
 *
 * Name:        gpsdTerm
 *
 * Purpose:    	Shut down GPSD interface before exiting from application.
 *
 * Inputs:	none.
 *
 * Returns:	none.
 *
 *--------------------------------------------------------------------*/

func gpsdTerm() {
	s_gpsd.closeAndClear()
}
