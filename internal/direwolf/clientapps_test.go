// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/stretchr/testify/assert"
)

// What we hear goes to a client application on each KISS transport, as a data
// frame for the channel it was heard on.
func TestClientAppsSendRecPacket(t *testing.T) {
	t.Parallel()

	var kns, netClients = newAttachedKissNet(t, -1, false, 1)
	var kp, ptClient = startKissPT(t, 0)
	var ks, serialClient = openKissSerialPort(t, 0)

	var apps = new(clientApps)
	apps.kissNet = kns
	apps.kissSerial = ks
	apps.kissPT = kp

	var pp = newTestPacket(t)

	apps.SendRecPacket(1, pp)

	var want = kiss.Encapsulate(append([]byte{1<<4 | kiss.CmdDataFrame}, pp.Pack()...))

	assert.Equal(t, want, readKissNetFrame(t, netClients[0]), "KISS TCP")
	assert.Equal(t, want, readKissFrame(t, ptClient), "KISS pseudo terminal")
	assert.Equal(t, want, readSerialKissFrame(t, serialClient), "KISS serial port")
}

// None of the client applications need be there, and nor need the lot of them.
func TestClientAppsSendRecPacketToNone(t *testing.T) {
	t.Parallel()

	var pp = newTestPacket(t)

	assert.NotPanics(t, func() {
		new(clientApps).SendRecPacket(0, pp)

		var apps *clientApps

		apps.SendRecPacket(0, pp)
	})
}
