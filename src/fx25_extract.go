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

import (
	"unsafe"
)

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
	for i = 0; C.uint(i) < rs.nroots; i++ {
		s[i] = *data
	}

	for j = 1; C.uint(j) < rs.nn; j++ {
		for i = 0; C.uint(i) < rs.nroots; i++ {
			var data_j = *(*C.uchar)(unsafe.Add(unsafe.Pointer(data), j))
			if s[i] == 0 {
				s[i] = data_j
			} else {
				var rs_index_of_val = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.index_of), s[i]))
				var rs_alpha_to_val = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.alpha_to), modnn(rs, C.int(rs_index_of_val)+(C.int(rs.fcr)+i)*C.int(rs.prim))))
				s[i] = data_j ^ rs_alpha_to_val
			}
		}
	}

	/* Convert syndromes to index form, checking for nonzero condition */
	syn_error = 0
	for i = 0; C.uint(i) < rs.nroots; i++ {
		syn_error |= C.int(s[i])
		s[i] = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.index_of), s[i]))
	}

	// fprintf(stderr,"syn_error = %4x\n",syn_error);
	if syn_error != 0 {
		/* if syndrome is zero, data[] is a codeword and there are no
		 * errors to correct. So return data[] unmodified
		 */
		count = 0
		goto finish
	}
	C.memset(unsafe.Pointer(&lambda[1]), 0, C.size_t(rs.nroots*C.sizeof_uchar))
	lambda[0] = 1

	if no_eras > 0 {
		/* Init lambda to be the erasure locator polynomial */
		var rs_alpha_to_val = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.alpha_to), modnn(rs, rs.prim*(C.int(rs.nn)-1-*eras_pos))))
		lambda[1] = rs_alpha_to_val
		for i = 1; i < no_eras; i++ {
			var eras_pos_val = *(*C.int)(unsafe.Add(unsafe.Pointer(eras_pos), i))
			u = C.uchar(modnn(rs, rs.prim*(C.int(rs.nn)-1-eras_pos_val)))
			for j = i + 1; j > 0; j-- {
				var rs_index_of_val = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.index_of), lambda[j-1]))
				tmp = rs.index_of[lambda[j-1]]
				if tmp != rs.nn {
					lambda[j] ^= rs.alpha_to[modnn(rs, u+tmp)]
				}

			}
		}

		/* TODO KG
		#if DEBUG >= 1
		    // Test code that verifies the erasure locator polynomial just constructed Needed only for decoder debugging.

		    // find roots of the erasure location polynomial
		    for i=1;i<=no_eras;i++  {
		      reg[i] = rs.index_of[lambda[i]];
		  }

		    count = 0;
		    for (i = 1,k=rs.iprim-1; i <= rs.nn; i++,k = modnn(rs,k+rs.iprim)) {
		      q = 1;
		      for (j = 1; j <= no_eras; j++) {
			if (reg[j] != rs.nn) {
			  reg[j] = modnn(rs,reg[j] + j);
			  q ^= rs.alpha_to[reg[j]];
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
		var rs_index_of_val = *(*C.uchar)(unsafe.Add(unsafe.Pointer(rs.index_of), lambda[i]))
		b[i] = rs_index_of_val
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
		if r > C.int(rs.nroots) {
			break
		}
		/* Compute discrepancy at the r-th step in poly-form */
		discr_r = 0
		for i = 0; i < r; i++ {
			if (lambda[i] != 0) && (C.uint(s[r-i-1]) != rs.nn) {
				discr_r ^= rs.alpha_to[modnn(rs, rs.index_of[lambda[i]]+s[r-i-1])]
			}
		}
		discr_r = rs.index_of[discr_r] /* Index form */
		if discr_r == rs.nn {
			/* 2 lines below: B(x) <-- x*B(x) */
			memmove(&b[1], b, rs.nroots*sizeof(b[0]))
			b[0] = rs.nn
		} else {
			/* 7 lines below: T(x) <-- lambda(x) - discr_r*x*b(x) */
			t[0] = lambda[0]
			for i = 0; i < rs.nroots; i++ {
				if b[i] != rs.nn {
					t[i+1] = lambda[i+1] ^ rs.alpha_to[modnn(rs, discr_r+b[i])]
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
					b[i] = IfThenElse((lambda[i] == 0), rs.nn, modnn(rs, rs.index_of[lambda[i]]-discr_r+rs.nn))
				}
			} else {
				/* 2 lines below: B(x) <-- x*B(x) */
				memmove(&b[1], b, rs.nroots*sizeof(b[0]))
				b[0] = rs.nn
			}
			memcpy(lambda, t, (rs.nroots+1)*sizeof(t[0]))
		}
	}

	/* Convert lambda to index form and compute deg(lambda(x)) */
	deg_lambda = 0
	for i = 0; i < rs.nroots+1; i++ {
		lambda[i] = rs.index_of[lambda[i]]
		if lambda[i] != rs.nn {
			deg_lambda = i
		}
	}
	/* Find roots of the error+erasure locator polynomial by Chien search */
	memcpy(&reg[1], &lambda[1], rs.nroots*sizeof(reg[0]))
	count = 0 /* Number of roots of lambda(x) */
	k = rs.iprim - 1
	for i = 1; i <= rs.nn; i++ {
		k = modnn(rs, k+rs.iprim)
		q = 1 /* lambda[0] is always 0 */
		for j = deg_lambda; j > 0; j-- {
			if reg[j] != rs.nn {
				reg[j] = modnn(rs, reg[j]+j)
				q ^= rs.alpha_to[reg[j]]
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
			if (s[i-j] != rs.nn) && (lambda[j] != rs.nn) {
				tmp ^= rs.alpha_to[modnn(rs, s[i-j]+lambda[j])]
			}
		}
		if tmp != 0 {
			deg_omega = i
		}
		omega[i] = rs.index_of[tmp]
	}
	omega[rs.nroots] = rs.nn

	/*
	 * Compute error values in poly-form. num1 = omega(inv(X(l))), num2 =
	 * inv(X(l))**(rs.fcr-1) and den = lambda_pr(inv(X(l))) all in poly-form
	 */
	for j = count - 1; j >= 0; j-- {
		num1 = 0
		for i = deg_omega; i >= 0; i-- {
			if omega[i] != rs.nn {
				num1 ^= rs.alpha_to[modnn(rs, omega[i]+i*root[j])]
			}
		}
		num2 = rs.alpha_to[modnn(rs, root[j]*(rs.fcr-1)+rs.nn)]
		den = 0

		/* lambda[i+1] for i even is the formal derivative lambda_pr of lambda[i] */
		for i = min(deg_lambda, rs.nroots-1) & ~1; i >= 0; i -= 2 {
			if lambda[i+1] != rs.nn {
				den ^= rs.alpha_to[modnn(rs, lambda[i+1]+i*root[j])]
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
			data[loc[j]] ^= rs.alpha_to[modnn(rs, rs.index_of[num1]+rs.index_of[num2]+rs.nn-rs.index_of[den])]
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

func modnn(rs *C.struct_rs, x C.int) C.int {
	for x >= rs.nn {
		x -= rs.nn
		x = (x >> rs.mm) + (x & rs.nn)
	}

	return x
}
