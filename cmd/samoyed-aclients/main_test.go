// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux || darwin

package main

// main never returns, and its client goroutines exit the process when a TNC
// goes away, so these run it as a process of its own, against TNCs played by
// the test.

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/doismellburning/samoyed/internal/testutils"
	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// acceptMonitor accepts the connection from a client on ln, as an AGW TNC
// would, and checks it asks to monitor raw frames.
func acceptMonitor(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()

	var conn = testutils.Accept(t, ln)

	var header direwolf.AGWPEHeader
	require.NoError(t, binary.Read(conn, binary.LittleEndian, &header))
	require.Equal(t, byte('k'), header.DataKind, "should ask for raw frames")

	return conn
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

func Test_main(t *testing.T) {
	t.Run("network and serial", func(t *testing.T) {
		var ln, port = testutils.Listen(t)

		var master, slave, openErr = pty.Open()
		require.NoError(t, openErr)

		t.Cleanup(func() { master.Close() })

		var serialPort = slave.Name()
		require.NoError(t, slave.Close())

		var p = testutils.StartMain(t, "127.0.0.1:"+port+"=Network", serialPort+"=Serial")

		var tnc = acceptMonitor(t, ln)

		p.WaitFor(t, "Client 1 now connected to Serial on "+serialPort)

		sendMonitored(t, tnc, 0, direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Over the network")))

		var seen = p.WaitFor(t, "Q1TEST>APDW17:>Over the network")

		// Only the first port heard from counts, as a TNC can report the
		// same thing on more than one.
		sendMonitored(t, tnc, 1, direwolf.AX25Pack(direwolf.MustAX25FromText("Q1TEST>APDW17:>Ignored")))

		// Something the far end can't make sense of is reported, and the
		// client carries on.
		sendMonitored(t, tnc, 0, []byte{0x01, 0x02})

		seen = append(seen, p.WaitFor(t, "Client 0 got invalid AX.25 frame from Network.")...)

		// A monitoring TNC on a serial port breaks the line after the
		// addresses, which is put back together.
		var _, writeErr = master.WriteString("Q2TEST>APDW17: <UI>:\r\n>Over the wire\r\n")
		require.NoError(t, writeErr)

		seen = append(seen, p.WaitFor(t, "Q2TEST>APDW17: <UI>:>Over the wire")...)

		assert.NotContains(t, strings.Join(seen, "\n"), "Ignored")
	})

	t.Run("unreachable", func(t *testing.T) {
		var p = testutils.StartMain(t, "127.0.0.1:"+testutils.UnusedPort(t)+"=Gone")

		p.WaitFor(t, "Client 0 unable to connect to Gone")
		assert.Equal(t, 1, p.Wait())
	})

	t.Run("closed", func(t *testing.T) {
		var ln, port = testutils.Listen(t)

		var p = testutils.StartMain(t, "127.0.0.1:"+port+"=Closing")

		require.NoError(t, acceptMonitor(t, ln).Close())

		p.WaitFor(t, "Client 0 connection to Closing closed.")
		assert.Equal(t, 1, p.Wait())
	})
}

func Test_main_badArguments(t *testing.T) {
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
			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 1, result.Status)
			assert.Contains(t, result.Output(), tc.want)
		})
	}
}
