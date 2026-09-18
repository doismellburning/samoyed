// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwgps

/*------------------------------------------------------------------
 *
 * Purpose:   	Convert the individual fields of an NMEA sentence.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"
	"strconv"
	"unicode"

	"github.com/doismellburning/samoyed/internal/maybe"
)

/* Dire Wolf's "value not known" sentinel, mirroring the direwolf package's
 * G_UNKNOWN.  A field parsed from a sentence could in principle carry it, and
 * it would then be indistinguishable from a real measurement downstream, so
 * recognise it here.  The conversion away from the sentinel hasn't finished
 * (see issue #619), which is why this package still has to know the value.
 */

const g_unknown = (-999999)

// unlessUnknown is Just the value, unless it is the G_UNKNOWN sentinel.
func unlessUnknown(value float64) maybe.Maybe[float64] {
	if value == g_unknown {
		return maybe.Nothing[float64]()
	}

	return maybe.Just(value)
}

// allDigits reports whether every byte of s is an ASCII digit.  An NMEA
// coordinate is fixed width and unsigned, so anything else in it - a sign, a
// second decimal point, a character corrupted in transit - makes the field
// unusable rather than something to interpret.
func allDigits(s string) bool {
	for i := range len(s) {
		if !unicode.IsDigit(rune(s[i])) {
			return false
		}
	}

	return true
}

/*------------------------------------------------------------------
 *
 * Function:	LatitudeFromNMEA
 *
 * Purpose:	Convert NMEA latitude encoding to degrees.
 *
 * Inputs:	pstr 	- Pointer to numeric string.
 *		phemi	- Pointer to following field.  Should be N or S.
 *
 * Returns:	Value in degrees, negative for South, or an error describing
 *		why the field could not be used.
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
 * Errors:	An error for any type of problem, including a value out of
 *		range or a hemisphere that is neither N nor S.  Callers are
 *		expected to have checked that the field is present at all;
 *		this does not print, because the caller knows which sentence
 *		the field came from and whether it was asked to stay quiet.
 *
 *------------------------------------------------------------------*/

func LatitudeFromNMEA(pstr string, phemi byte) (float64, error) {
	if len(pstr) < 5 {
		return 0, fmt.Errorf("latitude %q is too short for ddmm.mm", pstr)
	}

	if !allDigits(pstr[0:4]) {
		return 0, fmt.Errorf("latitude %q must have four digits of degrees and minutes before the decimal point", pstr)
	}

	if pstr[4] != '.' {
		return 0, fmt.Errorf("latitude %q must have a decimal point after the minutes", pstr)
	}

	if !allDigits(pstr[5:]) {
		return 0, fmt.Errorf("latitude %q must have only digits after the decimal point", pstr)
	}

	var mins, minsErr = strconv.ParseFloat(pstr[2:], 64)
	if minsErr != nil {
		return 0, fmt.Errorf("latitude %q has unusable minutes: %w", pstr, minsErr)
	}

	if mins >= 60 {
		return 0, fmt.Errorf("latitude %q has %.4f minutes, which must be less than 60", pstr, mins)
	}

	var lat = float64(pstr[0]-'0')*10 + float64(pstr[1]-'0')

	lat += mins / 60.0

	if lat < 0 || lat > 90 {
		return 0, fmt.Errorf("latitude %.4f is not in range of 0 to 90", lat)
	}

	// Saw this one time:
	//	$GPRMC,000000,V,0000.0000,0,00000.0000,0,000,000,000000,,*01

	// If location is unknown, I think the hemisphere should be
	// an empty string.  TODO: Check on this.
	// 'V' means void, so sentence should be discarded rather than
	// trying to extract any data from it.

	if phemi != 'N' && phemi != 'S' && phemi != 0 {
		return 0, fmt.Errorf("latitude hemisphere %q should be N or S", rune(phemi))
	}

	if phemi == 'S' {
		lat = (-lat)
	}

	return lat, nil
}

/*------------------------------------------------------------------
 *
 * Function:	LongitudeFromNMEA
 *
 * Purpose:	Convert NMEA longitude encoding to degrees.
 *
 * Inputs:	pstr 	- Pointer to numeric string.
 *		phemi	- Pointer to following field.  Should be E or W.
 *
 * Returns:	Value in degrees, negative for West, or an error describing why
 *		the field could not be used.
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
 * Errors:	An error for any type of problem, including a value out of
 *		range or a hemisphere that is neither E nor W.  Callers are
 *		expected to have checked that the field is present at all;
 *		this does not print, because the caller knows which sentence
 *		the field came from and whether it was asked to stay quiet.
 *
 *------------------------------------------------------------------*/

func LongitudeFromNMEA(pstr string, phemi byte) (float64, error) {
	if len(pstr) < 6 {
		return 0, fmt.Errorf("longitude %q is too short for dddmm.mm", pstr)
	}

	if !allDigits(pstr[0:5]) {
		return 0, fmt.Errorf("longitude %q must have five digits of degrees and minutes before the decimal point", pstr)
	}

	if pstr[5] != '.' {
		return 0, fmt.Errorf("longitude %q must have a decimal point after the minutes", pstr)
	}

	if !allDigits(pstr[6:]) {
		return 0, fmt.Errorf("longitude %q must have only digits after the decimal point", pstr)
	}

	var mins, minsErr = strconv.ParseFloat(pstr[3:], 64)
	if minsErr != nil {
		return 0, fmt.Errorf("longitude %q has unusable minutes: %w", pstr, minsErr)
	}

	if mins >= 60 {
		return 0, fmt.Errorf("longitude %q has %.4f minutes, which must be less than 60", pstr, mins)
	}

	var lon = float64(pstr[0]-'0')*100 + float64(pstr[1]-'0')*10 + float64(pstr[2]-'0')

	lon += mins / 60.0

	if lon < 0 || lon > 180 {
		return 0, fmt.Errorf("longitude %.4f is not in range of 0 to 180", lon)
	}

	if phemi != 'E' && phemi != 'W' && phemi != 0 {
		return 0, fmt.Errorf("longitude hemisphere %q should be E or W", rune(phemi))
	}

	if phemi == 'W' {
		lon = (-lon)
	}

	return lon, nil
}
