package direwolf

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note that any of the Dire Wolf colour formatting totally screws this for reasons I don't yet understand.
// See also what happens if you pipe output to a pager...
func AssertOutputContains(t *testing.T, command func(), expectedOutputContains string) {
	t.Helper()

	var oldStdout = os.Stdout
	defer func() {
		os.Stdout = oldStdout
	}()

	var r, w, _ = os.Pipe()
	os.Stdout = w

	command()

	w.Close() //nolint:gosec

	os.Stdout = oldStdout

	var outputBytes, readErr = io.ReadAll(r)

	require.NoError(t, readErr)

	var outputString = string(outputBytes)

	assert.Contains(t, outputString, expectedOutputContains)
}
