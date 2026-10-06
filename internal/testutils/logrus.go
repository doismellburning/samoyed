// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
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
