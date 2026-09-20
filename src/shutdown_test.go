// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// The network listeners all follow the same start/stop contract: a cancelled
// context closes the listening socket, the goroutines sitting in Accept and
// Read return, and the port is given up there and then.  The tests live
// together because it is one contract, and because being able to write them
// at all is the point - a listener that could not be stopped could not be
// started by a test either, which is why these files had no coverage.  Refs
// #650.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitUntilListening waits for something to accept connections on port.
func waitUntilListening(t *testing.T, port int) {
	t.Helper()

	require.Eventually(t, func() bool {
		var conn, err = new(net.Dialer).DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return false
		}

		conn.Close()

		return true
	}, 5*time.Second, 10*time.Millisecond, "nothing started listening on port %d", port)
}

// requirePortFree checks that port can be bound again, which it cannot be
// while a listener that was never closed still holds it.
func requirePortFree(t *testing.T, port int) {
	t.Helper()

	require.Eventually(t, func() bool {
		var listener, err = new(net.ListenConfig).Listen(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return false
		}

		listener.Close()

		return true
	}, 5*time.Second, 10*time.Millisecond, "port %d is still held after cancellation", port)
}

func TestServerConnectListenThreadStopsWhenCancelled(t *testing.T) {
	var port = freeTCPPort(t)

	// The listener attaches the probe connection waitUntilListening makes to
	// this server, and no cmdListenThread is running here to notice it go away
	// again.  Nothing else can see that, the server being this test's own, but
	// the socket is real: left alone it sits in CLOSE_WAIT for the rest of the
	// run, because a net.Conn is only closed by a finalizer if at all.
	var s = new(AGWServer)

	t.Cleanup(func() {
		for c := range MAX_NET_CLIENTS {
			var conn = s.clientConn(c)
			if conn != nil {
				conn.Close()
			}
		}
	})

	var ctx, cancel = context.WithCancel(t.Context())

	var stopped = make(chan struct{})

	go func() {
		defer close(stopped)

		s.connectListenThread(ctx, port)
	}()

	waitUntilListening(t, port)

	cancel()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("connectListenThread did not return after its context was cancelled")
	}

	requirePortFree(t, port)
}

func TestKissNetServiceStopsWhenCancelled(t *testing.T) {
	var port = freeTCPPort(t)

	var mc = new(misc_config_s)
	mc.kiss_port[0] = port
	mc.kiss_chan[0] = -1

	var ctx, cancel = context.WithCancel(t.Context())

	NewKissNetService(ctx, mc)

	waitUntilListening(t, port)

	// An attached client is what the per-client read threads are blocked on,
	// so it is how we can tell they noticed the cancellation rather than only
	// the thread doing the accepting.
	var client, dialErr = new(net.Dialer).DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, dialErr)

	defer client.Close()

	cancel()

	require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))

	// Being hung up on is what we are checking for, not how the hanging up
	// reads: a closed socket gives the other end EOF or, if the stack sends a
	// reset instead - which macOS does readily - ECONNRESET.  Either is the
	// far end going away; only the read deadline expiring means it didn't.
	var buf = make([]byte, 1)
	var _, readErr = client.Read(buf)
	require.Error(t, readErr, "the KISS TCP client was not disconnected when the service was stopped")

	var netErr net.Error
	require.False(t, errors.As(readErr, &netErr) && netErr.Timeout(),
		"the KISS TCP client was not disconnected when the service was stopped: %v", readErr)

	requirePortFree(t, port)
}
