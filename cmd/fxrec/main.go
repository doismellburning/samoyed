package main

// #include "direwolf.h"
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include <stdint.h>
// #include <stdlib.h>
// #include "fx25.h"
// #include "fcs_calc.h"
// #include "textcolor.h"
// #include "multi_modem.h"
// #include "demod.h"
// extern int FXTEST;
// extern int fx25_test_count;
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0
import "C"

import (
	"fmt"
	"os"
	"unsafe"

	_ "github.com/doismellburning/samoyed/src" // cgo
)

/* FIXME KG
struct fx_context_s {

	enum { FX_TAG=0, FX_DATA, FX_CHECK } state;
	uint64_t accum;		// Accumulate bits for matching to correlation tag.
	int ctag_num;		// Correlation tag number, CTAG_MIN to CTAG_MAX if approx. match found.
	int k_data_radio;	// Expected size of "data" sent over radio.
	int coffs;		// Starting offset of the check part.
	int nroots;		// Expected number of check bytes.
	int dlen;		// Accumulated length in "data" below.
	int clen;		// Accumulated length in "check" below.
	unsigned char imask;	// Mask for storing a bit.
	unsigned char block[FX25_BLOCK_SIZE+1];
};

static struct fx_context_s *fx_context[MAX_RADIO_CHANS][MAX_SUBCHANS][MAX_SLICERS];

static void process_rs_block (int chan, int subchan, int slice, struct fx_context_s *F);

static int my_unstuff (int chan, int subchan, int slice, unsigned char * restrict pin, int ilen, unsigned char * restrict frame_buf);

static int fx25_test_count = 0;
*/

func main() {
	C.FXTEST = 1

	C.fx25_init(3)

	var i C.int
	for i = C.CTAG_MIN; i <= C.CTAG_MAX; i++ {
		var fname = fmt.Sprintf("fx%02x.dat", i)
		var fp = C.fopen(C.CString(fname), C.CString("rb"))
		if fp == nil {
			fmt.Printf("****** Could not open %s ******\n", fname)
			fmt.Printf("****** Did you generate the test files first? ******\n")
			os.Exit(1)
		}

		var ch C.uchar
		for C.fread(unsafe.Pointer(&ch), 1, 1, fp) == 1 {
			var imask C.uchar
			for imask = 0x01; imask != 0; imask <<= 1 {
				C.fx25_rec_bit(0, 0, 0, C.int(ch&imask))
			}
		}
		C.fclose(fp)
	}

	if C.fx25_test_count == 11 {
		fmt.Printf("***** FX25 unit test Success - all tests passed. *****\n")
		return
	}
	fmt.Printf("***** FX25 unit test FAILED.  Only %d/11 tests passed. *****\n", C.fx25_test_count)
	os.Exit(1)
}
