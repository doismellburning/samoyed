// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTNC points send_to_kiss_tnc at a TCP connection and hands back the far
// end of it, where a test plays the TNC.
func fakeTNC(t *testing.T) net.Conn {
	t.Helper()

	var ln, port = testutils.Listen(t)

	var conn, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, dialErr)

	var tncConn = testutils.Accept(t, ln)

	var oldSock, oldUsingTCP = server_sock, using_tcp

	server_sock, using_tcp = conn, true

	t.Cleanup(func() {
		server_sock, using_tcp = oldSock, oldUsingTCP

		conn.Close()
	})

	return tncConn
}

// readKISS reads what has arrived at the TNC end of the connection.  The
// connection's deadline stops it waiting for good.
func readKISS(t *testing.T, conn net.Conn, n int) []byte {
	t.Helper()

	var buf = make([]byte, n)

	var _, err = io.ReadFull(conn, buf)
	require.NoError(t, err)

	return buf
}

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

func Test_parse_number(t *testing.T) {
	var testCases = map[string]struct {
		in   string
		want int
	}{
		"number":       {" 30 ", 30},
		"zero":         {"0", 0},
		"most":         {"255", 255},
		"missing":      {"  ", 99},
		"not a number": {"x", 99},
		"negative":     {"-1", 99},
		"too big":      {"256", 99},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var got int

			testutils.CaptureOutput(t, func() { got = parse_number(tc.in, 99) })

			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_process_input(t *testing.T) {
	var testCases = map[string]struct {
		in   string
		want []byte
	}{
		"txDelay":     {"d 30", []byte{0x01, 30}},
		"persistence": {"p 63", []byte{0x02, 63}},
		"slot time":   {"s 10", []byte{0x03, 10}},
		"txTail":      {"t 5", []byte{0x04, 5}},
		"full duplex": {"f 1", []byte{0x05, 1}},
		"hardware":    {"h TNC:", []byte{0x06, 'T', 'N', 'C', ':'}},
		"channel":     {"[9] p 63", []byte{0x92, 63}},
		"default":     {"d", []byte{0x01, byte(direwolf.DEFAULT_TXDELAY)}},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var tnc = fakeTNC(t)

			testutils.CaptureOutput(t, func() { process_input(tc.in) })

			var want = direwolf.KissEncapsulate(tc.want)

			assert.Equal(t, want, readKISS(t, tnc, len(want)))
		})
	}
}

func Test_process_input_frame(t *testing.T) {
	var tnc = fakeTNC(t)

	testutils.CaptureOutput(t, func() { process_input("[2] Q1TEST>APDW17:>Testing\r\n") })

	var want = direwolf.KissEncapsulate(append([]byte{0x20}, direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Testing"))...))

	assert.Equal(t, want, readKISS(t, tnc, len(want)))
}

// None of these are sent anywhere, so there's no TNC for them to go to.
func Test_process_input_rejects(t *testing.T) {
	var testCases = map[string]struct {
		in   string
		want string
	}{
		"bad channel":     {"[x] Q1TEST>APDW17:>Testing", "Channel number and ] was expected"},
		"channel too big": {"[16] Q1TEST>APDW17:>Testing", "must be in range of 0 thru 15"},
		"bad frame":       {"Q1TEST", "Could not convert to AX.25 frame"},
		"bad command":     {"x 1", "Invalid command. Must be one of d p s t f h."},
		"neither":         {"!", "Input, starting with upper case letter or digit"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var output = testutils.CaptureOutput(t, func() { process_input(tc.in) })

			assert.Contains(t, output, tc.want)
		})
	}

	// Blank lines, with or without a channel, are quietly ignored.
	assert.Empty(t, testutils.CaptureOutput(t, func() { process_input("  \r\n") }))
	assert.Empty(t, testutils.CaptureOutput(t, func() { process_input("[3]  ") }))
}

func Test_send_to_kiss_tnc_clamps(t *testing.T) {
	var tnc = fakeTNC(t)

	var output = testutils.CaptureOutput(t, func() {
		send_to_kiss_tnc(16, 16, bytes.Repeat([]byte{'x'}, direwolf.AX25_MAX_PACKET_LEN))
	})

	assert.Contains(t, output, "Invalid channel 16")
	assert.Contains(t, output, "Invalid command 16")
	assert.Contains(t, output, "Invalid data length")

	var want = direwolf.KissEncapsulate(append([]byte{0x00}, bytes.Repeat([]byte{'x'}, direwolf.AX25_MAX_PACKET_LEN-1)...))

	assert.Equal(t, want, readKISS(t, tnc, len(want)))
}

func Test_kissutil_kiss_process_msg(t *testing.T) {
	var frame = append([]byte{0x30}, direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Testing"))...)

	t.Run("data", func(t *testing.T) {
		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Equal(t, "[3] Q1TEST>APDW17:>Testing\n", output)
	})

	t.Run("timestamped", func(t *testing.T) {
		var old = timestamp_format

		t.Cleanup(func() { timestamp_format = old })

		timestamp_format = "%Y"

		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Regexp(t, `^\[3 \d{4}\] Q1TEST>APDW17:>Testing\n$`, output)
	})

	t.Run("saved", func(t *testing.T) {
		var old = receive_output

		t.Cleanup(func() { receive_output = old })

		receive_output = t.TempDir()

		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Contains(t, output, "Save received frame to "+receive_output)

		var entries, err = os.ReadDir(receive_output)
		require.NoError(t, err)
		require.Len(t, entries, 1)

		var content, readErr = os.ReadFile(filepath.Join(receive_output, entries[0].Name()))
		require.NoError(t, readErr)

		assert.Equal(t, "[3] Q1TEST>APDW17:>Testing\n", string(content))
	})

	t.Run("hardware", func(t *testing.T) {
		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg([]byte("\x16TNC:")) })

		assert.Equal(t, "[1] h TNC:\n", output)
	})

	t.Run("unexpected", func(t *testing.T) {
		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg([]byte{0x21, 30}) })

		assert.Equal(t, "Unexpected KISS command 1, channel 2\n", output)
	})

	t.Run("invalid", func(t *testing.T) {
		var output = testutils.CaptureOutput(t, func() { kissutil_kiss_process_msg([]byte{0x00, 'x'}) })

		assert.Contains(t, output, "Invalid KISS data frame from TNC.")
	})
}

func Test_timestamp_filename(t *testing.T) {
	assert.Regexp(t, `^\d{8}-\d{6}-\d{3}$`, timestamp_filename())
}

// main starts goroutines that exit the process when the TNC goes away, so
// these run it as a process of its own, against a TNC played by the test.
//
// Losing the TNC, or never reaching it, ends the process with an error.
func Test_main_withoutATNC(t *testing.T) {
	t.Run("goes away", func(t *testing.T) {
		var ln, port = testutils.Listen(t)

		// Stdin stays open, so that it is the TNC going away that ends it.
		var p = testutils.StartMain(t, "-h", "127.0.0.1", "-p", port)

		require.NoError(t, testutils.Accept(t, ln).Close())

		p.WaitFor(t, "Read error from TCP KISS TNC")
		assert.Equal(t, 1, p.Wait())
	})

	t.Run("unreachable", func(t *testing.T) {
		var port = testutils.UnusedPort(t)

		var p = testutils.StartMain(t, "-h", "127.0.0.1", "-p", port)

		p.WaitFor(t, "Unable to connect to 127.0.0.1 on port "+port)
		assert.Equal(t, 1, p.Wait())
	})
}
