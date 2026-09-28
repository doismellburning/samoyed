// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingLinkClient notes what the data link reports to it.
type recordingLinkClient struct {
	established []string
	terminated  []string
	data        [][]byte
	pids        []int
}

func (r *recordingLinkClient) LinkEstablished(_ int, _ int, remoteCall string, _ string, _ bool) {
	r.established = append(r.established, remoteCall)
}

func (r *recordingLinkClient) LinkTerminated(_ int, _ int, remoteCall string, _ string, _ bool) {
	r.terminated = append(r.terminated, remoteCall)
}

func (r *recordingLinkClient) RecConnData(_ int, _ int, _ string, _ string, pid int, data []byte) {
	r.pids = append(r.pids, pid)
	r.data = append(r.data, append([]byte(nil), data...))
}

func (r *recordingLinkClient) OutstandingFramesReply(int, int, string, string, int) {}

// useInternalLinkClient registers c for client for the length of the test.
func useInternalLinkClient(t *testing.T, client int, c linkClient) {
	t.Helper()

	setInternalLinkClient(client, c)
	t.Cleanup(func() { setInternalLinkClient(client, nil) })
}

// An internal client that registers a callsign gets an incoming connection
// for it, and the data that arrives on it, rather than those going to the AGW
// server, which has no slot for that client number.
func TestInternalLinkClientReceivesIncomingConnection(t *testing.T) {
	const (
		MY_CALL    = "Q1TEST-7"
		THEIR_CALL = "Q2TEST-7"
		CHANNEL    = 0
	)

	setupTestEnv(t)

	var rec = new(recordingLinkClient)
	useInternalLinkClient(t, netromClient, rec)

	var reg = new(dlq_item_t)
	reg._chan = CHANNEL
	reg.client = netromClient
	reg.addrs[0] = MY_CALL
	dl_register_callsign(reg)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = THEIR_CALL
	addrs[ax25.Destination] = MY_CALL

	var sabm = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUSABM, 1, 0, nil)
	require.NotNil(t, sabm)
	receiveFrame(t, sabm, CHANNEL)

	require.Equal(t, []string{THEIR_CALL}, rec.established)

	var info = ax25.IFrame(addrs, 2, ax25.CRCmd, ax25.Modulo8, 0, 0, 0, ax25.PIDNetROM, []byte("layer 3"))
	require.NotNil(t, info)
	receiveFrame(t, info, CHANNEL)

	require.Len(t, rec.data, 1)
	assert.Equal(t, []byte("layer 3"), rec.data[0])
	assert.Equal(t, []int{ax25.PIDNetROM}, rec.pids)
}

// An internal client number with nothing registered must not reach the AGW
// server, whose per-client arrays stop at MAX_NET_CLIENTS.
func TestLinkClientForUnregisteredInternalClient(t *testing.T) {
	var c = linkClientFor(netromClient)

	assert.IsType(t, discardLinkClient{}, c)
	assert.NotPanics(t, func() { c.RecConnData(0, netromClient, "Q2TEST", "Q1TEST", ax25.PIDNetROM, []byte("x")) })
}

// A network client number still goes to the AGW server.
func TestLinkClientForNetworkClient(t *testing.T) {
	var saved = agwServer
	t.Cleanup(func() { agwServer = saved })

	agwServer = new(AGWServer)

	assert.Same(t, agwServer, linkClientFor(0))
}
