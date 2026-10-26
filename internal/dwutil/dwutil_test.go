// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwutil

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssert(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() { Assert(true) })

	// The message names the caller, not Assert itself.
	defer func() {
		var message, ok = recover().(string)
		assert.True(t, ok)
		assert.Regexp(t, `^Assertion failed at .*/dwutil_test\.go:\d+$`, message)
	}()

	Assert(false)
}

func TestIfThenElse(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "yes", IfThenElse(true, "yes", "no"))
	assert.Equal(t, "no", IfThenElse(false, "yes", "no"))
}

//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestHexDump(t *testing.T) {
	var output = testutils.CaptureOutput(t, func() {
		HexDump([]byte("Q1TEST>Q2TEST:\x00hello world\x7f!"))
	})

	assert.Equal(t,
		"  000:  51 31 54 45 53 54 3e 51 32 54 45 53 54 3a 00 68  Q1TEST>Q2TEST:.h\n"+
			"  010:  65 6c 6c 6f 20 77 6f 72 6c 64 7f 21              ello world.!\n",
		output)
}

//nolint:paralleltest // Serial: captures os.Stdout, which is process-wide.
func TestHexDumpEmpty(t *testing.T) {
	assert.Empty(t, testutils.CaptureOutput(t, func() { HexDump(nil) }))
}

func TestHexDumpLines(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		[]string{
			"  000:  51 31 54 45 53 54 3e 51 32 54 45 53 54 3a 00 68  Q1TEST>Q2TEST:.h",
			"  010:  65 6c 6c 6f 20 77 6f 72 6c 64 7f 21              ello world.!",
		},
		HexDumpLines([]byte("Q1TEST>Q2TEST:\x00hello world\x7f!")))
}

func TestHexDumpLinesEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, HexDumpLines(nil))
}

func TestLogHexDump(t *testing.T) {
	t.Parallel()

	var logger, hook = test.NewNullLogger()

	LogHexDump(logrus.NewEntry(logger).WithField("channel", 1), logrus.WarnLevel,
		[]byte("Q1TEST>Q2TEST:\x00hello world\x7f!"))

	var entries = hook.AllEntries()
	assert.Len(t, entries, 2)

	for i, want := range HexDumpLines([]byte("Q1TEST>Q2TEST:\x00hello world\x7f!")) {
		assert.Equal(t, logrus.WarnLevel, entries[i].Level)
		assert.Equal(t, want, entries[i].Data["dump"])
		assert.Equal(t, 1, entries[i].Data["channel"])
	}
}

func TestLogHexDumpBelowLevel(t *testing.T) {
	t.Parallel()

	var logger, hook = test.NewNullLogger()

	LogHexDump(logrus.NewEntry(logger), logrus.DebugLevel, []byte("Q1TEST"))

	assert.Empty(t, hook.AllEntries())
}

// A command's output comes back as it was written, or as one line: Dire Wolf
// turned each CR, LF and TAB into a space, and could trim the trailing spaces
// that left.  It used to tidy the command instead, and hand the output back
// with its newline, which then ended up in the beacon or audible response.
func TestRunCmdTidiesOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tidy CmdOutput
		want string
	}{
		{CmdAsWritten, "Q1\tTEST\r\n"},
		{CmdOneLine, "Q1 TEST  "},
		{CmdOneLineTrimmed, "Q1 TEST"},
	} {
		var out, err = RunCmd(t.Context(), `printf 'Q1\tTEST\r\n'`, tc.tidy)
		require.NoError(t, err)
		assert.Equal(t, tc.want, string(out), "tidy %d", tc.tidy)
	}
}

// A command is a shell command line, as Dire Wolf's popen took it, so it can
// pass arguments to the program it runs: COMMENTCMD="date +%H%M", say.  It
// used to be taken whole as the program's name.
func TestRunCmdPassesArguments(t *testing.T) {
	t.Parallel()

	var out, err = RunCmd(t.Context(), "echo Q1TEST Q2TEST", CmdAsWritten)
	require.NoError(t, err)
	assert.Equal(t, "Q1TEST Q2TEST\n", string(out))
}

func TestRunCmdMissingCommand(t *testing.T) {
	t.Parallel()

	var _, err = RunCmd(t.Context(), "/nonexistent/q1test-cmd", CmdAsWritten)
	assert.Error(t, err)
}
