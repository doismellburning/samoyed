// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
