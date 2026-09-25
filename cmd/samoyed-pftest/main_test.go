// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
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

	testutils.CaptureOutput(t, func() {
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
	testutils.AssertOutputContains(t, func() {
		var exitStatus, _, _ = runWith(opts, positionPacket+"\n")
		assert.Equal(t, 0, exitStatus)
	}, "b/Q1TEST returns TRUE")
}
