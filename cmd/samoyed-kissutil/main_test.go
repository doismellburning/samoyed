// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

// testFrame builds the AX.25 frame described by a monitoring format string.
func testFrame(t *testing.T, monitor string) []byte {
	t.Helper()

	var pp = direwolf.AX25FromText(monitor, true)
	require.NotNil(t, pp)

	return direwolf.AX25Pack(pp)
}

// fakeTNC points send_to_kiss_tnc at a TCP connection and hands back the far
// end of it, where a test plays the TNC.
func fakeTNC(t *testing.T) net.Conn {
	t.Helper()

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	defer ln.Close()

	var conn, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", ln.Addr().String())
	require.NoError(t, dialErr)

	var tncConn, acceptErr = ln.Accept()
	require.NoError(t, acceptErr)

	var oldSock, oldUsingTCP = server_sock, using_tcp

	server_sock, using_tcp = conn, true

	t.Cleanup(func() {
		server_sock, using_tcp = oldSock, oldUsingTCP

		conn.Close()
		tncConn.Close()
	})

	return tncConn
}

// readKISS reads what has arrived at the TNC end of the connection.
func readKISS(t *testing.T, conn net.Conn, n int) []byte {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	var buf = make([]byte, n)

	var _, err = io.ReadFull(conn, buf)
	require.NoError(t, err)

	return buf
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

			captureStdout(t, func() { got = parse_number(tc.in, 99) })

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

			captureStdout(t, func() { process_input(tc.in) })

			var want = direwolf.KissEncapsulate(tc.want)

			assert.Equal(t, want, readKISS(t, tnc, len(want)))
		})
	}
}

func Test_process_input_frame(t *testing.T) {
	var tnc = fakeTNC(t)

	captureStdout(t, func() { process_input("[2] Q1TEST>APDW17:>Testing\r\n") })

	var want = direwolf.KissEncapsulate(append([]byte{0x20}, testFrame(t, "Q1TEST>APDW17:>Testing")...))

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
			var output = captureStdout(t, func() { process_input(tc.in) })

			assert.Contains(t, output, tc.want)
		})
	}

	// Blank lines, with or without a channel, are quietly ignored.
	assert.Empty(t, captureStdout(t, func() { process_input("  \r\n") }))
	assert.Empty(t, captureStdout(t, func() { process_input("[3]  ") }))
}

func Test_send_to_kiss_tnc_clamps(t *testing.T) {
	var tnc = fakeTNC(t)

	var output = captureStdout(t, func() {
		send_to_kiss_tnc(16, 16, bytes.Repeat([]byte{'x'}, direwolf.AX25_MAX_PACKET_LEN))
	})

	assert.Contains(t, output, "Invalid channel 16")
	assert.Contains(t, output, "Invalid command 16")
	assert.Contains(t, output, "Invalid data length")

	var want = direwolf.KissEncapsulate(append([]byte{0x00}, bytes.Repeat([]byte{'x'}, direwolf.AX25_MAX_PACKET_LEN-1)...))

	assert.Equal(t, want, readKISS(t, tnc, len(want)))
}

func Test_kissutil_kiss_process_msg(t *testing.T) {
	var frame = append([]byte{0x30}, testFrame(t, "Q1TEST>APDW17:>Testing")...)

	t.Run("data", func(t *testing.T) {
		var output = captureStdout(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Equal(t, "[3] Q1TEST>APDW17:>Testing\n", output)
	})

	t.Run("timestamped", func(t *testing.T) {
		var old = timestamp_format

		t.Cleanup(func() { timestamp_format = old })

		timestamp_format = "%Y"

		var output = captureStdout(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Regexp(t, `^\[3 \d{4}\] Q1TEST>APDW17:>Testing\n$`, output)
	})

	t.Run("saved", func(t *testing.T) {
		var old = receive_output

		t.Cleanup(func() { receive_output = old })

		receive_output = t.TempDir()

		var output = captureStdout(t, func() { kissutil_kiss_process_msg(frame) })

		assert.Contains(t, output, "Save received frame to "+receive_output)

		var entries, err = os.ReadDir(receive_output)
		require.NoError(t, err)
		require.Len(t, entries, 1)

		var content, readErr = os.ReadFile(filepath.Join(receive_output, entries[0].Name()))
		require.NoError(t, readErr)

		assert.Equal(t, "[3] Q1TEST>APDW17:>Testing\n", string(content))
	})

	t.Run("hardware", func(t *testing.T) {
		var output = captureStdout(t, func() { kissutil_kiss_process_msg([]byte("\x16TNC:")) })

		assert.Equal(t, "[1] h TNC:\n", output)
	})

	t.Run("unexpected", func(t *testing.T) {
		var output = captureStdout(t, func() { kissutil_kiss_process_msg([]byte{0x21, 30}) })

		assert.Equal(t, "Unexpected KISS command 1, channel 2\n", output)
	})

	t.Run("invalid", func(t *testing.T) {
		var output = captureStdout(t, func() { kissutil_kiss_process_msg([]byte{0x00, 'x'}) })

		assert.Contains(t, output, "Invalid KISS data frame from TNC.")
	})
}

func Test_timestamp_filename(t *testing.T) {
	assert.Regexp(t, `^\d{8}-\d{6}-\d{3}$`, timestamp_filename())
}

// main starts goroutines that exit the process when the TNC goes away, so it
// runs as a process of its own here, against a TNC played by the test.
func Test_main_endToEnd(t *testing.T) {
	var binary = filepath.Join(t.TempDir(), "samoyed-kissutil")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var buildOut, buildErr = build.CombinedOutput()
	require.NoError(t, buildErr, "Building the command failed: %s", buildOut)

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	defer ln.Close()

	var _, tcpPort, splitErr = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, splitErr)

	var ctx, cancel = context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var cmd = exec.CommandContext(ctx, binary, "-h", "127.0.0.1", "-p", tcpPort) //nolint:gosec

	// Something to send, then hold stdin open until the TNC has replied.
	var stdin, stdinErr = cmd.StdinPipe()
	require.NoError(t, stdinErr)

	var stdout, stdoutErr = cmd.StdoutPipe()
	require.NoError(t, stdoutErr)

	require.NoError(t, cmd.Start())

	var lines = make(chan string, 100)

	go func() {
		defer close(lines)

		var scanner = bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	var tnc, acceptErr = ln.Accept()
	require.NoError(t, acceptErr)

	defer tnc.Close()

	var _, writeErr = stdin.Write([]byte("Q1TEST>APDW17:>Outbound\n"))
	require.NoError(t, writeErr)

	var want = direwolf.KissEncapsulate(append([]byte{0x00}, testFrame(t, "Q1TEST>APDW17:>Outbound")...))

	assert.Equal(t, want, readKISS(t, tnc, len(want)))

	var _, replyErr = tnc.Write(direwolf.KissEncapsulate(append([]byte{0x10}, testFrame(t, "Q2TEST>APDW17:>Inbound")...)))
	require.NoError(t, replyErr)

	// Wait for it to print that before stdin closing ends the process.
	var printed []string

	for line := range lines {
		printed = append(printed, line)

		if line == "[1] Q2TEST>APDW17:>Inbound" {
			break
		}
	}

	assert.Contains(t, printed, "[1] Q2TEST>APDW17:>Inbound")

	require.NoError(t, stdin.Close())

	// Drain what's left so Wait can finish.
	for range lines {
	}

	require.NoError(t, cmd.Wait())
}
