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

	defer r.Close()

	os.Stdout = w

	// Drain the pipe while the command is still filling it.  A pipe holds only
	// so much - 64 KiB on Linux - so a command that writes more than that
	// blocks forever if nothing is reading until it returns.
	type captured struct {
		output string
		err    error
	}

	var done = make(chan captured, 1)

	go func() {
		var outputBytes, readErr = io.ReadAll(r)

		done <- captured{output: string(outputBytes), err: readErr}
	}()

	command()

	w.Close()

	os.Stdout = oldStdout

	var result = <-done

	require.NoError(t, result.err)

	return result.output
}

// AssertOutputContains runs command and asserts its stdout contains expectedOutputContains.
func AssertOutputContains(t *testing.T, command func(), expectedOutputContains string) {
	t.Helper()

	assert.Contains(t, CaptureOutput(t, command), expectedOutputContains)
}
