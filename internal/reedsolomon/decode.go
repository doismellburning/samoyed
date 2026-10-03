// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: 2019 John Langner, WB2OSZ
// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package reedsolomon

import (
	"errors"
	"fmt"
)

// -----------------------------------------------------------------------
//
// This is based on:
//
//
// FX25_extract.c
//	Author: Jim McGuire KB3MPL
//	Date: 	23 October 2007
//
//
// Accepts an FX.25 byte stream on STDIN, finds the correlation tag, stores 256 bytes,
//   corrects errors with FEC, removes the bit-stuffing, and outputs the resultant AX.25
//   byte stream out STDOUT.
//
// stdout prints a bunch of status information about the packet being processed.
//
//
// Usage : FX25_extract < infile > outfile [2> logfile]
//
//
//
// This program is a single-file implementation of the FX.25 extraction/decode
// structure for use with FX.25 data frames.  Details of the FX.25
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

// #define DEBUG 5

// ErrUncorrectable is returned by Decode for a block with more errors than
// its parity symbols can repair.
var ErrUncorrectable = errors.New("too many errors to correct")

// Decode checks a block of N() symbols - data followed by NRoots() parity
// symbols - and corrects it in place where it can.
//
// erasures optionally gives the positions of symbols already known to be bad,
// which costs half as much of the correcting power as an unknown error; it
// may be nil, and must not be longer than NRoots().
//
// It returns the positions of the symbols it corrected, or ErrUncorrectable,
// in which case data may have been left partly modified.
func (c *Codec) Decode(data []byte, erasures []int) ([]int, error) {
	if len(data) != c.N() {
		return nil, fmt.Errorf("block is %d symbols, want %d", len(data), c.N())
	}

	if len(erasures) > c.NRoots() {
		return nil, fmt.Errorf("%d erasures, but only %d parity symbols", len(erasures), c.NRoots())
	}

	for _, pos := range erasures {
		if pos < 0 || pos >= c.N() {
			return nil, fmt.Errorf("erasure position %d is outside the block", pos)
		}
	}

	var errLocs = make([]int, c.NRoots())
	copy(errLocs, erasures)

	var count = c.decode(data, errLocs, len(erasures))
	if count < 0 {
		return nil, ErrUncorrectable
	}

	return errLocs[:count], nil
}

