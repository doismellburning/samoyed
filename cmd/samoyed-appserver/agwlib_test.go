// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
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

	// The TNC says how much is still waiting to go to the station, as a
	// 32-bit little-endian count.
	process_from_tnc(fromTNC('Y', 0, testMyCall, testTheirCall, "\x03\x01\x00\x00"))

	s.mu.Lock()
	assert.Equal(t, maybe.Just(259), s.txQueueLen)
	s.mu.Unlock()

	// One too short to hold a count is ignored.
	process_from_tnc(fromTNC('Y', 0, testMyCall, testTheirCall, "\x05"))

	s.mu.Lock()
	assert.Equal(t, maybe.Just(259), s.txQueueLen)
	s.mu.Unlock()

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

// The TNC's list of ports is answered by registering our callsign on each.
// It can leave gaps in the numbering, and "Port1" is channel 0.
func TestProcessFromTNCPortInformation(t *testing.T) {
	var tnc = newTestServer(t)

	process_from_tnc(fromTNC('G', 0, Callsign{}, Callsign{}, "2;Port1 first soundcard mono;Port3 second soundcard mono;"))

	assert.Equal(t, []tncFrame{
		{kind: 'X', channel: 0, callFrom: testMyCall, callTo: Callsign{}, data: ""},
		{kind: 'X', channel: 2, callFrom: testMyCall, callTo: Callsign{}, data: ""},
	}, tnc.frames(t))
}

// The list came from the TNC, so anything in it that isn't a port we can have
// is skipped, however short.
func TestPortInformationRejectsBadDescriptions(t *testing.T) {
	var tnc = newTestServer(t)

	agw_cb_G_port_information(10, []string{
		"Port2 second", "Wibble wobble", "Port99 too far", "Port0 before the first",
		"Port", "P", "", "Portx unnumbered", "Port+3 signed", "port16 last",
	})

	assert.Equal(t, []tncFrame{
		{kind: 'X', channel: 1, callFrom: testMyCall, callTo: Callsign{}, data: ""},
		{kind: 'X', channel: 15, callFrom: testMyCall, callTo: Callsign{}, data: ""},
	}, tnc.frames(t))
}

// An empty or malformed list registers nothing.
func TestProcessFromTNCEmptyPortInformation(t *testing.T) {
	var tnc = newTestServer(t)

	for _, data := range []string{"0;", "", ";;;"} {
		process_from_tnc(fromTNC('G', 0, Callsign{}, Callsign{}, data))
	}

	assert.Empty(t, tnc.frames(t))
}

// A connection report that isn't one of the two expected is ignored, however
// short it is.
func TestProcessFromTNCUnexpectedConnection(t *testing.T) {
	var tnc = newTestServer(t)

	for _, data := range []string{"*** CONNECTED", "", "Something else entirely, and long"} {
		process_from_tnc(fromTNC('C', 0, testTheirCall, testMyCall, data))
	}

	assert.Nil(t, srv.findSession(0, testTheirCall))
	assert.Empty(t, tnc.frames(t))
}
