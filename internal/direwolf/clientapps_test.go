// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/kiss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What we hear goes to a client application on each KISS transport, as a data
// frame for the channel it was heard on.
func TestClientAppsSendRecPacket(t *testing.T) {
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
	var pp = newTestPacket(t)

	assert.NotPanics(t, func() {
		new(clientApps).SendRecPacket(0, pp)

		var apps *clientApps

		apps.SendRecPacket(0, pp)
	})
}

// The APRStt gateway sends its object reports to the client applications it
// was handed, when configured to, as though it had heard them.
func TestTTGatewaySendsObjectReportsToClientApps(t *testing.T) {
	var cfg tt_config_s
	cfg.obj_send_to_app = 1
	cfg.obj_recv_chan = 0
	cfg.obj_xmit_chan = -1

	var kns, clients = newAttachedKissNet(t, -1, false, 1)

	var apps = new(clientApps)
	apps.kissNet = kns

	var gw = NewTTGateway(new(RadioConfig), &cfg, apps, nil, nil, nil, nil, 0)

	var report = "Q1TEST>APRS:;Q2TEST   *111111z4237.14N/07120.83W-"

	var pp = ax25.FromText(report, true)
	require.NotNil(t, pp)

	gw.users.sendObjectReport(report, true)

	assert.Equal(t,
		kiss.Encapsulate(append([]byte{kiss.CmdDataFrame}, pp.Pack()...)),
		readKissNetFrame(t, clients[0]))
}
