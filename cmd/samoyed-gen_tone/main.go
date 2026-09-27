package main

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test program for generating tones.
 *
 *---------------------------------------------------------------*/

import (
	"context"

	"github.com/doismellburning/samoyed/internal/direwolf"
)

const chan1 = 0
const chan2 = 1

// NewGenToneTestConfig uses the default baud rate.
const baud = direwolf.DEFAULT_BAUD

func main() {
	genTone(direwolf.AudioDeviceSink{})
}

// genTone is main, but sending the samples to sink rather than necessarily
// the audio device, so that a test can see what would have been played.
func genTone(sink direwolf.AudioSink) {
	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var config = direwolf.NewGenToneTestConfig(1)

	direwolf.AudioOpen(context.Background(), config)
	direwolf.GenToneInit(config, 100, sink)

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

	config = direwolf.NewGenToneTestConfig(2)

	direwolf.AudioOpen(context.Background(), config)
	direwolf.GenToneInit(config, 100, sink)

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
