// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

// base91Min and base91Max are the range of digits for base 91 representation.
const base91Min = '!'
const base91Max = '{'

// isBase91Digit reports whether c is a base 91 digit.
func isBase91Digit(c byte) bool {
	return ((c) >= base91Min && (c) <= base91Max)
}

// base91Value is the number a run of base 91 digits encodes, most significant
// first.
func base91Value(digits []byte) int {
	var value = 0

	for _, digit := range digits {
		value = value*91 + int(digit) - base91Min
	}

	return value
}
