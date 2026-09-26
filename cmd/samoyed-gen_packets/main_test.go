// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// decode is how many frames a bare "samoyed-atest <name>" finds in name.
func decode(t *testing.T, name string) int {
	t.Helper()

	var opts = new(direwolf.AtestOptions)
	opts.IL2PVersion = "0.6"

	var atest, atestErr = direwolf.NewAtest(opts)
	require.NoError(t, atestErr)

	var result, decodeErr = atest.DecodeFile(name)
	require.NoError(t, decodeErr)

	return result.PacketsDecoded
}

func Test_main_generates(t *testing.T) {
	var dir = t.TempDir()

	var messages = filepath.Join(dir, "messages.txt")
	require.NoError(t, os.WriteFile(messages, []byte(
		"Q1TEST>APDW17:>First\n"+
			"Q2TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#Second\n",
	), 0o600))

	var testCases = map[string]struct {
		stdin   string
		args    []string
		want    string
		decoded int // Not checked if 0.
	}{
		"built in":       {"", nil, "built in message...", 4},
		"numbered":       {"", []string{"-N", "3"}, "built in message...", 3},
		"noisy":          {"", []string{"-n", "3"}, "built in message...", 2}, // The noise loses one.
		"variable speed": {"", []string{"-v", "1,0.5"}, "Variable speed.", 5},
		"from a file":    {"", []string{messages}, "Reading from " + messages, 2},
		"from stdin":     {"Q1TEST>APDW17:>First\n", []string{"-"}, "Reading from stdin", 1},
		"extra files":    {"", []string{messages, messages}, "Warning: File(s) beyond the first are ignored.", 2},
		"not TNC2":       {"bogus\nQ1TEST>APDW17:>First\n", []string{"-"}, `"bogus" is not valid TNC2 monitoring format`, 1},
		"options":        {"", []string{"-a", "100", "-r", "22050", "-2"}, "2 channels of sound rather than 1.", 4},
		"8 bit":          {"", []string{"-8"}, "8 bits per audio sample rather than 16.", 4},
		"Morse":          {"", []string{"-M", "40"}, "Morse code speed set to 40 WPM.", 0}, // Not AX.25 at all.
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var wav = filepath.Join(t.TempDir(), "out.wav")

			var result = testutils.RunMain(t, tc.stdin, append([]string{"-o", wav}, tc.args...)...)
			var out = result.Output()

			assert.Equal(t, 0, result.Status, out)
			assert.Contains(t, out, tc.want)

			if tc.decoded > 0 {
				assert.Equal(t, tc.decoded, decode(t, wav))
			}
		})
	}
}

func Test_main_badArguments(t *testing.T) {
	var dir = t.TempDir()
	var wav = filepath.Join(dir, "out.wav")

	var testCases = map[string]struct {
		args  []string
		want  string
		usage bool
	}{
		"help":                {[]string{"--help"}, "Generate audio file for AX.25 frames.", true},
		"no output file":      {nil, "The -o output file option must be specified.", true},
		"noisy and noiseless": {[]string{"-o", wav, "-n", "1", "-N", "1"}, "Cannot choose both noisy packets", false},
		"bad amplitude":       {[]string{"-o", wav, "-a", "201"}, "amplitude must be in range of 0 to 200", true},
		"bad Morse speed":     {[]string{"-o", wav, "-M", "51"}, "morse code speed must be in range", true},
		"bad variable speed":  {[]string{"-o", wav, "-v", "x"}, "Invalid variable speed x", true},
		"no speed increment":  {[]string{"-o", wav, "-v", "5,0"}, "variable speed increment must be more than 0", false},
		"bad mark":            {[]string{"-o", wav, "-m", "10"}, "more reasonable mark frequency", true},
		"bad output file":     {[]string{"-o", filepath.Join(dir, "missing", "out.wav")}, "no such file or directory", true},
		"missing input file":  {[]string{"-o", wav, filepath.Join(dir, "missing.txt")}, "can't open", false},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args...)
			var out = result.Output()

			assert.Equal(t, 1, result.Status, out)
			assert.Contains(t, out, tc.want)

			if tc.usage {
				assert.Contains(t, out, "Usage:")
			} else {
				assert.NotContains(t, out, "Usage:")
			}
		})
	}
}
