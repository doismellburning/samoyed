// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAX25 stands in for the link layer and the radio between NET/ROM nodes
// that all hear each other on channel 0: a connect request to a node succeeds
// at once, to anything else fails, and data and UI frames go straight across.
type fakeAX25 struct {
	nodes map[string]*netromNode
	from  string // Whose requests these are.
	net   *fakeNet
}

type fakeNet struct {
	t     *testing.T
	nodes map[string]*netromNode
	users map[string]*strings.Builder // What each plain station, not a node, was sent.

	// owners says which node a callsign other than its own - one it opened a
	// link onwards from - belongs to.
	owners map[string]*netromNode
	now    time.Time
}

func (f fakeAX25) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, _ int) {
	var self = f.net.nodes[f.from]

	var own = addrs[ax25.Source]
	f.net.owners[own] = self

	var to, ok = f.net.nodes[addrs[ax25.Destination]]
	if !ok {
		self.LinkTerminated(channel, client, addrs[ax25.Destination], own, true)

		return
	}

	to.LinkEstablished(channel, client, own, addrs[ax25.Destination], true)
	self.LinkEstablished(channel, client, addrs[ax25.Destination], own, false)
}

func (f fakeAX25) DisconnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int) {
	var self = f.net.nodes[f.from]
	self.LinkTerminated(channel, client, addrs[ax25.Destination], addrs[ax25.Source], false)

	var to, isNode = f.net.nodes[addrs[ax25.Destination]]
	if !isNode {
		to, isNode = f.net.owners[addrs[ax25.Destination]]
	}

	if isNode {
		to.LinkTerminated(channel, client, addrs[ax25.Source], addrs[ax25.Destination], false)
	}
}

func (f fakeAX25) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, pid int, xdata []byte) {
	var to, isNode = f.net.nodes[addrs[ax25.Destination]]
	if !isNode {
		to, isNode = f.net.owners[addrs[ax25.Destination]]
	}

	if isNode {
		to.RecConnData(channel, client, addrs[ax25.Source], addrs[ax25.Destination], pid, xdata)

		return
	}

	var user, ok = f.net.users[addrs[ax25.Destination]]
	if !ok {
		user = new(strings.Builder)
		f.net.users[addrs[ax25.Destination]] = user
	}

	user.Write(xdata)
}

func (f fakeAX25) RegisterCallsign(string, int, int) {}

func (n *fakeNet) add(call string, alias string) *netromNode {
	n.t.Helper()

	var cfg = netrom.DefaultConfig()
	cfg.Call = call
	cfg.Alias = alias
	cfg.Ports = []netrom.PortConfig{{Port: 0, Quality: 192, Broadcast: true}}

	var shellCfg = node.Config{Call: "", Alias: "", Info: "This is " + alias + ".", IdleTimeout: time.Hour, Applications: nil}
	var ports = []node.Port{{Number: 0, Description: "Test"}}

	var nn, err = newNetromNode(cfg, shellCfg, ports, fakeAX25{nodes: nil, from: call, net: n}, func(channel int, pp *ax25.Packet) {
		for other, o := range n.nodes {
			if other != call {
				o.heardFrame(channel, pp)
			}
		}
	})
	require.NoError(n.t, err)

	nn.now = func() time.Time { return n.now }
	nn.client = firstAppClient
	n.nodes[call] = nn

	return nn
}

func newFakeNet(t *testing.T) *fakeNet {
	t.Helper()

	return &fakeNet{t: t, nodes: map[string]*netromNode{}, users: map[string]*strings.Builder{}, owners: map[string]*netromNode{}, now: time.Now()}
}

// userConnects has the plain station user connect to the node.
func (n *fakeNet) userConnects(user string, nodeCall string) {
	n.nodes[nodeCall].LinkEstablished(0, firstAppClient, user, nodeCall, true)
	n.pump()
}

// userTypes has the plain station user send text to the node.
func (n *fakeNet) userTypes(user string, nodeCall string, text string) {
	n.nodes[nodeCall].RecConnData(0, firstAppClient, user, nodeCall, ax25.PIDNoLayer3, []byte(text))
	n.pump()
}

// userSaw returns what user has been sent since last asked, carriage returns
// as newlines.
func (n *fakeNet) userSaw(user string) string {
	var b, ok = n.users[user]
	if !ok {
		return ""
	}

	var s = strings.ReplaceAll(b.String(), "\r", "\n")
	b.Reset()

	return s
}

// pump runs every node's queued work until none is left.
func (n *fakeNet) pump() {
	for busy := true; busy; {
		busy = false

		for _, node := range n.nodes {
			select {
			case f := <-node.work:
				f()

				busy = true
			default:
			}
		}
	}
}

