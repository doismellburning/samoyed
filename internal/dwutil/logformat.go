// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwutil

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/sirupsen/logrus"
)

// LogTimestampFormat is the time layout logrus entries are stamped with: the
// wall-clock time to the millisecond, so that PTT, DCD and frame events within
// the same second still read in order.
const LogTimestampFormat = "2006-01-02T15:04:05.000Z07:00"

// SetLogFormat gives logrus's standard logger a wall-clock timestamp on every
// entry, in place of its default of seconds since start on a terminal.  When
// the logger writes straight into the systemd journal, which stamps each line
// itself, it leaves the timestamp out instead.
//
// Call it after logrus.SetOutput, as the journal check looks at where the
// output goes.
func SetLogFormat() {
	var logger = logrus.StandardLogger()

	logger.SetFormatter(newLogFormatter(writesToJournal(logger.Out, os.Getenv("JOURNAL_STREAM"))))
}

func newLogFormatter(toJournal bool) *logrus.TextFormatter {
	var formatter = new(logrus.TextFormatter)
	formatter.FullTimestamp = true
	formatter.TimestampFormat = LogTimestampFormat
	formatter.DisableTimestamp = toJournal

	return formatter
}

// writesToJournal reports whether out is the stream systemd connected to the
// journal.  systemd names that stream in JOURNAL_STREAM as "device:inode";
// the variable is inherited by anything we start, whether or not its output
// still goes there, hence the comparison rather than just checking it is set.
// See systemd.exec(5).
func writesToJournal(out io.Writer, journalStream string) bool {
	if journalStream == "" {
		return false
	}

	var file, isFile = out.(*os.File)
	if !isFile {
		return false
	}

	var info, err = file.Stat()
	if err != nil {
		return false
	}

	var stat, isStat = info.Sys().(*syscall.Stat_t)
	if !isStat {
		return false
	}

	return journalStream == fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
