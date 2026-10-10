// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzQuietly points stdout, and logrus, at the bin until tb finishes, then
// puts them back.  Some decoders, the ones ported from Dire Wolf above all,
// narrate a malformed packet at length on stdout as well as through logrus,
// and a fuzzing run has nobody to read it.
func FuzzQuietly(tb testing.TB) {
	tb.Helper()

	var devNull, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(tb, err)

	var saved = os.Stdout
	os.Stdout = devNull

	tb.Cleanup(func() {
		os.Stdout = saved
		devNull.Close()
	})

	DiscardLogrus(tb)
}
