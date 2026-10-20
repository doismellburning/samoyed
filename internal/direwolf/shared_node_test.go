// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
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
	s.attach(shell, netrom)

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
