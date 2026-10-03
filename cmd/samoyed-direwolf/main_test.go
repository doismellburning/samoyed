// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
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
const startupComplete = `msg="APRS log file"`

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// TestPrintUTF8Test covers -u, which is there so an operator can see whether
// their terminal shows UTF-8.  It used to hand the string's bytes to %c one at
// a time, and Go formats each as a rune of its own, so it printed "maÃ±ana"
// whatever the terminal could do.
func TestPrintUTF8Test(t *testing.T) {
	var result = testutils.RunMain(t, "", "-u")

	require.Equal(t, 0, result.Status, "stderr: %s", result.Stderr)
	require.Contains(t, result.Stdout, "UTF-8 test string: mañana ° Füße\n")
}

// startDirewolf starts the command's main with a configuration that needs no
// hardware.  Audio comes from its standard input, which nothing writes to, so
// the receive loop has something to block on and the process stays up until it
// is signalled.
func startDirewolf(t *testing.T) *testutils.Process {
	t.Helper()

	var dir = t.TempDir()

	var configName = filepath.Join(dir, "direwolf.conf")
	require.NoError(t, os.WriteFile(configName, []byte(shutdownTestConfig), 0o600))

	return testutils.StartMain(t, "-c", configName, "-L", filepath.Join(dir, "packets.log"), "-")
}

// TestSignalShutsDownCleanly covers the supervised stop: a .deb install is
// stopped with SIGTERM by systemd, a container runtime, or anything else that
// supervises a long-running process, and SIGTERM used to have its default
// disposition, so the process was killed outright and cleanup - which is what
// unkeys a CM108 or hamlib PTT - never ran.
func TestSignalShutsDownCleanly(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			var p = startDirewolf(t)

			p.WaitFor(t, startupComplete)
			p.Signal(t, signal)

			require.Equal(t, 0, p.Wait(), "%s did not shut the process down cleanly: %s", signal, p.Output())
			require.Contains(t, p.Output(), "QRT", "%s ended the process without running cleanup", signal)
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
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			var p = startDirewolf(t)

			// The output is read as it comes, so the signal lands while
			// startup is still under way.  The configuration file is read
			// after the signal handler is installed and before anything is
			// acquired.
			p.WaitFor(t, "Reading config file")
			p.Signal(t, signal)

			require.Equal(t, 0, p.Wait(), "%s did not shut the process down cleanly: %s", signal, p.Output())

			var printed = p.Output()

			var _, afterTeardown, tornDown = strings.Cut(printed, "QRT")
			require.True(t, tornDown, "%s ended the process without running cleanup: %s", signal, printed)
			require.NotContains(t, afterTeardown, startupComplete, "Startup carried on behind the teardown: %s", printed)
			require.NotContains(t, afterTeardown, "QRT", "Cleanup ran more than once: %s", printed)
		})
	}
}
