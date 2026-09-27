package main

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test program for generating tones.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"

	"github.com/doismellburning/samoyed/internal/direwolf"
)

const chan1 = 0
const chan2 = 1

// No baud rate is configured, so AudioOpen fills in the default.
const baud = direwolf.DEFAULT_BAUD

func main() {
	fmt.Println("Warning, known to fail with an assertion error, needs debugging and fixing.")

	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var config = direwolf.NewGenToneTestConfig(1, true)

	direwolf.AudioOpen(context.Background(), config)
	direwolf.GenToneInit(config, 100, direwolf.AudioDeviceSink{})

	for range 2 {
		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 1)
		}

		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 0)
		}
	}

	direwolf.AudioClose()

	/* Now try stereo. */

	config = direwolf.NewGenToneTestConfig(2, false)

	direwolf.AudioOpen(context.Background(), config)
	direwolf.GenToneInit(config, 100, direwolf.AudioDeviceSink{})

	for range 4 {
		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 1)
		}

		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 0)
		}

		for range baud * 2 {
			direwolf.ToneGenPutBit(chan2, 1)
		}

		for range baud * 2 {
			direwolf.ToneGenPutBit(chan2, 0)
		}
	}

	direwolf.AudioClose()
}
