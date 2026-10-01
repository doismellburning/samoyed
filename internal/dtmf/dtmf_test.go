// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dtmf

import (
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
		for sample := range ButtonSamples(button, 100, sampleRate) {
			decoder.Sample(sample)
		}
	}

	assert.Equal(t, []bool{false, true, false}, states)
}
