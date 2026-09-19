// SPDX-FileCopyrightText: 2026 The Samoyed Authors
//
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

/*
 * Debug tracing is grouped by subsystem, mirroring the "-d" command line
 * options, so one subsystem can be made chatty without drowning in all the
 * others.
 *
 * logrus has a level per logger and no notion of groups within one, so each
 * group gets a logger of its own and carries its name as a field.  Callers ask
 * for their group's logger once and log to it; whether anything comes out is
 * then a central decision, made by SetDebugGroup.
 */

// A DebugGroup names one subsystem's debug output.
type DebugGroup string

const (
	// DebugAPRSTT is the APRStt gateway, "-d d".
	DebugAPRSTT DebugGroup = "aprstt"
)

//nolint:gochecknoglobals // A process has one set of debug settings.
var (
	debugLoggersMu sync.Mutex
	debugLoggers   = make(map[DebugGroup]*logrus.Logger)
)

// debugLogger returns the logger for a group, creating it on first use.
// Groups start at logrus.InfoLevel: quiet, but not silent.
func debugLogger(group DebugGroup) *logrus.Entry {
	debugLoggersMu.Lock()
	defer debugLoggersMu.Unlock()

	var logger = debugLoggers[group]

	if logger == nil {
		logger = logrus.New()

		// Dire Wolf has always put its output on stdout and dw_printf still
		// does, so keep the two together.
		logger.SetOutput(os.Stdout)

		debugLoggers[group] = logger
	}

	return logger.WithField("group", string(group))
}

// SetDebugGroup turns Debug level output on or off for one group.
func SetDebugGroup(group DebugGroup, enabled bool) {
	var level = logrus.InfoLevel

	if enabled {
		level = logrus.DebugLevel
	}

	debugLogger(group).Logger.SetLevel(level)
}
