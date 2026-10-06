// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Nothing logged while a test is discarding reaches the output it had before,
// and that output is back once the test is done.
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
