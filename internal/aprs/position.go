// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

// latLongPosition is the uncompressed latitude, longitude and symbol of an APRS
// position report, laid out as it is on the air.
type latLongPosition struct {
	Lat        [8]byte
	SymTableId byte /* / \ 0-9 A-Z */
	Lon        [9]byte
	SymbolCode byte
}

// compressedPositionData is the base 91 compressed form of latLongPosition, with the
// optional course/speed, radio range or altitude that goes with it.
type compressedPositionData struct {
	SymTableId byte /* / \ a-j A-Z */
	/* "The presence of the leading Symbol Table Identifier */
	/* instead of a digit indicates that this is a compressed */
	/* Position Report and not a normal lat/long report." */
	/* "a-j" is not a typographical error. */
	/* The first 10 lower case letters represent the overlay */
	/* characters of 0-9 in the compressed format. */

	Y          [4]byte /* Compressed Latitude. */
	X          [4]byte /* Compressed Longitude. */
	SymbolCode byte
	C          byte /* Course/speed or radio range or altitude. */
	S          byte
	T          byte /* Compression type. */
}
