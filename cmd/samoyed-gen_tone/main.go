package main

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test program for generating tones.
 *
 *---------------------------------------------------------------*/

import (
	"fmt"

	direwolf "github.com/doismellburning/samoyed/src"
)

const chan1 = 0
const chan2 = 1

func main() {
	fmt.Println("Warning, known to fail with an assertion error, needs debugging and fixing.")

	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var baud = direwolf.GenToneTestOpen(1, true)

	for range 2 {
		for range baud[chan1] * 2 {
			direwolf.GenToneTestPutBit(chan1, 1)
		}

		for range baud[chan1] * 2 {
			direwolf.GenToneTestPutBit(chan1, 0)
		}
	}

	direwolf.GenToneTestClose()

	/* Now try stereo. */

	baud = direwolf.GenToneTestOpen(2, false)

	for range 4 {
		for range baud[chan1] * 2 {
			direwolf.GenToneTestPutBit(chan1, 1)
		}

		for range baud[chan1] * 2 {
			direwolf.GenToneTestPutBit(chan1, 0)
		}

		for range baud[chan2] * 2 {
			direwolf.GenToneTestPutBit(chan2, 1)
		}

		for range baud[chan2] * 2 {
			direwolf.GenToneTestPutBit(chan2, 0)
		}
	}

	direwolf.GenToneTestClose()
}
