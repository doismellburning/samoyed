// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClient is the client number the tests' nodes have with the data link.
const testClient = 4

// recordingReporter notes what a node reports about its circuits.
type recordingReporter struct {
	mu sync.Mutex

	established []string
	estChannels []int // The channel each circuit was reported established on.
	terminated  []string
	data        [][]byte
	pids        []int
	outstanding []int
}

func (r *recordingReporter) LinkEstablished(channel int, _ int, remoteCall string, _ string, _ bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.established = append(r.established, remoteCall)
	r.estChannels = append(r.estChannels, channel)
}

func (r *recordingReporter) LinkTerminated(_ int, _ int, remoteCall string, _ string, _ bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.terminated = append(r.terminated, remoteCall)
}

func (r *recordingReporter) RecConnData(_ int, _ int, _ string, _ string, pid int, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pids = append(r.pids, pid)
	r.data = append(r.data, slices.Clone(data))
}

func (r *recordingReporter) OutstandingFramesReply(_ int, _ int, _ string, _ string, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.outstanding = append(r.outstanding, count)
}

// fakeDataLink stands in for the data link.  It records what the node
// asks of it and, when joined to another node, carries the frames there as
// though over an AX.25 connection.
type fakeDataLink struct {
	mu sync.Mutex

	connects []string // Neighbours asked for.
	frames   []string // Neighbours sent to.

	connectChannels []int // The channel each connect was asked for on.
	frameChannels   []int // The channel each frame was sent on.

	registered []registration

	// onXmit, if set, is called on each frame sent, outside the lock.
	onXmit func()

	self     *Node
	peer     *Node // Where frames go, if anywhere; links to it come up at once.
	selfCall string
	queue    [][]byte
}

func (l *fakeDataLink) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, client int, _ int) {
	if client != testClient {
		panic("the node must connect as its own client")
	}

	l.mu.Lock()
	l.connects = append(l.connects, addrs[ax25.Destination])
	l.connectChannels = append(l.connectChannels, channel)

	var self, peer = l.self, l.peer
	l.mu.Unlock()

	if peer == nil {
		return
	}

	// The link is made, and both ends hear about it from their data links.
	self.LinkEstablished(0, testClient, addrs[ax25.Destination], addrs[ax25.Source], false)
	peer.LinkEstablished(0, testClient, addrs[ax25.Source], addrs[ax25.Destination], true)
}

func (l *fakeDataLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, pid int, data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if pid != ax25.PIDNetROM {
		panic("NET/ROM goes with its own PID")
	}

	l.frames = append(l.frames, addrs[ax25.Destination])
	l.frameChannels = append(l.frameChannels, channel)
	l.queue = append(l.queue, append([]byte(nil), data...))

	if l.onXmit != nil {
		var hook = l.onXmit
		l.mu.Unlock()
		hook()
		l.mu.Lock()
	}
}

func (l *fakeDataLink) RegisterCallsign(addr string, channel int, client int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.registered = append(l.registered, registration{addr: addr, channel: channel, client: client})
}

type registration struct {
	addr    string
	channel int
	client  int
}

// deliver passes what has been sent to the peer node.
func (l *fakeDataLink) deliver() int {
	l.mu.Lock()
	var queue = l.queue
	l.queue = nil
	l.mu.Unlock()

	for _, data := range queue {
		l.peer.RecConnData(0, testClient, l.selfCall, l.peer.cfg.Callsign, ax25.PIDNetROM, data)
	}

	return len(queue)
}

func testConfig(callsign string, alias string) Config {
	var cfg = DefaultConfig()
	cfg.Channels = []int{0}
	cfg.Callsign = callsign
	cfg.Alias = alias

	return cfg
}

// testNode is a node on channel 0 whose reports about circuits are recorded
// and whose UI frames are kept.
type testNode struct {
	*Node

	link       *fakeDataLink
	agw        *recordingReporter
	ui         []*ax25.Packet
	uiChannels []int // The channel each UI frame went out on.
}

