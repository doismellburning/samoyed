// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package testutils holds helpers for tests that more than one package wants,
// such as the commands under cmd/, whose tests drive main as a user would.
package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// WithStdin runs f with standard input reading input, putting os.Stdin back
// afterwards.  The input comes from a file rather than a pipe, so however much
// there is, f can read it all without anything having to feed it alongside.
func WithStdin(t *testing.T, input string, f func()) {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(name, []byte(input), 0o600))

	var stdin, err = os.Open(name) //nolint:gosec // A file of our own, in the test's temporary directory.
	require.NoError(t, err)

	defer stdin.Close()

	var oldStdin = os.Stdin

	defer func() { os.Stdin = oldStdin }()

	os.Stdin = stdin

	f()
}
