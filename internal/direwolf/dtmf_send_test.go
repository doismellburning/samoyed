// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