func newTestNode(callsign string, alias string) *testNode {
	var tn = new(testNode)
	tn.link = new(fakeDataLink)
	tn.link.selfCall = callsign
	tn.agw = new(recordingReporter)

	tn.Node = New(testConfig(callsign, alias), tn.link, func(channel int, _ int, pp *ax25.Packet) {
		tn.ui = append(tn.ui, pp)
		tn.uiChannels = append(tn.uiChannels, channel)
	}, tn.agw, new(fakeClock), testClient)

	return tn
}

// joinedNodes is two neighbouring nodes that have heard each other's
// broadcasts.
func joinedNodes(t *testing.T) (*testNode, *testNode) {
	t.Helper()

	var a = newTestNode("Q1TEST-7", "QNODEA")
	var b = newTestNode("Q2TEST-7", "QNODEB")
	a.link.self, a.link.peer = a.Node, b.Node
	b.link.self, b.link.peer = b.Node, a.Node

	a.broadcastNodes()
	b.broadcastNodes()
	b.HeardUI(0, a.ui[0])
	a.HeardUI(0, b.ui[0])

	return a, b
}

// exchange passes frames between two nodes until they fall quiet.
func exchange(t *testing.T, a *testNode, b *testNode) {
	t.Helper()

	for range 100 {
		if a.link.deliver()+b.link.deliver() == 0 {
			return
		}
	}

	t.Fatal("the two nodes never went quiet")
}

func TestNodeBroadcasts(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.broadcastNodes()

	require.Len(t, a.ui, 1)

	var pp = a.ui[0]
	assert.Equal(t, "NODES", pp.AddrWithSSID(ax25.Destination))
	assert.Equal(t, "Q1TEST-7", pp.AddrWithSSID(ax25.Source))
	assert.Equal(t, ax25.PIDNetROM, pp.PID())
	assert.Equal(t, append([]byte{0xFF}, "QNODEA"...), pp.Info())
}

// A node on several channels broadcasts on each, hears broadcasts on any of
// them and nothing on any other.
func TestNodeBroadcastsOnEachChannel(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")
	a.cfg.Channels = []int{0, 3}

	a.broadcastNodes()
	assert.Equal(t, []int{0, 3}, a.uiChannels)

	var b = newTestNode("Q2TEST-7", "QNODEB")
	b.broadcastNodes()

	var c = newTestNode("Q3TEST-7", "QNODEC")
	c.broadcastNodes()

	a.HeardUI(3, b.ui[0])
	a.HeardUI(1, c.ui[0])

	var hop, ok = a.router.nextHop("Q2TEST-7")
	require.True(t, ok)
	assert.Equal(t, neighbour{channel: 3, callsign: "Q2TEST-7"}, hop)

	_, ok = a.router.nextHop("Q3TEST-7")
	assert.False(t, ok, "heard on a channel A is not on")
}

func TestNodeHearsBroadcasts(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	var hop, ok = a.router.nextHop("Q2TEST-7")
	require.True(t, ok)
	assert.Equal(t, nbr("Q2TEST-7"), hop)

	_, ok = b.router.nextHop("Q1TEST-7")
	assert.True(t, ok)
}

func TestNodeIgnoresOtherUIFrames(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")
	var b = newTestNode("Q2TEST-7", "QNODEB")
	b.broadcastNodes()

	var nodes = b.ui[0]

	var digipeated = ax25.FromText("Q2TEST-7>NODES,Q3TEST*:x", true)
	require.NotNil(t, digipeated)
	digipeated.SetPID(ax25.PIDNetROM)
	digipeated.SetInfo(nodes.Info())

	var toSomeoneElse = ax25.FromText("Q2TEST-7>ID:x", true)
	require.NotNil(t, toSomeoneElse)
	toSomeoneElse.SetPID(ax25.PIDNetROM)
	toSomeoneElse.SetInfo(nodes.Info())

	a.HeardUI(1, nodes)
	a.HeardUI(0, digipeated)
	a.HeardUI(0, toSomeoneElse)

	var _, ok = a.router.nextHop("Q2TEST-7")
	assert.False(t, ok)
}

