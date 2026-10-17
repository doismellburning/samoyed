// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const neighbourCall = "Q5TEST"

// fakeNetROM is a NetROM that knows one neighbour and one node beyond it, and
// remembers what it is told.
type fakeNetROM struct {
	mu       sync.Mutex
	events   []string
	conn     *fakeConn
	downlink Downlink
	user     string
}

func (f *fakeNetROM) notef(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.events = append(f.events, fmt.Sprintf(format, args...))
}

func (f *fakeNetROM) IsNeighbour(_ int, call string) bool { return call == neighbourCall }

func (f *fakeNetROM) LinkUp(channel int, ownCall string, remoteCall string, incoming bool) {
	f.notef("up %d %s>%s %t", channel, ownCall, remoteCall, incoming)
}

func (f *fakeNetROM) LinkDown(channel int, ownCall string, remoteCall string, timeout bool) {
	f.notef("down %d %s>%s %t", channel, ownCall, remoteCall, timeout)
}

func (f *fakeNetROM) Packet(channel int, ownCall string, remoteCall string, data []byte) {
	f.notef("packet %d %s>%s %q", channel, ownCall, remoteCall, data)
}

func (f *fakeNetROM) Nodes() []netrom.Destination {
	var via = netrom.NeighbourKey{Port: testChannel, Call: neighbourCall}

	return []netrom.Destination{
		{Call: neighbourCall, Alias: "FIVE", Routes: []netrom.Route{{Neighbour: via, Quality: 200, Obsolescence: 6, Locked: false}}},
		{Call: "Q6TEST", Alias: "", Routes: []netrom.Route{{Neighbour: via, Quality: 150, Obsolescence: 5, Locked: false}}},
	}
}

func (f *fakeNetROM) Neighbours() []netrom.Neighbour {
	return []netrom.Neighbour{{Port: testChannel, Call: neighbourCall, Alias: "FIVE", Quality: 200, Locked: true}}
}

func (f *fakeNetROM) Knows(name string) bool {
	return slices.ContainsFunc(f.Nodes(), func(d netrom.Destination) bool {
		return strings.EqualFold(d.Call, name) || strings.EqualFold(d.Alias, name)
	})
}

func (f *fakeNetROM) Connect(dest string, user string, events Downlink) Conn { //nolint:ireturn // NetROM's signature.
	f.notef("connect %s for %s", dest, user)
	f.conn = new(fakeConn)
	f.downlink = events
	f.user = user

	return f.conn
}

func withNetROM(t *testing.T) (*Node, *fakeLink, *fakeNetROM) {
	t.Helper()

	var link = new(fakeLink)
	var nr = new(fakeNetROM)

	var n = New(testConfig(), link, noneHeard, testClient)
	n.SetNetROM(nr)
	n.LinkEstablished(testChannel, testClient, userCall, nodeCall, true)
	link.reset()

	return n, link, nr
}

func TestNetROMNeighbourLinks(t *testing.T) {
	t.Parallel()

	var n, link, nr = withNetROM(t)

	n.LinkEstablished(testChannel, testClient, neighbourCall, nodeCall, true)
	n.LinkEstablished(testChannel, testClient, "Q7TEST", nodeCall, false)
	n.RecConnData(testChannel, testClient, neighbourCall, nodeCall, ax25.PIDNetROM, []byte("L3"))
	n.LinkTerminated(testChannel, testClient, neighbourCall, nodeCall, true)

	assert.Empty(t, link.output(), "a neighbour node gets no greeting")
	assert.Equal(t, []string{
		"up 1 Q1TEST-7>Q5TEST true",
		"up 1 Q1TEST-7>Q7TEST false",
		`packet 1 Q1TEST-7>Q5TEST "L3"`,
		"down 1 Q1TEST-7>Q5TEST true",
	}, nr.events)
}

func TestNetROMFromAStationTakenForAUser(t *testing.T) {
	t.Parallel()

	var n, link, nr = withNetROM(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNetROM, []byte("L3"))
	assert.Equal(t, []string{"up 1 Q1TEST-7>Q2TEST true", `packet 1 Q1TEST-7>Q2TEST "L3"`}, nr.events)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("INFO\r"))
	assert.Empty(t, link.output(), "its session went when it turned out to be a node")
}

func TestNetROMNodesAndRoutes(t *testing.T) {
	t.Parallel()

	var n, link, _ = withNetROM(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("N\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} Nodes:\rFIVE:Q5TEST        Q6TEST\r", link.output())

	link.reset()
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("n five\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} Routes to FIVE:Q5TEST:\r  200 6  1 Q5TEST\r", link.output())

	link.reset()
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("N NOWHERE\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} No such node NOWHERE\r", link.output())

	link.reset()
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("R\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} Routes:\r   1 FIVE:Q5TEST         200 !\r", link.output())
}

func TestNetROMConnectOnward(t *testing.T) {
	t.Parallel()

	var n, link, nr = withNetROM(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C five\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying FIVE\r", link.output())
	require.NotNil(t, nr.downlink)
	assert.Equal(t, userCall, nr.user)

	link.reset()
	nr.downlink.Connected()
	assert.Equal(t, "SAMNOD:Q1TEST-7} Connected to FIVE\r", link.output())

	link.reset()
	nr.downlink.Received([]byte("FIVE:Q5TEST} Hello\r"))
	assert.Equal(t, "FIVE:Q5TEST} Hello\r", link.output())

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("I\r"))
	assert.Equal(t, "I\r", nr.conn.output(), "what the user types goes onward as it is")

	link.reset()
	nr.downlink.Closed("")
	assert.Equal(t, "SAMNOD:Q1TEST-7} Disconnected from FIVE\r", link.output())

	// Back at the node.
	link.reset()
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("XYZZY\r"))
	assert.Equal(t, invalidCommand, link.output())
}

func TestNetROMConnectFails(t *testing.T) {
	t.Parallel()

	var n, link, nr = withNetROM(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C Q6TEST\r"))
	link.reset()
	nr.downlink.Closed("no answer")
	assert.Equal(t, "SAMNOD:Q1TEST-7} Failure with Q6TEST - no answer\r", link.output())

	// A station NET/ROM does not know is connected to over AX.25, as before.
	link.reset()
	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C Q8TEST\r"))
	assert.Equal(t, "SAMNOD:Q1TEST-7} Trying Q8TEST on port 1\r", link.output())
}

func TestNetROMUserLeavesWhileConnecting(t *testing.T) {
	t.Parallel()

	var n, _, nr = withNetROM(t)

	n.RecConnData(testChannel, testClient, userCall, nodeCall, ax25.PIDNoLayer3, []byte("C FIVE\r"))
	n.LinkTerminated(testChannel, testClient, userCall, nodeCall, false)
	assert.Equal(t, 1, nr.conn.closes, "the user's downlink goes with them")
}
