// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package dwutil

import "math"

// The unit conversions below are the Dire Wolf macros of the same names, less
// their `(x) == G_UNKNOWN ? G_UNKNOWN :` guard: a value that might not be there
// is a maybe.Maybe, and maybe.Fmap keeps its absence out of the arithmetic.

// DW_KNOTS_TO_MPH converts knots to miles per hour.
func DW_KNOTS_TO_MPH(x float64) float64 {
	return x * 1.15077945
}

// DW_MPH_TO_KNOTS converts miles per hour to knots.
func DW_MPH_TO_KNOTS(x float64) float64 {
	return x * 0.868976
}

// DW_METERS_TO_FEET converts metres to feet.
func DW_METERS_TO_FEET(x float64) float64 {
	return x * 3.2808399
}

// DW_FEET_TO_METERS converts feet to metres.
func DW_FEET_TO_METERS(x float64) float64 {
	return x * 0.3048
}

// DW_MILES_TO_KM converts miles to kilometres.
func DW_MILES_TO_KM(x float64) float64 {
	return x * 1.609344
}

// DW_MBAR_TO_INHG converts millibars to inches of mercury.
func DW_MBAR_TO_INHG(x float64) float64 {
	return x * 0.0295333727
}

// DW_KM_TO_MILES converts kilometres to miles.
func DW_KM_TO_MILES(x float64) float64 {
	return x * 0.621371192
}

// D2R converts degrees to radians.
func D2R(d float64) float64 {
	return d * math.Pi / 180
}

// R2D converts radians to degrees.
func R2D(r float64) float64 {
	return r * 180 / math.Pi
}
