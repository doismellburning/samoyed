package direwolf

// #include "direwolf.h"
// #include <stdlib.h>
// #include <string.h>
// #include <stdio.h>
// #include <unistd.h>
// #include <ctype.h>
// #include <time.h>
// #include <math.h>
// #include <assert.h>
import "C"

import (
	"testing"
)

func latlong_test_main(t *testing.T) {
	t.Helper()

	var result string
	var errors = 0

	/* Latitude to APRS format. */

	result = latitude_to_str(45.25, 0)
	if result != "4515.00N" {
		errors++
		dw_printf("Error 1.1: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(-45.25, 0)
	if result != "4515.00S" {
		errors++
		dw_printf("Error 1.2: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.999830, 0)
	if result != "4559.99N" {
		errors++
		dw_printf("Error 1.3: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.99999, 0)
	if result != "4600.00N" {
		errors++
		dw_printf("Error 1.4: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.999830, 1)
	if result != "4559.9 N" {
		errors++
		dw_printf("Error 1.5: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.999830, 2)
	if result != "4559.  N" {
		errors++
		dw_printf("Error 1.6: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.999830, 3)
	if result != "455 .  N" {
		errors++
		dw_printf("Error 1.7: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(45.999830, 4)
	if result != "45  .  N" {
		errors++
		dw_printf("Error 1.8: Did not expect \"%s\"\n", result)
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	result = latitude_to_str(0.016666666, 0)
	if result != "0001.00N" {
		errors++
		dw_printf("Error 1.9: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_str(-1.999999, 0)
	if result != "0200.00S" {
		errors++
		dw_printf("Error 1.10: Did not expect \"%s\"\n", result)
	}

	/* Longitude to APRS format. */

	result = longitude_to_str(45.25, 0)
	if result != "04515.00E" {
		errors++
		dw_printf("Error 2.1: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(-45.25, 0)
	if result != "04515.00W" {
		errors++
		dw_printf("Error 2.2: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.999830, 0)
	if result != "04559.99E" {
		errors++
		dw_printf("Error 2.3: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.99999, 0)
	if result != "04600.00E" {
		errors++
		dw_printf("Error 2.4: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.999830, 1)
	if result != "04559.9 E" {
		errors++
		dw_printf("Error 2.5: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.999830, 2)
	if result != "04559.  E" {
		errors++
		dw_printf("Error 2.6: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.999830, 3)
	if result != "0455 .  E" {
		errors++
		dw_printf("Error 2.7: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(45.999830, 4)
	if result != "045  .  E" {
		errors++
		dw_printf("Error 2.8: Did not expect \"%s\"\n", result)
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	result = longitude_to_str(0.016666666, 0)
	if result != "00001.00E" {
		errors++
		dw_printf("Error 2.9: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_str(-1.999999, 0)
	if result != "00200.00W" {
		errors++
		dw_printf("Error 2.10: Did not expect \"%s\"\n", result)
	}

	/* Compressed format. */
	/* Protocol spec example has <*e7 but I got <*e8 due to rounding rather than truncation to integer. */

	result = latitude_to_comp_str(-90.0)
	if result != "{{!!" {
		errors++
		dw_printf("Error 3.1: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_comp_str(49.5)
	if result != "5L!!" {
		errors++
		dw_printf("Error 3.2: Did not expect \"%s\"\n", result)
	}

	result = latitude_to_comp_str(90.0)
	if result != "!!!!" {
		errors++
		dw_printf("Error 3.3: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_comp_str(-180.0)
	if result != "!!!!" {
		errors++
		dw_printf("Error 3.4: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_comp_str(-72.75)
	if result != "<*e8" {
		errors++
		dw_printf("Error 3.5: Did not expect \"%s\"\n", result)
	}

	result = longitude_to_comp_str(180.0)
	if result != "{{!!" {
		errors++
		dw_printf("Error 3.6: Did not expect \"%s\"\n", result)
	}

	// to be continued for others...  NMEA...

	/* Distance & bearing - Take a couple examples from other places and see if we get similar results. */

	// http://www.movable-type.co.uk/scripts/latlong.html

	var d = ll_distance_km(35., 45., 35., 135.)
	var b = ll_bearing_deg(35., 45., 35., 135.)

	if d < 7862 || d > 7882 {
		errors++
		dw_printf("Error 5.1: Did not expect distance %.1f\n", d)
	}

	if b < 59.7 || b > 60.3 {
		errors++
		dw_printf("Error 5.2: Did not expect bearing %.1f\n", b)
	}

	// Sydney to Kinsale.  https://woodshole.er.usgs.gov/staffpages/cpolloni/manitou/ccal.htm

	d = ll_distance_km(-33.8688, 151.2093, 51.7059, -8.5222)
	b = ll_bearing_deg(-33.8688, 151.2093, 51.7059, -8.5222)

	if d < 17435 || d > 17455 {
		errors++
		dw_printf("Error 5.3: Did not expect distance %.1f\n", d)
	}

	if b < 327-1 || b > 327+1 {
		errors++
		dw_printf("Error 5.4: Did not expect bearing %.1f\n", b)
	}

	/*
	 * More distance and bearing.
	 * Here we will start at some location1 (lat1,lon1) and go some distance (d1) at some bearing (b1).
	 * This results in a new location2 (lat2, lon2).
	 * We then calculate the distance and bearing from location1 to location2 and compare with the intention.
	 */
	var lat1, lon1, d1, b1 float64
	d1 = 10

	for lat1 = -60; lat1 <= 60; lat1 += 30 {
		for lon1 = -180; lon1 <= 180; lon1 += 30 {
			for b1 = 0; b1 < 360; b1 += 15 {
				var lat2 = ll_dest_lat(lat1, lon1, d1, b1)
				var lon2 = ll_dest_lon(lat1, lon1, d1, b1)

				var d2 = ll_distance_km(lat1, lon1, lat2, lon2)
				var b2 = ll_bearing_deg(lat1, lon1, lat2, lon2)
				if b2 > 359.9 && b2 < 360.1 {
					b2 = 0
				}

				// must be within 0.1% of distance and 0.1 degree.
				if d2 < 0.999*d1 || d2 > 1.001*d1 {
					errors++
					dw_printf("Error 5.8: lat1=%.5f, lon1=%.5f, lat2=%.5f, lon2=%.5f, d1=%.1f, b1=%.1f, d2=%.2f\n", lat1, lon1, lat2, lon2, d1, b1, d2)
				}
				if b2 < b1-0.1 || b2 > b1+0.1 {
					errors++
					dw_printf("Error 5.9: lat1=%.5f, lon1=%.5f, lat2=%.5f, lon2=%.5f, d1=%.1f, b1=%.1f, b2=%.2f\n", lat1, lon1, lat2, lon2, d1, b1, b2)
				}
			}
		}
	}

	/* Maidenhead locator to lat/long. */

	dlat, dlon, err := ll_from_grid_square("BL11")
	if err != nil || dlat < 20.4999999 || dlat > 21.5000001 || dlon < -157.0000001 || dlon > -156.9999999 {
		errors++
		dw_printf("Error 7.1: Did not expect %.6f %.6f\n", dlat, dlon)
	}

	dlat, dlon, err = ll_from_grid_square("BL11BH")
	if err != nil || dlat < 21.31249 || dlat > 21.31251 || dlon < -157.87501 || dlon > -157.87499 {
		errors++
		dw_printf("Error 7.2: Did not expect %.6f %.6f\n", dlat, dlon)
	}

	// TODO: add more test cases after comparing results with other cconverters.
	// Many other converters are limited to smaller number of characters,
	// or return corner rather than center of square, or return 3 decimal places for degrees.

	/*
		ok = ll_from_grid_square ("BL11BH16", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.3: Did not expect %.6f %.6f\n", dlat, dlon); }

		ok = ll_from_grid_square ("BL11BH16oo", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.4: Did not expect %.6f %.6f\n", dlat, dlon); }

		ok = ll_from_grid_square ("BL11BH16oo66", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.5: Did not expect %.6f %.6f\n", dlat, dlon); }
	*/

	if errors > 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("\nLocation Coordinate Conversion Test - FAILED!\n")
		t.Fail()
	} else {
		text_color_set(DW_COLOR_REC)
		dw_printf("\nLocation Coordinate Conversion Test - SUCCESS!\n")
	}
}
