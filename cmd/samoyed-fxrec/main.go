package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	direwolf "github.com/doismellburning/samoyed/src"
)

func main() {
	direwolf.FXTEST = true

	direwolf.FX25Init(3)

	for i := direwolf.CTAG_MIN; i <= direwolf.CTAG_MAX; i++ {
		var fname = fmt.Sprintf("fx%02x.dat", i)

		var fp, err = os.Open(fname) //nolint:gosec
		if err != nil {
			fmt.Printf("****** Could not open %s: %s ******\n", fname, err)
			fmt.Printf("****** Did you generate the test files first? ******\n")
			os.Exit(1)
		}

		var buf = make([]byte, 1)
		for {
			var _, readErr = fp.Read(buf)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				fmt.Printf("Error reading %s: %s.\n", fname, readErr)
				break
			}
			var ch = buf[0]

			var imask byte
			for imask = 0x01; imask != 0; imask <<= 1 {
				direwolf.FX25RecBit(0, 0, 0, int(ch&imask))
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}

		fp.Close()
	}

	if direwolf.FX25TestCount == 11 {
		fmt.Printf("***** FX25 unit test Success - all tests passed. *****\n")
		return
	}

	fmt.Printf("***** FX25 unit test FAILED.  Only %d/11 tests passed. *****\n", direwolf.FX25TestCount)
	os.Exit(1)
}
