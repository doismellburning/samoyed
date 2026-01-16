package direwolf

/*------------------------------------------------------------------
 *
 * Purpose:   	Various functions for dealing with latitude and longitude.
 *
 * Description: Originally, these were scattered around in many places.
 *		Over time they might all be gathered into one place
 *		for consistency, reuse, and easier maintenance.
 *
 *---------------------------------------------------------------*/

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
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

/*------------------------------------------------------------------
 *
 * Name:        latitude_to_str
 *
 * Purpose:     Convert numeric latitude to string for transmission.
 *
 * Inputs:      dlat		- Floating point degrees.
 * 		ambiguity	- If 1, 2, 3, or 4, blank out that many trailing digits.
 *
 * Outputs:	slat		- String in format ddmm.mm[NS]
 *				  Must always be exactly 8 characters + NUL.
 *				  Put in leading zeros if necessary.
 *				  We must have exactly ddmm.mm and hemisphere because
 *				  the APRS position report has fixed width fields.
 *				  Trailing digits can be blanked for position ambiguity.
 *
 * Returns:     None
 *
 * Idea for future:
 *		Non zero ambiguity removes least significant digits without rounding.
 *		Maybe we could use -1 and -2 to add extra digits using !DAO! as
 *		documented in http://www.aprs.org/datum.txt
 *
 *		For example, -1 adds one more human readable digit.
 *			lat minutes 12.345 would produce "12.34" and !W5 !
 *
 *		-2 would encode almost 2 digits in base 91.
 *			lat minutes 10.0027 would produce "10.00" and !w: !
 *
 *----------------------------------------------------------------*/

func latitude_to_str(dlat float64, ambiguity int) string {

	if dlat < -90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is less than -90.  Changing to -90.\n")
		dlat = -90.
	}
	if dlat > 90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is greater than 90.  Changing to 90.\n")
		dlat = 90.
	}

	var hemi rune /* Hemisphere: N or S */
	if dlat < 0 {
		dlat = (-dlat)
		hemi = 'S'
	} else {
		hemi = 'N'
	}

	var ideg = int(dlat)                    /* whole number of degrees. */
	var dmin = (dlat - float64(ideg)) * 60. /* Minutes after removing degrees. */

	// dmin is known to be in range of 0 <= dmin < 60.

	// Minutes must be exactly like 99.99 with leading zeros,
	// if needed, to make it fixed width.
	// Two digits, decimal point, two digits, nul terminator.

	var smin = fmt.Sprintf("%05.2f", dmin)
	/* Due to roundoff, 59.9999 could come out as "60.00" */
	if smin[0] == '6' {
		smin = "00.00"
		ideg++
	}

	// Assumes slat can hold 8 characters + nul.
	// Degrees must be exactly 2 digits, with leading zero, if needed.

	var slat = []byte(fmt.Sprintf("%02d%s%c", ideg, smin, hemi))

	if ambiguity >= 1 {
		slat[6] = ' '
		if ambiguity >= 2 {
			slat[5] = ' '
			if ambiguity >= 3 {
				slat[3] = ' '
				if ambiguity >= 4 {
					slat[2] = ' '
				}
			}
		}
	}

	return string(slat)
}

/*------------------------------------------------------------------
 *
 * Name:        longitude_to_str
 *
 * Purpose:     Convert numeric longitude to string for transmission.
 *
 * Inputs:      dlong		- Floating point degrees.
 * 		ambiguity	- If 1, 2, 3, or 4, blank out that many trailing digits.
 *
 * Outputs:	slong		- String in format dddmm.mm[NS]
 *				  Must always be exactly 9 characters + NUL.
 *				  Put in leading zeros if necessary.
 *				  We must have exactly dddmm.mm and hemisphere because
 *				  the APRS position report has fixed width fields.
 *				  Trailing digits can be blanked for position ambiguity.
 * Returns:     None
 *
 *----------------------------------------------------------------*/

