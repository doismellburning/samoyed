// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNet stands in for the data link and the air between nodes that all hear
// each other on channel 0: a connect request to a node succeeds at once, to
// anything else fails, and data and UI frames go straight across.  Plain
// stations - users - are only ever connected from, and what they are sent is
// kept for the test to read.
type fakeNet struct {
	t      *testing.T
	nodes  map[string]*fakeNode
	owners map[string]*fakeNode // Which node a callsign it sends from belongs to.
	users  map[string]*strings.Builder
	now    time.Time
}

type fakeNode struct {
	net    *fakeNet
	call   string
	shell  *node.Node
	netrom *netromNode
}

// fakeLink is one node's view of the fake data link.
type fakeLink struct {
	n *fakeNode
}

func (l fakeLink) RegisterCallsign(string, int, int) {}

func (l fakeLink) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, _ int) {
	var own, to = addrs[ax25.Source], addrs[ax25.Destination]
	l.n.net.owners[own] = l.n

	var far, ok = l.n.net.nodes[to]
	if !ok {
		l.n.shell.LinkTerminated(channel, client, to, own, true)

		return
	}

	far.shell.LinkEstablished(channel, client, own, to, true)
	l.n.shell.LinkEstablished(channel, client, to, own, false)
}

func (l fakeLink) DisconnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int) {
	var own, to = addrs[ax25.Source], addrs[ax25.Destination]
	l.n.shell.LinkTerminated(channel, client, to, own, false)

	if far := l.n.net.owner(to); far != nil {
		far.shell.LinkTerminated(channel, client, own, to, false)
	}
}

func (l fakeLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, pid int, xdata []byte) {
	var own, to = addrs[ax25.Source], addrs[ax25.Destination]

	if far := l.n.net.owner(to); far != nil {
		far.shell.RecConnData(channel, client, own, to, pid, xdata)

		return
	}

	var user, ok = l.n.net.users[to]
	if !ok {
		user = new(strings.Builder)
		l.n.net.users[to] = user
	}

	user.Write(xdata)
}

func (f *fakeNet) owner(call string) *fakeNode {
	if n, ok := f.nodes[call]; ok {
		return n
	}

	return f.owners[call]
}

func newFakeNet(t *testing.T) *fakeNet {
	t.Helper()

	return &fakeNet{t: t, nodes: map[string]*fakeNode{}, owners: map[string]*fakeNode{}, users: map[string]*strings.Builder{}, now: time.Now()}
}

func noneHeard(int) []mheard.Heard { return nil }

// add makes a node with NET/ROM, its routers' clocks starting a moment before
// the net's.
func (f *fakeNet) add(call string, alias string) *fakeNode {
	f.t.Helper()

	var n = &fakeNode{net: f, call: call, shell: nil, netrom: nil}

	var config = node.Config{
		Address: call, Alias: alias, Ports: []node.Port{{Channel: 0, Description: "Test"}},
		InfoMessage: "This is " + alias + ".", ConnectMessage: "",
	}
	n.shell = node.New(config, fakeLink{n: n}, noneHeard, nodeClient)

	var cfg = netrom.DefaultConfig()
	cfg.Call = call
	cfg.Alias = alias
	cfg.Ports = []netrom.PortConfig{{Port: 0, Quality: 192, Broadcast: true}}

	var nr, err = newNetromNode(cfg, n.shell, fakeLink{n: n}, nodeClient, func(channel int, _ int, pp *ax25.Packet) {
		for other, o := range f.nodes {
			if other != call {
				o.netrom.heardFrame(channel, pp)
			}
		}
	})
	require.NoError(f.t, err)

	nr.now = func() time.Time { return f.now }
	n.netrom = nr
	n.shell.SetNetROM(nr)
	n.shell.Start()
	f.nodes[call] = n

	return n
}

// pump runs every node's queued NET/ROM work until none is left.
func (f *fakeNet) pump() {
	for busy := true; busy; {
		busy = false

		for _, n := range f.nodes {
			for _, w := range n.netrom.work.take() {
				w()

				busy = true
			}
		}
	}
}

// tick moves the clock on a second and runs the routers' timers.  The first
// tick sends the NODES broadcasts.
func (f *fakeNet) tick() {
	f.now = f.now.Add(time.Second)

	for _, n := range f.nodes {
		n.netrom.do(func() { n.netrom.router.Tick(f.now) })
	}

	f.pump()
}

func (f *fakeNet) userConnects(user string, nodeCall string) {
	f.nodes[nodeCall].shell.LinkEstablished(0, nodeClient, user, nodeCall, true)
	f.pump()
}

func (f *fakeNet) userTypes(user string, nodeCall string, text string) {
	f.nodes[nodeCall].shell.RecConnData(0, nodeClient, user, nodeCall, ax25.PIDNoLayer3, []byte(text))
	f.pump()
}

// userSaw returns what user has been sent since last asked, carriage returns
// as newlines.
func (f *fakeNet) userSaw(user string) string {
	var b, ok = f.users[user]
	if !ok {
		return ""
	}

	var s = strings.ReplaceAll(b.String(), "\r", "\n")
	b.Reset()

	return s
}

