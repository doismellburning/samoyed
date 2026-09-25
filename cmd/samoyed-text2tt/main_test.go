// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func Test_Text2TT(t *testing.T) {
	// From `man text2tt`
	testutils.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A22A2223A33A33340A00122223333")
	testutils.AssertOutputContains(t, func() { text2tt([]string{"abcdefg", "0123"}) }, "2A2B2C3A3B3C4A0A0123")
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func Test_main(t *testing.T) {
	var result = testutils.RunMain(t, "", "abcdefg", "0123")
	var out, status = result.Output(), result.Status

	assert.Equal(t, 0, status)
	assert.Contains(t, out, "2A22A2223A33A33340A00122223333")

	result = testutils.RunMain(t, "")
	out, status = result.Output(), result.Status

	assert.Equal(t, 1, status)
	assert.Contains(t, out, "Supply text string on command line.")
}

// The checksum treats letters of either case alike, and skips anything that
// isn't a letter or digit.
func Test_checksum(t *testing.T) {
	// "Hello" in the two-key method, as text2tt prints it.
	assert.Equal(t, 1, checksum("4B3B5C5C6C"))
	assert.Equal(t, 6, checksum("4B3B5C"))

	assert.Equal(t, checksum("4B3B5C"), checksum("4b3b5c"))
	assert.Equal(t, checksum("4B3B5C"), checksum("4B 3B-5C"))
}
