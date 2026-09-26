//nolint:gochecknoglobals
package main

/*------------------------------------------------------------------
 *
 * Purpose:   	Quick hack to read GPS location and send very frequent
 *		position reports frames to a KISS TNC.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/doismellburning/samoyed/internal/maybe"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/pkg/term"
)

const HOWLONG = 20 /* Run for 20 seconds then quit. */

var MYCALL string

var tnc *term.Term

func main() {
	// Quick and dirty CLI parsing
	var tncSerialPort string
	var gpsSerialPort string

	if len(os.Args) != 4 {
		fmt.Printf("Syntax: %s <CALLSIGN> <TNC Serial Port> <GPS Serial Port>\n", os.Args[0])
		os.Exit(1)
	} else {
		MYCALL = os.Args[1]
		tncSerialPort = os.Args[2]
		gpsSerialPort = os.Args[3]
	}

	tnc = direwolf.SerialPortOpen(tncSerialPort, 9600)
	if tnc == nil {
		fmt.Printf("Can't open serial port to KISS TNC.\n")
		os.Exit(1)
	}

	var cmd = "\r\rhbaud 9600\rkiss on\rrestart\r"
	direwolf.SerialPortWrite(tnc, []byte(cmd))

	// GPS reading runs in a goroutine of its own, which this stops when the
	// user interrupts us.  Taking the signal this way also takes away the
	// default "an interrupt ends the process", so the loop below has to end
	// on it too - and it then still leaves the TNC out of KISS mode.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var debug_gps = 0
	var gps = direwolf.NewGPSNMEA(ctx, gpsSerialPort, debug_gps)

	// Wait for sample before reading.  An interrupt cuts the wait short, and
	// the loop below then does nothing, so we carry on to leaving KISS mode
	// rather than returning and abandoning the TNC in it.
	_ = direwolf.SleepSecCtx(ctx, 1)

	for range HOWLONG {
		if ctx.Err() != nil {
			break
		}

		var info = gps.Read()

		// A fix is reported before the position fields are, so a receiver can
		// claim a 3D fix while gpsd has yet to report a latitude at all.
		var latitude, haveLat = info.Lat.Get()
		var longitude, haveLon = info.Lon.Get()

		if info.Fix > direwolf.DWFIX_2D && haveLat && haveLon {
			walk96(latitude, longitude, info.SpeedKnots, info.Track, info.Altitude)
		} else if info.Fix < 0 {
			fmt.Printf("Can't communicate with GPS receiver.\n")
			os.Exit(1)
		} else {
			fmt.Printf("GPS fix not available.\n")
		}

		if !direwolf.SleepSecCtx(ctx, 1) {
			break
		}
	}

	// Exit out of KISS mode.

	direwolf.SerialPortWrite(tnc, []byte("\xc0\xff\xc0"))

	direwolf.SLEEP_MS(100)
}

var sequence = 0

/* Should be called once per second. */

func walk96(lat float64, lon float64, knots maybe.Maybe[float64], course maybe.Maybe[float64],
	alt maybe.Maybe[float64],
) {
	sequence++
	var comment = fmt.Sprintf("Sequence number %04d", sequence)

	/*
	 * Construct the packet in normal monitoring format.
	 */

	var messaging = false
	var compressed = false

	var info = direwolf.EncodePosition(messaging, compressed,
		lat, lon, 0,
		maybe.Fmap(func(meters float64) int { return int(direwolf.DW_METERS_TO_FEET(meters)) }, alt),
		'/', '=',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "", // PHGd not specified
		maybe.Fmap(func(degrees float64) int { return int(degrees) }, course),
		maybe.Fmap(func(speed float64) int { return int(speed) }, knots),
		maybe.Just(445.925), maybe.Nothing[float64](), maybe.Nothing[float64](),
		comment)

	var position_report = fmt.Sprintf("%s>WALK96:%s", MYCALL, info)

	fmt.Printf("%s\n", position_report)

	/*
	 * Convert it into AX.25 frame.
	 */

	var pp = direwolf.AX25FromText(position_report, true)

	if pp == nil {
		fmt.Printf("Unexpected error in AX25FromText.  Quitting.\n")
		os.Exit(1)
	}

	var ax25_frame = []byte{0} // Insert channel before KISS encapsulation.

	ax25_frame = append(ax25_frame, direwolf.AX25Pack(pp)...)

	/*
	 * Encapsulate as KISS and send to TNC.
	 */

	var kiss_frame = direwolf.KissEncapsulate(ax25_frame)

	// kiss_debug_print (1, NULL, kiss_frame, kiss_len);

	direwolf.SerialPortWrite(tnc, kiss_frame)
}
