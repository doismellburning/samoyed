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
