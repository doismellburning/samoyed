// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/wav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// morseWPM and morseSamplesPerSec are deliberately not the Dire Wolf defaults
// (10 WPM, DEFAULT_SAMPLES_PER_SEC). Both only affect how long the generated
// .WAV is, and both generating and decoding it cost time proportional to that
// length, so the defaults made this test spend all its time on audio nobody
// looks at. Sending faster into a lower sample rate shrinks the file ~8x
// (4x from the speed, 2x from the rate, less the fixed txdelay/txtail) and
// morse2ascii still decodes it comfortably.
const morseWPM = 40
const morseSamplesPerSec = 22050

// wavMorseSink is a morseSampleSink that writes 16 bit mono samples to a .WAV
// file, keeping the first error it meets.
type wavMorseSink struct {
	w          *wav.Writer
	sampleRate int
	err        error
}

func (s *wavMorseSink) PutSample(sam int) {
	if s.err == nil {
		s.err = s.w.WriteByte(byte(sam & 0xff))
	}

	if s.err == nil {
		s.err = s.w.WriteByte(byte((sam >> 8) & 0xff))
	}
}

func (s *wavMorseSink) PutQuietMs(ms int) {
	for range int((float64(ms) * float64(s.sampleRate) / 1000.) + 0.5) {
		s.PutSample(0)
	}
}

func (s *wavMorseSink) Flush() {}

func morseToFile(t *testing.T, filename string, message string) {
	t.Helper()

	var w, err = wav.Create(filename, wav.Format{
		NumChannels:   1,
		SamplesPerSec: morseSamplesPerSec,
		BitsPerSample: 16,
	})
	require.NoError(t, err)

	var sink = new(wavMorseSink)
	sink.w = w
	sink.sampleRate = morseSamplesPerSec

	var amplitude = 100
	morseSend(sink, morseSamplesPerSec, amplitude, message, morseWPM, 100, 100)
	require.NoError(t, sink.err)
	require.NoError(t, w.Close())
}

// gen_packets will generate Morse, so let's test it and try to decode
func Test_Morse_Generate_Decode(t *testing.T) {
	// First, generate
	var tmpdir = t.TempDir()

	var f = filepath.Join(tmpdir, "morse.wav")

	var message = "WB2OSZ-15>TEST:,The quick brown fox jumps over the lazy dog!  1 of 1"

	morseToFile(t, f, message)

	// Make sure that worked!

	assert.FileExists(t, f)

	// Now decode

	var cmd = exec.CommandContext(context.Background(), "morse2ascii", f) //nolint:gosec

	var output, outputErr = cmd.Output()

	require.NoError(t, outputErr)

	var outputStr = string(output)

	// morse2ascii doesn't like spaces, so we'll just check for individual strings
	assert.Contains(t, outputStr, "wb2osz")
	assert.Contains(t, outputStr, "15")
	assert.Contains(t, outputStr, "test")
	assert.Contains(t, outputStr, "the")
	assert.Contains(t, outputStr, "quick")
	assert.Contains(t, outputStr, "brown")
	assert.Contains(t, outputStr, "fox")
}
