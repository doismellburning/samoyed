// Simple test utility for dwgpsnmea functionality
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/doismellburning/samoyed/internal/maybe"
	direwolf "github.com/doismellburning/samoyed/src"
)

// show formats an optional GPS reading, so an absent one prints as "unknown"
// rather than as a plausible-looking number.
func show(format string, m maybe.Maybe[float64]) string {
	return maybe.Fold("unknown", func(value float64) string {
		return fmt.Sprintf(format, value)
	}, m)
}

func main() {
	// GPS reading runs in a goroutine of its own, which this stops when the
	// user interrupts us.  Taking the signal this way also takes away the
	// default "an interrupt ends the process", so the loop in run has to end
	// on it too.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)

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

	var gps = direwolf.DWGPSInit(ctx, gpsPort, 3)

	for ctx.Err() == nil {
		var fix, lat, lon, speedKnots, track, altitude = direwolf.DWGPSRead(gps)

		switch fix {
		case int(direwolf.DWFIX_2D), int(direwolf.DWFIX_3D):
			fmt.Fprintf(out, "%s  %s", show("%.6f", lat), show("%.6f", lon))
			fmt.Fprintf(out, "  %s knots  %s degrees", show("%.1f", speedKnots), show("%.0f", track))

			if fix == int(direwolf.DWFIX_3D) {
				fmt.Fprintf(out, "  altitude = %s meters", show("%.1f", altitude))
			}

			fmt.Fprintf(out, "\n")
		case int(direwolf.DWFIX_NOT_SEEN), int(direwolf.DWFIX_NO_FIX):
			fmt.Fprintf(out, "Location currently not available.\n")
		case int(direwolf.DWFIX_NOT_INIT):
			fmt.Fprintf(out, "GPS Init failed.\n")

			return 1
		default:
			fmt.Fprintf(out, "ERROR getting GPS information.\n")
		}

		if !direwolf.SleepSecCtx(ctx, 3) {
			break
		}
	}

	return 0
}