// Frames go to a neighbour over a connected link, which is asked for the
// first time and not again while it is up or on its way.  Until it is up,
// frames wait: the data link throws away data for a link it is still making.
func TestNodeConnectsToNeighbours(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("one"))
	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("two"))
	assert.Equal(t, []string{"Q2TEST-7"}, a.link.connects)
	assert.Empty(t, a.link.frames, "nothing sent before the link is up")

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Equal(t, [][]byte{[]byte("one"), []byte("two")}, a.link.queue, "then what was waiting, in order")

	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("three"))
	assert.Len(t, a.link.connects, 1)
	assert.Len(t, a.link.frames, 3)

	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("four"))
	assert.Len(t, a.link.connects, 2, "and again once it has gone down")
	assert.Len(t, a.link.frames, 3)

	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Len(t, a.link.frames, 3, "what was waiting for a link that failed is dropped")

	for range 2 * maxPending {
		a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	}

	for range 2 * maxPending {
		a.sendToNeighbour(nbr("Q2TEST-7"), []byte("x"))
	}

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Len(t, a.link.frames, 3+maxPending, "and only so much waits")
}

// A neighbour connecting to us makes the link up for our traffic too.
// A neighbour is reached on the channel it was heard on, and the data
// link's reports of links on a channel the node is not on are nothing to it.
func TestNodeReachesANeighbourOnItsChannel(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")
	a.cfg.Channels = []int{2}

	var b = newTestNode("Q2TEST-7", "QNODEB")
	b.broadcastNodes()

	a.HeardUI(0, b.ui[0])

	var _, ok = a.router.nextHop("Q2TEST-7")
	require.False(t, ok, "heard on a channel A is not on")

	a.HeardUI(2, b.ui[0])

	var hop, _ = a.router.nextHop("Q2TEST-7")
	assert.Equal(t, neighbour{channel: 2, callsign: "Q2TEST-7"}, hop)

	a.sendToNeighbour(hop, []byte("one"))
	assert.Equal(t, []int{2}, a.link.connectChannels)

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Empty(t, a.link.frames, "not the link asked for")

	a.LinkEstablished(2, testClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Equal(t, []int{2}, a.link.frameChannels)
}

func TestNodeUsesAnIncomingLink(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", true)
	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("one"))

	assert.Empty(t, a.link.connects)
	assert.Len(t, a.link.frames, 1)
}

func TestNodeForwards(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	var f = new(Frame)
	f.origin = "Q3TEST-7"
	f.destination = "Q2TEST-7"
	f.ttl = 5
	f.opcode = OpDiscReq

	var route = func() {
		var raw, err = f.encode()
		require.NoError(t, err)
		a.route(f, raw)
	}

	route()
	require.Equal(t, []string{"Q2TEST-7"}, a.link.frames, "sent on towards its destination")

	var sent, err = DecodeFrame(a.link.queue[0])
	require.NoError(t, err)
	assert.Equal(t, byte(4), sent.ttl, "a hop older")

	f.ttl = 1
	route()
	assert.Len(t, a.link.frames, 1, "but not once its time is up")

	f.ttl = 5
	f.destination = "Q9TEST"
	route()
	assert.Len(t, a.link.frames, 1, "nor where there is no route")
}

// A frame passed on is the one received, but for its time to live: what
// another implementation put in it, which we may not understand, goes on
// with it.
func TestNodeForwardsTheFrameAsReceived(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	var f = new(Frame)
	f.origin = "Q3TEST-7"
	f.destination = "Q2TEST-7"
	f.ttl = 5
	f.opcode = OpDiscReq

	var raw, err = f.encode()
	require.NoError(t, err)

	raw = append(raw, 0xAA, 0x55) // Something past what a DISC REQ carries.

	var decoded, decodeErr = DecodeFrame(raw)
	require.NoError(t, decodeErr)

	a.route(decoded, raw)
	require.Len(t, a.link.queue, 1)

	var want = slices.Clone(raw)
	want[ttlOffset] = 4
	assert.Equal(t, want, a.link.queue[0])
	assert.Equal(t, byte(5), raw[ttlOffset], "and the frame received is left as it was")
}

