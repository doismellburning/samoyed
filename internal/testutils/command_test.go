// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMain stands in for a command's main, doing whatever its first argument
// says with the rest.
func fakeMain() {
	var args = os.Args[1:]

	switch args[0] {
	case "args":
		fmt.Printf("%s %q\n", os.Args[0], args[1:])
	case "cat":
		var _, _ = io.Copy(os.Stdout, os.Stdin)
	case "stderr":
		fmt.Fprintln(os.Stderr, strings.Join(args[1:], " "))
	case "exit":
		var status, _ = strconv.Atoi(args[1])
		fmt.Println("exiting")
		os.Exit(status)
	case "coverdir":
		fmt.Println(os.Getenv(coverDirEnv))
	case "hang":
		fmt.Println("hanging")
		time.Sleep(time.Hour)
	case "lines":
		for _, line := range args[1:] {
			fmt.Println(line)
		}

		var _, _ = io.Copy(io.Discard, os.Stdin) // Until stdin closes.

		fmt.Println("stdin closed")
	}
}

func TestMain(m *testing.M) {
	RunMainIfAsked(fakeMain)

	os.Exit(m.Run())
}

func TestRunMain(t *testing.T) {
	t.Parallel()

	t.Run("arguments arrive intact", func(t *testing.T) {
		t.Parallel()

		var result = RunMain(t, "", "args", "with space", "")

		assert.Equal(t, 0, result.Status)
		assert.Equal(t, "testutils [\"with space\" \"\"]\n", result.Stdout)
	})

	t.Run("stdin", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "Q1TEST>APDW17:>Testing\n", RunMain(t, "Q1TEST>APDW17:>Testing\n", "cat").Stdout)
	})

	t.Run("stderr", func(t *testing.T) {
		t.Parallel()

		var result = RunMain(t, "", "stderr", "went", "wrong")

		assert.Empty(t, result.Stdout)
		assert.Equal(t, "went wrong\n", result.Stderr)
		assert.Equal(t, "went wrong\n", result.Output())
	})

	t.Run("exit status", func(t *testing.T) {
		t.Parallel()

		var result = RunMain(t, "", "exit", "3")

		assert.Equal(t, 3, result.Status)
		assert.Equal(t, "exiting\n", result.Stdout)
	})
}

func TestCoverDirPerCommand(t *testing.T) {
	var parent = t.TempDir()
	t.Setenv(coverDirEnv, parent)

	var first = strings.TrimSpace(RunMain(t, "", "coverdir").Stdout)

	var process = StartMain(t, "coverdir")
	process.Wait()

	var second = strings.TrimSpace(process.Output())

	// Each its own, so they can't trip over each other writing coverage.
	assert.NotEqual(t, parent, first)
	assert.NotEqual(t, parent, second)
	assert.NotEqual(t, first, second)
	assert.Equal(t, parent, filepath.Dir(first))
	assert.Equal(t, parent, filepath.Dir(second))

	// RunMain has tidied up its command's already.  StartMain's goes when
	// the test ends.
	assert.NoDirExists(t, first)
}

func TestStartMain(t *testing.T) {
	t.Parallel()

	var p = StartMain(t, "lines", "one", "two", "three")

	assert.Equal(t, []string{"one", "two"}, p.WaitFor(t, "two"))
	assert.Equal(t, []string{"three"}, p.WaitFor(t, "three"))

	// It carries on until its stdin closes.
	require.NoError(t, p.Stdin.Close())
	p.WaitFor(t, "stdin closed")

	assert.Equal(t, 0, p.Wait())
	assert.Equal(t, 0, p.Wait(), "waiting again gives the same answer")
}

func TestOutput(t *testing.T) {
	t.Parallel()

	var p = StartMain(t, "lines", "one", "two", "three")

	p.WaitFor(t, "one")
	require.NoError(t, p.Stdin.Close())
	assert.Equal(t, 0, p.Wait())

	assert.Equal(t, "one\ntwo\nthree\nstdin closed\n", p.Output(), "including what WaitFor never read")
}

func TestSignal(t *testing.T) {
	t.Parallel()

	// It carries on until stdin closes, which here it never does.
	var p = StartMain(t, "lines", "started")

	p.WaitFor(t, "started")
	p.Signal(t, os.Kill)

	assert.Equal(t, -1, p.Wait(), "killed by a signal")
}

func TestStartExitStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2, StartMain(t, "exit", "2").Wait())
}

// Start runs whatever it is given; here that is this test binary, told to be
// the fake command just as RunMain would.
//

func TestStart(t *testing.T) {
	t.Setenv(runMainEnv, encodeArgs(t, []string{"args", "given"}))

	var p = Start(t, os.Args[0])

	p.WaitFor(t, `["given"]`)
	assert.Equal(t, 0, p.Wait())
}

// timeoutEnv, when set, has TestTimingOut run the helper it names with a short
// ProcessTimeout, on a command that never finishes, for TestTimeoutFailsTheTest
// to see it fail.
const timeoutEnv = "SAMOYED_TESTUTILS_TIMEOUT"

// timeoutLengthEnv is the ProcessTimeout TestTimingOut gives the helper.
const timeoutLengthEnv = "SAMOYED_TESTUTILS_TIMEOUT_LENGTH"

func TestTimingOut(t *testing.T) {
	t.Parallel()

	var helper = os.Getenv(timeoutEnv)
	if helper == "" {
		t.Skip("Only run by TestTimeoutFailsTheTest")
	}

	var timeout, parseErr = time.ParseDuration(os.Getenv(timeoutLengthEnv))
	require.NoError(t, parseErr)

	ProcessTimeout = timeout

	switch helper {
	case "RunMain":
		var result = RunMain(t, "", "hang")

		t.Logf("RunMain carried on, with status %d", result.Status)
	case "StartMain":
		var p = StartMain(t, "hang")

		p.WaitFor(t, "hanging")

		t.Logf("Wait returned %d", p.Wait())
	}
}

// A command that times out can't be relied on, whatever its exit status, so it
// fails the test.
func TestTimeoutFailsTheTest(t *testing.T) {
	t.Parallel()

	for _, helper := range []string{"RunMain", "StartMain"} {
		t.Run(helper, func(t *testing.T) {
			t.Parallel()

			var out = runTimingOut(t, helper, "100ms")

			assert.Contains(t, string(out), "The command didn't finish within 100ms")
			assert.Contains(t, string(out), "hanging", "and shows what it printed")
		})
	}
}

// A timeout counts from when the command starts, not from when the helper was
// called, so one that has run out by the time the command is started - as a
// short one can on a busy machine - still reports the command as not
// finishing, rather than failing to start it.
func TestTimeoutBeforeStartFailsTheTest(t *testing.T) {
	t.Parallel()

	for _, helper := range []string{"RunMain", "StartMain"} {
		t.Run(helper, func(t *testing.T) {
			t.Parallel()

			var out = runTimingOut(t, helper, "1ns")

			assert.Contains(t, string(out), "The command didn't finish within 1ns")
			assert.NotContains(t, string(out), "context deadline exceeded")
		})
	}
}

// runTimingOut runs TestTimingOut, which should fail, on helper with a
// ProcessTimeout of timeout, and hands back what it printed.
func runTimingOut(t *testing.T, helper string, timeout string) []byte {
	t.Helper()

	var cmd = exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTimingOut$", "-test.v") //nolint:gosec // Running ourselves.
	cmd.Env = append(os.Environ(), timeoutEnv+"="+helper, timeoutLengthEnv+"="+timeout)

	var out, err = cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the test should have failed:\n%s", out)

	return out
}
