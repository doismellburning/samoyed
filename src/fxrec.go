package direwolf

import (
	"fmt"
	"os"
	"unsafe"
)

func FxrecMain() {
	FXTEST = true

	fx25_init(3)

	for i := CTAG_MIN; i <= CTAG_MAX; i++ {
		var fname = fmt.Sprintf("fx%02x.dat", i)
		var fp, err = os.Open(fname)
		if err != nil {
			fmt.Printf("****** Could not open %s: %s ******\n", fname, err)
			fmt.Printf("****** Did you generate the test files first? ******\n")
			os.Exit(1)
		}
		defer fp.Close()

		var ch = make([]byte, 1)
		for {
			var n, readErr = fp.Read(ch)
			if n == 0 && readErr = io.EOF {
				break
			}

			if readErr != nil {
				panic(readErr)
			}

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
