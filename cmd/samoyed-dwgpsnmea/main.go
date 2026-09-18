// Simple test utility for dwgpsnmea functionality
package main

import (
	"fmt"
	"os"

	"github.com/doismellburning/samoyed/internal/dwgps"
	direwolf "github.com/doismellburning/samoyed/src"
)

func main() {
	var gpsPort = "COM22"

	if len(os.Args) > 1 {
		gpsPort = os.Args[1]
	}

	direwolf.DWGPSInit(gpsPort, 3)

	for {
		var fix, lat, lon, speedKnots, track, altitude = direwolf.DWGPSRead()

		switch fix {
		case dwgps.Fix2D, dwgps.Fix3D:
			fmt.Printf("%.6f  %.6f", lat, lon)
			fmt.Printf("  %.1f knots  %.0f degrees", speedKnots, track)

			if fix == dwgps.Fix3D {
				fmt.Printf("  altitude = %.1f meters", altitude)
			}

			fmt.Printf("\n")
		case dwgps.FixNotSeen, dwgps.FixNoFix:
			fmt.Printf("Location currently not available.\n")
		case dwgps.FixNotInit:
			fmt.Printf("GPS Init failed.\n")
			os.Exit(1)
		default:
			fmt.Printf("ERROR getting GPS information.\n")
		}

		direwolf.SLEEP_SEC(3)
	}
}
