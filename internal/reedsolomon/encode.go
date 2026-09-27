// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package reedsolomon

import "fmt"

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

// Encode computes the NRoots() parity symbols for a block of N()-NRoots() data
// symbols. It panics if data is any other length, which is a programming
// error rather than something a received signal can cause.
func (c *Codec) Encode(data []byte) []byte {
	if len(data) != c.N()-c.NRoots() {
		panic(fmt.Sprintf("reedsolomon: Encode given %d data symbols, want %d", len(data), c.N()-c.NRoots()))
	}

	var parity = make([]byte, c.NRoots())
	c.encode(data, parity)

	return parity
}

func (c *Codec) encode(data []byte, bb []byte) {
	var nroots = int(c.nroots)
	var nn = int(c.nn)
	var dataLen = nn - nroots

	// Clear out the FEC data area
	for k := range bb {
		bb[k] = 0
	}

	for i := range dataLen {
		// feedback = INDEX_OF[data[i] ^ bb[0]]
		var feedback = c.index_of[data[i]^bb[0]]

		if uint(feedback) != c.nn { // feedback term is non-zero
			for j := 1; j < nroots; j++ {
				// bb[j] ^= ALPHA_TO[modnn(feedback + GENPOLY[NROOTS-j])]
				var genpolyVal = c.genpoly[nroots-j]
				var modnnResult = c.modnn(int(feedback) + int(genpolyVal))
				bb[j] ^= c.alpha_to[modnnResult]
			}
		}

		// Shift
		copy(bb, bb[1:])

		// bb[NROOTS-1] = ...
		if uint(feedback) != c.nn {
			// ALPHA_TO[modnn(feedback + GENPOLY[0])]
			var genpolyVal = c.genpoly[0]
			var modnnResult = c.modnn(int(feedback) + int(genpolyVal))
			bb[nroots-1] = c.alpha_to[modnnResult]
		} else {
			bb[nroots-1] = 0
		}
	}
}
