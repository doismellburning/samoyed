// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAtest is the Atest main sets up for a bare "samoyed-atest <file>".
func newAtest(t *testing.T) *direwolf.Atest {
	t.Helper()

	var opts = new(direwolf.AtestOptions)
	opts.IL2PVersion = "0.6"

	var atest, err = direwolf.NewAtest(opts)
	require.NoError(t, err)

	return atest
}

// genPackets writes the packets Dire Wolf's gen_packets makes by default, as
// in the examples from the original atest.c source.
func genPackets(t *testing.T) string {
	t.Helper()

	var f = filepath.Join(t.TempDir(), "test1.wav")

	var cmd = exec.CommandContext(context.Background(), "gen_packets", "-o", f) //nolint:gosec
	require.NoError(t, cmd.Run())

	return f
}

func Test_run_decodesAFile(t *testing.T) {
	var out strings.Builder

	var exitStatus = run(newAtest(t), []string{genPackets(t)}, expected{atLeast: 4, atMost: 4}, false, &out)

	assert.Equal(t, 0, exitStatus)
	assert.Contains(t, out.String(), "4 packets decoded")
	assert.NotContains(t, out.String(), "DCD count")
}

func Test_run_failsOutsideTheExpectedRange(t *testing.T) {
	var f = genPackets(t)

	var testCases = map[string]expected{
		"too few":  {atLeast: 5, atMost: -1},
		"too many": {atLeast: -1, atMost: 3},
	}

	for name, want := range testCases {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder

			var exitStatus = run(newAtest(t), []string{f}, want, false, &out)

			assert.Equal(t, 1, exitStatus)
			assert.Contains(t, out.String(), "TEST FAILED")
		})
	}
}

func Test_run_failsOnAMissingFile(t *testing.T) {
	var out strings.Builder

	var exitStatus = run(newAtest(t), []string{filepath.Join(t.TempDir(), "missing.wav")}, expected{atLeast: -1, atMost: -1}, false, &out)

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, out.String(), "couldn't open file")
	assert.NotContains(t, out.String(), "packets decoded")
}

func Test_run_reportsDCD(t *testing.T) {
	var out strings.Builder

	var exitStatus = run(newAtest(t), []string{genPackets(t)}, expected{atLeast: -1, atMost: -1}, true, &out)

	assert.Equal(t, 0, exitStatus)
	assert.Contains(t, out.String(), "DCD count = ")
}
