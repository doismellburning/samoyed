// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/require"
)

func Test_main(t *testing.T) {
	var input = strings.Join([]string{
		"# A comment is echoed back",
		"",
		"Q1TEST>APDW17:>Testing",
		"Q1TEST-9>APDW17,WIDE1-1:!4237.14NS07120.83W#PHG7130Chelmsford, MA",
	}, "\n") + "\n"

	var output string

	testutils.WithStdin(t, input, func() {
		output = testutils.CaptureOutput(t, main)
	})

	require.Contains(t, output, "# A comment is echoed back\n\n")
	require.Contains(t, output, "Status Report")
	require.Contains(t, output, "Testing")
	require.Contains(t, output, "N 42°37.1400, W 071°20.8300")
	require.Contains(t, output, "Chelmsford, MA")
}
