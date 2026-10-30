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

// What is logged while a test captures it is recorded, and once the test is
// done the logger has only the hooks it had before.
//
//nolint:paralleltest // Serial: changes logrus's global logger.
func TestCaptureLogrusPutsTheHooksBack(t *testing.T) {
	var before = len(logrus.StandardLogger().Hooks[logrus.InfoLevel])

	t.Run("capturing", func(t *testing.T) { //nolint:paralleltest // Serial: changes logrus's global logger.
		var hook = CaptureLogrus(t)

		logrus.Info("captured")

		require.Len(t, hook.AllEntries(), 1)
		assert.Equal(t, "captured", hook.LastEntry().Message)
	})

	assert.Len(t, logrus.StandardLogger().Hooks[logrus.InfoLevel], before)
}
