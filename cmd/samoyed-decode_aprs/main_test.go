// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/direwolf"
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

func Test_DecodeAPRSLine1(t *testing.T) {
	var expected = "Yaesu"

	testutils.AssertOutputContains(t, func() {
		decodeAPRSLine(direwolf.NewAPRSDecoderFromDataFiles(), "N1EDF-9>T2QT8Y,W1CLA-1,WIDE1*,WIDE2-2,00000:`bSbl!Mv/`\"4%}_ <0x0d>")
	}, expected)
}

func Test_DecodeAPRSLine2(t *testing.T) {
	var expected = "Kantronics"

	testutils.AssertOutputContains(t, func() {
		decodeAPRSLine(direwolf.NewAPRSDecoderFromDataFiles(), "WB2OSZ-1>APN383,qAR,N1EDU-2:!4237.14NS07120.83W#PHG7130Chelmsford, MA")
	}, expected)
}

func Test_DecodeAPRSLine3(t *testing.T) {
	var expected = "Echolink"

	testutils.AssertOutputContains(t, func() {
		decodeAPRSLine(direwolf.NewAPRSDecoderFromDataFiles(),
			"00 82 a0 ae ae 62 60 e0 82 96 68 84 40 40 60 9c 68 b0 ae 86 40 e0 40 ae 92 88 8a 64 63 03 f0 3e 45 "+
				"4d 36 34 6e 65 2f 23 20 45 63 68 6f 6c 69 6e 6b 20 31 34 35 2e 33 31 30 2f 31 30 30 68 7a 20 54 6f 6e 65",
		)
	}, expected)
}

func Test_DecodeAPRSLine3NoSpaces(t *testing.T) {
	var expected = "Echolink"

	testutils.AssertOutputContains(t, func() {
		decodeAPRSLine(direwolf.NewAPRSDecoderFromDataFiles(),
			"0082a0aeae6260e0829668844040609c68b0ae8640e040ae92888a646303f03e454d36346e652f23204563686f6c696e6b203134352e3331302f313030687a20546f6e65",
		)
	}, expected)
}
