// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func Test_TT2Text(t *testing.T) {
	// From `man tt2text`
	testutils.AssertOutputContains(t, func() { tt2text("2A22A2223A33A33340A00122223333") }, "ABCDEFG 0123")
	testutils.AssertOutputContains(t, func() { tt2text("2A22A2223A33A33340A00122223333") }, "A2A222D3D3334 00122223333")
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func Test_main(t *testing.T) {
	// Arguments are run together, so spaces can break up a long sequence.
	var result = testutils.RunMain(t, "", "2A22A2223A33A33340", "A00122223333")
	var out, status = result.Output(), result.Status

	assert.Equal(t, 0, status)
	assert.Contains(t, out, "ABCDEFG 0123")

	result = testutils.RunMain(t, "")
	out, status = result.Output(), result.Status

	assert.Equal(t, 1, status)
	assert.Contains(t, out, "Supply button sequence on command line.")
}
