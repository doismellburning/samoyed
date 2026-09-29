package main

/*------------------------------------------------------------------
 *
 * Purpose:     Quick test program for generating tones.
 *
 *---------------------------------------------------------------*/

import (
	"context"
	"fmt"
	"os"

	"github.com/doismellburning/samoyed/internal/direwolf"
)

const chan1 = 0
const chan2 = 1

// NewGenToneTestConfig uses the default baud rate.
const baud = direwolf.DEFAULT_BAUD

func main() {
	// Play the samples through the audio device.
	genTone(func(config *direwolf.AudioConfig) (direwolf.AudioSink, func()) {
		var devices, err = direwolf.AudioOpen(context.Background(), config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not open the audio device: %v\n", err)
			os.Exit(1)
		}

		return devices, devices.Close
	})
}

// openSink opens somewhere to send the samples for config, returning it and
// what closes it again.
type openSink func(config *direwolf.AudioConfig) (direwolf.AudioSink, func())

// genTone is main, but sending the samples wherever open says rather than
// necessarily the audio device, so that a test can see what would have been
// played.
func genTone(open openSink) {
	/* to sound card */
	/* one channel.  2 times:  one second of each tone. */

	var config = direwolf.NewGenToneTestConfig(1)

	var sink, closeSink = open(config)
	direwolf.GenToneInit(config, 100, sink)

	for range 2 {
		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 1)
		}

		for range baud * 2 {
			direwolf.ToneGenPutBit(chan1, 0)
		}
	}

	closeSink()

	/* Now try stereo. */

	config = direwolf.NewGenToneTestConfig(2)

	sink, closeSink = open(config)
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

	closeSink()
}
