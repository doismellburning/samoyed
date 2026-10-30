// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
)

// DiscardLogrus sends logrus's output to the bin until tb finishes, then puts
// it back.  For tests, fuzz targets above all, whose code complains at length
// about input that nobody is there to read.
func DiscardLogrus(tb testing.TB) {
	tb.Helper()

	var saved = logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)

	tb.Cleanup(func() { logrus.SetOutput(saved) })
}

// CaptureLogrus records what is logged through logrus's standard logger until
// tb finishes, then puts back the hooks the logger had before, so the hook
// doesn't outlive the test.  It changes the shared logger, so a test using it
// stays serial.
func CaptureLogrus(tb testing.TB) *test.Hook {
	tb.Helper()

	var saved = logrus.StandardLogger().ReplaceHooks(make(logrus.LevelHooks))

	tb.Cleanup(func() { logrus.StandardLogger().ReplaceHooks(saved) })

	return test.NewGlobal()
}
