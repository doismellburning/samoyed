// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwutil

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestAssert(t *testing.T) {
	assert.NotPanics(t, func() { Assert(true) })

	// The message names the caller, not Assert itself.
	defer func() {
		var message, ok = recover().(string)
		assert.True(t, ok)
		assert.Regexp(t, `^Assertion failed at .*/dwutil_test\.go:\d+$`, message)
	}()

	Assert(false)
}

func TestIfThenElse(t *testing.T) {
	assert.Equal(t, "yes", IfThenElse(true, "yes", "no"))
	assert.Equal(t, "no", IfThenElse(false, "yes", "no"))
}

func TestHexDump(t *testing.T) {
	var output = testutils.CaptureOutput(t, func() {
		HexDump([]byte("Q1TEST>Q2TEST:\x00hello world\x7f!"))
	})

	assert.Equal(t,
		"  000:  51 31 54 45 53 54 3e 51 32 54 45 53 54 3a 00 68  Q1TEST>Q2TEST:.h\n"+
			"  010:  65 6c 6c 6f 20 77 6f 72 6c 64 7f 21              ello world.!\n",
		output)
}

func TestHexDumpEmpty(t *testing.T) {
	assert.Empty(t, testutils.CaptureOutput(t, func() { HexDump(nil) }))
}
