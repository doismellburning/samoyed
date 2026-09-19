package direwolf

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CaptureOutput runs command with stdout redirected, and returns what it wrote
// there.  Much of the codebase prints via dw_printf, i.e. straight to stdout,
// so this is how a test gets hold of it.
// Note that any of the Dire Wolf colour formatting totally screws this for reasons I don't yet understand.
// See also what happens if you pipe output to a pager...
func CaptureOutput(t *testing.T, command func()) string {
	t.Helper()

	var oldStdout = os.Stdout

	defer func() {
		os.Stdout = oldStdout
	}()

	var r, w, pipeErr = os.Pipe()

	require.NoError(t, pipeErr)

	os.Stdout = w

	command()

	w.Close()

	os.Stdout = oldStdout

	var outputBytes, readErr = io.ReadAll(r)

	require.NoError(t, readErr)

	return string(outputBytes)
}

// AssertOutputContains runs command and asserts its stdout contains expectedOutputContains.
func AssertOutputContains(t *testing.T, command func(), expectedOutputContains string) {
	t.Helper()

	assert.Contains(t, CaptureOutput(t, command), expectedOutputContains)
}
