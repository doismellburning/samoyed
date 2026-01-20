package direwolf

// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include <stdint.h>
// #include <stdlib.h>
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

func FxrecMain() {
	FXTEST = true

	fx25_init(3)

	var i C.int
	for i = CTAG_MIN; i <= CTAG_MAX; i++ {
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
				fx25_rec_bit(0, 0, 0, C.int(ch&imask))
			}
		}
		C.fclose(fp)
	}

	if fx25_test_count == 11 {
		fmt.Printf("***** FX25 unit test Success - all tests passed. *****\n")
		return
	}
	fmt.Printf("***** FX25 unit test FAILED.  Only %d/11 tests passed. *****\n", fx25_test_count)
	os.Exit(1)
}
