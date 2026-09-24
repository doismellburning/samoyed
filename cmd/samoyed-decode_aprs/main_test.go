// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// withStdin runs f with standard input reading from input.
func withStdin(t *testing.T, input string, f func()) {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(name, []byte(input), 0o600))

	var stdin, err = os.Open(name) //nolint:gosec
	require.NoError(t, err)

	defer stdin.Close()

	var oldStdin = os.Stdin

	defer func() { os.Stdin = oldStdin }()

	os.Stdin = stdin

	f()
}

// captureStdout runs f and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	var tmp, err = os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)

	var oldStdout = os.Stdout

	defer func() { os.Stdout = oldStdout }()

	os.Stdout = tmp

	f()

	os.Stdout = oldStdout

	require.NoError(t, tmp.Close())

	var output, readErr = os.ReadFile(tmp.Name())
	require.NoError(t, readErr)

	return string(output)
}

func Test_main(t *testing.T) {
	var input = strings.Join([]string{
		"# A comment is echoed back",
		"",
		"Q1TEST>APDW17:>Testing",
		"Q1TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#PHG7130Chelmsford, MA",
	}, "\n") + "\n"

	var output string

	withStdin(t, input, func() {
		output = captureStdout(t, main)
	})

	require.Contains(t, output, "# A comment is echoed back\n\n")
	require.Contains(t, output, "Status Report")
	require.Contains(t, output, "Testing")
	require.Contains(t, output, "N 42°37.1400, W 071°20.8300")
	require.Contains(t, output, "Chelmsford, MA")
}