func TestNetROMBetweenShells(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")
	f.add("Q2TEST", "TWO")
	f.tick()

	var d, ok = a.netrom.router.Table().Lookup("TWO")
	require.True(t, ok, "A learned B from its NODES broadcast")
	assert.Equal(t, "Q2TEST", d.Call)
	assert.True(t, a.netrom.Knows("two"))
	assert.True(t, a.netrom.IsNeighbour(0, "Q2TEST"))

	f.userConnects("Q4TEST", "Q1TEST")
	assert.Equal(t, "ONE:Q1TEST} Welcome to Samoyed\n", f.userSaw("Q4TEST"))

	f.userTypes("Q4TEST", "Q1TEST", "N\r")
	assert.Equal(t, "ONE:Q1TEST} Nodes:\nTWO:Q2TEST\n", f.userSaw("Q4TEST"))

	// Onward over NET/ROM, to the other node's shell.
	f.userTypes("Q4TEST", "Q1TEST", "C TWO\r")
	assert.Equal(t, "ONE:Q1TEST} Trying TWO\nONE:Q1TEST} Connected to TWO\nTWO:Q2TEST} Welcome to Samoyed\n", f.userSaw("Q4TEST"))
	assert.True(t, a.netrom.links[netrom.NeighbourKey{Port: 0, Call: "Q2TEST"}].up, "the AX.25 link to the neighbour opened on demand")

	f.userTypes("Q4TEST", "Q1TEST", "I\r")
	assert.Equal(t, "TWO:Q2TEST} This is TWO.\n", f.userSaw("Q4TEST"))

	f.userTypes("Q4TEST", "Q1TEST", "BYE\r")
	assert.Equal(t, "ONE:Q1TEST} Disconnected from TWO\n", f.userSaw("Q4TEST"))

	f.userTypes("Q4TEST", "Q1TEST", "C Q8TEST\r")
	assert.Contains(t, f.userSaw("Q4TEST"), "Trying Q8TEST on port 0", "a node NET/ROM does not know is tried over AX.25")

	// A user of the other node has a session there of their own.
	f.userConnects("Q5TEST", "Q2TEST")
	f.userTypes("Q5TEST", "Q2TEST", "I\r")
	assert.Equal(t, "TWO:Q2TEST} Welcome to Samoyed\nTWO:Q2TEST} This is TWO.\n", f.userSaw("Q5TEST"))
}

func TestNetROMNeighbourUnreachable(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")

	// A broadcast from a node that will not answer a connect request.
	var frames, err = netrom.EncodeNodes("GHOST", nil)
	require.NoError(t, err)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = netrom.NodesDestination
	addrs[ax25.Source] = "Q9TEST"
	a.netrom.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))
	f.pump()
	require.True(t, a.netrom.Knows("GHOST"))

	f.userConnects("Q4TEST", "Q1TEST")
	f.userSaw("Q4TEST")
	f.userTypes("Q4TEST", "Q1TEST", "C GHOST\r")

	assert.False(t, a.netrom.Knows("GHOST"), "routes through an unreachable neighbour are dropped")
	assert.Contains(t, f.userSaw("Q4TEST"), "Failure with GHOST - no route")
	assert.Empty(t, a.netrom.links)
}

func TestNetROMIgnoresOtherFrames(t *testing.T) {
	var f = newFakeNet(t)
	var a = f.add("Q1TEST", "ONE")

	var frames, _ = netrom.EncodeNodes("OTHER", nil)

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = "APRS"
	addrs[ax25.Source] = "Q9TEST"
	a.netrom.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))

	addrs[ax25.Destination] = netrom.NodesDestination
	a.netrom.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNoLayer3, frames[0]))

	addrs[ax25.Repeater1] = "Q8TEST"
	a.netrom.heardFrame(0, ax25.UFrame(addrs, 3, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))

	assert.Empty(t, a.netrom.work.take(), "only a NODES broadcast heard directly is taken in")

	var none *netromNode
	none.heardFrame(0, ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, frames[0]))
}

func TestConfigNetROM(t *testing.T) {
	var c = parseYAMLConfig(t, `
channels:
  - channel: 0
    mycall: Q1TEST-7
node:
  address: Q1TEST-7
  alias: ONE
netrom:
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

	var settings, ok = c.misc.netrom.Get()
	require.True(t, ok)
	assert.Equal(t, 15*time.Minute, settings.BroadcastInterval)
	assert.Equal(t, 7, settings.Window)

	var nodeSettings, _ = c.misc.node.Get()

	var nodeConfig, nerr = c.audio.nodeConfig(nodeSettings)
	require.NoError(t, nerr)

	var cfg, err = netromConfigFor(settings, c.audio, nodeConfig)
	require.NoError(t, err)
	assert.Equal(t, "Q1TEST-7", cfg.Call, "NET/ROM goes by the node's address")
	assert.Equal(t, "ONE", cfg.Alias)
	assert.Equal(t, []netrom.PortConfig{{Port: 0, Quality: 200, Broadcast: true}}, cfg.Ports)
	assert.Equal(t, []netrom.LockedNeighbour{{Port: 0, Call: "Q2TEST", Alias: "TWO", Quality: 150}}, cfg.Neighbours)

	// A port the node does not listen on is no NET/ROM port.
	nodeConfig.Ports = nil
	_, err = netromConfigFor(settings, c.audio, nodeConfig)
	require.Error(t, err)
}

func TestConfigNetROMRejects(t *testing.T) {
	for name, yaml := range map[string]string{
		"no ports":    "netrom:\n  window: 4\n",
		"bad channel": "netrom:\n  ports:\n    - channel: 999\n",
		"bad window":  "netrom:\n  window: 0\n  ports:\n    - channel: 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			var c = parseYAMLConfig(t, yaml)
			assert.NotZero(t, c.errors, c.output)
		})
	}
}
