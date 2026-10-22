//go:build unix

// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// newFullTestListener listens on a free loopback port with the smallest accept
// backlog the kernel allows and never accepts, so once a connection or two has
// filled the backlog, the kernel drops each further SYN without a word - just
// like a TNC host behind a firewall that silently drops them.  net.Listen
// can't be asked for a backlog, hence the raw socket.
func newFullTestListener(t *testing.T) int {
	t.Helper()

	var fd, socketErr = unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	require.NoError(t, socketErr)
	t.Cleanup(func() { _ = unix.Close(fd) })

	var addr = new(unix.SockaddrInet4)
	addr.Addr = [4]byte{127, 0, 0, 1}
	require.NoError(t, unix.Bind(fd, addr))
	require.NoError(t, unix.Listen(fd, 0))

	var bound, nameErr = unix.Getsockname(fd)
	require.NoError(t, nameErr)

	var inet4, ok = bound.(*unix.SockaddrInet4)
	require.True(t, ok)

	return inet4.Port
}

func TestDialNetTNCGivesUpOnATNCThatNeverAnswers(t *testing.T) {
	t.Parallel()

	var port = newFullTestListener(t)

	// Long enough that an attempt with no timeout of its own stands out,
	// short of the minutes the kernel would leave it waiting.
	var ctx, cancel = context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	const timeout = 200 * time.Millisecond

	// The first attempts may fill the backlog rather than hit it; how many
	// that takes is up to the kernel.
	for range 8 {
		var start = time.Now()
		var conn, dialErr = dialNetTNC(ctx, "127.0.0.1", port, timeout)
		var elapsed = time.Since(start)

		if dialErr == nil {
			t.Cleanup(func() { _ = conn.Close() })

			continue
		}

		var netErr net.Error
		if !errors.As(dialErr, &netErr) || !netErr.Timeout() {
			t.Skipf("This kernel refuses a connection to a full backlog rather than ignoring it: %v", dialErr)
		}

		require.NoError(t, ctx.Err(), "the attempt waited for the context, not its own timeout")
		assert.Less(t, elapsed, 5*time.Second)

		return
	}

	t.Skip("This kernel never let the backlog fill")
}
