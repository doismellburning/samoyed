// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package testutils

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenAndAccept(t *testing.T) {
	t.Parallel()

	var ln, port = Listen(t)

	var client, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp4", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, dialErr)

	defer client.Close()

	var server = Accept(t, ln)

	var _, writeErr = client.Write([]byte("hello"))
	require.NoError(t, writeErr)

	var buf = make([]byte, 5)

	var _, readErr = server.Read(buf)
	require.NoError(t, readErr)
	assert.Equal(t, "hello", string(buf))
}

//nolint:paralleltest // Serial: relies on a port staying unused, which a parallel test could take.
func TestUnusedPort(t *testing.T) {
	var _, err = new(net.Dialer).DialContext(t.Context(), "tcp4", net.JoinHostPort("127.0.0.1", UnusedPort(t)))
	assert.Error(t, err)
}

//nolint:paralleltest // Serial: relies on a port staying unused, which a parallel test could take.
func TestUnusedPortNumber(t *testing.T) {
	var _, err = new(net.Dialer).DialContext(t.Context(), "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(UnusedPortNumber(t))))
	assert.Error(t, err)
}
