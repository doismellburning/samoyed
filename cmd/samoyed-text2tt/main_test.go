// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Text2TT(t *testing.T) {
	// From `man text2tt`
	direwolf.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A22A2223A33A33340A00122223333")
	direwolf.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A2B2C3A3B3C4A0A0123")
}

// runMainEnv, when set, has the test binary run main with the arguments it
// holds instead of the tests, so a test can see main exit.
const runMainEnv = "SAMOYED_TEXT2TT_RUN_MAIN"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(runMainEnv); ok {
		os.Args = append([]string{"text2tt"}, strings.Fields(args)...)

		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// runMain runs main in a process of its own, returning what it printed to
// stdout and stderr, and its exit status.
func runMain(t *testing.T, args ...string) (string, int) {
	t.Helper()

	var cmd = exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec
	cmd.Env = append(os.Environ(), runMainEnv+"="+strings.Join(args, " "))

	var out, err = cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)

		return string(out), exitErr.ExitCode()
	}

	return string(out), 0
}

func Test_main(t *testing.T) {
	var out, status = runMain(t, "abcdefg", "0123")

	assert.Equal(t, 0, status)
	assert.Contains(t, out, "2A22A2223A33A33340A00122223333")

	out, status = runMain(t)

	assert.Equal(t, 1, status)
	assert.Contains(t, out, "Supply text string on command line.")
}

// The checksum treats letters of either case alike, and skips anything that
// isn't a letter or digit.
func Test_checksum(t *testing.T) {
	assert.Equal(t, checksum("4B3B5C"), checksum("4b3b5c"))
	assert.Equal(t, checksum("4B3B5C"), checksum("4B 3B-5C"))
}
