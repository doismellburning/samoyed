// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleRecorder is a dtmfSampleSink that keeps what it is given, and counts
// flushes.
type sampleRecorder struct {
	samples []int
	flushes int
}

func (r *sampleRecorder) PutSample(sam int) {
	r.samples = append(r.samples, sam)
}

func (r *sampleRecorder) Flush() {
	r.flushes++
}

// What dtmfSend puts on the air, a decoder on the same channel reads back.
func TestDTMFSendSamplesDecodeBack(t *testing.T) {
	const sampleRate = 8000

	var out = new(sampleRecorder)

	dtmfSend(out, sampleRate, 50, "159D*#", 10, 300, 250)

	assert.Equal(t, 1, out.flushes, "the tones should be flushed out once, at the end")

	var decoder = NewDTMFDecoder(0, sampleRate, nil)

	var heard strings.Builder

	for _, sam := range out.samples {
		var x = decoder.Sample(float64(sam) / 16384.)
		if x != ' ' && x != '.' {
			heard.WriteRune(x)
		}
	}

	assert.Equal(t, "159D*#", heard.String())
}

// The PTT is held for the delays either side and half a tone period of tone
// and of quiet per button.
func TestDTMFDuration(t *testing.T) {
	assert.Equal(t, 300+400+250, dtmfDuration("1234", 10, 300, 250))
}

// A channel with no tone generator still says how long the transmission would
// have been, as xmit_thread holds the PTT for that long.
func TestDTMFSendWithoutToneGenerator(t *testing.T) {
	assert.Equal(t, 300+400+250, dtmf_send(nil, 0, "1234", 10, 300, 250))
}

// What dtmf_send puts through a channel's tone generator, a decoder on the
// same channel reads back.
func TestDTMFSendDecodesBack(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var audioConfig = new(RadioConfig)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate

	var sink = new(byteSink)
	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	dtmf_send(tg, channel, "159D*#", 10, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")
	require.Zero(t, len(sink.data)%2, "16 bit samples come in pairs of bytes")

	var decoder = NewDTMFDecoder(channel, sampleRate, nil)

	var heard strings.Builder

	for i := 0; i < len(sink.data); i += 2 {
		var sam int16

		var _, err = binary.Decode(sink.data[i:i+2], binary.LittleEndian, &sam)
		require.NoError(t, err)

		var x = decoder.Sample(float64(sam) / 16384.)
		if x != ' ' && x != '.' {
			heard.WriteRune(x)
		}
	}

	assert.Equal(t, "159D*#", heard.String())
}
