// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package reedsolomon is a Reed-Solomon codec for symbols of up to 8 bits,
// as used by the FX.25 and IL2P forward error correction schemes.
//
// It is based on Phil Karn's (KA9Q) codec, by way of Jim McGuire's (KB3MPL)
// FX.25 encoder and Dire Wolf.
package reedsolomon

import (
	"fmt"

	"github.com/ccoveille/go-safecast/v2"
)

// Interesting related stuff:
// https://www.kernel.org/doc/html/v4.15/core-api/librs.html
// https://berthub.eu/articles/posts/reed-solomon-for-programmers/

// Codec is a Reed-Solomon codec control block.
type Codec struct {
	mm       uint   /* Bits per symbol */
	nn       uint   /* Symbols per block (= (1<<mm)-1) */
	alpha_to []byte /* log lookup table */
	index_of []byte /* Antilog lookup table */
	genpoly  []byte /* Generator polynomial */
	nroots   uint   /* Number of generator roots = number of parity symbols */
	fcr      byte   /* First consecutive root, index form */
	prim     byte   /* Primitive element, index form */
	iprim    byte   /* prim-th root of 1, index form */
}

// N is the number of symbols in a block, data and parity together.
func (c *Codec) N() int {
	return int(c.nn)
}

// NRoots is the number of parity symbols in a block.
func (c *Codec) NRoots() int {
	return int(c.nroots)
}

func (c *Codec) modnn(_x int) int {
	var x = uint(_x)

	for x >= c.nn {
		x -= c.nn
		x = (x >> c.mm) + (x & c.nn)
	}

	return int(x)
}

// modnnByte is modnn for a result headed for a byte, which it always fits:
// it is less than nn, which New keeps to at most 255.  Masking rather than
// safecast keeps the decoder's inner loops cheap.
func (c *Codec) modnnByte(x int) byte {
	return byte(c.modnn(x) & 0xff)
}

// New initializes a Reed-Solomon codec.
//
//	symsize = symbol size, bits (1-8) - always 8 for FX.25 and IL2P.
//	gfpoly = Field generator polynomial coefficients
//	fcr = first root of RS code generator polynomial, index form
//	prim = primitive element to generate polynomial roots
//	nroots = RS code generator polynomial degree (number of roots)
func New(symsize uint, gfpoly uint, fcr uint, prim uint, nroots uint) (*Codec, error) {
	if symsize > 8 {
		return nil, fmt.Errorf("symbol size %d is more than 8 bits", symsize) // Need version with ints rather than chars
	}

	if fcr >= (1 << symsize) {
		return nil, fmt.Errorf("first consecutive root %d is out of range", fcr)
	}

	if prim == 0 || prim >= (1<<symsize) {
		return nil, fmt.Errorf("primitive element %d is out of range", prim)
	}

	if nroots >= (1 << symsize) {
		return nil, fmt.Errorf("%d roots is more than there are symbol values", nroots) // Can't have more roots than symbol values!
	}

	var rs = new(Codec)

	rs.mm = symsize
	rs.nn = uint((1 << symsize) - 1)

	rs.alpha_to = make([]byte, rs.nn+1)
	rs.index_of = make([]byte, rs.nn+1)

	// Generate Galois field lookup tables
	rs.index_of[0] = safecast.MustConvert[byte](rs.nn) // log(zero) = -inf (A0)
	rs.alpha_to[rs.nn] = 0                             // alpha**-inf = 0

	var sr = 1
	for i := range rs.nn {
		rs.index_of[sr] = byte(i)
		rs.alpha_to[i] = byte(sr)

		sr <<= 1
		if sr&(1<<symsize) != 0 {
			sr ^= int(gfpoly)
		}

		sr &= int(rs.nn)
	}

	if sr != 1 {
		// field generator polynomial is not primitive!
		return nil, fmt.Errorf("field generator polynomial 0x%x is not primitive", gfpoly)
	}

	// Form RS code generator polynomial from its roots
	rs.genpoly = make([]byte, nroots+1)
	rs.fcr = safecast.MustConvert[byte](fcr)
	rs.prim = safecast.MustConvert[byte](prim)
	rs.nroots = nroots

	// Find prim-th root of 1, used in decoding
	var iprim = 1
	for (iprim % int(prim)) != 0 {
		iprim += int(rs.nn)
	}

	rs.iprim = safecast.MustConvert[byte](iprim / int(prim))

	rs.genpoly[0] = 1
	for i, root := 0, int(fcr)*int(prim); i < int(nroots); i, root = i+1, root+int(prim) {
		rs.genpoly[i+1] = 1

		// Multiply rs->genpoly[] by  @**(root + x)
		for j := i; j > 0; j-- {
			if rs.genpoly[j] != 0 {
				rs.genpoly[j] = rs.genpoly[j-1] ^ rs.alpha_to[rs.modnn(int(rs.index_of[rs.genpoly[j]])+root)]
			} else {
				rs.genpoly[j] = rs.genpoly[j-1]
			}
		}
		// rs->genpoly[0] can never be zero
		rs.genpoly[0] = rs.alpha_to[rs.modnn(int(rs.index_of[rs.genpoly[0]])+root)]
	}
	// convert rs->genpoly[] to index form for quicker encoding
	for i := 0; i <= int(nroots); i++ {
		rs.genpoly[i] = rs.index_of[rs.genpoly[i]]
	}

	return rs, nil
}
