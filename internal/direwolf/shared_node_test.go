// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sharedChannel       = 1
	sharedClient        = 7
	sharedNodeCall      = "Q1TEST-7"
	sharedUserCall      = "Q2TEST"
	sharedNeighbourCall = "Q3TEST-7"
	sharedNodeAlias     = "QNODEA"
)

// newTestSharedNode is a shared node in front of a recording data link, with
// a shell and a NET/ROM node that note what they are told.
func newTestSharedNode() (*sharedNode, *recordingAGWDataLink, *loggingLinkClients, *loggingLinkClients) {
	var link = new(recordingAGWDataLink)
	var shell = new(loggingLinkClients)
	var netrom = new(loggingLinkClients)

	var s = newSharedNode(link, sharedChannel, sharedNodeCall)
	s.attach(shell, noLinkClients{}, netrom)

	return s, link, shell, netrom
}

// sharedAddrs is the address list for a link from the node to remote.
func sharedAddrs(remote string) [ax25.MaxAddrs]string {
	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = sharedNodeCall
	addrs[ax25.Destination] = remote

	return addrs
}

func TestSharedNodeUser(t *testing.T) {
	t.Parallel()

	var s, _, shell, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, true)
	s.RecConnData(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, ax25.PIDNoLayer3, []byte("N\r"))
	s.RecConnData(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, ax25.PIDNetROM, []byte("later"))
	s.LinkTerminated(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, false)

	assert.Equal(t, []string{
		"established 1 7 Q2TEST Q1TEST-7 true",
		"data 1 7 Q2TEST Q1TEST-7 240 \"N\\r\"",
		"data 1 7 Q2TEST Q1TEST-7 207 \"later\"",
		"terminated 1 7 Q2TEST Q1TEST-7 false",
	}, shell.calls, "a user's link is the shell's, whatever arrives on it after the first")
	assert.Empty(t, netrom.calls)
}

func TestSharedNodeNeighbourConnects(t *testing.T) {
	t.Parallel()

	var s, _, shell, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("l3"))
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNoLayer3, []byte("text"))
	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.LinkTerminated(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)

	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q1TEST-7 true",
		"terminated 1 7 Q3TEST-7 Q1TEST-7 false",
	}, shell.calls, "the shell greets it, then forgets it")
	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q1TEST-7 true",
		"data 1 7 Q3TEST-7 Q1TEST-7 207 \"l3\"",
		"data 1 7 Q3TEST-7 Q1TEST-7 240 \"text\"",
		"established 1 7 Q3TEST-7 Q1TEST-7 true",
		"terminated 1 7 Q3TEST-7 Q1TEST-7 true",
	}, netrom.calls)
}

func TestSharedNodeForgetsLinkOnceDown(t *testing.T) {
	t.Parallel()

	var s, _, shell, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("l3"))
	s.LinkTerminated(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, false)

	shell.calls = nil
	netrom.calls = nil

	// The same station, connecting as a user this time.
	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNoLayer3, []byte("N\r"))

	assert.Len(t, shell.calls, 2)
	assert.Empty(t, netrom.calls)
}

func TestSharedNodeNetROMConnects(t *testing.T) {
	t.Parallel()

	var s, link, shell, netrom = newTestSharedNode()
	var netromLink = s.netromLink()

	netromLink.ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNetROM)
	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, false)
	netromLink.XmitDataRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNetROM, []byte("out"))
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("in"))
	s.LinkTerminated(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)

	assert.Equal(t, []string{
		"connect 1 7 Q1TEST-7>Q3TEST-7 0xcf",
		"data 1 7 Q1TEST-7>Q3TEST-7 \"out\"",
	}, link.taken())
	assert.Empty(t, shell.calls)
	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q1TEST-7 false",
		"data 1 7 Q3TEST-7 Q1TEST-7 207 \"in\"",
		"terminated 1 7 Q3TEST-7 Q1TEST-7 true",
	}, netrom.calls)
}

