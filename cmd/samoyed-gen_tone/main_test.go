// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/binary"
	"testing"

	"github.com/doismellburning/samoyed/internal/direwolf"
	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink is an AudioSink that keeps what it is given, standing in
// for the soundcard.
type recordingSink struct {
	data []byte
}

func (s *recordingSink) Put(a int, c uint8) int {
	if a == 0 {
		s.data = append(s.data, c)
	}

	return 0
}

func (s *recordingSink) Flush(int) int {
	return 0
}

// risingZeroCrossings counts the times a run of 16 bit little endian samples
// goes from negative to non-negative, which for a steady tone is its
// frequency times its duration.
func risingZeroCrossings(data []byte) int {
	var crossings = 0

	var previous = int16(binary.LittleEndian.Uint16(data)) //nolint:gosec // G115: unchecked narrowing conversion, see #294

	for i := 2; i+1 < len(data); i += 2 {
		var sample = int16(binary.LittleEndian.Uint16(data[i:])) //nolint:gosec // G115: unchecked narrowing conversion, see #294

		if previous < 0 && sample >= 0 {
			crossings++
		}

		previous = sample
	}

	return crossings
}

func Test_genTone(t *testing.T) {
	var sink = new(recordingSink)

	var output = testutils.CaptureOutput(t, func() {
		genTone(func(*direwolf.RadioConfig) (direwolf.AudioSink, func()) {
			return sink, func() {}
		})
	})

	// Every channel of the stereo run has a tone generator to put bits to.
	assert.NotContains(t, output, "Invalid channel")

	// The mono part comes first: 16 bit samples at the default rate, two
	// seconds of each tone, twice.
	const bytesPerSample = 2

	var secondsBytes = direwolf.DEFAULT_SAMPLES_PER_SEC * bytesPerSample * 2

	require.GreaterOrEqual(t, len(sink.data), 4*secondsBytes)

	for i, want := range []int{
		direwolf.DEFAULT_MARK_FREQ,  // A 1 bit is mark...
		direwolf.DEFAULT_SPACE_FREQ, // ...and a 0 bit is space.
		direwolf.DEFAULT_MARK_FREQ,
		direwolf.DEFAULT_SPACE_FREQ,
	} {
		var segment = sink.data[i*secondsBytes : (i+1)*secondsBytes]

		// Two seconds of the tone, give or take a cycle at either end.
		assert.InDelta(t, 2*want, risingZeroCrossings(segment), 2, "segment %d", i)
	}
}