func longitude_to_str(dlong float64, ambiguity int) string {

	if dlong < -180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Longitude is less than -180.  Changing to -180.\n")
		dlong = -180.
	}
	if dlong > 180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Longitude is greater than 180.  Changing to 180.\n")
		dlong = 180.
	}

	var hemi rune /* Hemisphere: E or W */
	if dlong < 0 {
		dlong = (-dlong)
		hemi = 'W'
	} else {
		hemi = 'E'
	}

	var ideg = int(dlong)                    /* whole number of degrees. */
	var dmin = (dlong - float64(ideg)) * 60. /* Minutes after removing degrees. */

	var smin = fmt.Sprintf("%05.2f", dmin)
	/* Due to roundoff, 59.9999 could come out as "60.00" */
	if smin[0] == '6' {
		smin = "00.00"
		ideg++
	}

	// Assumes slong can hold 9 characters + nul.
	// Degrees must be exactly 3 digits, with leading zero, if needed.

	var slong = []byte(fmt.Sprintf("%03d%s%c", ideg, smin, hemi))

	/*
	 * The spec says position ambiguity in latitude also
	 * applies to longitude automatically.
	 * Blanking longitude digits is not necessary but I do it
	 * because it makes things clearer.
	 */
	if ambiguity >= 1 {
		slong[7] = ' '
		if ambiguity >= 2 {
			slong[6] = ' '
			if ambiguity >= 3 {
				slong[4] = ' '
				if ambiguity >= 4 {
					slong[3] = ' '
				}
			}
		}
	}

	return string(slong)
}

/*------------------------------------------------------------------
 *
 * Name:        latitude_to_comp_str
 *
 * Purpose:     Convert numeric latitude to compressed string for transmission.
 *
 * Inputs:      dlat		- Floating point degrees.
 *
 * Outputs:	slat		- String in format yyyy.
 *				  Exactly 4 bytes
 *
 *----------------------------------------------------------------*/

func latitude_to_comp_str(dlat float64) string {

	if dlat < -90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is less than -90.  Changing to -90.\n")
		dlat = -90.
	}
	if dlat > 90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is greater than 90.  Changing to 90.\n")
		dlat = 90.
	}

	var y = int(math.Round(380926. * (90. - dlat)))

	var y0 = y / (91 * 91 * 91)
	y -= y0 * (91 * 91 * 91)

	var y1 = y / (91 * 91)
	y -= y1 * (91 * 91)

	var y2 = y / (91)
	y -= y2 * (91)

	var y3 = y

	return fmt.Sprintf("%c%c%c%c", y0+33, y1+33, y2+33, y3+33)
}

/*------------------------------------------------------------------
 *
 * Name:        longitude_to_comp_str
 *
 * Purpose:     Convert numeric longitude to compressed string for transmission.
 *
 * Inputs:      dlong		- Floating point degrees.
 *
 * Outputs:	slat		- String in format xxxx.
 *				  Exactly 4 bytes
 *
 *----------------------------------------------------------------*/

func longitude_to_comp_str(dlong float64) string {

	if dlong < -180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Longitude is less than -180.  Changing to -180.\n")
		dlong = -180.
	}
	if dlong > 180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Longitude is greater than 180.  Changing to 180.\n")
		dlong = 180.
	}

	var x = int(math.Round(190463. * (180. + dlong)))

	var x0 = x / (91 * 91 * 91)
	x -= x0 * (91 * 91 * 91)

	var x1 = x / (91 * 91)
	x -= x1 * (91 * 91)

	var x2 = x / (91)
	x -= x2 * (91)

	var x3 = x

	return fmt.Sprintf("%c%c%c%c", x0+33, x1+33, x2+33, x3+33)
}

/*------------------------------------------------------------------
 *
 * Name:        latitude_to_nmea
 *
 * Purpose:     Convert numeric latitude to strings for NMEA sentence.
 *
 * Inputs:      dlat		- Floating point degrees.
 *
 * Outputs:	slat		- String in format ddmm.mmmm
 *		hemi		- Hemisphere or empty string.
 *
 *----------------------------------------------------------------*/

func latitude_to_nmea(dlat float64) (string, string) {

	if dlat == G_UNKNOWN {
		return "", ""
	}

	if dlat < -90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is less than -90.  Changing to -90.\n")
		dlat = -90.
	}
	if dlat > 90. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Latitude is greater than 90.  Changing to 90.\n")
		dlat = 90.
	}

	var hemi string
	if dlat < 0 {
		dlat = (-dlat)
		hemi = "S"
	} else {
		hemi = "N"
	}

	var ideg = int(dlat)                    /* whole number of degrees. */
	var dmin = (dlat - float64(ideg)) * 60. /* Minutes after removing degrees. */

	var smin = fmt.Sprintf("%07.4f", dmin)
	/* Due to roundoff, 59.99999 could come out as "60.0000" */
	if smin[0] == '6' {
		smin = "00.0000"
		ideg++
	}

	var slat = fmt.Sprintf("%02d%s", ideg, smin)

	return slat, hemi

}

