// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What SendDTMF puts on the air, a decoder on the same channel reads back.
func TestSendDTMFDecodesBack(t *testing.T) {
	const channel = 0
	const sampleRate = 8000

	var audioConfig = new(RadioConfig)
	audioConfig.adev[0].num_channels = 1
	audioConfig.adev[0].bits_per_sample = 16
	audioConfig.adev[0].samples_per_sec = sampleRate

	var sink = new(byteSink)
	var tg = NewToneGenerator(channel, audioConfig, 50, sink)

	tg.SendDTMF("159D*#", 10, 300, 250)

	assert.Equal(t, 1, sink.flushes, "the tones should be flushed out once, at the end")
	require.Zero(t, len(sink.data)%2, "16 bit samples come in pairs of bytes")

	var decoder = NewDTMFDecoder(channel, sampleRate, nil)

	var heard strings.Builder

	for i := 0; i < len(sink.data); i += 2 {
		var sam = int16(binary.LittleEndian.Uint16(sink.data[i:])) //nolint:gosec // G115: unchecked narrowing conversion, see #294

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

// Hearing a button raises the channel's DCD, as subchannel MAX_SUBCHANS so it
// can't be mistaken for a demodulator's, and it drops again once the button
// is let go.
func TestDTMFDecoderSetsDCD(t *testing.T) {
	const channel = 1
	const sampleRate = 44100

	var dcd []string

	var decoder = NewDTMFDecoder(channel, sampleRate, func(c int, subchannel int, slice int, state int) {
		var entry = fmt.Sprintf("%d/%d/%d=%d", c, subchannel, slice, state)
		if len(dcd) == 0 || dcd[len(dcd)-1] != entry {
			dcd = append(dcd, entry)
		}
	})

	for sample := range dtmfButtonSamples('5', 100, sampleRate) {
		decoder.Sample(sample)
	}

	for range sampleRate / 10 {
		decoder.Sample(0)
	}

	var on = fmt.Sprintf("%d/%d/0=1", channel, MAX_SUBCHANS)
	var off = fmt.Sprintf("%d/%d/0=0", channel, MAX_SUBCHANS)

	require.Contains(t, dcd, on, "hearing the button should raise DCD")
	assert.Equal(t, off, dcd[len(dcd)-1], "letting it go should drop DCD again")
}