// A neighbour can connect and ask for a circuit before we have heard its
// NODES broadcast.  The frame itself says it can be reached through that
// neighbour, so the answer has somewhere to go.
func TestNodeAnswersANodeItHasNotHeardBroadcast(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")
	var b = newTestNode("Q2TEST-7", "QNODEB")
	a.link.self, a.link.peer = a.Node, b.Node
	b.link.self, b.link.peer = b.Node, a.Node

	b.broadcastNodes()
	a.HeardUI(0, b.ui[0]) // A knows B; B has heard nothing from A.

	require.True(t, b.Register(0, 1, "Q2TEST-7"))
	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Equal(t, []string{"QNODEB"}, a.agw.established, "B's acknowledgement found its way back")
	assert.Equal(t, []string{"Q1TEST"}, b.agw.established)

	var hop, ok = b.router.nextHop("Q1TEST-7")
	require.True(t, ok)
	assert.Equal(t, nbr("Q1TEST-7"), hop)

	assert.Empty(t, b.router.broadcastEntries(), "but a route learned so is not advertised as though a broadcast had told of it")
}

// A destination in the table with no way there that works - every
// neighbour on the way failing - is refused at once, rather than the
// client waiting out every retry of a CONNECT REQUEST nobody can carry.
func TestNodeRefusesAConnectWithNoWorkingRoute(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)

	var connectsBefore = len(a.link.connects)

	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM))
	assert.Equal(t, []string{"QNODEB"}, a.agw.terminated, "told at once")
	assert.Len(t, a.link.connects, connectsBefore)
	assert.Empty(t, a.circuits.circuits)
}

// A client can ask how much of what it sent on a circuit has yet to go or
// be acknowledged, and pace itself by the answer.
func TestNodeOutstanding(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, b.Register(0, 1, "Q2TEST-7"))
	require.True(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	require.True(t, a.Send(0, 0, "Q1TEST", "Q2TEST-7", make([]byte, defaultWindow*maxInfo)))

	assert.True(t, a.Outstanding(0, 0, "Q1TEST", "Q2TEST-7"))
	assert.False(t, a.Outstanding(0, 0, "Q1TEST", "Q3TEST"), "no such circuit")
	assert.False(t, a.Outstanding(1, 0, "Q1TEST", "Q2TEST-7"), "another channel")

	exchange(t, a, b) // A full window, so acknowledged at once.
	assert.True(t, a.Outstanding(0, 0, "Q1TEST", "Q2TEST-7"))

	a.agw.mu.Lock()
	assert.Equal(t, []int{defaultWindow, 0}, a.agw.outstanding)
	a.agw.mu.Unlock()
}

// Frames waiting for a link go out ahead of any sent once it is up, even
// one sent while they are still being handed to the data link.
func TestNodeSendsWhatWasWaitingFirst(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("one"))
	a.sendToNeighbour(nbr("Q2TEST-7"), []byte("two"))

	var sentDuring = make(chan struct{})

	var once sync.Once

	a.link.onXmit = func() {
		once.Do(func() {
			go func() {
				a.sendToNeighbour(nbr("Q2TEST-7"), []byte("three"))
				close(sentDuring)
			}()

			time.Sleep(20 * time.Millisecond) // Long enough for "three" to get in first, if it can.
		})
	}

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", false)
	<-sentDuring

	a.link.mu.Lock()
	defer a.link.mu.Unlock()

	assert.Equal(t, [][]byte{[]byte("one"), []byte("two"), []byte("three")}, a.link.queue)
}

func TestNodeCircuitEndToEnd(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, b.Register(0, 1, "Q2TEST-7"), "B's client takes its incoming circuits")

	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM), "by alias")
	exchange(t, a, b)

	assert.Equal(t, []string{"QNODEB"}, a.agw.established)
	assert.Equal(t, []string{"Q1TEST"}, b.agw.established)

	require.True(t, a.Send(0, 0, "Q1TEST", "QNODEB", []byte("hello B")))
	require.True(t, b.Send(0, 1, "Q2TEST-7", "Q1TEST", []byte("hello A")))
	exchange(t, a, b)

	assert.Equal(t, [][]byte{[]byte("hello B")}, b.agw.data)
	assert.Equal(t, [][]byte{[]byte("hello A")}, a.agw.data)
	assert.Equal(t, []int{ax25.PIDNetROM}, a.agw.pids)

	require.True(t, a.Disconnect(0, 0, "Q1TEST", "QNODEB"))
	exchange(t, a, b)

	assert.Equal(t, []string{"QNODEB"}, a.agw.terminated)
	assert.Equal(t, []string{"Q1TEST"}, b.agw.terminated)
}