/*------------------------------------------------------------------
 *
 * Name:        longitude_to_nmea
 *
 * Purpose:     Convert numeric longitude to strings for NMEA sentence.
 *
 * Inputs:      dlong		- Floating point degrees.
 *
 * Outputs:	slong		- String in format dddmm.mmmm
 *		hemi		- Hemisphere or empty string.
 *
 *----------------------------------------------------------------*/

func longitude_to_nmea(dlong float64) (string, string) {

	if dlong == G_UNKNOWN {
		return "", ""
	}

	if dlong < -180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("longitude is less than -180.  Changing to -180.\n")
		dlong = -180.
	}
	if dlong > 180. {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("longitude is greater than 180.  Changing to 180.\n")
		dlong = 180.
	}

	var hemi string
	if dlong < 0 {
		dlong = (-dlong)
		hemi = "W"
	} else {
		hemi = "E"
	}

	var ideg = int(dlong)                    /* whole number of degrees. */
	var dmin = (dlong - float64(ideg)) * 60. /* Minutes after removing degrees. */

	var smin = fmt.Sprintf("%07.4f", dmin)
	/* Due to roundoff, 59.99999 could come out as "60.0000" */
	if smin[0] == '6' {
		smin = "00.0000"
		ideg++
	}

	var slong = fmt.Sprintf("%03d%s", ideg, smin)

	return slong, hemi
}

/*------------------------------------------------------------------
 *
 * Function:	latitude_from_nmea
 *
 * Purpose:	Convert NMEA latitude encoding to degrees.
 *
 * Inputs:	pstr 	- Pointer to numeric string.
 *		phemi	- Pointer to following field.  Should be N or S.
 *
 * Returns:	Double precision value in degrees.  Negative for South.
 *
 * Description:	Latitude field has
 *			2 digits for degrees
 *			2 digits for minutes
 *			period
 *			Variable number of fractional digits for minutes.
 *			I've seen 2, 3, and 4 fractional digits.
 *
 *
 * Bugs:	Very little validation of data.
 *
 * Errors:	Return constant G_UNKNOWN for any type of error.
 *
 *------------------------------------------------------------------*/

func latitude_from_nmea(pstr string, phemi byte) float64 {

	if len(pstr) < 5 {
		return (G_UNKNOWN)
	}
	if !unicode.IsDigit(rune(pstr[0])) {
		return (G_UNKNOWN)
	}

	if pstr[4] != '.' {
		return (G_UNKNOWN)
	}

	var lat = float64(pstr[0]-'0')*10 + float64(pstr[1]-'0')
	var mins, _ = strconv.ParseFloat(pstr[2:], 64)
	lat += mins / 60.0

	if lat < 0 || lat > 90 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error: Latitude not in range of 0 to 90.\n")
	}

	// Saw this one time:
	//	$GPRMC,000000,V,0000.0000,0,00000.0000,0,000,000,000000,,*01

	// If location is unknown, I think the hemisphere should be
	// an empty string.  TODO: Check on this.
	// 'V' means void, so sentence should be discarded rather than
	// trying to extract any data from it.

	if phemi != 'N' && phemi != 'S' && phemi != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error: Latitude hemisphere should be N or S.\n")
	}

	if phemi == 'S' {
		lat = (-lat)
	}

	return (lat)
}

/*------------------------------------------------------------------
 *
 * Function:	longitude_from_nmea
 *
 * Purpose:	Convert NMEA longitude encoding to degrees.
 *
 * Inputs:	pstr 	- Pointer to numeric string.
 *		phemi	- Pointer to following field.  Should be E or W.
 *
 * Returns:	Double precision value in degrees.  Negative for West.
 *
 * Description:	Longitude field has
 *			3 digits for degrees
 *			2 digits for minutes
 *			period
 *			Variable number of fractional digits for minutes
 *
 *
 * Bugs:	Very little validation of data.
 *
 * Errors:	Return constant G_UNKNOWN for any type of error.
 *
 *------------------------------------------------------------------*/