func TestSharedNodeShellConnectsOnward(t *testing.T) {
	t.Parallel()

	var s, link, shell, netrom = newTestSharedNode()
	var shellLink = s.shellLink()

	shellLink.ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNoLayer3)
	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, false)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("raw"))
	shellLink.DisconnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient)

	assert.Equal(t, []string{
		"connect 1 7 Q1TEST-7>Q3TEST-7 0xf0",
		"disconnect 1 7 Q1TEST-7>Q3TEST-7",
	}, link.taken())
	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q1TEST-7 false",
		"data 1 7 Q3TEST-7 Q1TEST-7 207 \"raw\"",
	}, shell.calls, "a link the shell made is its own, whatever arrives on it")
	assert.Empty(t, netrom.calls)
}

func TestSharedNodeShellCannotTakeNetROMLink(t *testing.T) {
	t.Parallel()

	var s, link, shell, _ = newTestSharedNode()

	s.netromLink().ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNetROM)
	link.taken()

	s.shellLink().ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNoLayer3)

	assert.Empty(t, link.taken())
	assert.Equal(t, []string{"terminated 1 7 Q3TEST-7 Q1TEST-7 false"}, shell.calls)
}

func TestSharedNodeNetROMCannotTakeUserLink(t *testing.T) {
	t.Parallel()

	var s, link, _, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNoLayer3, []byte("N\r"))

	s.netromLink().ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNetROM)

	assert.Empty(t, link.taken())
	assert.Equal(t, []string{"terminated 1 7 Q3TEST-7 Q1TEST-7 false"}, netrom.calls)
}

// A neighbour that has connected but not yet sent anything is NET/ROM's as
// soon as NET/ROM wants to send to it, on the link that is already up.
func TestSharedNodeNetROMTakesUndecidedLink(t *testing.T) {
	t.Parallel()

	var s, link, shell, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	shell.calls = nil

	s.netromLink().ConnectRequest(sharedAddrs(sharedNeighbourCall), 2, sharedChannel, sharedClient, ax25.PIDNetROM)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("l3"))

	assert.Empty(t, link.taken(), "no second connect for a link that is up")
	assert.Equal(t, []string{"terminated 1 7 Q3TEST-7 Q1TEST-7 false"}, shell.calls)
	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q1TEST-7 true",
		"data 1 7 Q3TEST-7 Q1TEST-7 207 \"l3\"",
	}, netrom.calls)
}

// NET/ROM sends from its callsign, so a link made to the alias is no link to
// NET/ROM, and stays the shell's whatever arrives on it.
func TestSharedNodeNetROMNotOnAlias(t *testing.T) {
	t.Parallel()

	var s, _, shell, netrom = newTestSharedNode()

	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeAlias, true)
	s.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeAlias, ax25.PIDNetROM, []byte("l3"))

	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 QNODEA true",
		"data 1 7 Q3TEST-7 QNODEA 207 \"l3\"",
	}, shell.calls)
	assert.Empty(t, netrom.calls)
}

func TestSharedNodeRegistration(t *testing.T) {
	t.Parallel()

	var s, link, _, _ = newTestSharedNode()

	s.netromLink().RegisterCallsign(sharedNodeCall, sharedChannel, sharedClient)
	s.shellLink().RegisterCallsign(sharedNodeCall, sharedChannel, sharedClient)

	assert.Equal(t, []string{"register Q1TEST-7 1 7"}, link.taken(), "the shell registers for both")
}

// A neighbour can only reach NET/ROM on its own channel: one connecting on
// another of the shell's channels is the shell's, whatever it sends.
func TestSharedNodeNetROMOnlyOnItsChannel(t *testing.T) {
	t.Parallel()

	var s, _, shell, netrom = newTestSharedNode()

	const otherChannel = sharedChannel + 1

	s.LinkEstablished(otherChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, true)
	s.RecConnData(otherChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, ax25.PIDNetROM, []byte("l3"))

	assert.Equal(t, []string{
		"established 2 7 Q3TEST-7 Q1TEST-7 true",
		"data 2 7 Q3TEST-7 Q1TEST-7 207 \"l3\"",
	}, shell.calls)
	assert.Empty(t, netrom.calls)
}

