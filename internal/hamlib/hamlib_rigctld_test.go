// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package hamlib

// Integration test against a real rigctld (from Hamlib's utilities) fronting
// the dummy rig, so PTT goes over Hamlib's NET rigctl backend and is read back
// with rigctl - the hardware-free check that Hamlib PTT works end to end.

import (
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

// startRigctld runs rigctld for the dummy rig on a free port and returns its
// address once it is listening.
func startRigctld(t *testing.T) string {
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
			conn.Close()

			return addr
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("rigctld never started listening on %s", addr)

	return ""
}

// rigctlPTT asks rigctld what PTT is, independently of the rig under test.
func rigctlPTT(t *testing.T, addr string) string {
	t.Helper()

	var out, err = exec.CommandContext(t.Context(), "rigctl", //nolint:gosec // G204: our own rigctld's address.
		"-m", strconv.Itoa(netRigctlModel), "-r", addr, "t").Output()
	require.NoError(t, err)

	return strings.TrimSpace(string(out))
}

func TestRigctldPTT(t *testing.T) {
	t.Parallel()

	var addr = startRigctld(t)

	var r, err = NewRig(netRigctlModel)
	require.NoError(t, err)

	require.NoError(t, r.SetConf("rig_pathname", addr))
	require.NoError(t, r.Open())

	t.Cleanup(func() {
		r.Close()   //nolint:errcheck
		r.Cleanup() //nolint:errcheck
	})

	require.NoError(t, r.SetPTT(true))
	assert.Equal(t, "1", rigctlPTT(t, addr))

	require.NoError(t, r.SetPTT(false))
	assert.Equal(t, "0", rigctlPTT(t, addr))
}