func longitude_from_nmea(pstr string, phemi byte) float64 {

	if len(pstr) < 6 {
		return (G_UNKNOWN)
	}
	if !unicode.IsDigit(rune(pstr[0])) {
		return (G_UNKNOWN)
	}

	if pstr[5] != '.' {
		return (G_UNKNOWN)
	}

	var lon = float64(pstr[0]-'0')*100 + float64(pstr[1]-'0')*10 + float64(pstr[2]-'0')
	var mins, _ = strconv.ParseFloat(pstr[3:], 64)
	lon += mins / 60.0

	if lon < 0 || lon > 180 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error: Longitude not in range of 0 to 180.\n")
	}

	if phemi != 'E' && phemi != 'W' && phemi != 0 {
		text_color_set(DW_COLOR_ERROR)
		dw_printf("Error: Longitude hemisphere should be E or W.\n")
	}

	if phemi == 'W' {
		lon = (-lon)
	}

	return (lon)
}

/*------------------------------------------------------------------
 *
 * Function:	ll_distance_km
 *
 * Purpose:	Calculate distance between two locations.
 *
 * Inputs:	lat1, lon1	- One location, in degrees.
 *		lat2, lon2	- other location
 *
 * Returns:	Distance in km.
 *
 * Description:	The Ubiquitous Haversine formula.
 *
 *------------------------------------------------------------------*/

const R_KM = 6371

func ll_distance_km(lat1, lon1, lat2, lon2 float64) float64 {

	lat1 *= math.Pi / 180
	lon1 *= math.Pi / 180
	lat2 *= math.Pi / 180
	lon2 *= math.Pi / 180

	var a = math.Pow(math.Sin((lat2-lat1)/2), 2) + math.Cos(lat1)*math.Cos(lat2)*math.Pow(math.Sin((lon2-lon1)/2), 2)

	return (R_KM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a)))
}

/*------------------------------------------------------------------
 *
 * Function:	ll_bearing_deg
 *
 * Purpose:	Calculate bearing between two locations.
 *
 * Inputs:	lat1, lon1	- starting location, in degrees.
 *		lat2, lon2	- destination location
 *
 * Returns:	Initial Bearing, in degrees.
 *		The calculation produces Range +- 180 degrees.
 *		But I think that 0 - 360 would be more customary?
 *
 *------------------------------------------------------------------*/

func ll_bearing_deg(lat1, lon1, lat2, lon2 float64) float64 {

	lat1 *= math.Pi / 180
	lon1 *= math.Pi / 180
	lat2 *= math.Pi / 180
	lon2 *= math.Pi / 180

	var b = math.Atan2(math.Sin(lon2-lon1)*math.Cos(lat2),
		math.Cos(lat1)*math.Sin(lat2)-math.Sin(lat1)*math.Cos(lat2)*math.Cos(lon2-lon1))

	b *= 180 / math.Pi
	if b < 0 {
		b += 360
	}

	return (b)
}

/*------------------------------------------------------------------
 *
 * Function:	ll_dest_lat
 *		ll_dest_lon
 *
 * Purpose:	Calculate the destination location given a starting point,
 *		distance, and bearing,
 *
 * Inputs:	lat1, lon1	- starting location, in degrees.
 *		dist		- distance in km.
 *		bearing		- direction in degrees.  Shouldn't matter
 *				  if it is in +- 180 or 0 to 360 range.
 *
 * Returns:	New latitude or longitude.
 *
 *------------------------------------------------------------------*/

func ll_dest_lat(lat1, _, dist, bearing float64) float64 {

	lat1 *= math.Pi / 180.0 // Everything to radians.
	bearing *= math.Pi / 180.0

	var lat2 = math.Asin(math.Sin(lat1)*math.Cos(dist/R_KM) + math.Cos(lat1)*math.Sin(dist/R_KM)*math.Cos(bearing))

	lat2 *= 180.0 / math.Pi // Back to degrees.

	return (lat2)
}

func ll_dest_lon(lat1, lon1, dist, bearing float64) float64 {

	lat1 *= math.Pi / 180 // Everything to radians.
	lon1 *= math.Pi / 180
	bearing *= math.Pi / 180

	var lat2 = math.Asin(math.Sin(lat1)*math.Cos(dist/R_KM) + math.Cos(lat1)*math.Sin(dist/R_KM)*math.Cos(bearing))

	var lon2 = lon1 + math.Atan2(math.Sin(bearing)*math.Sin(dist/R_KM)*math.Cos(lat1), math.Cos(dist/R_KM)-math.Sin(lat1)*math.Sin(lat2))

	lon2 *= 180 / math.Pi // Back to degrees.

	return (lon2)
}