func TestSharedNodeCircuitReporter(t *testing.T) {
	t.Parallel()

	const agwClient = 3

	var s, _, _, _ = newTestSharedNode()
	var shellCircuits = new(loggingLinkClients)
	var agw = new(loggingLinkClients)

	s.attach(noLinkClients{}, shellCircuits, noLinkClients{})

	var r = s.circuitReporter(sharedClient, agw)

	r.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedUserCall, false)
	r.RecConnData(sharedChannel, sharedClient, sharedNeighbourCall, sharedUserCall, ax25.PIDNetROM, []byte("hi"))
	r.LinkTerminated(sharedChannel, agwClient, sharedNeighbourCall, sharedUserCall, true)

	assert.Equal(t, []string{
		"established 1 7 Q3TEST-7 Q2TEST false",
		"data 1 7 Q3TEST-7 Q2TEST 207 \"hi\"",
	}, shellCircuits.calls, "the shell's circuits are the shell's")
	assert.Equal(t, []string{
		"terminated 1 3 Q3TEST-7 Q2TEST true",
	}, agw.calls, "the rest are the AGW clients'")
}

func TestSharedNodeCircuitReporterWithoutAGW(t *testing.T) {
	t.Parallel()

	var s, _, _, _ = newTestSharedNode()

	var r = s.circuitReporter(sharedClient, nil)

	assert.NotPanics(t, func() {
		r.LinkTerminated(sharedChannel, 3, sharedNeighbourCall, sharedUserCall, true)
	})
}

// A user of the shell connects to a node NET/ROM knows over a circuit, which
// the NET/ROM node opens through its neighbour, and reports to the shell.
func TestSharedNodeShellOpensCircuit(t *testing.T) {
	t.Parallel()

	var link = new(recordingAGWDataLink)
	var s = newSharedNode(link, sharedChannel, sharedNodeCall)

	var config = netrom.DefaultConfig()
	config.Channel = sharedChannel
	config.Callsign = sharedNodeCall
	config.Alias = sharedNodeAlias

	var netromNode = netrom.New(config, s.netromLink(), func(int, int, *ax25.Packet) {}, s.circuitReporter(sharedClient, nil), netrom.RealClock{}, sharedClient)

	var shell = node.New(node.Config{
		Address:        sharedNodeCall,
		Alias:          sharedNodeAlias,
		Ports:          []node.Port{{Channel: sharedChannel, Description: "AXUDP"}},
		InfoMessage:    "",
		ConnectMessage: "",
	}, s.shellLink(), nil, netromNode, sharedClient)

	s.attach(shell, shell.Circuits(), netromNode)

	// The neighbour says it is a node, and so reachable.
	var nodes, nodesErr = netrom.EncodeNodes("QNODEB", nil)
	require.NoError(t, nodesErr)

	var broadcast [ax25.MaxAddrs]string
	broadcast[ax25.Destination] = netrom.NodesCallsign
	broadcast[ax25.Source] = sharedNeighbourCall

	var pp = ax25.UFrame(broadcast, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, nodes[0])
	require.NotNil(t, pp)
	netromNode.HeardUI(sharedChannel, pp)

	s.LinkEstablished(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, true)
	link.taken()

	s.RecConnData(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, ax25.PIDNoLayer3, []byte("C QNODEB\r"))

	assert.Equal(t, []string{
		`data 1 7 Q1TEST-7>Q2TEST "QNODEA:Q1TEST-7} Trying QNODEB:Q3TEST-7\r"`,
		"connect 1 7 Q1TEST-7>Q3TEST-7 0xcf",
	}, link.taken(), "the NET/ROM node links to its neighbour to open the circuit")

	// The link up, the CONNECT REQUEST goes over it.
	s.LinkEstablished(sharedChannel, sharedClient, sharedNeighbourCall, sharedNodeCall, false)

	var calls = link.taken()
	require.Len(t, calls, 1)
	assert.True(t, strings.HasPrefix(calls[0], "data 1 7 Q1TEST-7>Q3TEST-7 "), calls[0])

	require.Len(t, netromNode.Status().Circuits, 1)
	assert.Equal(t, sharedUserCall, netromNode.Status().Circuits[0].User, "the circuit is the user's")

	// The user going takes the circuit with them; one still connecting
	// goes without a word to the far end.
	s.LinkTerminated(sharedChannel, sharedClient, sharedUserCall, sharedNodeCall, false)

	assert.Empty(t, netromNode.Status().Circuits)
	assert.Empty(t, link.taken())
}
