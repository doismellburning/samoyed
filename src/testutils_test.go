// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Output longer than a pipe will hold - 64 KiB on Linux - used to wedge the
// command against a pipe nobody was reading from until it returned.
func Test_CaptureOutput_more_than_a_pipe_holds(t *testing.T) {
	var expected = strings.Repeat("x", 256*1024)

	assert.Equal(t, expected, CaptureOutput(t, func() { dw_printf("%s", expected) }))
}

func Test_CaptureOutput_nothing_at_all(t *testing.T) {
	assert.Empty(t, CaptureOutput(t, func() {}))
}
