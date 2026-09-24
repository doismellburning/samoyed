// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/doismellburning/samoyed/internal/maybe"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout runs f and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()

	var tmp, err = os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)

	var oldStdout = os.Stdout

	defer func() { os.Stdout = oldStdout }()

	os.Stdout = tmp

	f()

	os.Stdout = oldStdout

	require.NoError(t, tmp.Close())

	var output, readErr = os.ReadFile(tmp.Name())
	require.NoError(t, readErr)

	return string(output)
}

// fakeTNC points the global tnc at a pseudo terminal, standing in for the
// serial port to a KISS TNC, and hands back the far end of it.
func fakeTNC(t *testing.T) *os.File {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	var oldTNC = tnc

	tnc = direwolf.SerialPortOpen(slave.Name(), 9600)
	require.NotNil(t, tnc)

	// SerialPortOpen opens the device by name, so this handle is surplus.
	require.NoError(t, slave.Close())

	t.Cleanup(func() {
		tnc.Close()
		tnc = oldTNC
		master.Close()
	})

	return master
}

// readN reads exactly n bytes from f, failing the test rather than hanging
// if they don't turn up.
func readN(t *testing.T, f *os.File, n int) []byte {
	t.Helper()

	var got = make(chan []byte, 1)

	go func() {
		var buf = make([]byte, n)

		var _, err = io.ReadFull(f, buf)
		if err != nil {
			buf = nil
		}

		got <- buf
	}()

	select {
	case buf := <-got:
		require.NotNil(t, buf, "short read from the TNC")

		return buf
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out reading from the TNC")

		return nil
	}
}

func Test_walk96(t *testing.T) {
	var master = fakeTNC(t)

	var oldMYCALL = MYCALL

	t.Cleanup(func() { MYCALL = oldMYCALL })

	MYCALL = "Q1TEST-9"

	var output = captureStdout(t, func() {
		walk96(int(direwolf.DWFIX_3D), 42.61875, -71.347212,
			maybe.Just(5.07), maybe.Just(291.42), maybe.Just(33.5))
	})

	var report = strings.TrimSpace(output)

	assert.Equal(t, "Q1TEST-9>WALK96:!4237.12N/07120.83W=291/005445.925MHz /A=000109Sequence number 0001", report)

	// What went to the TNC is that same report as a KISS data frame for
	// channel 0.
	var pp = direwolf.AX25FromText(report, true)
	require.NotNil(t, pp)

	var want = direwolf.KissEncapsulate(append([]byte{0}, direwolf.AX25Pack(pp)...))

	assert.Equal(t, want, readN(t, master, len(want)))

	// The next report carries the next sequence number.
	output = captureStdout(t, func() {
		walk96(int(direwolf.DWFIX_3D), 42.61875, -71.347212,
			maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Nothing[float64]())
	})

	assert.Contains(t, output, "Sequence number 0002")
}
