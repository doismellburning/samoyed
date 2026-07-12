//nolint:gochecknoglobals
package main

import (
	"fmt"

	direwolf "github.com/doismellburning/samoyed/src"
)

var preload = []byte{
	'T' << 1, 'E' << 1, 'S' << 1, 'T' << 1, ' ' << 1, ' ' << 1, 0x60,
	'W' << 1, 'B' << 1, '2' << 1, 'O' << 1, 'S' << 1, 'Z' << 1, 0x63,
	0x03, 0xf0,
	'F', 'o', 'o', '?', 'B', 'a', 'r', '?', //  '?' causes bit stuffing
}

func main() {
	fmt.Println("fxsend - FX.25 unit test.")
	fmt.Println("This generates 11 files named fx01.dat, fx02.dat, ..., fx0b.dat")
	fmt.Println("Run fxrec as second part of test.")

	direwolf.FX25Init(3)

	for i := 100 + direwolf.CTAG_MIN; i <= 100+direwolf.CTAG_MAX; i++ {
		direwolf.FX25SendFrame(0, preload, i, true)
	}
}
