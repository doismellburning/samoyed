// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"

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
