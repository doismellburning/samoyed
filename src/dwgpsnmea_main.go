// Simple test utility for dwgpsnmea functionality
package direwolf

// #include <string.h>
// #include "dwgps.h"
// #include "config.h"
import "C"

import (
	"fmt"
	"os"
)

func DWGPSNMEAMain() {
	var config C.struct_misc_config_s
	C.strcpy(&config.gpsnmea_port[0], C.CString("COM22"))

	C.dwgps_init(&config, 3)

	for {
		var info C.dwgps_info_t
		var fix = C.dwgps_read(&info)

		switch fix {
		case C.DWFIX_2D, C.DWFIX_3D:
			fmt.Printf("%.6f  %.6f", info.dlat, info.dlon)
			fmt.Printf("  %.1f knots  %.0f degrees", info.speed_knots, info.track)
			if fix == 3 {
				fmt.Printf("  altitude = %.1f meters", info.altitude)
			}
			fmt.Printf("\n")
		case C.DWFIX_NOT_SEEN, C.DWFIX_NO_FIX:
			fmt.Printf("Location currently not available.\n")
		case C.DWFIX_NOT_INIT:
			fmt.Printf("GPS Init failed.\n")
			os.Exit(1)
		default:
			fmt.Printf("ERROR getting GPS information.\n")
		}
		SLEEP_SEC(3)
	}
}
