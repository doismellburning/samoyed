package direwolf

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	var cmd = exec.Command("morse2ascii", f) //nolint:gosec

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
