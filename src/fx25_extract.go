package direwolf

// SPDX-FileCopyrightText: 2002 Phil Carn, KA9Q
// SPDX-FileCopyrightText: 2007 Jim McGuire KB3MPL
// SPDX-FileCopyrightText: The Samoyed Authors

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
// #include "fx25.h"
import "C"

//#define DEBUG 5

//-----------------------------------------------------------------------
// Revision History
//-----------------------------------------------------------------------
// 0.0.1  - initial release
//          Modifications from Phil Karn's GPL source code.
//          Initially added code to run with PC file
//          I/O and use the (255,239) decoder exclusively.  Confirmed that the
//          code produces the correct results.
//
//-----------------------------------------------------------------------
// 0.0.2  -

func DECODE_RS(rs *C.struct_rs, data *C.uchar, eras_pos *C.int, no_eras C.int) {

	var deg_lambda, el, deg_omega C.int
	var i, j, r, k C.int
	var u, q, tmp, num1, num2, den, discr_r C.uchar
	//  uchar lambda[rs.nroots+1], s[rs.nroots];	/* Err+Eras Locator poly and syndrome poly */
	//  uchar b[rs.nroots+1], t[rs.nroots+1], omega[rs.nroots+1];
	//  uchar root[rs.nroots], reg[rs.nroots+1], loc[rs.nroots];
	var lambda [FX25_MAX_CHECK + 1]C.uchar
	var s [FX25_MAX_CHECK]C.uchar /* Err+Eras Locator poly and syndrome poly */
	var b [FX25_MAX_CHECK + 1]C.uchar
	var t [FX25_MAX_CHECK + 1]C.uchar
	var omega [FX25_MAX_CHECK + 1]C.uchar
	var root [FX25_MAX_CHECK]C.uchar
	var reg [FX25_MAX_CHECK + 1]C.uchar
	var loc [FX25_MAX_CHECK]C.uchar
	var syn_error, count C.int

	/* form the syndromes; i.e., evaluate data(x) at roots of g(x) */
	for i = 0; i < rs.nroots; i++ {
		s[i] = data[0]
	}

	for j = 1; j < NN; j++ {
		for i = 0; i < rs.nroots; i++ {
			if s[i] == 0 {
				s[i] = data[j]
			} else {
				s[i] = data[j] ^ ALPHA_TO[MODNN(INDEX_OF[s[i]]+(FCR+i)*PRIM)]
			}
		}
	}

	/* Convert syndromes to index form, checking for nonzero condition */
	syn_error = 0
	for i = 0; i < rs.nroots; i++ {
		syn_error |= s[i]
		s[i] = INDEX_OF[s[i]]
	}

	// fprintf(stderr,"syn_error = %4x\n",syn_error);
	if !syn_error {
		/* if syndrome is zero, data[] is a codeword and there are no
		 * errors to correct. So return data[] unmodified
		 */
		count = 0
		goto finish
	}
	memset(&lambda[1], 0, rs.nroots*sizeof(lambda[0]))
	lambda[0] = 1

	if no_eras > 0 {
		/* Init lambda to be the erasure locator polynomial */
		lambda[1] = ALPHA_TO[MODNN(PRIM*(NN-1-eras_pos[0]))]
		for i = 1; i < no_eras; i++ {
			u = MODNN(PRIM * (NN - 1 - eras_pos[i]))
			for j = i + 1; j > 0; j-- {
				tmp = INDEX_OF[lambda[j-1]]
				if tmp != A0 {
					lambda[j] ^= ALPHA_TO[MODNN(u+tmp)]
				}

			}
		}

		/* TODO KG
		#if DEBUG >= 1
		    // Test code that verifies the erasure locator polynomial just constructed Needed only for decoder debugging.

		    // find roots of the erasure location polynomial
		    for i=1;i<=no_eras;i++  {
		      reg[i] = INDEX_OF[lambda[i]];
		  }

		    count = 0;
		    for (i = 1,k=IPRIM-1; i <= NN; i++,k = MODNN(k+IPRIM)) {
		      q = 1;
		      for (j = 1; j <= no_eras; j++) {
			if (reg[j] != A0) {
			  reg[j] = MODNN(reg[j] + j);
			  q ^= ALPHA_TO[reg[j]];
			}
		}
		      if (q != 0) {
			continue;
		}
		      // store root and error location number indices
		      root[count] = i;
		      loc[count] = k;
		      count++;
		    }
		    if (count != no_eras) {
		      fprintf(stderr,"count = %d no_eras = %d\n lambda(x) is WRONG\n",count,no_eras);
		      count = -1;
		      goto finish;
		    }
		#if DEBUG >= 2
		    fprintf(stderr,"\n Erasure positions as determined by roots of Eras Loc Poly:\n");
		    for (i = 0; i < count; i++)
		      fprintf(stderr,"%d ", loc[i]);
		    fprintf(stderr,"\n");
		#endif
		#endif
		*/
	}
	for i = 0; i < rs.nroots+1; i++ {
		b[i] = INDEX_OF[lambda[i]]
	}

	/*
	 * Begin Berlekamp-Massey algorithm to determine error+erasure
	 * locator polynomial
	 */
	r = no_eras
	el = no_eras
	for {
		/* r is the step number */
		r++
		if r > rs.nroots {
			break
		}
		/* Compute discrepancy at the r-th step in poly-form */
		discr_r = 0
		for i = 0; i < r; i++ {
			if (lambda[i] != 0) && (s[r-i-1] != A0) {
				discr_r ^= ALPHA_TO[MODNN(INDEX_OF[lambda[i]]+s[r-i-1])]
			}
		}
		discr_r = INDEX_OF[discr_r] /* Index form */
		if discr_r == A0 {
			/* 2 lines below: B(x) <-- x*B(x) */
			memmove(&b[1], b, rs.nroots*sizeof(b[0]))
			b[0] = A0
		} else {
			/* 7 lines below: T(x) <-- lambda(x) - discr_r*x*b(x) */
			t[0] = lambda[0]
			for i = 0; i < rs.nroots; i++ {
				if b[i] != A0 {
					t[i+1] = lambda[i+1] ^ ALPHA_TO[MODNN(discr_r+b[i])]
				} else {
					t[i+1] = lambda[i+1]
				}
			}
			if 2*el <= r+no_eras-1 {
				el = r + no_eras - el
				/*
				 * 2 lines below: B(x) <-- inv(discr_r) *
				 * lambda(x)
				 */
				for i = 0; i <= rs.nroots; i++ {
					b[i] = IfThenElse((lambda[i] == 0), A0, MODNN(INDEX_OF[lambda[i]]-discr_r+NN))
				}
			} else {
				/* 2 lines below: B(x) <-- x*B(x) */
				memmove(&b[1], b, rs.nroots*sizeof(b[0]))
				b[0] = A0
			}
			memcpy(lambda, t, (rs.nroots+1)*sizeof(t[0]))
		}
	}

	/* Convert lambda to index form and compute deg(lambda(x)) */
	deg_lambda = 0
	for i = 0; i < rs.nroots+1; i++ {
		lambda[i] = INDEX_OF[lambda[i]]
		if lambda[i] != A0 {
			deg_lambda = i
		}
	}
	/* Find roots of the error+erasure locator polynomial by Chien search */
	memcpy(&reg[1], &lambda[1], rs.nroots*sizeof(reg[0]))
	count = 0 /* Number of roots of lambda(x) */
	k = IPRIM - 1
	for i = 1; i <= NN; i++ {
		k = MODNN(k + IPRIM)
		q = 1 /* lambda[0] is always 0 */
		for j = deg_lambda; j > 0; j-- {
			if reg[j] != A0 {
				reg[j] = MODNN(reg[j] + j)
				q ^= ALPHA_TO[reg[j]]
			}
		}
		if q != 0 {
			continue /* Not a root */
		}
		/* store root (index-form) and error location number */
		/*
		#if DEBUG>=2
		    fprintf(stderr,"count %d root %d loc %d\n",count,i,k);
		#endif
		*/
		root[count] = i
		loc[count] = k
		/* If we've already found max possible roots,
		 * abort the search to save time
		 */
		count++
		if count == deg_lambda {
			break
		}
	}
	if deg_lambda != count {
		/*
		 * deg(lambda) unequal to number of roots => uncorrectable
		 * error detected
		 */
		count = -1
		goto finish
	}
	/*
	 * Compute err+eras evaluator poly omega(x) = s(x)*lambda(x) (modulo
	 * x**rs.nroots). in index form. Also find deg(omega).
	 */
	deg_omega = 0
	for i = 0; i < rs.nroots; i++ {
		tmp = 0
		j = IfThenElse((deg_lambda < i), deg_lambda, i)
		for ; j >= 0; j-- {
			if (s[i-j] != A0) && (lambda[j] != A0) {
				tmp ^= ALPHA_TO[MODNN(s[i-j]+lambda[j])]
			}
		}
		if tmp != 0 {
			deg_omega = i
		}
		omega[i] = INDEX_OF[tmp]
	}
	omega[rs.nroots] = A0

	/*
	 * Compute error values in poly-form. num1 = omega(inv(X(l))), num2 =
	 * inv(X(l))**(FCR-1) and den = lambda_pr(inv(X(l))) all in poly-form
	 */
	for j = count - 1; j >= 0; j-- {
		num1 = 0
		for i = deg_omega; i >= 0; i-- {
			if omega[i] != A0 {
				num1 ^= ALPHA_TO[MODNN(omega[i]+i*root[j])]
			}
		}
		num2 = ALPHA_TO[MODNN(root[j]*(FCR-1)+NN)]
		den = 0

		/* lambda[i+1] for i even is the formal derivative lambda_pr of lambda[i] */
		for i = min(deg_lambda, rs.nroots-1) & ~1; i >= 0; i -= 2 {
			if lambda[i+1] != A0 {
				den ^= ALPHA_TO[MODNN(lambda[i+1]+i*root[j])]
			}
		}
		if den == 0 {
			/* TODO KG
			#if DEBUG >= 1
			      fprintf(stderr,"\n ERROR: denominator = 0\n");
			#endif
			*/
			count = -1
			goto finish
		}
		/* Apply error to data */
		if num1 != 0 {
			data[loc[j]] ^= ALPHA_TO[MODNN(INDEX_OF[num1]+INDEX_OF[num2]+NN-INDEX_OF[den])]
		}
	}
finish:
	if eras_pos != NULL {
		for i = 0; i < count; i++ {
			eras_pos[i] = loc[i]
		}
	}
	return count
}

// end fx25_extract.c
