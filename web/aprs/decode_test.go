// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestDecode(t *testing.T) {
	var output = testutils.CaptureOutput(t, func() {
		Decode(newDecoder(), "# A comment\r\n\r\nQ1TEST>APDW18:>Testing\r\nQ1TEST-9>APN383:!4237.14NS07120.83W#PHG7130Chelmsford, MA\n")
	})

	assert.Contains(t, output, "# A comment\n\n")
	assert.Contains(t, output, "Status Report")
	assert.Contains(t, output, "WB2OSZ DireWolf", "the compiled-in tocalls.yaml identifies the device")
	assert.Contains(t, output, "Kantronics")
	assert.Contains(t, output, "Chelmsford, MA")
}

func TestDecodeHex(t *testing.T) {
	testutils.AssertOutputContains(t, func() {
		Decode(newDecoder(), "0082a0aeae6260e0829668844040609c68b0ae8640e040ae92888a646303f03e454d36346e652f23204563686f6c696e6b203134352e3331302f313030687a20546f6e65")
	}, "Echolink")
}
