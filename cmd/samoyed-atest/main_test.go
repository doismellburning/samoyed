// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAtest is the Atest main sets up for a bare "samoyed-atest <file>".
func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

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

func Test_main_decodes(t *testing.T) {
	var f = genPackets(t)

	var testCases = map[string]struct {
		args   []string
		status int
		want   string
	}{
		"plain":         {[]string{f}, 0, "4 packets decoded"},
		"in range":      {[]string{"-L", "4", "-G", "4", f}, 0, "4 packets decoded"},
		"too few":       {[]string{"-L", "5", f}, 1, "TEST FAILED: number decoded is less than 5"},
		"DCD":           {[]string{"-d", "o", f}, 0, "DCD count = "},
		"right channel": {[]string{"-1", f}, 0, "0 packets decoded"}, // The file is mono.
		"both channels": {[]string{"-2", "-d", "x", "-d", "2", f}, 0, "4 packets decoded"},
		"fix bits":      {[]string{"-F", "1", f}, 0, "4 packets decoded"},
		"IL2P version":  {[]string{"--il2p-version", "0.4", f}, 0, "4 packets decoded"},
		"hex display":   {[]string{"-h", f}, 0, "  010:  2c 54 68 65 20 71 75 69 63 6b 20 62 72 6f 77 6e  ,The quick brown"},
		"bit errors":    {[]string{"-e", "0.5", f}, 0, "0 packets decoded"}, // Half the bits wrong, at random.
		"missing wav":   {[]string{f + ".missing"}, 1, "couldn't open file"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)
			var out, status = result.Output(), result.Status

			assert.Equal(t, tc.status, status, out)
			assert.Contains(t, out, tc.want)
		})
	}
}

func Test_main_badArguments(t *testing.T) {
	var testCases = map[string]struct {
		args []string
		want string
	}{
		"help":              {[]string{"--help"}, "decodes AX.25 frames from audio recordings"},
		"no files":          {nil, "Specify .WAV file name on command line."},
		"unknown debug":     {[]string{"-d", "q", "x.wav"}, "Unrecognised debug flag: q"},
		"too many channels": {[]string{"-0", "-1", "x.wav"}, "Exactly one of left/right/both channels must be selected."},
		"bad IL2P version":  {[]string{"--il2p-version", "9", "x.wav"}, "invalid IL2P version 9"},
		"bad fix bits":      {[]string{"-F", "99", "x.wav"}, "fix bits should be between"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)
			var out, status = result.Output(), result.Status

			assert.Equal(t, 1, status)
			assert.Contains(t, out, tc.want)
			assert.Contains(t, out, "Usage:")
		})
	}
}
