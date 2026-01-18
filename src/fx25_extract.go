package direwolf

// SPDX-FileCopyrightText: 2002 Phil Karn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: 2019 John Langner, WB2OSZ
// SPDX-FileCopyrightText: The Samoyed Authors

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

// #include <stdio.h>
// #include <stdlib.h>
// #include <string.h>
import "C"

import (
	"unsafe"
)

// #define DEBUG 5

func decode_rs_char(rs *rs_t, data *C.uchar, eras_pos *C.int, no_eras C.int) C.int {
	// Access rs struct members
	var nn = int(rs.nn)
	var nroots = int(rs.nroots)
	var fcr = int(rs.fcr)
	var prim = int(rs.prim)
	var iprim = int(rs.iprim)
	var A0 = nn // A0 is defined as NN

	// Create Go slice views over C arrays
	var dataSlice = unsafe.Slice((*byte)(data), nn)
	var alphaTo = unsafe.Slice((*byte)(rs.alpha_to), nn+1)
	var indexOf = unsafe.Slice((*byte)(rs.index_of), nn+1)

	var degLambda, el, degOmega int
	var i, j, r, k int
	var u, q, tmp, num1, num2, den, discrR byte

	// Err+Eras Locator poly and syndrome poly
	var lambda = make([]byte, FX25_MAX_CHECK+1)
	var s = make([]byte, FX25_MAX_CHECK)
	var b = make([]byte, FX25_MAX_CHECK+1)
	var t = make([]byte, FX25_MAX_CHECK+1)
	var omega = make([]byte, FX25_MAX_CHECK+1)
	var root = make([]byte, FX25_MAX_CHECK)
	var reg = make([]byte, FX25_MAX_CHECK+1)
	var loc = make([]int, FX25_MAX_CHECK)

	var synError int
	var count int

	// form the syndromes; i.e., evaluate data(x) at roots of g(x)
	for i = 0; i < nroots; i++ {
		s[i] = dataSlice[0]
	}

	for j = 1; j < nn; j++ {
		for i = 0; i < nroots; i++ {
			if s[i] == 0 {
				s[i] = dataSlice[j]
			} else {
				s[i] = dataSlice[j] ^ alphaTo[modnn(rs, int(indexOf[s[i]])+(fcr+i)*prim)]
			}
		}
	}

	// Convert syndromes to index form, checking for nonzero condition
	synError = 0
	for i = 0; i < nroots; i++ {
		synError |= int(s[i])
		s[i] = indexOf[s[i]]
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
		var erasPosSlice = unsafe.Slice(eras_pos, no_eras)
		lambda[1] = alphaTo[modnn(rs, prim*(nn-1-int(erasPosSlice[0])))]
		for i = 1; i < int(no_eras); i++ {
			u = byte(modnn(rs, prim*(nn-1-int(erasPosSlice[i]))))
			for j = i + 1; j > 0; j-- {
				tmp = indexOf[lambda[j-1]]
				if int(tmp) != A0 {
					lambda[j] ^= alphaTo[modnn(rs, int(u)+int(tmp))]
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
	for i = 0; i < nroots+1; i++ {
		b[i] = indexOf[lambda[i]]
	}

	// Begin Berlekamp-Massey algorithm to determine error+erasure
	// locator polynomial
	r = int(no_eras)
	el = int(no_eras)
	for r++; r <= nroots; r++ {
		// Compute discrepancy at the r-th step in poly-form
		discrR = 0
		for i = 0; i < r; i++ {
			if lambda[i] != 0 && int(s[r-i-1]) != A0 {
				discrR ^= alphaTo[modnn(rs, int(indexOf[lambda[i]])+int(s[r-i-1]))]
			}
		}
		discrR = indexOf[discrR] // Index form
		if int(discrR) == A0 {
			// 2 lines below: B(x) <-- x*B(x)
			// memmove(&b[1],b,NROOTS*sizeof(b[0]));
			copy(b[1:nroots+1], b[0:nroots])
			b[0] = byte(A0)
		} else {
			// 7 lines below: T(x) <-- lambda(x) - discr_r*x*b(x)
			t[0] = lambda[0]
			for i = 0; i < nroots; i++ {
				if int(b[i]) != A0 {
					t[i+1] = lambda[i+1] ^ alphaTo[modnn(rs, int(discrR)+int(b[i]))]
				} else {
					t[i+1] = lambda[i+1]
				}
			}
			if 2*el <= r+int(no_eras)-1 {
				el = r + int(no_eras) - el
				// 2 lines below: B(x) <-- inv(discr_r) * lambda(x)
				for i = 0; i <= nroots; i++ {
					if lambda[i] == 0 {
						b[i] = byte(A0)
					} else {
						b[i] = byte(modnn(rs, int(indexOf[lambda[i]])-int(discrR)+nn))
					}
				}
			} else {
				// 2 lines below: B(x) <-- x*B(x)
				// memmove(&b[1],b,NROOTS*sizeof(b[0]));
				copy(b[1:nroots+1], b[0:nroots])
				b[0] = byte(A0)
			}
			// memcpy(lambda,t,(NROOTS+1)*sizeof(t[0]));
			copy(lambda, t[:nroots+1])
		}
	}

	// Convert lambda to index form and compute deg(lambda(x))
	degLambda = 0
	for i = 0; i < nroots+1; i++ {
		lambda[i] = indexOf[lambda[i]]
		if int(lambda[i]) != A0 {
			degLambda = i
		}
	}
	// Find roots of the error+erasure locator polynomial by Chien search
	// memcpy(&reg[1],&lambda[1],NROOTS*sizeof(reg[0]));
	copy(reg[1:nroots+1], lambda[1:nroots+1])
	count = 0 // Number of roots of lambda(x)
	for i, k = 1, iprim-1; i <= nn; i, k = i+1, int(modnn(rs, k+iprim)) {
		q = 1 // lambda[0] is always 0
		for j = degLambda; j > 0; j-- {
			if int(reg[j]) != A0 {
				reg[j] = byte(modnn(rs, int(reg[j])+j))
				q ^= alphaTo[reg[j]]
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
	for i = 0; i < nroots; i++ {
		tmp = 0
		if degLambda < i {
			j = degLambda
		} else {
			j = i
		}
		for ; j >= 0; j-- {
			if int(s[i-j]) != A0 && int(lambda[j]) != A0 {
				tmp ^= alphaTo[modnn(rs, int(s[i-j])+int(lambda[j]))]
			}
		}
		if tmp != 0 {
			degOmega = i
		}
		omega[i] = indexOf[tmp]
	}
	omega[nroots] = byte(A0)

	// Compute error values in poly-form. num1 = omega(inv(X(l))), num2 =
	// inv(X(l))**(FCR-1) and den = lambda_pr(inv(X(l))) all in poly-form
	for j = count - 1; j >= 0; j-- {
		num1 = 0
		for i = degOmega; i >= 0; i-- {
			if int(omega[i]) != A0 {
				num1 ^= alphaTo[modnn(rs, int(omega[i])+i*int(root[j]))]
			}
		}
		num2 = alphaTo[modnn(rs, int(root[j])*(fcr-1)+nn)]
		den = 0

		// lambda[i+1] for i even is the formal derivative lambda_pr of lambda[i]
		var maxI int
		if degLambda < nroots-1 {
			maxI = degLambda
		} else {
			maxI = nroots - 1
		}
		for i = maxI & ^1; i >= 0; i -= 2 {
			if int(lambda[i+1]) != A0 {
				den ^= alphaTo[modnn(rs, int(lambda[i+1])+i*int(root[j]))]
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
			dataSlice[loc[j]] ^= alphaTo[modnn(rs, int(indexOf[num1])+int(indexOf[num2])+nn-int(indexOf[den]))]
		}
	}
finish:
	if eras_pos != nil {
		var erasPosSlice = unsafe.Slice(eras_pos, max(count, 0)+int(no_eras))
		for i = 0; i < count; i++ {
			erasPosSlice[i] = C.int(loc[i])
		}
	}
	return C.int(count)
}

// end fx25_extract.go
