// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !js

package dwgps

import (
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestGPSNMEA starts a GPSNMEA reader on a pseudo-terminal, returning the
// GPS, the name of its port and the master side, to play the receiver.
func openTestGPSNMEA(t *testing.T) (*GPS, string, *os.File) {
	t.Helper()

	var master, slave, openErr = pty.Open()
	require.NoError(t, openErr)

	// dwgpsnmea_init opens the slave by name, so we only need its name here.
	require.NoError(t, slave.Close())

	t.Cleanup(func() { master.Close() })

	var config = new(Config)
	config.NMEAPort = slave.Name()
	config.NMEASpeed = 4800

	var gps = new(GPS)

	require.Equal(t, 1, dwgpsnmea_init(t.Context(), gps, config, 0))

	return gps, slave.Name(), master
}

func TestSharedNMEAPort(t *testing.T) {
	var gps, name, _ = openTestGPSNMEA(t)

	assert.NotNil(t, gps.SharedNMEAPort(name, 4800), "the same port at the same speed is shared")
	assert.Nil(t, gps.SharedNMEAPort(name, 9600), "a different speed is not")
	assert.Nil(t, gps.SharedNMEAPort("/dev/Q1TEST", 4800), "nor is a different port")
	assert.Nil(t, new(GPS).SharedNMEAPort(name, 4800), "a GPS with no receiver has no port to share")
	assert.Nil(t, (*GPS)(nil).SharedNMEAPort(name, 4800), "nor does a nil GPS")
}

// When the receiver goes away, the reader goroutine closes the port and
// forgets it, while the waypoint sender may be asking for it on another
// goroutine.  The two used to meet in an unguarded package variable.
func TestSharedNMEAPortAfterReceiverIsLost(t *testing.T) {
	var gps, name, master = openTestGPSNMEA(t)

	require.NoError(t, master.Close())

	var deadline = time.Now().Add(5 * time.Second)
	for gps.Read().Fix != DWFIX_ERROR && time.Now().Before(deadline) {
		_ = gps.SharedNMEAPort(name, 4800)

		time.Sleep(time.Millisecond)
	}

	require.Equal(t, DWFIX_ERROR, gps.Read().Fix, "the reader never noticed the receiver had gone")
	assert.Nil(t, gps.SharedNMEAPort(name, 4800), "a closed port is not shared")
}
