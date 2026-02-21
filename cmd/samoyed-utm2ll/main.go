/* UTM to Latitude / Longitude conversion */
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/tzneal/coordconv"
)

func R2D(radians float64) float64 {
	return radians * 180 / math.Pi
}

func main() {
	if len(os.Args) == 4 {
		// 3 command line arguments for UTM
		var zlet rune

		var zoneStr = os.Args[1] // e.g. "19T" or just "19"
		if len(zoneStr) > 0 && zoneStr[len(zoneStr)-1] >= 'A' && zoneStr[len(zoneStr)-1] <= 'Z' {
			zlet = rune(zoneStr[len(zoneStr)-1])
			zoneStr = zoneStr[:len(zoneStr)-1]
		}
		var zone, _ = strconv.Atoi(zoneStr)

		var hemisphere coordconv.Hemisphere
		if zlet == 0 {
			hemisphere = coordconv.HemisphereNorth
		} else {
			// TODO KG uppercase zlet?
			if !strings.ContainsRune("CDEFGHJKLMNPQRSTUVWX", zlet) {
				fmt.Printf("Latitudinal band must be one of CDEFGHJKLMNPQRSTUVWX.")
				usage()
			}

			if zlet >= 'N' {
				hemisphere = coordconv.HemisphereNorth
			} else {
				hemisphere = coordconv.HemisphereSouth
			}
		}

		var easting, _ = strconv.ParseFloat(os.Args[2], 64)
		var northing, _ = strconv.ParseFloat(os.Args[3], 64)

		var utmCoord = coordconv.UTMCoord{
			Zone:       zone,
			Hemisphere: hemisphere,
			Easting:    easting,
			Northing:   northing,
		}

		var latlng, utmErr = coordconv.DefaultUTMConverter.ConvertToGeodetic(utmCoord)
		if utmErr == nil {
			var lat = R2D(float64(latlng.Lat))
			var lon = R2D(float64(latlng.Lng))

			fmt.Printf("from UTM, latitude = %.6f, longitude = %.6f\n", lat, lon)
		} else {
			fmt.Printf("Conversion from UTM failed:\n%s\n\n", utmErr)
		}
	} else if len(os.Args) == 2 {
		// One command line argument, MGRS.
		var mgrsLatlng, mgrsErr = coordconv.DefaultMGRSConverter.ConvertToGeodetic(os.Args[1])
		if mgrsErr == nil {
			var lat = R2D(float64(mgrsLatlng.Lat))
			var lon = R2D(float64(mgrsLatlng.Lng))
			fmt.Printf("from MGRS, latitude = %.6f, longitude = %.6f\n", lat, lon)
		} else {
			fmt.Printf("Conversion from MGRS failed:\n%s\n\n", mgrsErr)
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
