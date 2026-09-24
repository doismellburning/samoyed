// SPDX-FileCopyrightText: 2026 The Samoyed Authors
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

const (
	positionPacket = "Q1TEST>APDW17:!4237.14NS07120.83W#PHG7130Chelmsford MA"
	messagePacket  = "Q1TEST>APDW17::Q2TEST   :Hello"
)

// defaultOptions is what main builds for a bare "samoyed-pftest <filter>".
func defaultOptions(filter string) options {
	var opts = options{
		filter:       filter,
		isAPRS:       true,
		fromChannel:  0,
		toChannel:    0,
		validateOnly: false,
		debugLevel:   0,
	}

	return opts
}

// runWith is run, with the streams a test wants to look at afterwards.
func runWith(opts options, input string) (int, string, string) {
	var out strings.Builder

	var errOut strings.Builder

	var exitStatus = run(opts, strings.NewReader(input), &out, &errOut)

	return exitStatus, out.String(), errOut.String()
}

func Test_run_reportsAVerdictPerPacket(t *testing.T) {
	var exitStatus, out, errOut = runWith(defaultOptions("t/m"), positionPacket+"\n"+messagePacket+"\n")

	assert.Equal(t, 0, exitStatus)
	assert.Equal(t, verdictDrop+"\t"+positionPacket+"\n"+verdictPass+"\t"+messagePacket+"\n", out)
	assert.Empty(t, errOut)
}

func Test_run_passesThroughBlankAndCommentLines(t *testing.T) {
	var exitStatus, out, errOut = runWith(defaultOptions("b/Q1TEST"), "# a comment\n\n"+positionPacket+"\n")

	assert.Equal(t, 0, exitStatus)
	assert.Equal(t, "# a comment\n\n"+verdictPass+"\t"+positionPacket+"\n", out)
	assert.Empty(t, errOut)
}

func Test_run_reportsAnUnparseableLine(t *testing.T) {
	// AX25FromText explains what is wrong with the line on stdout, which is
	// not the output under test here.
	var exitStatus, out, errOut = 0, "", ""

	direwolf.CaptureOutput(t, func() {
		exitStatus, out, errOut = runWith(defaultOptions("b/Q1TEST"), "this is not a packet\n"+positionPacket+"\n")
	})

	assert.Equal(t, 1, exitStatus, "an unparseable line should make the exit status non-zero")
	assert.Contains(t, out, verdictError+"\tthis is not a packet")
	assert.Contains(t, out, verdictPass+"\t"+positionPacket)
	assert.Contains(t, errOut, "could not parse monitoring format input")
}

func Test_run_rejectsAnInvalidFilter(t *testing.T) {
	var exitStatus, out, errOut = runWith(defaultOptions("x/"), positionPacket+"\n")

	assert.Equal(t, 1, exitStatus)
	assert.Empty(t, out, "no packet should be evaluated against a filter that does not parse")
	assert.Contains(t, errOut, "Invalid filter")
	assert.Contains(t, errOut, "Unrecognized filter type 'x'")
}

func Test_run_rejectsAChannelOutOfRange(t *testing.T) {
	var opts = defaultOptions("b/Q1TEST")
	opts.toChannel = direwolf.MAX_TOTAL_CHANS + 1

	var exitStatus, _, errOut = runWith(opts, positionPacket+"\n")

	assert.Equal(t, 1, exitStatus)
	assert.Contains(t, errOut, "Invalid filter")
}

func Test_run_validateOnly(t *testing.T) {
	t.Run("a valid filter says nothing and succeeds", func(t *testing.T) {
		var opts = defaultOptions("t/m & ! d/WIDE*")
		opts.validateOnly = true

		var exitStatus, out, errOut = runWith(opts, positionPacket+"\n")

		assert.Equal(t, 0, exitStatus)
		assert.Empty(t, out, "--validate should not read any packets")
		assert.Empty(t, errOut)
	})

	t.Run("an invalid filter explains itself and fails", func(t *testing.T) {
		var opts = defaultOptions("t/m & ( t/w")
		opts.validateOnly = true

		var exitStatus, _, errOut = runWith(opts, "")

		assert.Equal(t, 1, exitStatus)
		assert.Contains(t, errOut, "Invalid filter")
	})
}

