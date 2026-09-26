package direwolf

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

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

func morseToFile(t *testing.T, filename string, message string) {
	t.Helper()

	// Copied from gen_packets without using all the CLI parsing...

	var modem audio_s
	modem.adev[0].defined = 1
	modem.adev[0].num_channels = DEFAULT_NUM_CHANNELS
	modem.adev[0].samples_per_sec = morseSamplesPerSec

	modem.adev[0].bits_per_sample = DEFAULT_BITS_PER_SAMPLE
	for channel := range MAX_RADIO_CHANS {
		modem.achan[channel].modem_type = MODEM_AFSK
		modem.achan[channel].mark_freq = DEFAULT_MARK_FREQ
		modem.achan[channel].space_freq = DEFAULT_SPACE_FREQ
		modem.achan[channel].baud = DEFAULT_BAUD
	}

	modem.chan_medium[0] = MEDIUM_RADIO

	var sink, err = audio_file_open(filename, &modem)
	require.NoError(t, err)

	var amplitude = 100
	gen_tone_init(&modem, amplitude, sink)
	morse_init(&modem, amplitude)
	morse_send(0, message, morseWPM, 100, 100)
	require.NoError(t, audio_file_close(sink)) // I just realised this all works on globals :s
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
