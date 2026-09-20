// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package latlong

import "testing"

func Test_latlong(t *testing.T) {
	var result string

	/* Latitude to APRS format. */

	result = LatitudeToString(45.25, 0)
	if result != "4515.00N" {
		t.Errorf("Error 1.1: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(-45.25, 0)
	if result != "4515.00S" {
		t.Errorf("Error 1.2: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.999830, 0)
	if result != "4559.99N" {
		t.Errorf("Error 1.3: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.99999, 0)
	if result != "4600.00N" {
		t.Errorf("Error 1.4: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.999830, 1)
	if result != "4559.9 N" {
		t.Errorf("Error 1.5: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.999830, 2)
	if result != "4559.  N" {
		t.Errorf("Error 1.6: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.999830, 3)
	if result != "455 .  N" {
		t.Errorf("Error 1.7: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(45.999830, 4)
	if result != "45  .  N" {
		t.Errorf("Error 1.8: Did not expect \"%s\"", result)
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	result = LatitudeToString(0.016666666, 0)
	if result != "0001.00N" {
		t.Errorf("Error 1.9: Did not expect \"%s\"", result)
	}

	result = LatitudeToString(-1.999999, 0)
	if result != "0200.00S" {
		t.Errorf("Error 1.10: Did not expect \"%s\"", result)
	}

	/* Longitude to APRS format. */

	result = LongitudeToString(45.25, 0)
	if result != "04515.00E" {
		t.Errorf("Error 2.1: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(-45.25, 0)
	if result != "04515.00W" {
		t.Errorf("Error 2.2: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.999830, 0)
	if result != "04559.99E" {
		t.Errorf("Error 2.3: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.99999, 0)
	if result != "04600.00E" {
		t.Errorf("Error 2.4: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.999830, 1)
	if result != "04559.9 E" {
		t.Errorf("Error 2.5: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.999830, 2)
	if result != "04559.  E" {
		t.Errorf("Error 2.6: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.999830, 3)
	if result != "0455 .  E" {
		t.Errorf("Error 2.7: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(45.999830, 4)
	if result != "045  .  E" {
		t.Errorf("Error 2.8: Did not expect \"%s\"", result)
	}

	// Test for leading zeros for small values.  Result must be fixed width.

	result = LongitudeToString(0.016666666, 0)
	if result != "00001.00E" {
		t.Errorf("Error 2.9: Did not expect \"%s\"", result)
	}

	result = LongitudeToString(-1.999999, 0)
	if result != "00200.00W" {
		t.Errorf("Error 2.10: Did not expect \"%s\"", result)
	}

	/* Compressed format. */
	/* Protocol spec example has <*e7 but I got <*e8 due to rounding rather than truncation to integer. */

	result = LatitudeToCompressedString(-90.0)
	if result != "{{!!" {
		t.Errorf("Error 3.1: Did not expect \"%s\"", result)
	}

	result = LatitudeToCompressedString(49.5)
	if result != "5L!!" {
		t.Errorf("Error 3.2: Did not expect \"%s\"", result)
	}

	result = LatitudeToCompressedString(90.0)
	if result != "!!!!" {
		t.Errorf("Error 3.3: Did not expect \"%s\"", result)
	}

	result = LongitudeToCompressedString(-180.0)
	if result != "!!!!" {
		t.Errorf("Error 3.4: Did not expect \"%s\"", result)
	}

	result = LongitudeToCompressedString(-72.75)
	if result != "<*e8" {
		t.Errorf("Error 3.5: Did not expect \"%s\"", result)
	}

	result = LongitudeToCompressedString(180.0)
	if result != "{{!!" {
		t.Errorf("Error 3.6: Did not expect \"%s\"", result)
	}

	// to be continued for others...  NMEA...

	/* Distance & bearing - Take a couple examples from other places and see if we get similar results. */

	// http://www.movable-type.co.uk/scripts/latlong.html

	var d = DistanceKm(35., 45., 35., 135.)
	var b = BearingDeg(35., 45., 35., 135.)

	if d < 7862 || d > 7882 {
		t.Errorf("Error 5.1: Did not expect distance %.1f", d)
	}

	if b < 59.7 || b > 60.3 {
		t.Errorf("Error 5.2: Did not expect bearing %.1f", b)
	}

	// Sydney to Kinsale.  https://woodshole.er.usgs.gov/staffpages/cpolloni/manitou/ccal.htm

	d = DistanceKm(-33.8688, 151.2093, 51.7059, -8.5222)
	b = BearingDeg(-33.8688, 151.2093, 51.7059, -8.5222)

	if d < 17435 || d > 17455 {
		t.Errorf("Error 5.3: Did not expect distance %.1f", d)
	}

	if b < 327-1 || b > 327+1 {
		t.Errorf("Error 5.4: Did not expect bearing %.1f", b)
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
				var lat2 = DestLat(lat1, lon1, d1, b1)
				var lon2 = DestLon(lat1, lon1, d1, b1)

				var d2 = DistanceKm(lat1, lon1, lat2, lon2)

				var b2 = BearingDeg(lat1, lon1, lat2, lon2)
				if b2 > 359.9 && b2 < 360.1 {
					b2 = 0
				}

				// must be within 0.1% of distance and 0.1 degree.
				if d2 < 0.999*d1 || d2 > 1.001*d1 {
					t.Errorf("Error 5.8: lat1=%.5f, lon1=%.5f, lat2=%.5f, lon2=%.5f, d1=%.1f, b1=%.1f, d2=%.2f", lat1, lon1, lat2, lon2, d1, b1, d2)
				}

				if b2 < b1-0.1 || b2 > b1+0.1 {
					t.Errorf("Error 5.9: lat1=%.5f, lon1=%.5f, lat2=%.5f, lon2=%.5f, d1=%.1f, b1=%.1f, b2=%.2f", lat1, lon1, lat2, lon2, d1, b1, b2)
				}
			}
		}
	}

	/* Maidenhead locator to lat/long. */

	dlat, dlon, err := FromGridSquare("BL11")
	if err != nil || dlat < 20.4999999 || dlat > 21.5000001 || dlon < -157.0000001 || dlon > -156.9999999 {
		t.Errorf("Error 7.1: Did not expect %.6f %.6f", dlat, dlon)
	}

	dlat, dlon, err = FromGridSquare("BL11BH")
	if err != nil || dlat < 21.31249 || dlat > 21.31251 || dlon < -157.87501 || dlon > -157.87499 {
		t.Errorf("Error 7.2: Did not expect %.6f %.6f", dlat, dlon)
	}

	// TODO: add more test cases after comparing results with other cconverters.
	// Many other converters are limited to smaller number of characters,
	// or return corner rather than center of square, or return 3 decimal places for degrees.

	/*
		ok = FromGridSquare ("BL11BH16", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.3: Did not expect %.6f %.6f\n", dlat, dlon); }

		ok = FromGridSquare ("BL11BH16oo", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.4: Did not expect %.6f %.6f\n", dlat, dlon); }

		ok = FromGridSquare ("BL11BH16oo66", &dlat, &dlon);
		if (!ok || dlat < 21.? || dlat > 21.? || dlon < -157.? || dlon > -157.?) {
			errors++; dw_printf ("Error 7.5: Did not expect %.6f %.6f\n", dlat, dlon); }
	*/
}
