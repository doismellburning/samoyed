/* Latitude / Longitude to UTM conversion */
package main

import (
	"fmt"
	"math"
	"os"
	"strconv"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/golang/geo/s1"
	"github.com/golang/geo/s2"
	"github.com/tzneal/coordconv"
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

	var latlng = s2.LatLng{
		Lat: s1.Angle(D2R(lat)),
		Lng: s1.Angle(D2R(lon)),
	}

	// UTM
	var utmCoord, utmErr = coordconv.DefaultUTMConverter.ConvertFromGeodetic(latlng, 0)
	if utmErr == nil {
		fmt.Printf("UTM zone = %d, hemisphere = %c, easting = %.0f, northing = %.0f\n", utmCoord.Zone, direwolf.HemisphereToRune(utmCoord.Hemisphere), utmCoord.Easting, utmCoord.Northing)
	} else {
		fmt.Printf("Conversion to UTM failed:\n%s\n\n", utmErr)

		// Others could still succeed, keep going.
	}

	// Practice run with MGRS to see if it will succeed

	var _, mgrsErr = coordconv.DefaultMGRSConverter.ConvertFromGeodetic(latlng, 5)
	if mgrsErr == nil {
		// OK, hope changing precision doesn't make a difference.
		fmt.Printf("MGRS =")

		for precision := 1; precision <= 5; precision++ {
			var mgrsCoord, _ = coordconv.DefaultMGRSConverter.ConvertFromGeodetic(latlng, precision)
			fmt.Printf("  %s", mgrsCoord)
		}

		fmt.Printf("\n")
	} else {
		fmt.Printf("Conversion to MGRS failed:\n%s\n", mgrsErr)
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
