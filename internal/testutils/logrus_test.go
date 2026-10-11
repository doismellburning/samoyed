// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing logged while a test is discarding reaches the output it had before,
// and that output is back once the test is done.
//
//nolint:paralleltest // Serial: changes logrus's global logger.
func TestDiscardLogrusPutsTheOutputBack(t *testing.T) {
	var buf bytes.Buffer

	var saved = logrus.StandardLogger().Out
	logrus.SetOutput(&buf)

	t.Cleanup(func() { logrus.SetOutput(saved) })

	t.Run("discarding", func(t *testing.T) {
		DiscardLogrus(t)

		logrus.Error("Q1TEST should not be seen")
	})

	assert.Empty(t, buf.String())
	assert.Same(t, &buf, logrus.StandardLogger().Out)
}

// What is logged at the level asked for or above is caught, and once the test
// is done the level and the hooks are as they were.
//
//nolint:paralleltest // Serial: changes logrus's global logger.
func TestCaptureLogrus(t *testing.T) {
	var savedLevel = logrus.GetLevel()
	var savedHooks = len(logrus.StandardLogger().Hooks[logrus.InfoLevel])

	t.Run("capturing", func(t *testing.T) {
		var hook = CaptureLogrus(t, logrus.DebugLevel)

		logrus.Trace("Q1TEST is below the level")
		logrus.Debug("Q2TEST is at the level")

		require.Len(t, hook.AllEntries(), 1)
		assert.Equal(t, "Q2TEST is at the level", hook.LastEntry().Message)
	})

	assert.Equal(t, savedLevel, logrus.GetLevel())
	assert.Len(t, logrus.StandardLogger().Hooks[logrus.InfoLevel], savedHooks)
}

// LoggedEntry finds the entry with the message asked for, among others.
//
//nolint:paralleltest // Serial: changes logrus's global logger.
func TestLoggedEntry(t *testing.T) {
	var hook = CaptureLogrus(t, logrus.InfoLevel)

	logrus.Info("Q1TEST")
	logrus.WithField("callsign", "Q2TEST").Warn("Q2TEST")

	var entry = LoggedEntry(t, hook, "Q2TEST")
	assert.Equal(t, logrus.WarnLevel, entry.Level)
	assert.Equal(t, "Q2TEST", entry.Data["callsign"])
}
