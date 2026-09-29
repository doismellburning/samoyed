// Simple test utility for dwgpsnmea functionality
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/doismellburning/samoyed/internal/direwolf"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus"
)

func main() {
	// GPS reading runs in a goroutine of its own, which this stops when the
	// user interrupts us.  Taking the signal this way also takes away the
	// default "an interrupt ends the process", so the loop in run has to end
	// on it too.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)

	// The GPS code logs what it reads, down to each NMEA sentence at the
	// debug level run asks for, alongside the report printed here.
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.TraceLevel)

	var status = run(ctx, os.Args[1:], os.Stdout)

	stop()
	os.Exit(status)
}

// run is main's body, reading from the GPS receiver on the port named by the
// first of args and reporting to out until ctx is cancelled.
func run(ctx context.Context, args []string, out io.Writer) int {
	var gpsPort = "COM22"

	if len(args) > 0 {
		gpsPort = args[0]
	}

	var gps = direwolf.NewGPSNMEA(ctx, gpsPort, 3)

	for ctx.Err() == nil {
		var info = gps.Read()

		switch info.Fix {
		case direwolf.DWFIX_2D, direwolf.DWFIX_3D:
			fmt.Fprintf(out, "%s  %s", maybe.Format("%.6f", "unknown", info.Lat), maybe.Format("%.6f", "unknown", info.Lon))
			fmt.Fprintf(out, "  %s knots  %s degrees", maybe.Format("%.1f", "unknown", info.SpeedKnots), maybe.Format("%.0f", "unknown", info.Track))

			if info.Fix == direwolf.DWFIX_3D {
				fmt.Fprintf(out, "  altitude = %s meters", maybe.Format("%.1f", "unknown", info.Altitude))
			}

			fmt.Fprintf(out, "\n")
		case direwolf.DWFIX_NOT_SEEN, direwolf.DWFIX_NO_FIX:
			fmt.Fprintf(out, "Location currently not available.\n")
		case direwolf.DWFIX_NOT_INIT:
			fmt.Fprintf(out, "GPS Init failed.\n")

			return 1
		default:
			fmt.Fprintf(out, "ERROR getting GPS information.\n")
		}

		if !dwutil.SleepSecCtx(ctx, 3) {
			break
		}
	}

	return 0
}
