// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hamlib

// Integration test against a real rigctld (from Hamlib's utilities) fronting
// the dummy rig, so PTT goes over Hamlib's NET rigctl backend and is read back
// over a second connection to rigctld - the hardware-free check that Hamlib
// PTT works end to end.

import (
	"bufio"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// netRigctlModel is Hamlib's NET rigctl backend, which talks to rigctld.
const netRigctlModel = 2

// rigctldClient is a connection to rigctld that reads its state independently
// of the rig under test.
type rigctldClient struct {
	conn net.Conn
	r    *bufio.Reader
}

// startRigctld runs rigctld for the dummy rig on a free port and returns its
// address, and a client connected to it, once it is listening.
//
// The client that saw it listening stays connected until the test ends: when
// rigctld 4.5 drops a client it closes that socket three times over (fclose on
// two FILEs sharing the descriptor, then close), and a client accepted in
// between gets the reused descriptor closed under it - the rig under test,
// connecting straight after a probe that hung up, failed rig_open that way.
func startRigctld(t *testing.T) (string, *rigctldClient) {
	t.Helper()

	var rigctld, err = exec.LookPath("rigctld")
	if err != nil {
		t.Skip("rigctld not installed (libhamlib-utils)")
	}

	var l net.Listener
	l, err = new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var port = l.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert
	require.NoError(t, l.Close())

	var cmd = exec.CommandContext(t.Context(), rigctld, //nolint:gosec // G204: rigctld from PATH, with our own arguments.
		"-m", strconv.Itoa(dummyModel), "-P", "RIG", "-T", "127.0.0.1", "-t", strconv.Itoa(port))
	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
	})

	var addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	for range 50 {
		var conn, dialErr = new(net.Dialer).DialContext(t.Context(), "tcp", addr)
		if dialErr == nil {
			t.Cleanup(func() { conn.Close() })

			return addr, &rigctldClient{conn: conn, r: bufio.NewReader(conn)}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("rigctld never started listening on %s", addr)

	return "", nil
}

// ptt asks rigctld what PTT is.
func (c *rigctldClient) ptt(t *testing.T) string {
	t.Helper()

	require.NoError(t, c.conn.SetDeadline(time.Now().Add(5*time.Second)))

	var _, err = c.conn.Write([]byte("t\n"))
	require.NoError(t, err)

	var line string
	line, err = c.r.ReadString('\n')
	require.NoError(t, err)

	return strings.TrimSpace(line)
}

func TestRigctldPTT(t *testing.T) {
	t.Parallel()

	var addr, observer = startRigctld(t)

	var r, err = NewRig(netRigctlModel)
	require.NoError(t, err)

	require.NoError(t, r.SetConf("rig_pathname", addr))
	require.NoError(t, r.Open())

	t.Cleanup(func() {
		r.Close()   //nolint:errcheck
		r.Cleanup() //nolint:errcheck
	})

	require.NoError(t, r.SetPTT(true))
	assert.Equal(t, "1", observer.ptt(t))

	require.NoError(t, r.SetPTT(false))
	assert.Equal(t, "0", observer.ptt(t))
}
