/* UTM to Latitude / Longitude conversion */
package main

// #cgo CFLAGS: -I../../src -I../../external/geotranz -DMAJOR_VERSION=0 -DMINOR_VERSION=0
// #cgo LDFLAGS: -lm
// #include "direwolf.h"
// #include <stdio.h>
// #include <stdlib.h>
// #include <math.h>
// #include <string.h>
// #include <ctype.h>
// #include "utm.h"
// #include "mgrs.h"
// #include "usng.h"
// #include "error_string.h"
import "C"

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	_ "github.com/doismellburning/samoyed/external/geotranz" // Pulls this in for cgo
)

func R2D(radians float64) float64 {
	return radians * 180 / math.Pi
}

func main() {
	if len(os.Args) == 4 {
		// 3 command line arguments for UTM

		var szone [100]C.char
		C.strncpy(&szone[0], C.CString(os.Args[1]), C.ulong(len(szone)))
		var zletC *C.char
		var lzone = C.strtoul(&szone[0], &zletC, 10)
		var zlet = C.GoString(zletC)

		var hemi C.char
		if zlet == "" {
			hemi = 'N'
		} else {
			// FIXME KG uppercase first character of zlet
			if !strings.ContainsRune("CDEFGHJKLMNPQRSTUVWX", rune(zlet[0])) {
				fmt.Printf("Latitudinal band must be one of CDEFGHJKLMNPQRSTUVWX.")
				usage()
			}
			if rune(zlet[0]) >= 'N' {
				hemi = 'N'
			} else {
				hemi = 'S'
			}
		}

		var easting, _ = strconv.ParseFloat(os.Args[2], 64)
		var northing, _ = strconv.ParseFloat(os.Args[3], 64)

		var lat C.double
		var lon C.double
		var err = C.Convert_UTM_To_Geodetic(C.long(lzone), hemi, C.double(easting), C.double(northing), &lat, &lon)
		if err == 0 {
			lat = C.double(R2D(float64(lat)))
			lon = C.double(R2D(float64(lon)))

			fmt.Printf("from UTM, latitude = %.6f, longitude = %.6f\n", lat, lon)
		} else {
			var message *C.char
			C.utm_error_string(err, message)
			fmt.Printf("Conversion from UTM failed:\n%s\n\n", C.GoString(message))
		}
	} else if len(os.Args) == 2 {
		// One command line argument, USNG or MGRS.
		// TODO: continue here.
		var lat C.double
		var lon C.double
		var err = C.Convert_USNG_To_Geodetic(C.CString(os.Args[1]), &lat, &lon)
		if err == 0 {
			lat = C.double(R2D(float64(lat)))
			lon = C.double(R2D(float64(lon)))
			fmt.Printf("from USNG, latitude = %.6f, longitude = %.6f\n", lat, lon)
		} else {
			var message *C.char
			C.usng_error_string(err, message)
			fmt.Printf("Conversion from USNG failed:\n%s\n\n", C.GoString(message))
		}

		err = C.Convert_MGRS_To_Geodetic(C.CString(os.Args[1]), &lat, &lon)
		if err == 0 {
			lat = C.double(R2D(float64(lat)))
			lon = C.double(R2D(float64(lon)))
			fmt.Printf("from MGRS, latitude = %.6f, longitude = %.6f\n", lat, lon)
		} else {
			var message *C.char
			C.mgrs_error_string(err, message)
			fmt.Printf("Conversion from MGRS failed:\n%s\n\n", C.GoString(message))
		}
	} else {
		usage()
	}
}

func usage() {
	fmt.Println("UTM to Latitude / Longitude conversion")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("\tutm2ll  zone  easting  northing")
	fmt.Println("")
	fmt.Println("where,")
	fmt.Println("\tzone is UTM zone 1 thru 60 with optional latitudinal band.")
	fmt.Println("\teasting is x coordinate in meters")
	fmt.Println("\tnorthing is y coordinate in meters")
	fmt.Println("")
	fmt.Println("or:")
	fmt.Println("\tutm2ll  x")
	fmt.Println("")
	fmt.Println("where,")
	fmt.Println("\tx is USNG or MGRS location.")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("\tutm2ll 19T 306130 4726010")
	fmt.Println("\tutm2ll 19TCH06132600")

	os.Exit(1)
}
