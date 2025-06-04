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
// #include "latlong.h"
// #include "textcolor.h"
// double ll_bearing_deg (double lat1, double lon1, double lat2, double lon2);
// double ll_dest_lat (double lat1, double lon1, double dist, double bearing);
// double ll_dest_lon (double lat1, double lon1, double dist, double bearing);
import "C"

import (
	"testing"
	"unsafe"
)

// Hack to save a lot of conversions
func strcmpLL(a *C.char, b string) C.int {
	return C.strcmp(a, C.CString(b))
}

func latlong_test_main(t *testing.T) {
	t.Helper()

	var resultAlloc [20]C.char
	var result = &resultAlloc[0]
	var errors = 0

	/* Latitude to APRS format. */

	C.latitude_to_str(45.25, 0, result)
	if strcmpLL(result, "4515.00N") != 0 {
		errors++
		dw_printf("Error 1.1: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(-45.25, 0, result)
	if strcmpLL(result, "4515.00S") != 0 {
		errors++
		dw_printf("Error 1.2: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.999830, 0, result)
	if strcmpLL(result, "4559.99N") != 0 {
		errors++
		dw_printf("Error 1.3: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.99999, 0, result)
	if strcmpLL(result, "4600.00N") != 0 {
		errors++
		dw_printf("Error 1.4: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.999830, 1, result)
	if strcmpLL(result, "4559.9 N") != 0 {
		errors++
		dw_printf("Error 1.5: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.999830, 2, result)
	if strcmpLL(result, "4559.  N") != 0 {
		errors++
		dw_printf("Error 1.6: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.999830, 3, result)
	if strcmpLL(result, "455 .  N") != 0 {
		errors++
		dw_printf("Error 1.7: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(45.999830, 4, result)
	if strcmpLL(result, "45  .  N") != 0 {
		errors++
		dw_printf("Error 1.8: Did not expect \"%s\"\n", C.GoString(result))
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	C.latitude_to_str(0.016666666, 0, result)
	if strcmpLL(result, "0001.00N") != 0 {
		errors++
		dw_printf("Error 1.9: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_str(-1.999999, 0, result)
	if strcmpLL(result, "0200.00S") != 0 {
		errors++
		dw_printf("Error 1.10: Did not expect \"%s\"\n", C.GoString(result))
	}

	/* Longitude to APRS format. */

	C.longitude_to_str(45.25, 0, result)
	if strcmpLL(result, "04515.00E") != 0 {
		errors++
		dw_printf("Error 2.1: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(-45.25, 0, result)
	if strcmpLL(result, "04515.00W") != 0 {
		errors++
		dw_printf("Error 2.2: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.999830, 0, result)
	if strcmpLL(result, "04559.99E") != 0 {
		errors++
		dw_printf("Error 2.3: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.99999, 0, result)
	if strcmpLL(result, "04600.00E") != 0 {
		errors++
		dw_printf("Error 2.4: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.999830, 1, result)
	if strcmpLL(result, "04559.9 E") != 0 {
		errors++
		dw_printf("Error 2.5: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.999830, 2, result)
	if strcmpLL(result, "04559.  E") != 0 {
		errors++
		dw_printf("Error 2.6: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.999830, 3, result)
	if strcmpLL(result, "0455 .  E") != 0 {
		errors++
		dw_printf("Error 2.7: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(45.999830, 4, result)
	if strcmpLL(result, "045  .  E") != 0 {
		errors++
		dw_printf("Error 2.8: Did not expect \"%s\"\n", C.GoString(result))
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	C.longitude_to_str(0.016666666, 0, result)
	if strcmpLL(result, "00001.00E") != 0 {
		errors++
		dw_printf("Error 2.9: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_str(-1.999999, 0, result)
	if strcmpLL(result, "00200.00W") != 0 {
		errors++
		dw_printf("Error 2.10: Did not expect \"%s\"\n", C.GoString(result))
	}

	/* Compressed format. */
	/* Protocol spec example has <*e7 but I got <*e8 due to rounding rather than truncation to integer. */

	C.memset(unsafe.Pointer(result), 0, C.ulong(len(resultAlloc)))

	C.latitude_to_comp_str(-90.0, result)
	if strcmpLL(result, "{{!!") != 0 {
		errors++
		dw_printf("Error 3.1: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_comp_str(49.5, result)
	if strcmpLL(result, "5L!!") != 0 {
		errors++
		dw_printf("Error 3.2: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.latitude_to_comp_str(90.0, result)
	if strcmpLL(result, "!!!!") != 0 {
		errors++
		dw_printf("Error 3.3: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_comp_str(-180.0, result)
	if strcmpLL(result, "!!!!") != 0 {
		errors++
		dw_printf("Error 3.4: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_comp_str(-72.75, result)
	if strcmpLL(result, "<*e8") != 0 {
		errors++
		dw_printf("Error 3.5: Did not expect \"%s\"\n", C.GoString(result))
	}

	C.longitude_to_comp_str(180.0, result)
	if strcmpLL(result, "{{!!") != 0 {
		errors++
		dw_printf("Error 3.6: Did not expect \"%s\"\n", C.GoString(result))
	}

	// to be continued for others...  NMEA...

	/* Distance & bearing - Take a couple examples from other places and see if we get similar results. */

	// http://www.movable-type.co.uk/scripts/latlong.html

	var d = C.ll_distance_km(35., 45., 35., 135.)
	var b = C.ll_bearing_deg(35., 45., 35., 135.)

	if d < 7862 || d > 7882 {
		errors++
		dw_printf("Error 5.1: Did not expect distance %.1f\n", d)
	}

	if b < 59.7 || b > 60.3 {
		errors++
		dw_printf("Error 5.2: Did not expect bearing %.1f\n", b)
	}

	// Sydney to Kinsale.  https://woodshole.er.usgs.gov/staffpages/cpolloni/manitou/ccal.htm

	d = C.ll_distance_km(-33.8688, 151.2093, 51.7059, -8.5222)
	b = C.ll_bearing_deg(-33.8688, 151.2093, 51.7059, -8.5222)

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
	var lat1, lon1, d1, b1 C.int
	d1 = 10
	var lat2, lon2, d2, b2 C.double

	for lat1 = -60; lat1 <= 60; lat1 += 30 {
		for lon1 = -180; lon1 <= 180; lon1 += 30 {
			for b1 = 0; b1 < 360; b1 += 15 {
				lat2 = C.ll_dest_lat(C.double(lat1), C.double(lon1), C.double(d1), C.double(b1))
				lon2 = C.ll_dest_lon(C.double(lat1), C.double(lon1), C.double(d1), C.double(b1))

				d2 = C.ll_distance_km(C.double(lat1), C.double(lon1), lat2, lon2)
				b2 = C.ll_bearing_deg(C.double(lat1), C.double(lon1), lat2, lon2)
				if b2 > 359.9 && b2 < 360.1 {
					b2 = 0
				}

				// must be within 0.1% of distance and 0.1 degree.
				if d2 < 0.999*C.double(d1) || d2 > 1.001*C.double(d1) {
					errors++
					dw_printf("Error 5.8: lat1=%d, lon2=%d, d1=%d, b1=%d, d2=%.2f\n", lat1, lon1, d1, b1, d2)
				}
				if b2 < C.double(b1)-0.1 || b2 > C.double(b1)+0.1 {
					errors++
					dw_printf("Error 5.9: lat1=%d, lon2=%d, d1=%d, b1=%d, b2=%.2f\n", lat1, lon1, d1, b1, b2)
				}
			}
		}
	}

	/* Maidenhead locator to lat/long. */

	var dlat, dlon C.double
	var ok C.int
	ok = C.ll_from_grid_square(C.CString("BL11"), &dlat, &dlon)
	if ok < 1 || dlat < 20.4999999 || dlat > 21.5000001 || dlon < -157.0000001 || dlon > -156.9999999 {
		errors++
		dw_printf("Error 7.1: Did not expect %.6f %.6f\n", dlat, dlon)
	}

	ok = C.ll_from_grid_square(C.CString("BL11BH"), &dlat, &dlon)
	if ok < 1 || dlat < 21.31249 || dlat > 21.31251 || dlon < -157.87501 || dlon > -157.87499 {
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
		C.text_color_set(C.DW_COLOR_ERROR)
		dw_printf("\nLocation Coordinate Conversion Test - FAILED!\n")
		t.Fail()
	}
	C.text_color_set(C.DW_COLOR_REC)
	dw_printf("\nLocation Coordinate Conversion Test - SUCCESS!\n")
}