func (c *Codec) decode(data []byte, eras_pos []int, no_eras int) int {
	// Access codec struct members
	var nn = int(c.nn)
	var nroots = int(c.nroots)
	var fcr = int(c.fcr)
	var prim = int(c.prim)
	var iprim = int(c.iprim)
	var A0 = nn // A0 is defined as NN

	var degLambda, el, degOmega int
	var i, j, r, k int
	var u, q, tmp, num1, num2, den, discrR byte

	// Err+Eras Locator poly and syndrome poly
	var lambda = make([]byte, nroots+1)
	var s = make([]byte, nroots)
	var b = make([]byte, nroots+1)
	var t = make([]byte, nroots+1)
	var omega = make([]byte, nroots+1)
	var root = make([]byte, nroots)
	var reg = make([]byte, nroots+1)
	var loc = make([]int, nroots)

	var synError int
	var count int

	// form the syndromes; i.e., evaluate data(x) at roots of g(x)
	for i = range nroots {
		s[i] = data[0]
	}

	for j = 1; j < nn; j++ {
		for i = range nroots {
			if s[i] == 0 {
				s[i] = data[j]
			} else {
				s[i] = data[j] ^ c.alpha_to[c.modnn(int(c.index_of[s[i]])+(fcr+i)*prim)]
			}
		}
	}

	// Convert syndromes to index form, checking for nonzero condition
	synError = 0
	for i = range nroots {
		synError |= int(s[i])
		s[i] = c.index_of[s[i]]
	}

	// fprintf(stderr,"syn_error = %4x\n",syn_error);
	if synError == 0 {
		// if syndrome is zero, data[] is a codeword and there are no
		// errors to correct. So return data[] unmodified
		count = 0

		goto finish
	}

	// memset(&lambda[1],0,NROOTS*sizeof(lambda[0]));
	for i = 1; i <= nroots; i++ {
		lambda[i] = 0
	}

	lambda[0] = 1

	if no_eras > 0 {
		// Init lambda to be the erasure locator polynomial
		lambda[1] = c.alpha_to[c.modnn(prim*(nn-1-eras_pos[0]))]
		for i = 1; i < no_eras; i++ {
			u = c.modnnByte(prim * (nn - 1 - eras_pos[i]))
			for j = i + 1; j > 0; j-- {
				tmp = c.index_of[lambda[j-1]]
				if int(tmp) != A0 {
					lambda[j] ^= c.alpha_to[c.modnn(int(u)+int(tmp))]
				}
			}
		}

		// #if DEBUG >= 1
		// /* Test code that verifies the erasure locator polynomial just constructed
		//    Needed only for decoder debugging. */
		//
		// /* find roots of the erasure location polynomial */
		// for(i=1;i<=no_eras;i++)
		//   reg[i] = INDEX_OF[lambda[i]];
		//
		// count = 0;
		// for (i = 1,k=IPRIM-1; i <= NN; i++,k = modnn(k+IPRIM)) {
		//   q = 1;
		//   for (j = 1; j <= no_eras; j++)
		// 	if (reg[j] != A0) {
		// 	  reg[j] = modnn(reg[j] + j);
		// 	  q ^= ALPHA_TO[reg[j]];
		// 	}
		//   if (q != 0)
		// 	continue;
		//   /* store root and error location number indices */
		//   root[count] = i;
		//   loc[count] = k;
		//   count++;
		// }
		// if (count != no_eras) {
		//   fprintf(stderr,"count = %d no_eras = %d\n lambda(x) is WRONG\n",count,no_eras);
		//   count = -1;
		//   goto finish;
		// }
		// #if DEBUG >= 2
		// fprintf(stderr,"\n Erasure positions as determined by roots of Eras Loc Poly:\n");
		// for (i = 0; i < count; i++)
		//   fprintf(stderr,"%d ", loc[i]);
		// fprintf(stderr,"\n");
		// #endif
		// #endif
	}

	// for(i=0;i<NROOTS+1;i++)
	//   b[i] = INDEX_OF[lambda[i]];
	for i = range nroots + 1 {
		b[i] = c.index_of[lambda[i]]
	}

	// Begin Berlekamp-Massey algorithm to determine error+erasure
	// locator polynomial
	r = no_eras
	el = no_eras

	for r++; r <= nroots; r++ {
		// Compute discrepancy at the r-th step in poly-form
		discrR = 0

		for i = range r {
			if lambda[i] != 0 && int(s[r-i-1]) != A0 {
				discrR ^= c.alpha_to[c.modnn(int(c.index_of[lambda[i]])+int(s[r-i-1]))]
			}
		}

		discrR = c.index_of[discrR] // Index form
		if int(discrR) == A0 {
			// 2 lines below: B(x) <-- x*B(x)
			// memmove(&b[1],b,NROOTS*sizeof(b[0]));
			copy(b[1:nroots+1], b[0:nroots])
			b[0] = c.a0
		} else {
			// 7 lines below: T(x) <-- lambda(x) - discr_r*x*b(x)
			t[0] = lambda[0]

			for i = range nroots {
				if int(b[i]) != A0 {
					t[i+1] = lambda[i+1] ^ c.alpha_to[c.modnn(int(discrR)+int(b[i]))]
				} else {
					t[i+1] = lambda[i+1]
				}
			}

			if 2*el <= r+no_eras-1 {
				el = r + no_eras - el
				// 2 lines below: B(x) <-- inv(discr_r) * lambda(x)
				for i = range nroots + 1 {
					if lambda[i] == 0 {
						b[i] = c.a0
					} else {
						b[i] = c.modnnByte(int(c.index_of[lambda[i]]) - int(discrR) + nn)
					}
				}
			} else {
				// 2 lines below: B(x) <-- x*B(x)
				// memmove(&b[1],b,NROOTS*sizeof(b[0]));
				copy(b[1:nroots+1], b[0:nroots])
				b[0] = c.a0
			}
			// memcpy(lambda,t,(NROOTS+1)*sizeof(t[0]));
			copy(lambda, t[:nroots+1])
		}
	}

	// Convert lambda to index form and compute deg(lambda(x))
	degLambda = 0

	for i = range nroots + 1 {
		lambda[i] = c.index_of[lambda[i]]
		if int(lambda[i]) != A0 {
			degLambda = i
		}
	}
	// Find roots of the error+erasure locator polynomial by Chien search
	// memcpy(&reg[1],&lambda[1],NROOTS*sizeof(reg[0]));
	copy(reg[1:nroots+1], lambda[1:nroots+1])

	count = 0 // Number of roots of lambda(x)

	for i, k = 1, iprim-1; i <= nn; i, k = i+1, c.modnn(k+iprim) {
		q = 1 // lambda[0] is always 0

		for j = degLambda; j > 0; j-- {
			if int(reg[j]) != A0 {
				reg[j] = c.modnnByte(int(reg[j]) + j)
				q ^= c.alpha_to[reg[j]]
			}
		}

		if q != 0 {
			continue // Not a root
		}
		// store root (index-form) and error location number
		// #if DEBUG>=2
		// fprintf(stderr,"count %d root %d loc %d\n",count,i,k);
		// #endif
		root[count] = byte(i)
		loc[count] = k
		// If we've already found max possible roots,
		// abort the search to save time
		count++
		if count == degLambda {
			break
		}
	}

	if degLambda != count {
		// deg(lambda) unequal to number of roots => uncorrectable
		// error detected
		count = -1

		goto finish
	}
	// Compute err+eras evaluator poly omega(x) = s(x)*lambda(x) (modulo
	// x**NROOTS). in index form. Also find deg(omega).
	degOmega = 0

	for i = range nroots {
		tmp = 0

		j = min(degLambda, i)

		for ; j >= 0; j-- {
			if int(s[i-j]) != A0 && int(lambda[j]) != A0 {
				tmp ^= c.alpha_to[c.modnn(int(s[i-j])+int(lambda[j]))]
			}
		}

		if tmp != 0 {
			degOmega = i
		}

		omega[i] = c.index_of[tmp]
	}

	omega[nroots] = c.a0

	// Compute error values in poly-form. num1 = omega(inv(X(l))), num2 =
	// inv(X(l))**(FCR-1) and den = lambda_pr(inv(X(l))) all in poly-form
	for j = count - 1; j >= 0; j-- {
		num1 = 0

		for i = degOmega; i >= 0; i-- {
			if int(omega[i]) != A0 {
				num1 ^= c.alpha_to[c.modnn(int(omega[i])+i*int(root[j]))]
			}
		}

		num2 = c.alpha_to[c.modnn(int(root[j])*(fcr-1)+nn)]
		den = 0

		// lambda[i+1] for i even is the formal derivative lambda_pr of lambda[i]
		var maxI = min(degLambda, nroots-1)

		for i = maxI & ^1; i >= 0; i -= 2 {
			if int(lambda[i+1]) != A0 {
				den ^= c.alpha_to[c.modnn(int(lambda[i+1])+i*int(root[j]))]
			}
		}

		if den == 0 {
			// #if DEBUG >= 1
			// fprintf(stderr,"\n ERROR: denominator = 0\n");
			// #endif
			count = -1

			goto finish
		}
		// Apply error to data
		if num1 != 0 {
			data[loc[j]] ^= c.alpha_to[c.modnn(int(c.index_of[num1])+int(c.index_of[num2])+nn-int(c.index_of[den]))]
		}
	}

finish:
	if eras_pos != nil {
		for i = range count {
			eras_pos[i] = loc[i]
		}
	}

	return count
}
