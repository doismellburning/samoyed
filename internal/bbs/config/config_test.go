// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	var cfg, err = Parse([]byte(`
call: q1bbs-3
store: /var/lib/samoyed-bbs/bbs.db
banner: |
  Welcome to the club BBS.
  Sysop: Q1TEST
prompt: "Q1BBS>"
idleTimeout: 5m
`))
	require.NoError(t, err)

	assert.Equal(t, "Q1BBS-3", cfg.Call)
	assert.Equal(t, "/var/lib/samoyed-bbs/bbs.db", cfg.Store)
	assert.Equal(t, []string{"Welcome to the club BBS.", "Sysop: Q1TEST"}, cfg.BannerLines())
	assert.Equal(t, "Q1BBS>", cfg.Prompt)
	assert.Equal(t, 5*time.Minute, cfg.IdleTimeout)
}

func TestParseDefaults(t *testing.T) {
	t.Parallel()

	var cfg, err = Parse([]byte("call: Q1BBS\nstore: bbs.db\n"))
	require.NoError(t, err)

	assert.Equal(t, []string{"Welcome to Q1BBS, a Samoyed BBS."}, cfg.BannerLines())
	assert.Equal(t, "de Q1BBS>", cfg.Prompt)
	assert.Equal(t, DefaultIdleTimeout, cfg.IdleTimeout)
}

func TestParseReportsEveryProblem(t *testing.T) {
	t.Parallel()

	var _, err = Parse([]byte("call: Q1BBS-16\nidleTimeout: -1s\n"))
	require.Error(t, err)

	assert.Contains(t, err.Error(), `call: "Q1BBS-16" is not a callsign`)
	assert.Contains(t, err.Error(), "store: the path of the message database is needed")
	assert.Contains(t, err.Error(), "idleTimeout: -1s is negative")
}

func TestParseRefusesUnknownSettings(t *testing.T) {
	t.Parallel()

	var _, err = Parse([]byte("call: Q1BBS\nstore: bbs.db\nsysop: Q1TEST\n"))
	require.ErrorContains(t, err, "sysop")
}

func TestParseRefusesNonsense(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"call: [", "idleTimeout: soon\n", ""} {
		var _, err = Parse([]byte(data))
		require.Error(t, err, "%q", data)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	var file = filepath.Join(t.TempDir(), "samoyed-bbs.yaml")
	require.NoError(t, os.WriteFile(file, []byte("call: Q1BBS\nstore: bbs.db\n"), 0o600))

	var cfg, err = Load(file)
	require.NoError(t, err)
	assert.Equal(t, "Q1BBS", cfg.Call)

	var _, missingErr = Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.ErrorIs(t, missingErr, os.ErrNotExist)
}

func TestValidCall(t *testing.T) {
	t.Parallel()

	for _, call := range []string{"Q1TEST", "Q1TEST-0", "Q1TEST-15", "Q1"} {
		assert.True(t, ValidCall(call), call)
	}

	for _, call := range []string{"", "q1test", "Q1TEST-16", "Q1TESTX", "Q1TEST-", "Q1 TEST"} {
		assert.False(t, ValidCall(call), call)
	}
}