func (n *fakeNet) tick(d time.Duration) {
	n.now = n.now.Add(d)

	for _, node := range n.nodes {
		node.do(func() { node.router.Tick(n.now) })
	}

	n.pump()
}

type circuitRecorder struct {
	up   bool
	data bytes.Buffer
	down bool
}

func (r *circuitRecorder) Connected(*netrom.Circuit)            { r.up = true }
func (r *circuitRecorder) Received(_ *netrom.Circuit, d []byte) { r.data.Write(d) }
func (r *circuitRecorder) Closed(*netrom.Circuit, error)        { r.down = true }

func TestNetromNodesOverAX25(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")
	var b = n.add("Q2TEST", "TWO")

	n.tick(time.Second) // The routers started on the real clock, a moment ago.

	var d, ok = a.router.Table().Lookup("TWO")
	require.True(t, ok, "A learned B from its NODES broadcast")
	assert.Equal(t, "Q2TEST", d.Call)

	var far = new(circuitRecorder)
	require.NoError(t, b.router.Listen("Q2TEST", "TWO", 0, func(*netrom.Circuit) netrom.CircuitHandler { return far }))

	var near = new(circuitRecorder)

	var c *netrom.Circuit

	a.do(func() {
		var err error

		c, err = a.router.Connect("TWO", "Q1TEST-2", near, n.now)
		assert.NoError(t, err)
	})
	n.pump()

	require.True(t, near.up)
	require.True(t, far.up)
	assert.True(t, a.sessions[axKey{port: 0, own: "Q1TEST", remote: "Q2TEST"}].up, "the AX.25 link opened on demand")

	a.do(func() { assert.NoError(t, c.Write([]byte("hello over NET/ROM"))) })
	n.pump()
	assert.Equal(t, "hello over NET/ROM", far.data.String())
}

func TestNetromNeighbourUnreachable(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")

	// A broadcast from a node that will not answer a connect request.
	var frames, err = netrom.EncodeNodes("GHOST", nil)
	require.NoError(t, err)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = netrom.NodesDestination
	addrs[ax25.Source] = "Q9TEST"
	a.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))
	n.pump()

	var _, known = a.router.Table().Lookup("GHOST")
	require.True(t, known)

	var near = new(circuitRecorder)

	a.do(func() {
		var _, cerr = a.router.Connect("GHOST", "Q1TEST", near, n.now)
		assert.NoError(t, cerr)
	})
	n.pump()

	var _, stillKnown = a.router.Table().Lookup("GHOST")
	assert.False(t, stillKnown, "routes through an unreachable neighbour are dropped")
	assert.True(t, near.down)
	assert.Empty(t, a.sessions)
}

func TestNetromIgnoresOtherFrames(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")

	var frames, _ = netrom.EncodeNodes("OTHER", nil)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "APRS"
	addrs[ax25.Source] = "Q9TEST"
	a.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))

	addrs[ax25.Destination] = netrom.NodesDestination
	a.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNoLayer3, frames[0]))

	addrs[ax25.Repeater1] = "Q8TEST"
	a.heardFrame(0, ax25.UFrame(addrs, 3, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))

	n.pump()
	assert.Empty(t, a.router.Table().Destinations(), "only a NODES broadcast heard directly is taken in")

	var none *netromNode
	none.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))
}

func TestConfigNetROM(t *testing.T) {
	var c = parseYAMLConfig(t, `
channels:
  - channel: 0
    mycall: Q1TEST-7
netrom:
  alias: one
  ports:
    - channel: 0
      quality: 200
  neighbours:
    - channel: 0
      call: q2test
      alias: two
      quality: 150
  broadcastInterval: 15m
  window: 7
`)
	require.Zero(t, c.errors, c.output)
	require.NotNil(t, c.misc.netrom)
	assert.Equal(t, 15*time.Minute, c.misc.netrom.BroadcastInterval)
	assert.Equal(t, 7, c.misc.netrom.Window)

	var cfg, err = netromConfigFor(*c.misc.netrom, c.audio)
	require.NoError(t, err)
	assert.Equal(t, "Q1TEST-7", cfg.Call, "the call defaults to the port's MYCALL")
	assert.Equal(t, "ONE", cfg.Alias)
	assert.Equal(t, []netrom.PortConfig{{Port: 0, Quality: 200, Broadcast: true}}, cfg.Ports)
	assert.Equal(t, []netrom.LockedNeighbour{{Port: 0, Call: "Q2TEST", Alias: "TWO", Quality: 150}}, cfg.Neighbours)
}

