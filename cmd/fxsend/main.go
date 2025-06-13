package main

// #include "direwolf.h"
// #include <stdlib.h>
// #include <stdio.h>
// #include <assert.h>
// #include <string.h>
// #include "fx25.h"
// #include "fcs_calc.h"
// #include "textcolor.h"
// #include "audio.h"
// #include "gen_tone.h"
// #cgo CFLAGS: -I../../src -DMAJOR_VERSION=0 -DMINOR_VERSION=0
import "C"

import (
	"fmt"

	_ "github.com/doismellburning/samoyed/src" // cgo
)

var preload []C.uchar = []C.uchar{
	'T' << 1, 'E' << 1, 'S' << 1, 'T' << 1, ' ' << 1, ' ' << 1, 0x60,
	'W' << 1, 'B' << 1, '2' << 1, 'O' << 1, 'S' << 1, 'Z' << 1, 0x63,
	0x03, 0xf0,
	'F', 'o', 'o', '?', 'B', 'a', 'r', '?', //  '?' causes bit stuffing
	0, 0, 0, // Room for FCS + extra
}

func main() {
	fmt.Println("fxsend - FX.25 unit test.")
	fmt.Println("This generates 11 files named fx01.dat, fx02.dat, ..., fx0b.dat")
	fmt.Println("Run fxrec as second part of test.")

	C.fx25_init(3)

	var i C.int
	for i = 100 + C.CTAG_MIN; i <= 100+C.CTAG_MAX; i++ {
		C.fx25_send_frame(0, &preload[0], C.int(len(preload)-3), i, 1)
	}
}
