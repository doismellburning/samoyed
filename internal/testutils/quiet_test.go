// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"io"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Stdout and logrus both go somewhere else while a test is being quiet, and
// are back as they were once it is done.
func TestFuzzQuietlyPutsStdoutBack(t *testing.T) {
	var savedStdout = os.Stdout
	var savedLogrus = logrus.StandardLogger().Out

	t.Run("quiet", func(t *testing.T) {
		FuzzQuietly(t)

		assert.NotSame(t, savedStdout, os.Stdout)
		assert.Equal(t, io.Discard, logrus.StandardLogger().Out)
	})

	assert.Same(t, savedStdout, os.Stdout)
	assert.Equal(t, savedLogrus, logrus.StandardLogger().Out)
}
