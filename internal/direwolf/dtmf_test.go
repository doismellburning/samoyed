// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// What SendDTMF puts on the air, a decoder on the same channel reads back.
func TestSendDTMFDecodesBack(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var audioConfig = new(AudioConfig)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate

	var sink = new(byteSink)
	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	tg.SendDTMF("159D*#", 10, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")
	require.Zero(t, len(sink.data)%2, "16 bit samples come in pairs of bytes")

	var decoder = NewDTMFDecoder(sampleRate, nil)

	var heard strings.Builder

	for i := 0; i < len(sink.data); i += 2 {
		var sam = int16(binary.LittleEndian.Uint16(sink.data[i:]))

		var x = decoder.Sample(float64(sam) / 16384.)
		if x != ' ' && x != '.' {
			heard.WriteRune(x)
		}
	}

	assert.Equal(t, "159D*#", heard.String())
}

// A channel with no tone generator still says how long the transmission would
// have been, as xmit_thread holds the PTT for that long.
func TestDTMFSendWithoutToneGenerator(t *testing.T) {
	assert.Equal(t, 300+400+250, dtmf_send(nil, 0, "1234", 10, 300, 250))
}

// The DCD callback hears a button come on while it is held, and go off again
// once it is released.
func TestDTMFDecoderReportsDCD(t *testing.T) {
	const sampleRate = 8000

	var states []bool

	var decoder = NewDTMFDecoder(sampleRate, func(on bool) {
		// Only the changes are interesting here.
		if len(states) == 0 || states[len(states)-1] != on {
			states = append(states, on)
		}
	})

	for _, button := range []rune{' ', '5', ' '} {
		for sample := range dtmfButtonSamples(button, 100, sampleRate) {
			decoder.Sample(sample)
		}
	}

	assert.Equal(t, []bool{false, true, false}, states)
}
