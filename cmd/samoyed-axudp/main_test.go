// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

// main runs until interrupted, and exits the process on the way, so these run
// it as a process of its own.

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// unusedUDPPort finds a UDP port nothing is listening on, for the command to
// take.
func unusedUDPPort(t *testing.T) string {
	t.Helper()

	var conn, err = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer conn.Close()

	return strconv.Itoa(conn.LocalAddr().(*net.UDPAddr).Port) //nolint:forcetypeassert
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	var name = filepath.Join(t.TempDir(), "axudp.yaml")
	require.NoError(t, os.WriteFile(name, []byte(content), 0o600))

	return name
}

func Test_main_bridges(t *testing.T) {
	// The remote AXUDP node, played by the test.
	var remote, remoteErr = new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, remoteErr)

	defer remote.Close()

	var remotePort = remote.LocalAddr().(*net.UDPAddr).Port //nolint:forcetypeassert

	var config = writeConfig(t, fmt.Sprintf("maps:\n  - ax25addr: Q2TEST\n    host: 127.0.0.1\n    port: %d\n", remotePort))

	var udpPort = unusedUDPPort(t)
	var kissPort = testutils.UnusedPort(t)

	var p = testutils.StartMain(t, "--config", config, "--udpport", udpPort, "--kissport", kissPort)

	var output = strings.Join(p.WaitFor(t, "KISS TCP server listening"), "\n")

	assert.Contains(t, output, "WARNING: this is beta software")
	assert.Contains(t, output, fmt.Sprintf("Q2TEST -> 127.0.0.1:%d", remotePort))

	var kiss, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", kissPort))
	require.NoError(t, dialErr)

	defer kiss.Close()

	// KISS to AXUDP: a frame for Q2TEST goes to the node mapped to it, with
	// a checksum on the end.
	var outbound = direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>Q2TEST:>Outbound"))

	var _, writeErr = kiss.Write(direwolf.KissEncapsulate(append([]byte{0x00}, outbound...)))
	require.NoError(t, writeErr)

	require.NoError(t, remote.SetReadDeadline(time.Now().Add(5*time.Second)))

	var datagram = make([]byte, 1024)

	var n, from, readErr = remote.ReadFrom(datagram)
	require.NoError(t, readErr)

	datagram = datagram[:n]

	require.Len(t, datagram, len(outbound)+2)
	assert.Equal(t, outbound, datagram[:len(outbound)])
	assert.Equal(t, udpPort, strconv.Itoa(from.(*net.UDPAddr).Port), "should be sent from the port it listens on") //nolint:forcetypeassert

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
	p.Signal(t, os.Interrupt)

	assert.Equal(t, 0, p.Wait())
}

func Test_main_fails(t *testing.T) {
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
					"--udpport", unusedUDPPort(t),
					"--kissport", strconv.Itoa(taken.Addr().(*net.TCPAddr).Port), //nolint:forcetypeassert
				}
			},
			"TCP listen on port",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var result = testutils.RunMain(t, "", tc.args(t)...)

			assert.Equal(t, 1, result.Status, "output: %s", result.Output())
			assert.Contains(t, result.Output(), tc.want)
		})
	}
}

func Test_main_help(t *testing.T) {
	var result = testutils.RunMain(t, "", "--help")

	assert.Equal(t, 0, result.Status)
	assert.Contains(t, result.Output(), "AXUDP bridge for samoyed-direwolf")
	assert.Contains(t, result.Output(), "--kissport")
}
