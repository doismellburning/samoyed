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

func encode_rs_char(rs *rs_t, data []byte, bb []byte) {

	var nroots = int(rs.nroots)
	var nn = int(rs.nn)
	var dataLen = nn - nroots

	// Clear out the FEC data area
	for k := range bb {
		bb[k] = 0
	}

	for i := 0; i < dataLen; i++ {
		// feedback = INDEX_OF[data[i] ^ bb[0]]
		var feedback = rs.index_of[data[i]^bb[0]]

		if uint(feedback) != rs.nn { // feedback term is non-zero
			for j := 1; j < nroots; j++ {
				// bb[j] ^= ALPHA_TO[modnn(feedback + GENPOLY[NROOTS-j])]
				var genpolyVal = rs.genpoly[nroots-j]
				var modnnResult = modnn(rs, int(feedback)+int(genpolyVal))
				bb[j] ^= rs.alpha_to[modnnResult]
			}
		}

		// Shift
		copy(bb, bb[1:])

		// bb[NROOTS-1] = ...
		if uint(feedback) != rs.nn {
			// ALPHA_TO[modnn(feedback + GENPOLY[0])]
			var genpolyVal = rs.genpoly[0]
			var modnnResult = modnn(rs, int(feedback)+int(genpolyVal))
			bb[nroots-1] = rs.alpha_to[modnnResult]
		} else {
			bb[nroots-1] = 0
		}
	}
}

// end fx25_encode.go
