/* Latitude / Longitude to UTM conversion */
package main

// #cgo CFLAGS: -I../../src -I../../external/geotranz -I../../external/misc -DMAJOR_VERSION=0 -DMINOR_VERSION=0
// #cgo LDFLAGS: -lm
// #include "direwolf.h"
// #include <stdio.h>
// #include <stdlib.h>
// #include <math.h>
// #include "utm.h"
// #include "mgrs.h"
// #include "usng.h"
// #include "error_string.h"
// #define D2R(d) ((d) * M_PI / 180.)
// #define R2D(r) ((r) * 180. / M_PI)
import "C"

import (
	"fmt"
	"math"
	"os"
	"strconv"

	_ "github.com/doismellburning/samoyed/external/geotranz" // Pulls this in for cgo
)

func D2R(degrees float64) float64 {
	return degrees * math.Pi / 180
}

func main() {
	if len(os.Args) != 3 {
		usage()
		return
	}

	var lat, _ = strconv.ParseFloat(os.Args[1], 64)
	var lon, _ = strconv.ParseFloat(os.Args[2], 64)

	// UTM

	var lzone C.long
	var hemisphere C.char
	var easting C.double
	var northing C.double
	var err = C.Convert_Geodetic_To_UTM(C.double(D2R(lat)), C.double(D2R(lon)), &lzone, &hemisphere, &easting, &northing)
	if err == 0 {
		fmt.Printf("UTM zone = %d, hemisphere = %c, easting = %.0f, northing = %.0f\n", lzone, hemisphere, easting, northing)
	} else {
		var message *C.char
		C.utm_error_string(err, message)
		fmt.Printf("Conversion to UTM failed:\n%s\n\n", C.GoString(message))

		// Others could still succeed, keep going.
	}

	// Practice run with MGRS to see if it will succeed

	var mgrs [32]C.char
	err = C.Convert_Geodetic_To_MGRS(C.double(D2R(lat)), C.double(D2R(lon)), 5, &mgrs[0])
	if err == 0 {
		// OK, hope changing precision doesn't make a difference.

		var precision C.long

		fmt.Printf("MGRS =")
		for precision = 1; precision <= 5; precision++ {
			C.Convert_Geodetic_To_MGRS(C.double(D2R(lat)), C.double(D2R(lon)), precision, &mgrs[0])
			fmt.Printf("  %s", C.GoString(&mgrs[0]))
		}
		fmt.Printf("\n")
	} else {
		var message *C.char
		C.mgrs_error_string(err, message)
		fmt.Printf("Conversion to MGRS failed:\n%s\n", C.GoString(message))
	}

	// Same for USNG.

	var usng [32]C.char
	err = C.Convert_Geodetic_To_USNG(C.double(D2R(lat)), C.double(D2R(lon)), 5, &usng[0])
	if err == 0 {
		var precision C.long

		fmt.Printf("USNG =")
		for precision = 1; precision <= 5; precision++ {
			C.Convert_Geodetic_To_USNG(C.double(D2R(lat)), C.double(D2R(lon)), precision, &usng[0])
			fmt.Printf("  %s", C.GoString(&usng[0]))
		}
		fmt.Printf("\n")
	} else {
		var message *C.char
		C.usng_error_string(err, message)
		fmt.Printf("Conversion to USNG failed:\n%s\n", C.GoString(message))
	}
}

func usage() {
	fmt.Printf("Latitude / Longitude to UTM conversion\n")
	fmt.Printf("\n")
	fmt.Printf("Usage:\n")
	fmt.Printf("\tll2utm  latitude  longitude\n")
	fmt.Printf("\n")
	fmt.Printf("where,\n")
	fmt.Printf("\tLatitude and longitude are in decimal degrees.\n")
	fmt.Printf("\t   Use negative for south or west.\n")
	fmt.Printf("\n")
	fmt.Printf("Example:\n")
	fmt.Printf("\tll2utm 42.662139 -71.365553\n")
}
