// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"io"
	"os"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureVersionStdout collects what fn prints to stdout. The pipe is drained
// concurrently, as verbose output includes the whole BuildInfo and could
// otherwise fill the pipe buffer and block.
func captureVersionStdout(t *testing.T, fn func()) string {
	t.Helper()

	var reader, writer, err = os.Pipe()
	require.NoError(t, err)

	var oldStdout = os.Stdout

	os.Stdout = writer

	t.Cleanup(func() { os.Stdout = oldStdout })

	var done = make(chan []byte)

	go func() {
		var out, _ = io.ReadAll(reader)
		done <- out
	}()

	fn()

	os.Stdout = oldStdout

	require.NoError(t, writer.Close())

	var out = <-done

	require.NoError(t, reader.Close())

	return string(out)
}

func TestVersionGetBuildSettingOrDefault(t *testing.T) {
	var bi = new(debug.BuildInfo)
	bi.Settings = []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}

	assert.Equal(t, "abc123", getBuildSettingOrDefault(bi, "vcs.revision", "UNKNOWN"))
	assert.Equal(t, "true", getBuildSettingOrDefault(bi, "vcs.modified", "INVALID"))
	assert.Equal(t, "UNKNOWN", getBuildSettingOrDefault(bi, "vcs.time", "UNKNOWN"))
	assert.Equal(t, "fallback", getBuildSettingOrDefault(new(debug.BuildInfo), "vcs.time", "fallback"))
}

func TestVersionPrintVersion(t *testing.T) {
	var oldVersion = SAMOYED_VERSION

	t.Cleanup(func() { SAMOYED_VERSION = oldVersion })

	t.Run("unknown version", func(t *testing.T) {
		SAMOYED_VERSION = ""

		var out = captureVersionStdout(t, func() { printVersion(false) })

		assert.Contains(t, out, "Samoyed - Version !UNKNOWN! (revision ")
		assert.NotContains(t, out, "BuildInfo:")
	})

	t.Run("set version, verbose", func(t *testing.T) {
		SAMOYED_VERSION = "2026.09.27"

		var out = captureVersionStdout(t, func() { printVersion(true) })

		assert.Contains(t, out, "Samoyed - Version 2026.09.27 (revision ")
		assert.Contains(t, out, ", built at ")
		assert.Contains(t, out, "\nBuildInfo: ")
	})
}
