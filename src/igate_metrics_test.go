// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIgateDialCountsOnlySuccessfulConnections is a regression test: the
// connect counter used to be incremented before the dial error was checked, so
// a failed attempt bumped both samoyed_igate_connects_total and
// samoyed_igate_failed_connects_total, and the "successful connections" metric
// was really an attempts counter.
func TestIgateDialCountsOnlySuccessfulConnections(t *testing.T) {
	const (
		connects = "samoyed_igate_connects_total"
		failed   = "samoyed_igate_failed_connects_total"
	)

	var noLabels = map[string]string{}

	// A server that accepts.
	var listener, listenErr = new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)

	t.Cleanup(func() { listener.Close() }) //nolint:errcheck

	var addr = listener.Addr().(*net.TCPAddr) //nolint:forcetypeassert

	var connectsBefore = metricValue(t, connects, noLabels)
	var failedBefore = metricValue(t, failed, noLabels)

	var conn, dialErr = igate_dial(addr.IP.String(), addr.Port)
	require.NoError(t, dialErr)
	require.NotNil(t, conn)

	t.Cleanup(func() { conn.Close() }) //nolint:errcheck

	assert.InDelta(t, connectsBefore+1, metricValue(t, connects, noLabels), 0, "a connection was established")
	assert.InDelta(t, failedBefore, metricValue(t, failed, noLabels), 0, "nothing failed")

	// Now a port with nothing listening on it.
	var closedPort = freeTCPPort(t)

	connectsBefore = metricValue(t, connects, noLabels)
	failedBefore = metricValue(t, failed, noLabels)

	var badConn, badErr = igate_dial("127.0.0.1", closedPort)
	require.Error(t, badErr)
	assert.Nil(t, badConn)

	assert.InDelta(t, connectsBefore, metricValue(t, connects, noLabels), 0,
		"a failed attempt is not a connection")
	assert.InDelta(t, failedBefore+1, metricValue(t, failed, noLabels), 0, "the failure was counted")
}

// freeTCPPort returns a loopback port that was free a moment ago.
func freeTCPPort(t *testing.T) int {
	t.Helper()

	var l, err = new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var port = l.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert

	require.NoError(t, l.Close())

	return port
}
