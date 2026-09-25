// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package testutils

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenAndAccept(t *testing.T) {
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

func TestUnusedPort(t *testing.T) {
	var _, err = new(net.Dialer).DialContext(t.Context(), "tcp4", net.JoinHostPort("127.0.0.1", UnusedPort(t)))
	assert.Error(t, err)
}
