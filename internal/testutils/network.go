// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package testutils

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Listen listens on a free TCP port on the loopback interface, closing it when
// the test ends, and returns it along with the port number as text.
func Listen(t *testing.T) (net.Listener, string) {
	t.Helper()

	var ln, err = new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { ln.Close() })

	var _, port, splitErr = net.SplitHostPort(ln.Addr().String())
	require.NoError(t, splitErr)

	return ln, port
}

// UnusedPort returns, as text, the number of a TCP port on the loopback
// interface that nothing is listening on, for a test of what happens when
// there is nothing there.
func UnusedPort(t *testing.T) string {
	t.Helper()

	var ln, port = Listen(t)
	require.NoError(t, ln.Close())

	return port
}

// Accept accepts a connection on ln, failing the test if none comes in good
// time.  The connection is closed when the test ends, and has a deadline of
// ProcessTimeout for anything the test reads or writes.
func Accept(t *testing.T, ln net.Listener) net.Conn {
	t.Helper()

	if tcp, ok := ln.(*net.TCPListener); ok {
		require.NoError(t, tcp.SetDeadline(time.Now().Add(ProcessTimeout)))
	}

	var conn, err = ln.Accept()
	require.NoError(t, err, "no connection came in")

	t.Cleanup(func() { conn.Close() })

	require.NoError(t, conn.SetDeadline(time.Now().Add(ProcessTimeout)))

	return conn
}
