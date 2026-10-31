// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// writeConfig writes a configuration with its store in the test's directory.
func writeConfig(t *testing.T) string {
	t.Helper()

	var dir = t.TempDir()
	var file = filepath.Join(dir, "samoyed-bbs.yaml")

	require.NoError(t, os.WriteFile(file, []byte("call: Q1BBS\nstore: "+filepath.Join(dir, "bbs.db")+"\n"), 0o600))

	return file
}

func TestHelp(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "--help")

	assert.Equal(t, 0, result.Status)
	assert.Contains(t, result.Output(), "packet BBS")
}

func TestConfigCheck(t *testing.T) {
	t.Parallel()

	var good = testutils.RunMain(t, "", "-c", writeConfig(t), "--config-check")
	assert.Equal(t, 0, good.Status)
	assert.Contains(t, good.Output(), "is OK")

	var badFile = filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(badFile, []byte("call: Q1BBS-16\n"), 0o600))

	var bad = testutils.RunMain(t, "", "-c", badFile, "--config-check")
	assert.Equal(t, 1, bad.Status)
	assert.Contains(t, bad.Output(), `call: "Q1BBS-16" is not a callsign`)
	assert.Contains(t, bad.Output(), "store: the path of the message database is needed")

	var missing = testutils.RunMain(t, "", "-c", filepath.Join(t.TempDir(), "missing.yaml"))
	assert.Equal(t, 1, missing.Status)
	assert.Contains(t, missing.Output(), "no such file")
}

func TestNothingToServe(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "-c", writeConfig(t))

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "Use --console CALL")
}

func TestConsoleRefusesABadCall(t *testing.T) {
	t.Parallel()

	var result = testutils.RunMain(t, "", "-c", writeConfig(t), "--console", "not a call")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "not a callsign")
}

func TestConsoleSessions(t *testing.T) {
	t.Parallel()

	var file = writeConfig(t)

	var first = testutils.RunMain(t, "SP Q1TEST\nHello\nHello there.\n/EX\nL\nB\n", "-c", file, "--console", "q2test")

	assert.Equal(t, 0, first.Status)
	assert.Contains(t, first.Output(), "Welcome to Q1BBS, a Samoyed BBS.\n[SAMBBS-0.0-$]\nde Q1BBS>\n")
	assert.Contains(t, first.Output(), "Message 1 saved, ID 1_Q1BBS.")
	assert.Contains(t, first.Output(), "Q1TEST          Q2TEST")
	assert.NotContains(t, first.Output(), "Begin session", "the console is kept for the session unless asked")

	// The message is still there for the next session.
	var second = testutils.RunMain(t, "R 1\n", "-c", file, "--console", "Q1TEST", "-v")

	assert.Equal(t, 0, second.Status, second.Output())
	assert.Contains(t, second.Output(), "Hello there.")
	assert.Contains(t, second.Output(), "Begin session")
}

func TestConsoleWithAnUnopenableStore(t *testing.T) {
	t.Parallel()

	var file = filepath.Join(t.TempDir(), "samoyed-bbs.yaml")
	require.NoError(t, os.WriteFile(file, []byte("call: Q1BBS\nstore: "+filepath.Join(t.TempDir(), "no", "such", "dir", "bbs.db")+"\n"), 0o600))

	var result = testutils.RunMain(t, "", "-c", file, "--console", "Q1TEST")

	assert.Equal(t, 1, result.Status)
	assert.Contains(t, result.Output(), "message store")
}
