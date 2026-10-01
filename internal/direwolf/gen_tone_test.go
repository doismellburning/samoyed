// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/dtmf"
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

// byteSink is an AudioSink that keeps what it is given, and counts flushes.
type byteSink struct {
	data    []byte
	flushes int
}

func (s *byteSink) Put(_ int, c uint8) int {
	s.data = append(s.data, c)

	return 0
}

func (s *byteSink) Flush(int) int {
	s.flushes++

	return 0
}

// What a DTMF sender puts on the air through a tone generator, a decoder on
// the same channel reads back.
func TestToneGeneratorDTMFDecodesBack(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var audioConfig = new(AudioConfig)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate

	var sink = new(byteSink)
	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	dtmf.NewSender(tg).Send("159D*#", 10, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")
	require.Zero(t, len(sink.data)%2, "16 bit samples come in pairs of bytes")

	var decoder = dtmf.NewDecoder(sampleRate, nil)

	var heard strings.Builder

	for i := 0; i < len(sink.data); i += 2 {
		var sam = int16(binary.LittleEndian.Uint16(sink.data[i:]))

		var event, button = decoder.Sample(float64(sam) / 16384.)
		if event == dtmf.Pressed {
			heard.WriteRune(button)
		}
	}

	assert.Equal(t, "159D*#", heard.String())
}
