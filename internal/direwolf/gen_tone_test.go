// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The configuration samoyed-gen_tone plays its tones with sets up a
// transmitting device, and a tone generator for every channel on it, mono
// and stereo alike.
func TestGenToneTestConfigGeneratesEveryChannel(t *testing.T) {
	for _, numChannels := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d channels", numChannels), func(t *testing.T) {
			var config = NewGenToneTestConfig(numChannels)

			// AudioOpen only sets up an output buffer for a defined device.
			assert.NotZero(t, config.adev[0].defined)

			var sink = new(byteSink)

			var toneGenerators = NewToneGenerators(config, 100, sink)

			var bytesPerFrame = config.adev[0].num_channels * config.adev[0].bits_per_sample / 8

			for channel := range numChannels {
				require.NotNil(t, toneGenerators[channel], "channel %d has no tone generator", channel)
				assert.Same(t, config, toneGenerators[channel].audioConfig)

				sink.data = nil

				// One second of bits is one second of audio.
				for range config.achan[channel].baud {
					toneGenerators[channel].PutBit(1)
				}

				assert.InDelta(t, config.adev[0].samples_per_sec*bytesPerFrame, len(sink.data), float64(bytesPerFrame))
			}
		})
	}
}

// putCountingSink counts the bytes put to it.
type putCountingSink struct {
	puts int
}

func (s *putCountingSink) Put(int, uint8) int {
	s.puts++

	return 0
}

func (s *putCountingSink) Flush(int) int { return 0 }

// A channel the tone generator has no way to modulate for is an internal
// error, which the transmitter is told of once and otherwise carries on past,
// sending nothing.  It runs mid-transmission, with the PTT keyed, so ending
// the process there would leave the transmitter keyed.
func TestPutBitWithUnknownModemSendsNothing(t *testing.T) {
	var sink = new(putCountingSink)
	var audioConfig = newTestRadioConfig(0, MODEM_OFF, 1200, 1200, 2200, 44100)
	var tg = NewToneGenerator(0, audioConfig, 100, sink)

	var output = testutils.CaptureOutput(t, func() {
		tg.PutBit(1)
		tg.PutBit(0)
	})

	assert.Equal(t, 0, sink.puts, "nothing should have been sent")
	assert.Equal(t, 1, strings.Count(output, "INTERNAL ERROR"), "the error should be reported once: %s", output)
}