func TestConfigNetROMRejects(t *testing.T) {
	for name, yaml := range map[string]string{
		"no ports":    "netrom:\n  alias: one\n",
		"bad channel": "netrom:\n  ports:\n    - channel: 999\n",
		"bad window":  "netrom:\n  window: 0\n  ports:\n    - channel: 0\n",
		"bad call":    "netrom:\n  call: not a call\n  ports:\n    - channel: 0\n",
		"bad alias":   "netrom:\n  alias: much too long\n  ports:\n    - channel: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			var c = parseYAMLConfig(t, yaml)
			assert.NotZero(t, c.errors, c.output)
		})
	}
}

func TestNetromConfigForChecksChannels(t *testing.T) {
	var c = parseYAMLConfig(t, "netrom:\n  ports:\n    - channel: 3\n")
	require.Zero(t, c.errors, c.output)

	var _, err = netromConfigFor(*c.misc.netrom, c.audio)
	require.Error(t, err, "channel 3 is not in use")

	c = parseYAMLConfig(t, "netrom:\n  ports:\n    - channel: 0\n")
	_, err = netromConfigFor(*c.misc.netrom, c.audio)
	require.Error(t, err, "no callsign anywhere")
}

func TestNodeShellOverAX25AndNetROM(t *testing.T) {
	var n = newFakeNet(t)
	n.add("Q1TEST", "ONE")
	n.add("Q2TEST", "TWO")
	n.tick(time.Second)

	n.userConnects("Q4TEST", "Q1TEST")
	assert.Equal(t, "ONE:Q1TEST} Welcome to ONE:Q1TEST, Q4TEST.  Type ? for a list of commands.\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "N\r")
	assert.Equal(t, "ONE:Q1TEST} Nodes:\nTWO:Q2TEST\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "MH\r")
	assert.Contains(t, n.userSaw("Q4TEST"), "Heard:", "the heard list is reachable")

	// Onwards over NET/ROM to the other node's shell.
	n.userTypes("Q4TEST", "Q1TEST", "C TWO\r")
	assert.Equal(t, "ONE:Q1TEST} Connected to TWO\nTWO:Q2TEST} Welcome to TWO:Q2TEST, Q4TEST.  Type ? for a list of commands.\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "I\r")
	assert.Equal(t, "TWO:Q2TEST} This is TWO.\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "BYE\r")
	assert.Equal(t, "ONE:Q1TEST} Reconnected to ONE:Q1TEST\n", n.userSaw("Q4TEST"))

	// Onwards over AX.25, from the user's callsign with its SSID turned round.
	n.userTypes("Q4TEST", "Q1TEST", "C 0 Q2TEST\r")
	assert.Equal(t, "ONE:Q1TEST} Connected to Q2TEST\nTWO:Q2TEST} Welcome to TWO:Q2TEST, Q4TEST-15.  Type ? for a list of commands.\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "B\r")
	assert.Equal(t, "ONE:Q1TEST} Reconnected to ONE:Q1TEST\n", n.userSaw("Q4TEST"))

	n.userTypes("Q4TEST", "Q1TEST", "C 7 Q2TEST\r")
	assert.Contains(t, n.userSaw("Q4TEST"), "Failure with Q2TEST: no such port")

	// A second user has a shell of their own.
	n.userConnects("Q5TEST", "Q1TEST")
	n.userTypes("Q5TEST", "Q1TEST", "P\r")
	assert.Equal(t, "ONE:Q1TEST} Welcome to ONE:Q1TEST, Q5TEST.  Type ? for a list of commands.\nONE:Q1TEST} Ports:\n   0 Test\n", n.userSaw("Q5TEST"))
	assert.Empty(t, n.userSaw("Q4TEST"))

	// And a user of the other node has one there.
	n.userConnects("Q6TEST", "Q2TEST")
	n.userTypes("Q6TEST", "Q2TEST", "I\r")
	assert.Contains(t, n.userSaw("Q6TEST"), "TWO:Q2TEST} This is TWO.\n")
}

func TestNodeShellGoesWithTheUser(t *testing.T) {
	var n = newFakeNet(t)
	var a = n.add("Q1TEST", "ONE")

	n.userConnects("Q4TEST", "Q1TEST")
	require.Len(t, a.shells, 1)

	a.LinkTerminated(0, firstAppClient, "Q4TEST", "Q1TEST", false)
	n.pump()
	assert.Empty(t, a.shells)
	assert.Empty(t, a.sessions)
}

func TestDownlinkCall(t *testing.T) {
	for user, want := range map[string]string{"Q4TEST": "Q4TEST-15", "Q4TEST-15": "Q4TEST", "Q4TEST-3": "Q4TEST-12"} {
		var got, err = downlinkCall(user)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
