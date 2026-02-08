package direwolf

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func FxrecMain() {
	FXTEST = true

	fx25_init(3)

	for i := CTAG_MIN; i <= CTAG_MAX; i++ {
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
				fmt.Printf("Error reading %s: %s.\n", fname, err)
				break
			}
			var ch = buf[0]
			var imask byte
			for imask = 0x01; imask != 0; imask <<= 1 {
				fx25_rec_bit(0, 0, 0, int(ch&imask))
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}

		fp.Close()
	}

	if fx25_test_count == 11 {
		fmt.Printf("***** FX25 unit test Success - all tests passed. *****\n")
		return
	}
	fmt.Printf("***** FX25 unit test FAILED.  Only %d/11 tests passed. *****\n", fx25_test_count)
	os.Exit(1)
}
