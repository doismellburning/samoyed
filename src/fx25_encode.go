package direwolf

// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors

// Most of this is based on:
//
// FX.25 Encoder
//	Author: Jim McGuire KB3MPL
//	Date: 	23 October 2007
//
// This program is a single-file implementation of the FX.25 encapsulation
// structure for use with AX.25 data packets.  Details of the FX.25
// specification are available at:
//     http://www.stensat.org/Docs/Docs.htm
//
// This program implements a single RS(255,239) FEC structure.  Future
// releases will incorporate more capabilities as accommodated in the FX.25
// spec.
//
// The Reed Solomon encoding routines are based on work performed by
// Phil Karn.  Phil was kind enough to release his code under the GPL, as
// noted below.  Consequently, this FX.25 implementation is also released
// under the terms of the GPL.
//
// Phil Karn's original copyright notice:
/* Test the Reed-Solomon codecs
 * for various block sizes and with random data and random error patterns
 *
 * Copyright 2002 Phil Karn, KA9Q
 * May be used under the terms of the GNU General Public License (GPL)
 *
 */

// #include <stdio.h>
// #include <stdlib.h>
// #include <string.h>
// #include <ctype.h>
// #include "fx25.h"
import "C"

import (
	"unsafe"
)

func encode_rs_char(rs *C.struct_rs, data *C.uchar, bb *C.uchar) {

	var nroots = int(rs.nroots)
	var nn = int(rs.nn)
	var dataLen = nn - nroots

	// Create Go slice views over C arrays for cleaner indexing
	var dataSlice = unsafe.Slice((*byte)(data), dataLen)
	var bbSlice = unsafe.Slice((*byte)(bb), nroots)
	var indexOfSlice = unsafe.Slice((*byte)(rs.index_of), nn+1)
	var alphaToSlice = unsafe.Slice((*byte)(rs.alpha_to), nn+1)
	var genpolySlice = unsafe.Slice((*byte)(rs.genpoly), nroots+1)

	// Clear out the FEC data area
	for k := range bbSlice {
		bbSlice[k] = 0
	}

	for i := 0; i < dataLen; i++ {
		// feedback = INDEX_OF[data[i] ^ bb[0]]
		var feedback = C.uchar(indexOfSlice[dataSlice[i]^bbSlice[0]])

		if C.uint(feedback) != rs.nn { // feedback term is non-zero
			for j := 1; j < nroots; j++ {
				// bb[j] ^= ALPHA_TO[MODNN(feedback + GENPOLY[NROOTS-j])]
				var genpolyVal = C.uchar(genpolySlice[nroots-j])
				var modnnResult = C.modnn(rs, C.int(feedback)+C.int(genpolyVal))
				bbSlice[j] ^= alphaToSlice[modnnResult]
			}
		}

		// Shift
		copy(bbSlice, bbSlice[1:])

		// bb[NROOTS-1] = ...
		if C.uint(feedback) != rs.nn {
			// ALPHA_TO[MODNN(feedback + GENPOLY[0])]
			var genpolyVal = C.uchar(genpolySlice[0])
			var modnnResult = C.modnn(rs, C.int(feedback)+C.int(genpolyVal))
			bbSlice[nroots-1] = alphaToSlice[modnnResult]
		} else {
			bbSlice[nroots-1] = 0
		}
	}
}

// end fx25_encode.go
