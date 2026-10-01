// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dtmf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The DCD callback hears a button come on while it is held, and go off again
// once it is released.
func TestDecoderReportsDCD(t *testing.T) {
	const sampleRate = 8000

	var states []bool

	var decoder = NewDecoder(sampleRate, func(on bool) {
		// Only the changes are interesting here.
		if len(states) == 0 || states[len(states)-1] != on {
			states = append(states, on)
		}
	})

	for _, button := range []rune{' ', '5', ' '} {
		for sample := range buttonSamples(button, 100, sampleRate) {
			decoder.Sample(sample)
		}
	}

	assert.Equal(t, []bool{false, true, false}, states)
}

// levelOutput is an Output that keeps the levels it is given, and counts
// flushes.
type levelOutput struct {
	sampleRate int
	levels     []float64
	flushes    int
}

func (o *levelOutput) SampleRate() int { return o.sampleRate }

func (o *levelOutput) PutLevel(level float64) { o.levels = append(o.levels, level) }

func (o *levelOutput) Flush() { o.flushes++ }

// What a Sender sends decodes back to the text it was given, flushed out once
// at the end, and lasts as long as Duration says.
func TestSenderDecodesBack(t *testing.T) {
	const sampleRate = 8000
	const str = "159D*#"
	const speed, txdelay, txtail = 10, 300, 250

	var output = new(levelOutput)
	output.sampleRate = sampleRate

	NewSender(output).Send(str, speed, txdelay, txtail)

	assert.Equal(t, 1, output.flushes, "the tones should be flushed out once, at the end")

	var decoder = NewDecoder(sampleRate, nil)

	var heard strings.Builder

	for _, level := range output.levels {
		var event, button = decoder.Sample(level)
		if event == Pressed {
			heard.WriteRune(button)
		}
	}

	assert.Equal(t, str, heard.String())

	// Each button and gap is rounded to a whole millisecond, then to a whole
	// sample, so the total can fall short of Duration by a little.
	var expected = Duration(str, speed, txdelay, txtail) * sampleRate / 1000
	assert.InDelta(t, expected, len(output.levels), float64(2*len(str)*sampleRate/1000))
}
