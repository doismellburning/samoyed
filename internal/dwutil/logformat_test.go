// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwutil

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func formatEntry(t *testing.T, formatter logrus.Formatter) string {
	t.Helper()

	var entry = logrus.NewEntry(logrus.New())
	entry.Time = time.Date(2026, 10, 16, 9, 12, 3, 456_000_000, time.UTC)
	entry.Level = logrus.InfoLevel
	entry.Message = "Heard"

	var line, err = formatter.Format(entry)
	require.NoError(t, err)

	return string(line)
}

func TestLogFormatterStampsWallClockToTheMillisecond(t *testing.T) {
	t.Parallel()

	assert.Contains(t, formatEntry(t, newLogFormatter(false)), `time="2026-10-16T09:12:03.456Z"`)
}

func TestLogFormatterLeavesTimestampToTheJournal(t *testing.T) {
	t.Parallel()

	var line = formatEntry(t, newLogFormatter(true))

	assert.NotContains(t, line, "time=")
	assert.Contains(t, line, "msg=Heard")
}

func journalStreamFor(t *testing.T, file *os.File) string {
	t.Helper()

	var info, err = file.Stat()
	require.NoError(t, err)

	var stat, ok = info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

func TestWritesToJournal(t *testing.T) {
	t.Parallel()

	var dir = t.TempDir()

	var journal, err = os.CreateTemp(dir, "journal")
	require.NoError(t, err)
	t.Cleanup(func() { _ = journal.Close() })

	other, err := os.CreateTemp(dir, "other")
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	var journalStream = journalStreamFor(t, journal)

	assert.True(t, writesToJournal(journal, journalStream), "the stream JOURNAL_STREAM names")
	assert.False(t, writesToJournal(other, journalStream), "inherited JOURNAL_STREAM, output elsewhere")
	assert.False(t, writesToJournal(journal, ""), "JOURNAL_STREAM unset")
	assert.False(t, writesToJournal(new(bytes.Buffer), journalStream), "not a file")
}