func Test_run_connectedMode(t *testing.T) {
	var opts = defaultOptions("t/m")
	opts.isAPRS = false

	var exitStatus, _, errOut = runWith(opts, messagePacket+"\n")

	assert.Equal(t, 1, exitStatus, "t/ is an APRS filter type, not a connected mode one")
	assert.Contains(t, errOut, "Invalid filter")
}

func Test_run_verboseExplainsTheDecision(t *testing.T) {
	var opts = defaultOptions("b/Q1TEST")
	opts.debugLevel = 2

	// The filter engine's debug output goes to stdout, not to the writer run
	// prints verdicts to.
	direwolf.AssertOutputContains(t, func() {
		var exitStatus, _, _ = runWith(opts, positionPacket+"\n")
		assert.Equal(t, 0, exitStatus)
	}, "b/Q1TEST returns TRUE")
}

// runMainEnv, when set, has the test binary run main with the arguments it
// holds, one per line, instead of the tests, so a test can see main exit.
const runMainEnv = "SAMOYED_PFTEST_RUN_MAIN"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(runMainEnv); ok {
		os.Args = []string{"pftest"}
		if args != "" {
			os.Args = append(os.Args, strings.Split(args, "\n")...)
		}

		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// runMain runs main in a process of its own with input on stdin, returning
// its exit status and what it printed to stdout and stderr.
func runMain(t *testing.T, input string, args ...string) (int, string, string) {
	t.Helper()

	var cmd = exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec
	cmd.Env = append(os.Environ(), runMainEnv+"="+strings.Join(args, "\n"))
	cmd.Stdin = strings.NewReader(input)

	var out, errOut strings.Builder

	cmd.Stdout = &out
	cmd.Stderr = &errOut

	var err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)

		return exitErr.ExitCode(), out.String(), errOut.String()
	}

	return 0, out.String(), errOut.String()
}

func Test_main(t *testing.T) {
	var input = positionPacket + "\n" + messagePacket + "\n"

	var exitStatus, out, _ = runMain(t, input, "t/m")

	assert.Equal(t, 0, exitStatus)
	assert.Contains(t, out, "DROP\t"+positionPacket+"\n")
	assert.Contains(t, out, "PASS\t"+messagePacket+"\n")
}

func Test_main_options(t *testing.T) {
	var testCases = map[string]struct {
		args   []string
		status int
		out    string
		errOut string
	}{
		"validate": {
			[]string{"--validate", "t/m & ! d/WIDE*"}, 0, "", "",
		},
		"verbose": {
			[]string{"-vv", "t/m"}, 0, "PASS", "",
		},
		"connected mode": {
			[]string{"-c", "--from-channel", "1", "--to-channel", "2", "b/Q1TEST"}, 0, "PASS\t" + messagePacket, "",
		},
		"not for connected mode": {
			[]string{"-c", "t/m"}, 1, "", "Only b, d, v, and u specifications are allowed",
		},
		"help": {
			[]string{"--help"}, 0, "", "tries a packet filter expression out on packets",
		},
		"no filter": {
			nil, 1, "", "Expected exactly one filter expression, got 0.",
		},
		"two filters": {
			[]string{"t/m", "b/Q1TEST"}, 1, "", "Expected exactly one filter expression, got 2.",
		},
		"from channel out of range": {
			[]string{"--from-channel", "-1", "t/m"}, 1, "", "--from-channel must be between 0 and",
		},
		"to channel out of range": {
			[]string{"--to-channel", "9999", "t/m"}, 1, "", "--to-channel must be between 0 and",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var exitStatus, out, errOut = runMain(t, messagePacket+"\n", tc.args...)

			assert.Equal(t, tc.status, exitStatus, "stdout: %s\nstderr: %s", out, errOut)
			assert.Contains(t, out, tc.out)
			assert.Contains(t, errOut, tc.errOut)
		})
	}
}
