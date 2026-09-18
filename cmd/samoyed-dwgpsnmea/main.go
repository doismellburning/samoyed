// Simple test utility for dwgpsnmea functionality
package main

import (
	"fmt"
	"os"

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
	var gpsPort = "COM22"

	if len(os.Args) > 1 {
		gpsPort = os.Args[1]
	}

	direwolf.DWGPSInit(gpsPort, 3)

	for {
		var fix, lat, lon, speedKnots, track, altitude = direwolf.DWGPSRead()

		switch fix {
		case int(direwolf.DWFIX_2D), int(direwolf.DWFIX_3D):
			fmt.Printf("%s  %s", show("%.6f", lat), show("%.6f", lon))
			fmt.Printf("  %s knots  %s degrees", show("%.1f", speedKnots), show("%.0f", track))

			if fix == int(direwolf.DWFIX_3D) {
				fmt.Printf("  altitude = %s meters", show("%.1f", altitude))
			}

			fmt.Printf("\n")
		case int(direwolf.DWFIX_NOT_SEEN), int(direwolf.DWFIX_NO_FIX):
			fmt.Printf("Location currently not available.\n")
		case int(direwolf.DWFIX_NOT_INIT):
			fmt.Printf("GPS Init failed.\n")
			os.Exit(1)
		default:
			fmt.Printf("ERROR getting GPS information.\n")
		}

		direwolf.SLEEP_SEC(3)
	}
}