// A client registered on one of the node's channels takes incoming
// circuits there, and one asking on another of them has its circuit there.
func TestNodeCircuitOnTheClientsChannel(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)
	a.cfg.Channels = []int{0, 4}
	b.cfg.Channels = []int{0, 5}

	assert.False(t, b.Register(1, 1, "Q2TEST-7"), "not one of B's channels")
	require.True(t, b.Register(5, 1, "Q2TEST-7"))

	require.True(t, a.Connect(4, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Equal(t, []int{4}, a.agw.estChannels)
	assert.Equal(t, []int{5}, b.agw.estChannels)

	assert.False(t, a.Send(0, 0, "Q1TEST", "QNODEB", []byte("x")), "the circuit is on channel 4")
	require.True(t, a.Send(4, 0, "Q1TEST", "QNODEB", []byte("hello B")))
	exchange(t, a, b)

	assert.Equal(t, [][]byte{[]byte("hello B")}, b.agw.data)
	assert.Equal(t, []int{0}, a.link.frameChannels[:1], "the neighbour is still on channel 0")
}

// Asking again for a circuit the client has already leaves it be, rather
// than telling the client it has gone.
func TestNodeDuplicateConnect(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, b.Register(0, 1, "Q2TEST-7"))
	require.True(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	require.True(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Empty(t, a.agw.terminated)
	assert.Len(t, a.circuits.circuits, 1)
	assert.True(t, a.Send(0, 0, "Q1TEST", "Q2TEST-7", []byte("still here")))
}

func TestNodeRefusesWithNoListener(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Empty(t, a.agw.established)
	assert.Equal(t, []string{"Q2TEST-7"}, a.agw.terminated)
	assert.Empty(t, b.agw.established)
}

func TestNodeConnectWithNoRoute(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEX", ax25.PIDNetROM))
	assert.Equal(t, []string{"QNODEX"}, a.agw.terminated, "told at once")
	assert.Empty(t, a.link.connects)
}

// What is not NET/ROM's is left for AX.25.
func TestNodeLeavesAX25Alone(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	assert.False(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", 0xF0), "another PID")
	assert.False(t, a.Connect(1, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM), "another channel")
	assert.False(t, a.Send(0, 0, "Q1TEST", "Q2TEST", []byte("x")), "no such circuit")
	assert.False(t, a.Disconnect(0, 0, "Q1TEST", "Q2TEST"), "no such circuit")
	assert.False(t, a.Register(0, 0, "Q1TEST"), "another callsign")
	assert.False(t, a.Register(1, 0, "Q1TEST-7"), "the node's callsign, on another channel")
	assert.Empty(t, a.link.connects)
}

// The review of the first implementation found a departed client's circuits
// left open.
func TestNodeClientGone(t *testing.T) {
	t.Parallel()

	var a, b = joinedNodes(t)

	require.True(t, b.Register(0, 1, "Q2TEST-7"))
	require.True(t, a.Connect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)
	require.Len(t, b.agw.established, 1)

	a.ClientGone(0)
	exchange(t, a, b)

	assert.Equal(t, []string{"Q1TEST"}, b.agw.terminated, "the far end is told")
	assert.Empty(t, a.agw.terminated, "the client that left is not")
	assert.Empty(t, a.circuits.circuits)

	b.ClientGone(1)

	var _, _, ok = b.acceptCircuit("Q1TEST", "Q1TEST-7")
	assert.False(t, ok, "nor does it take circuits any more")
}

func TestNodeFailedLink(t *testing.T) {
	t.Parallel()

	var a, _ = joinedNodes(t)

	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkTerminated(0, testClient, "Q2TEST-7", "Q1TEST-7", true)

	var _, ok = a.router.nextHop("Q2TEST-7")
	assert.False(t, ok, "a neighbour that keeps failing is passed over")

	a.LinkEstablished(0, testClient, "Q2TEST-7", "Q1TEST-7", true)

	_, ok = a.router.nextHop("Q2TEST-7")
	assert.True(t, ok, "until it is heard from")
}

func TestNodeIgnoresOtherPIDs(t *testing.T) {
	t.Parallel()

	var a = newTestNode("Q1TEST-7", "QNODEA")

	assert.NotPanics(t, func() { a.RecConnData(0, testClient, "Q2TEST-7", "Q1TEST-7", 0xF0, []byte("text")) })
	assert.NotPanics(t, func() { a.RecConnData(0, testClient, "Q2TEST-7", "Q1TEST-7", ax25.PIDNetROM, []byte("junk")) })
}

// Start registers the node's callsign, so that its neighbours can connect
// to it, and broadcasts straight away.  A nil Reporter is allowed.
func TestNodeStart(t *testing.T) {
	t.Parallel()

	var link = new(fakeDataLink)

	type sent struct {
		prio int
		pp   *ax25.Packet
	}

	var broadcasts = make(chan sent, 4)

	var cfg = testConfig("Q1TEST-7", "QNODEA")
	cfg.Channels = []int{0, 2}

	var n = New(cfg, link, func(_ int, prio int, pp *ax25.Packet) {
		broadcasts <- sent{prio: prio, pp: pp}
	}, nil, new(fakeClock), testClient)

	n.Start(t.Context())

	link.mu.Lock()
	assert.Equal(t, []registration{
		{addr: "Q1TEST-7", channel: 0, client: testClient},
		{addr: "Q1TEST-7", channel: 2, client: testClient},
	}, link.registered, "on each channel")
	link.mu.Unlock()

	select {
	case got := <-broadcasts:
		assert.Equal(t, dwcore.PrioLow, got.prio)
		assert.Equal(t, "NODES", got.pp.AddrWithSSID(ax25.Destination))
	case <-time.After(5 * time.Second):
		t.Fatal("no NODES broadcast")
	}

	assert.NotPanics(t, func() {
		n.circuitData(circuitID{channel: 0, client: 0, ownCall: "Q1TEST", remoteCall: "Q2TEST"}, []byte("x"))
	})
}

// mesh stands in for the data link and the radio between several nodes,
// each on channels of its own: a station is reached by its callsign on a
// channel, and a UI frame sent on a channel is heard by every other node on
// it.
type mesh struct {
	mu sync.Mutex

	nodes map[neighbour]*testNode // Each node, on each of its channels.

	frames []meshFrame
	ui     []meshUI
}

type meshFrame struct {
	channel int
	from    string
	to      string
	data    []byte
}

type meshUI struct {
	channel int
	from    *testNode
	pp      *ax25.Packet
}

// meshLink is the data link as one node on the mesh sees it.
type meshLink struct {
	mesh *mesh
}

func (l meshLink) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, _ int) {
	var m = l.mesh

	m.mu.Lock()
	var self = m.nodes[neighbour{channel: channel, callsign: addrs[ax25.Source]}]
	var peer = m.nodes[neighbour{channel: channel, callsign: addrs[ax25.Destination]}]
	m.mu.Unlock()

	if self == nil || peer == nil {
		return
	}

	self.LinkEstablished(channel, testClient, addrs[ax25.Destination], addrs[ax25.Source], false)
	peer.LinkEstablished(channel, testClient, addrs[ax25.Source], addrs[ax25.Destination], true)
}

func (l meshLink) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, channel int, _ int, _ int, data []byte) {
	l.mesh.mu.Lock()
	defer l.mesh.mu.Unlock()

	l.mesh.frames = append(l.mesh.frames, meshFrame{channel: channel, from: addrs[ax25.Source], to: addrs[ax25.Destination], data: slices.Clone(data)})
}

func (l meshLink) RegisterCallsign(string, int, int) {}

// add puts a node on the mesh, on channels.
func (m *mesh) add(callsign string, alias string, channels ...int) *testNode {
	var tn = new(testNode)
	tn.agw = new(recordingReporter)

	var cfg = testConfig(callsign, alias)
	cfg.Channels = channels

	tn.Node = New(cfg, meshLink{mesh: m}, func(channel int, _ int, pp *ax25.Packet) {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.ui = append(m.ui, meshUI{channel: channel, from: tn, pp: pp})
	}, tn.agw, new(fakeClock), testClient)

	for _, channel := range channels {
		m.nodes[neighbour{channel: channel, callsign: callsign}] = tn
	}

	return tn
}

// deliver hands each node what has been sent to it, saying how much that
// was.
func (m *mesh) deliver() int {
	m.mu.Lock()
	var frames, ui = m.frames, m.ui
	m.frames, m.ui = nil, nil

	var hearers = make(map[int][]*testNode)
	for nb, tn := range m.nodes {
		hearers[nb.channel] = append(hearers[nb.channel], tn)
	}

	var to = make([]*testNode, len(frames))
	for i, f := range frames {
		to[i] = m.nodes[neighbour{channel: f.channel, callsign: f.to}]
	}
	m.mu.Unlock()

	for _, u := range ui {
		for _, tn := range hearers[u.channel] {
			if tn != u.from {
				tn.HeardUI(u.channel, u.pp)
			}
		}
	}

	for i, f := range frames {
		if to[i] != nil {
			to[i].RecConnData(f.channel, testClient, f.from, f.to, ax25.PIDNetROM, f.data)
		}
	}

	return len(frames) + len(ui)
}

// settle delivers until the mesh falls quiet.
func (m *mesh) settle(t *testing.T) {
	t.Helper()

	for range 100 {
		if m.deliver() == 0 {
			return
		}
	}

	t.Fatal("the mesh never went quiet")
}

// A node on two channels routes between them: A and B share one channel,
// B and C another, and A learns of C through B, and holds a circuit to it
// that carries data both ways.
func TestNodeRoutesBetweenChannels(t *testing.T) {
	t.Parallel()

	var m = new(mesh)
	m.nodes = make(map[neighbour]*testNode)

	var a = m.add("Q1TEST-7", "QNODEA", 0)
	var b = m.add("Q2TEST-7", "QNODEB", 0, 1)
	var c = m.add("Q3TEST-7", "QNODEC", 1)

	// C's broadcast reaches B, and B's then reach A and C.
	for range 2 {
		c.broadcastNodes()
		b.broadcastNodes()
		a.broadcastNodes()
		m.settle(t)
	}

	var hop, ok = a.router.nextHop("Q3TEST-7")
	require.True(t, ok, "A learns of C")
	assert.Equal(t, neighbour{channel: 0, callsign: "Q2TEST-7"}, hop, "through B, on the channel they share")

	hop, ok = c.router.nextHop("Q1TEST-7")
	require.True(t, ok, "and C of A")
	assert.Equal(t, neighbour{channel: 1, callsign: "Q2TEST-7"}, hop)

	require.True(t, c.Register(1, 1, "Q3TEST-7"))

	require.True(t, a.Connect(0, 0, "Q1TEST", "QNODEC", ax25.PIDNetROM))
	m.settle(t)

	assert.Equal(t, []string{"QNODEC"}, a.agw.established)
	assert.Equal(t, []string{"Q1TEST"}, c.agw.established)
	assert.Equal(t, []int{1}, c.agw.estChannels, "where C's client registered")

	require.True(t, a.Send(0, 0, "Q1TEST", "QNODEC", []byte("hello C")))
	require.True(t, c.Send(1, 1, "Q3TEST-7", "Q1TEST", []byte("hello A")))
	m.settle(t)

	assert.Equal(t, [][]byte{[]byte("hello C")}, c.agw.data)
	assert.Equal(t, [][]byte{[]byte("hello A")}, a.agw.data)

	var bs = b.Status()
	assert.Equal(t, []NeighbourStatus{
		{Channel: 0, Callsign: "Q1TEST-7", Link: "up", Quality: int(DefaultQuality), Failures: 0, Failing: false},
		{Channel: 1, Callsign: "Q3TEST-7", Link: "up", Quality: int(DefaultQuality), Failures: 0, Failing: false},
	}, bs.Neighbours, "B has a link on each channel")

	require.True(t, a.Disconnect(0, 0, "Q1TEST", "QNODEC"))
	m.settle(t)

	assert.Equal(t, []string{"QNODEC"}, a.agw.terminated)
	assert.Equal(t, []string{"Q1TEST"}, c.agw.terminated)
}
