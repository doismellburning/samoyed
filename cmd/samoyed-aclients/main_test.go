// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

// main never returns, and its client goroutines exit the process when a TNC
// goes away, so these run the built command as a process of its own, against
// TNCs played by the test.

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildAclients builds the command under test.
func buildAclients(t *testing.T) string {
	t.Helper()

	var binary = filepath.Join(t.TempDir(), "samoyed-aclients")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
}

// startAclients starts the built command and hands back a channel of the lines
// it prints.  It is killed when the test ends.
func startAclients(t *testing.T, binary string, args ...string) <-chan string {
	t.Helper()

	var ctx, cancel = context.WithCancel(context.Background())

	var cmd = exec.CommandContext(ctx, binary, args...) //nolint:gosec

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

	t.Cleanup(func() {
		cancel()

		for range lines { // Drain what's left so Wait can finish.
		}

		_ = cmd.Wait()
	})

	return lines
}

// waitFor reads lines until one contains want, failing the test if none does
// in good time.  It hands back every line it read.
func waitFor(t *testing.T, lines <-chan string, want string) []string {
	t.Helper()

	var timeout = time.After(10 * time.Second)

	var seen []string

	for {
		select {
		case line, ok := <-lines:
			require.True(t, ok, "output ended without %q", want)

			seen = append(seen, line)

			if strings.Contains(line, want) {
				return seen
			}
		case <-timeout:
			require.FailNow(t, "timed out waiting for output", "wanted %q", want)
		}
	}
}

// testFrame builds the AX.25 frame described by a monitoring format string.
func testFrame(t *testing.T, monitor string) []byte {
	t.Helper()

	var pp = direwolf.AX25FromText(monitor, true)
	require.NotNil(t, pp)

	return direwolf.AX25Pack(pp)
}

// fakeAGW listens as an AGW TNC would, and hands back its address along with
// a channel that delivers the connection once a client has asked to monitor
// raw frames.
func fakeAGW(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()

	var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { ln.Close() })

	var conns = make(chan net.Conn, 1)

	go func() {
		var conn, err = ln.Accept()
		if err != nil {
			return
		}

		t.Cleanup(func() { conn.Close() })

		var header direwolf.AGWPEHeader

		err = binary.Read(conn, binary.LittleEndian, &header)
		if err != nil || header.DataKind != 'k' {
			return
		}

		conns <- conn
	}()

	return ln.Addr().String(), conns
}

// sendMonitored sends frame to a client as a raw monitored frame on portx.
func sendMonitored(t *testing.T, conn net.Conn, portx byte, frame []byte) {
	t.Helper()

	var data = append([]byte{portx << 4}, frame...)

	var header = new(direwolf.AGWPEHeader)
	header.Portx = portx
	header.DataKind = 'K'
	header.DataLen = uint32(len(data))

	require.NoError(t, binary.Write(conn, binary.LittleEndian, header))

	var _, err = conn.Write(data)
	require.NoError(t, err)
}

func accept(t *testing.T, conns <-chan net.Conn) net.Conn {
	t.Helper()

	select {
	case conn := <-conns:
		return conn
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for the client to connect")

		return nil
	}
}

func Test_main(t *testing.T) {
	var binary = buildAclients(t)

	t.Run("network and serial", func(t *testing.T) {
		var addr, conns = fakeAGW(t)

		var master, slave, openErr = pty.Open()
		require.NoError(t, openErr)

		t.Cleanup(func() { master.Close() })

		var serialPort = slave.Name()
		require.NoError(t, slave.Close())

		var lines = startAclients(t, binary, addr+"=Network", serialPort+"=Serial")

		waitFor(t, lines, "Client 1 now connected to Serial on "+serialPort)

		var tnc = accept(t, conns)

		sendMonitored(t, tnc, 0, testFrame(t, "Q1TEST>APDW17:>Over the network"))

		waitFor(t, lines, "Q1TEST>APDW17:>Over the network")

		// Only the first port heard from counts, as a TNC can report the
		// same thing on more than one.
		sendMonitored(t, tnc, 1, testFrame(t, "Q1TEST>APDW17:>Ignored"))

		// Something the far end can't make sense of is reported, and the
		// client carries on.
		sendMonitored(t, tnc, 0, []byte{0x01, 0x02})

		var seen = waitFor(t, lines, "Client 0 got invalid AX.25 frame from Network.")

		// A monitoring TNC on a serial port breaks the line after the
		// addresses, which is put back together.
		var _, writeErr = master.WriteString("Q2TEST>APDW17: <UI>:\r\n>Over the wire\r\n")
		require.NoError(t, writeErr)

		seen = append(seen, waitFor(t, lines, "Q2TEST>APDW17: <UI>:>Over the wire")...)

		assert.NotContains(t, strings.Join(seen, "\n"), "Ignored")
	})

	t.Run("unreachable", func(t *testing.T) {
		var ln, listenErr = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
		require.NoError(t, listenErr)

		var addr = ln.Addr().String()

		// Nothing is listening once this is closed.
		require.NoError(t, ln.Close())

		var lines = startAclients(t, binary, addr+"=Gone")

		waitFor(t, lines, "Client 0 unable to connect to Gone")
	})

	t.Run("closed", func(t *testing.T) {
		var addr, conns = fakeAGW(t)

		var lines = startAclients(t, binary, addr+"=Closing")

		require.NoError(t, accept(t, conns).Close())

		waitFor(t, lines, "Client 0 connection to Closing closed.")
	})
}

func Test_main_badArguments(t *testing.T) {
	var binary = buildAclients(t)

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"none":           {nil, "Specify up to 6 TNCs on the command line."},
		"too many":       {strings.Fields("1=a 2=b 3=c 4=d 5=e 6=f 7=g"), "Specify up to 6 TNCs on the command line."},
		"no description": {[]string{"8000"}, "Missing description after = in '8000'."},
		"empty port":     {[]string{"localhost:=Nowhere"}, "Port must not be empty for 'Nowhere'."},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var cmd = exec.CommandContext(t.Context(), binary, tc.args...) //nolint:gosec

			var out, err = cmd.Output()

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Equal(t, 1, exitErr.ExitCode())
			assert.Contains(t, string(out), tc.want)
		})
	}
}
