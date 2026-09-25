// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package testutils

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithStdin(t *testing.T) {
	var oldStdin = os.Stdin

	// More than a pipe would hold, to show nothing needs to be feeding it.
	var input = strings.Repeat("Q1TEST>APDW17:>Testing\n", 10000)

	var got []byte

	WithStdin(t, input, func() {
		var err error

		got, err = io.ReadAll(os.Stdin)
		require.NoError(t, err)
	})

	assert.Equal(t, input, string(got))
	assert.Same(t, oldStdin, os.Stdin, "stdin should be put back")
}

// Output longer than a pipe will hold - 64 KiB on Linux - used to wedge the
// command against a pipe nobody was reading from until it returned.
func Test_CaptureOutput_more_than_a_pipe_holds(t *testing.T) {
	var expected = strings.Repeat("x", 256*1024)

	assert.Equal(t, expected, CaptureOutput(t, func() { fmt.Print(expected) }))
}

func Test_CaptureOutput_nothing_at_all(t *testing.T) {
	assert.Empty(t, CaptureOutput(t, func() {}))
}
