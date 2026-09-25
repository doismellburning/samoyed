// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgwlibCommands(t *testing.T) {
	var tnc = newTestServer(t)

	require.NoError(t, agwlib_X_register_callsign(1, testMyCall))
	require.NoError(t, agwlib_x_unregister_callsign(1, testMyCall))
	require.NoError(t, agwlib_G_ask_port_information())
	require.NoError(t, agwlib_C_connect(2, testMyCall, testTheirCall))

	assert.Equal(t, []tncFrame{
		{kind: 'X', channel: 1, callFrom: testMyCall, callTo: Callsign{}, data: ""},
		{kind: 'x', channel: 1, callFrom: testMyCall, callTo: Callsign{}, data: ""},
		{kind: 'G', channel: 0, callFrom: Callsign{}, callTo: Callsign{}, data: ""},
		{kind: 'C', channel: 2, callFrom: testMyCall, callTo: testTheirCall, data: ""},
	}, tnc.frames(t))
}

// fromTNC builds a command as tnc_listen_thread would hand it on.
func fromTNC(kind byte, channel byte, callFrom Callsign, callTo Callsign, data string) *AGWPECommand {
	var h = new(AGWPEHeader)

	h.DataKind = kind
	h.Portx = channel
	h.CallFrom = callFrom
	h.CallTo = callTo
	h.DataLen = uint32(len(data))

	return &AGWPECommand{Header: h, Data: []byte(data)}
}

func TestProcessFromTNCFollowsASession(t *testing.T) {
	var tnc = newTestServer(t)

	// The other station connects, and is greeted.
	process_from_tnc(fromTNC('C', 0, testTheirCall, testMyCall, "*** CONNECTED To Station Q2TEST\r"))

	var s = srv.findSession(0, testTheirCall)
	require.NotNil(t, s)
	assert.NotEmpty(t, sentText(tnc.frames(t)))

	// What it sends is taken as a command.
	process_from_tnc(fromTNC('D', 0, testTheirCall, testMyCall, "help\r"))
	assert.Contains(t, sentText(tnc.frames(t)), "WHO")

	// Things the appserver has no use for are ignored.
	for _, kind := range []byte{'R', 'g', 'K', 'U', 'y', '?'} {
		process_from_tnc(fromTNC(kind, 0, testTheirCall, testMyCall, "ignored"))
	}

	assert.Empty(t, tnc.frames(t))

	// Once it disconnects, the session is gone.
	process_from_tnc(fromTNC('d', 0, testTheirCall, testMyCall, "*** DISCONNECTED From Station Q2TEST\r"))
	assert.Nil(t, srv.findSession(0, testTheirCall))
}

// An outgoing connection, accepted by the other station, also starts a
// session.
func TestProcessFromTNCOutgoingConnection(t *testing.T) {
	var tnc = newTestServer(t)

	process_from_tnc(fromTNC('C', 1, testTheirCall, testMyCall, "*** CONNECTED With Station Q2TEST\r"))

	assert.NotNil(t, srv.findSession(1, testTheirCall))

	tnc.frames(t)
}

func TestPortInformationRejectsBadDescriptions(t *testing.T) {
	var tnc = newTestServer(t)

	agw_cb_G_port_information(3, []string{"Port2 second", "Wibble wobble", "Port99 too far"})

	assert.Equal(t, []tncFrame{{kind: 'X', channel: 1, callFrom: testMyCall, callTo: Callsign{}, data: ""}}, tnc.frames(t))
}
