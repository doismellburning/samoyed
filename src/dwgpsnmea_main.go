// Simple test utility for dwgpsnmea functionality
package direwolf

// #include <string.h>
import "C"

import (
	"fmt"
	"os"
)

func DWGPSNMEAMain() {
	var config misc_config_s
	C.strcpy(&config.gpsnmea_port[0], C.CString("COM22"))

	dwgps_init(&config, 3)

	for {
		var info dwgps_info_t
		var fix = dwgps_read(&info)

		switch fix {
		case DWFIX_2D, DWFIX_3D:
			fmt.Printf("%.6f  %.6f", info.dlat, info.dlon)
			fmt.Printf("  %.1f knots  %.0f degrees", info.speed_knots, info.track)
			if fix == DWFIX_3D {
				fmt.Printf("  altitude = %.1f meters", info.altitude)
			}
			fmt.Printf("\n")
		case DWFIX_NOT_SEEN, DWFIX_NO_FIX:
			fmt.Printf("Location currently not available.\n")
		case DWFIX_NOT_INIT:
			fmt.Printf("GPS Init failed.\n")
			os.Exit(1)
		default:
			fmt.Printf("ERROR getting GPS information.\n")
		}
		SLEEP_SEC(3)
	}
}
