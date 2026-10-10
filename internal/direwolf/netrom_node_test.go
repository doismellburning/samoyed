// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"bytes"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
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
	now   time.Time
}

func (f fakeAX25) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, _ int) {
	var self = f.net.nodes[f.from]

	var to, ok = f.net.nodes[addrs[ax25.Destination]]
	if !ok {
		self.LinkTerminated(channel, client, addrs[ax25.Destination], f.from, true)

		return
	}

	to.LinkEstablished(channel, client, f.from, addrs[ax25.Destination], true)
	self.LinkEstablished(channel, client, addrs[ax25.Destination], f.from, false)
}

func (f fakeAX25) DisconnectRequest([ax25.MaxAddrs]string, int, int, int) {}

func (f fakeAX25) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, pid int, xdata []byte) {
	if to, ok := f.net.nodes[addrs[ax25.Destination]]; ok {
		to.RecConnData(channel, client, f.from, addrs[ax25.Destination], pid, xdata)
	}
}

func (f fakeAX25) RegisterCallsign(string, int, int) {}

func (n *fakeNet) add(call string, alias string) *netromNode {
	n.t.Helper()

	var cfg = netrom.DefaultConfig()
	cfg.Call = call
	cfg.Alias = alias
	cfg.Ports = []netrom.PortConfig{{Port: 0, Quality: 192, Broadcast: true}}

	var node, err = newNetromNode(cfg, fakeAX25{nodes: nil, from: call, net: n}, func(channel int, pp *ax25.Packet) {
		for other, o := range n.nodes {
			if other != call {
				o.heardFrame(channel, pp)
			}
		}
	})
	require.NoError(n.t, err)

	node.now = func() time.Time { return n.now }
	node.client = firstAppClient
	n.nodes[call] = node

	return node
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
	var n = &fakeNet{t: t, nodes: map[string]*netromNode{}, now: time.Now()}
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
	assert.True(t, a.sessions[netrom.NeighbourKey{Port: 0, Call: "Q2TEST"}].up, "the AX.25 link opened on demand")

	a.do(func() { assert.NoError(t, c.Write([]byte("hello over NET/ROM"))) })
	n.pump()
	assert.Equal(t, "hello over NET/ROM", far.data.String())
}

func TestNetromNeighbourUnreachable(t *testing.T) {
	var n = &fakeNet{t: t, nodes: map[string]*netromNode{}, now: time.Now()}
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
	var n = &fakeNet{t: t, nodes: map[string]*netromNode{}, now: time.Now()}
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

	assert.Empty(t, a.work, "only a NODES broadcast heard directly is taken in")

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
