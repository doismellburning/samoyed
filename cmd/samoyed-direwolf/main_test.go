// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// shutdownTestConfig runs a channel with no hardware behind it: audio comes
// from standard input, so this works on a machine with no soundcard, and the
// network ports are off so that two of these can run at once.
const shutdownTestConfig = `
ADEVICE stdin
ACHANNELS 1
CHANNEL 0
MYCALL Q1TEST
AGWPORT 0
KISSPORT 0
`

// startupComplete is the last thing printed before the receive loop takes over,
// so a test that has seen it knows the signal handler is installed.
const startupComplete = `Log file is`

// buildDirewolf builds the command under test, since the point of the test is
// what a signal does to the process rather than to a function call.
func buildDirewolf(t *testing.T) string {
	t.Helper()

	var binary = filepath.Join(t.TempDir(), "samoyed-direwolf")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
}

// startDirewolf starts the built command and waits for it to finish starting
// up.  It returns the running process and a function that reads back whatever
// it has printed so far.
func startDirewolf(t *testing.T, binary string) (*exec.Cmd, func() string) {
	t.Helper()

	var dir = t.TempDir()

	var configName = filepath.Join(dir, "direwolf.conf")
	require.NoError(t, os.WriteFile(configName, []byte(shutdownTestConfig), 0o600))

	var outputName = filepath.Join(dir, "output.txt")

	var output, createErr = os.Create(outputName) //nolint:gosec
	require.NoError(t, createErr)

	t.Cleanup(func() { output.Close() })

	var printed = func() string {
		var content, readErr = os.ReadFile(outputName) //nolint:gosec
		require.NoError(t, readErr)

		return string(content)
	}

	// Audio comes from a pipe nothing ever writes to, so the receive loop has
	// something to block on and the process stays up until it is signalled.
	var audio, audioWriter, pipeErr = os.Pipe()
	require.NoError(t, pipeErr)

	t.Cleanup(func() { audio.Close(); audioWriter.Close() })

	var cmd = exec.CommandContext(t.Context(), binary, "-c", configName, "-t", "0", "-L", filepath.Join(dir, "packets.log"), "-") //nolint:gosec
	cmd.Stdin = audio
	cmd.Stdout = output
	cmd.Stderr = output

	require.NoError(t, cmd.Start())

	require.Eventually(t, func() bool {
		return strings.Contains(printed(), startupComplete)
	}, 30*time.Second, 50*time.Millisecond, "Never finished starting up: %s", printed())

	return cmd, printed
}

// TestSignalShutsDownCleanly covers the supervised stop: a .deb install is
// stopped with SIGTERM by systemd, a container runtime, or anything else that
// supervises a long-running process, and SIGTERM used to have its default
// disposition, so the process was killed outright and cleanup - which is what
// unkeys a CM108 or hamlib PTT - never ran.
func TestSignalShutsDownCleanly(t *testing.T) {
	var binary = buildDirewolf(t)

	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			var cmd, printed = startDirewolf(t, binary)

			require.NoError(t, cmd.Process.Signal(signal))

			var waited = make(chan error, 1)
			go func() { waited <- cmd.Wait() }()

			select {
			case err := <-waited:
				require.NoError(t, err, "%s did not shut the process down cleanly: %s", signal, printed())
			case <-time.After(30 * time.Second):
				t.Fatalf("%s did not shut the process down at all: %s", signal, printed())
			}

			require.Contains(t, printed(), "QRT", "%s ended the process without running cleanup", signal)
		})
	}
}

// TestSignalDuringStartupStopsStartup covers a stop that arrives while startup
// is still acquiring things.  The teardown used to run as soon as the signal
// did, alongside a startup that carried on regardless, so a PTT could be
// released before it was opened and then opened with nothing left to release
// it.  Startup has to stop at the signal and the teardown has to come after
// it: nothing startup does may appear once the teardown has begun.
func TestSignalDuringStartupStopsStartup(t *testing.T) {
	var binary = buildDirewolf(t)

	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			var dir = t.TempDir()

			var configName = filepath.Join(dir, "direwolf.conf")
			require.NoError(t, os.WriteFile(configName, []byte(shutdownTestConfig), 0o600))

			var audio, audioWriter, pipeErr = os.Pipe()
			require.NoError(t, pipeErr)

			t.Cleanup(func() { audio.Close(); audioWriter.Close() })

			var cmd = exec.CommandContext(t.Context(), binary, "-c", configName, "-t", "0", "-L", filepath.Join(dir, "packets.log"), "-") //nolint:gosec
			cmd.Stdin = audio

			// Read the output as it comes, rather than polling a file for it,
			// so that the signal lands while startup is still under way.
			var stdout, stdoutErr = cmd.StdoutPipe()
			require.NoError(t, stdoutErr)

			cmd.Stderr = cmd.Stdout

			require.NoError(t, cmd.Start())

			var output strings.Builder

			var scanner = bufio.NewScanner(stdout)

			// The configuration file is read after the signal handler is
			// installed and before anything is acquired.
			for scanner.Scan() {
				output.WriteString(scanner.Text() + "\n")

				if strings.HasPrefix(scanner.Text(), "Reading config file") {
					break
				}
			}

			require.NoError(t, cmd.Process.Signal(signal))

			for scanner.Scan() {
				output.WriteString(scanner.Text() + "\n")
			}

			require.NoError(t, cmd.Wait(), "%s did not shut the process down cleanly: %s", signal, output.String())

			var printed = output.String()

			var _, afterTeardown, tornDown = strings.Cut(printed, "QRT")
			require.True(t, tornDown, "%s ended the process without running cleanup: %s", signal, printed)
			require.NotContains(t, afterTeardown, startupComplete, "Startup carried on behind the teardown: %s", printed)
			require.NotContains(t, afterTeardown, "QRT", "Cleanup ran more than once: %s", printed)
		})
	}
}
