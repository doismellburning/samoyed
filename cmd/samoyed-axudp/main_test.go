// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

// main runs until interrupted, and exits the process on the way, so these run
// the built command as a process of its own.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildAxudp builds the command under test.
func buildAxudp(t *testing.T) string {
	t.Helper()

	var binary = filepath.Join(t.TempDir(), "samoyed-axudp")

	var build = exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".") //nolint:gosec

	var out, err = build.CombinedOutput()
	require.NoError(t, err, "Building the command failed: %s", out)

	return binary
}

// freePort finds a port nothing is listening on, for the command to take.
func freePort(t *testing.T, network string) int {
	t.Helper()

	switch network {
	case "udp":
		var conn, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
		require.NoError(t, err)

		defer conn.Close()

		return conn.LocalAddr().(*net.UDPAddr).Port //nolint:forcetypeassert
	default:
		var ln, err = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)

		defer ln.Close()

		return ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert
	}
}

// testFrame builds the AX.25 frame described by a monitoring format string.
func testFrame(t *testing.T, monitor string) []byte {
	t.Helper()

	var pp = direwolf.AX25FromText(monitor, true)
	require.NotNil(t, pp)

	return direwolf.AX25Pack(pp)
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "axudp.yaml")
	require.NoError(t, os.WriteFile(name, []byte(content), 0o600))

	return name
}

func Test_main_bridges(t *testing.T) {
	var binary = buildAxudp(t)

	// The remote AXUDP node, played by the test.
	var remote, remoteErr = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, remoteErr)

	defer remote.Close()

	var remotePort = remote.LocalAddr().(*net.UDPAddr).Port //nolint:forcetypeassert

	var config = writeConfig(t, fmt.Sprintf("maps:\n  - ax25addr: Q2TEST\n    host: 127.0.0.1\n    port: %d\n", remotePort))

	var udpPort = freePort(t, "udp")
	var kissPort = freePort(t, "tcp")

	var cmd = exec.CommandContext(t.Context(), binary, //nolint:gosec
		"--config", config, "--udpport", strconv.Itoa(udpPort), "--kissport", strconv.Itoa(kissPort))

	var stdout, stdoutErr = cmd.StdoutPipe()
	require.NoError(t, stdoutErr)

	require.NoError(t, cmd.Start())

	var output strings.Builder

	var scanner = bufio.NewScanner(stdout)
	for scanner.Scan() {
		output.WriteString(scanner.Text() + "\n")

		if strings.Contains(scanner.Text(), "KISS TCP server listening") {
			break
		}
	}

	assert.Contains(t, output.String(), "WARNING: this is beta software")
	assert.Contains(t, output.String(), fmt.Sprintf("Q2TEST -> 127.0.0.1:%d", remotePort))

	var kiss, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(kissPort)))
	require.NoError(t, dialErr)

	defer kiss.Close()

	// KISS to AXUDP: a frame for Q2TEST goes to the node mapped to it, with
	// a checksum on the end.
	var outbound = testFrame(t, "Q1TEST>Q2TEST:>Outbound")

	var _, writeErr = kiss.Write(direwolf.KissEncapsulate(append([]byte{0x00}, outbound...)))
	require.NoError(t, writeErr)

	require.NoError(t, remote.SetReadDeadline(time.Now().Add(5*time.Second)))

	var datagram = make([]byte, 1024)

	var n, from, readErr = remote.ReadFrom(datagram)
	require.NoError(t, readErr)

	datagram = datagram[:n]

	require.Len(t, datagram, len(outbound)+2)
	assert.Equal(t, outbound, datagram[:len(outbound)])
	assert.Equal(t, udpPort, from.(*net.UDPAddr).Port, "should be sent from the port it listens on") //nolint:forcetypeassert

	// AXUDP to KISS: sending that same datagram back arrives at the KISS
	// client, checksum stripped.
	var _, sendErr = remote.WriteTo(datagram, from)
	require.NoError(t, sendErr)

	var want = direwolf.KissEncapsulate(append([]byte{0x00}, outbound...))

	require.NoError(t, kiss.SetReadDeadline(time.Now().Add(5*time.Second)))

	var got = make([]byte, len(want))

	var _, kissReadErr = io.ReadFull(kiss, got)
	require.NoError(t, kissReadErr)

	assert.Equal(t, want, got)

	// An interrupt is how it is meant to stop, so it isn't a failure.
	require.NoError(t, cmd.Process.Signal(os.Interrupt))

	for scanner.Scan() { // Drain what's left so Wait can finish.
	}

	require.NoError(t, cmd.Wait())
}

func Test_main_fails(t *testing.T) {
	var binary = buildAxudp(t)

	var testCases = map[string]struct {
		args func(t *testing.T) []string
		want string
	}{
		"missing config": {
			func(t *testing.T) []string {
				t.Helper()

				return []string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}
			},
			"samoyed-axudp: reading config:",
		},
		"UDP port taken": {
			func(t *testing.T) []string {
				t.Helper()

				var taken, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", ":0")
				require.NoError(t, err)

				t.Cleanup(func() { taken.Close() })

				return []string{
					"--config", writeConfig(t, "maps: []\n"),
					"--udpport", strconv.Itoa(taken.LocalAddr().(*net.UDPAddr).Port), //nolint:forcetypeassert
				}
			},
			"UDP listen on port",
		},
		"KISS port taken": {
			func(t *testing.T) []string {
				t.Helper()

				var taken, err = new(net.ListenConfig).Listen(t.Context(), "tcp", ":0")
				require.NoError(t, err)

				t.Cleanup(func() { taken.Close() })

				return []string{
					"--config", writeConfig(t, "maps: []\n"),
					"--udpport", strconv.Itoa(freePort(t, "udp")),
					"--kissport", strconv.Itoa(taken.Addr().(*net.TCPAddr).Port), //nolint:forcetypeassert
				}
			},
			"TCP listen on port",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var ctx, cancel = context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			var cmd = exec.CommandContext(ctx, binary, tc.args(t)...) //nolint:gosec

			var out, err = cmd.CombinedOutput()

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, "output: %s", out)
			assert.Equal(t, 1, exitErr.ExitCode())
			assert.Contains(t, string(out), tc.want)
		})
	}
}

func Test_main_help(t *testing.T) {
	var binary = buildAxudp(t)

	var out, err = exec.CommandContext(t.Context(), binary, "--help").CombinedOutput() //nolint:gosec
	require.NoError(t, err)

	assert.Contains(t, string(out), "AXUDP bridge for samoyed-direwolf")
	assert.Contains(t, string(out), "--kissport")
}
