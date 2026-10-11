// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
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

// CaptureLogrus records what logrus logs at level and above until tb
// finishes, in the hook it returns, and sends the output itself to the bin
// meanwhile.  The level, the hooks and the output are all put back afterwards.
func CaptureLogrus(tb testing.TB, level logrus.Level) *test.Hook {
	tb.Helper()

	var logger = logrus.StandardLogger()

	var hooks = make(logrus.LevelHooks)
	for l, hs := range logger.Hooks {
		hooks[l] = append([]logrus.Hook(nil), hs...)
	}

	var savedHooks = logger.ReplaceHooks(hooks)
	var savedLevel = logger.GetLevel()

	var hook = new(test.Hook)
	logger.AddHook(hook)
	logger.SetLevel(level)

	DiscardLogrus(tb)

	tb.Cleanup(func() {
		logger.SetLevel(savedLevel)
		logger.ReplaceHooks(savedHooks)
	})

	return hook
}

// LoggedEntry is the one entry hook caught with message msg, failing the test
// if there is not exactly one.
func LoggedEntry(tb testing.TB, hook *test.Hook, msg string) *logrus.Entry {
	tb.Helper()

	var found []logrus.Entry

	for _, e := range hook.AllEntries() {
		if e.Message == msg {
			found = append(found, *e)
		}
	}

	require.Len(tb, found, 1, "entries with message %q", msg)

	return &found[0]
}