/*------------------------------------------------------------------
 *
 * Function:	ll_from_grid_square
 *
 * Purpose:	Convert Maidenhead locator to latitude and longitude.
 *
 * Inputs:	maidenhead	- 2, 4, 6, 8, 10, or 12 character grid square locator.
 *
 * Outputs:	dlat, dlon	- Latitude and longitude.
 *				  Original values unchanged if error.
 *
 * Returns:	1 for success, 0 if error.
 *
 * Reference:	A good converter for spot checking.  Only handles 4 or 6 characters :-(
 *		http://home.arcor.de/waldemar.kebsch/The_Makrothen_Contest/fmaidenhead.html
 *
 * Rambling:	What sort of resolution does this provide?
 *		For 8 character form, each latitude unit is 0.25 minute.
 *		(Longitude can be up to twice that around the equator.)
 *		6371 km * 2 * pi * 0.25 / 60 / 360 = 0.463 km.  Is that right?
 *
 *		Using this calculator, http://www.earthpoint.us/Convert.aspx
 *		It gives lower left corner of square rather than the middle.  :-(
 *
 *		FN42MA00  -->  19T 334361mE 4651711mN
 *		FN42MA11  -->  19T 335062mE 4652157mN
 *				   ------   -------
 *				      701       446    meters difference.
 *
 *		With another two pairs, we are down around 2 meters for latitude.
 *
 *------------------------------------------------------------------*/

const MH_MIN_PAIR = 1
const MH_MAX_PAIR = 6
const MH_UNITS = (18 * 10 * 24 * 10 * 24 * 10 * 2)

type mhPair struct {
	position string
	min_ch   byte
	max_ch   byte
	value    int
}

var MHPairs = []*mhPair{
	{"first", 'A', 'R', 10 * 24 * 10 * 24 * 10 * 2},
	{"second", '0', '9', 24 * 10 * 24 * 10 * 2},
	{"third", 'A', 'X', 10 * 24 * 10 * 2},
	{"fourth", '0', '9', 24 * 10 * 2},
	{"fifth", 'A', 'X', 10 * 2},
	{"sixth", '0', '9', 2},
} // Even so we can get center of square.

func ll_from_grid_square(maidenhead string) (float64, float64, error) {

	var np = len(maidenhead) / 2 /* Number of pairs of characters. */

	if len(maidenhead)%2 != 0 || np < MH_MIN_PAIR || np > MH_MAX_PAIR {
		text_color_set(DW_COLOR_ERROR)
		var s = fmt.Sprintf("Maidenhead locator \"%s\" must from 1 to %d pairs of characters.\n", maidenhead, MH_MAX_PAIR)
		dw_printf("%s", s)
		return 0, 0, errors.New(s)
	}

	var mh = strings.ToUpper(maidenhead)

	var ilat, ilon int
	for n := 0; n < np; n++ {
		if mh[2*n] < MHPairs[n].min_ch || mh[2*n] > MHPairs[n].max_ch ||
			mh[2*n+1] < MHPairs[n].min_ch || mh[2*n+1] > MHPairs[n].max_ch {
			text_color_set(DW_COLOR_ERROR)
			var s = fmt.Sprintf("The %s pair of characters in Maidenhead locator \"%s\" must be in range of %c thru %c.\n",
				MHPairs[n].position, maidenhead, MHPairs[n].min_ch, MHPairs[n].max_ch)
			dw_printf("%s", s)
			return 0, 0, errors.New(s)
		}

		ilon += int(mh[2*n]-MHPairs[n].min_ch) * MHPairs[n].value
		ilat += int(mh[2*n+1]-MHPairs[n].min_ch) * MHPairs[n].value

		if n == np-1 { // If last pair, take center of square.
			ilon += MHPairs[n].value / 2
			ilat += MHPairs[n].value / 2
		}
	}

	var dlat = float64(ilat)/MH_UNITS*180. - 90.
	var dlon = float64(ilon)/MH_UNITS*360. - 180.

	//text_color_set(DW_COLOR_DEBUG);
	//dw_printf("DEBUG: Maidenhead conversion \"%s\" -> %.6f %.6f\n", maidenhead, *dlat, *dlon);

	return dlat, dlon, nil
}

/* end ll_from_grid_square */
