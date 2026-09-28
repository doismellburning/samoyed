// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"net"
	"sync"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeNetromLinkLayer stands in for the data link.  It records what the node
// asks of it and, when joined to another node, carries the frames there as
// though over an AX.25 connection.
type fakeNetromLinkLayer struct {
	mu sync.Mutex

	connects []string // Neighbours asked for.
	frames   []string // Neighbours sent to.

	self     *netromNode
	peer     *netromNode // Where frames go, if anywhere; links to it come up at once.
	selfCall string
	queue    [][]byte
}

func (l *fakeNetromLinkLayer) ConnectRequest(addrs [ax25.MaxAddrs]string, _ int, _ int, client int, _ int) {
	if client != netromClient {
		panic("the node must connect as its own client")
	}

	l.mu.Lock()
	l.connects = append(l.connects, addrs[PEERCALL])

	var self, peer = l.self, l.peer
	l.mu.Unlock()

	if peer == nil {
		return
	}

	// The link is made, and both ends hear about it from their data links.
	self.LinkEstablished(0, netromClient, addrs[PEERCALL], addrs[OWNCALL], false)
	peer.LinkEstablished(0, netromClient, addrs[OWNCALL], addrs[PEERCALL], true)
}

func (l *fakeNetromLinkLayer) XmitDataRequest(addrs [ax25.MaxAddrs]string, _ int, _ int, _ int, pid int, data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if pid != ax25.PIDNetROM {
		panic("NET/ROM goes with its own PID")
	}

	l.frames = append(l.frames, addrs[PEERCALL])
	l.queue = append(l.queue, append([]byte(nil), data...))
}

func (l *fakeNetromLinkLayer) RegisterCallsign(string, int, int) {}

// deliver passes what has been sent to the peer node.
func (l *fakeNetromLinkLayer) deliver() int {
	l.mu.Lock()
	var queue = l.queue
	l.queue = nil
	l.mu.Unlock()

	for _, data := range queue {
		l.peer.RecConnData(0, netromClient, l.selfCall, l.peer.cfg.callsign, ax25.PIDNetROM, data)
	}

	return len(queue)
}

func testNetromConfig(callsign string, alias string) netromConfig {
	var cfg = defaultNetromConfig()
	cfg.enabled = true
	cfg.callsign = callsign
	cfg.alias = alias

	return cfg
}

// testNetromNode is a node on channel 0 whose AGW reports are recorded and
// whose UI frames are kept.
type testNetromNode struct {
	*netromNode

	link *fakeNetromLinkLayer
	agw  *recordingLinkClient
	ui   []*ax25.Packet
}

func newTestNetromNode(callsign string, alias string) *testNetromNode {
	var tn = new(testNetromNode)
	tn.link = new(fakeNetromLinkLayer)
	tn.link.selfCall = callsign
	tn.agw = new(recordingLinkClient)

	tn.netromNode = newNetromNode(testNetromConfig(callsign, alias), tn.link, func(_ int, pp *ax25.Packet) {
		tn.ui = append(tn.ui, pp)
	}, new(fakeNetromClock))
	tn.netromNode.agw = func() linkClient { return tn.agw }

	return tn
}

// joinedNetromNodes is two neighbouring nodes that have heard each other's
// broadcasts.
func joinedNetromNodes(t *testing.T) (*testNetromNode, *testNetromNode) {
	t.Helper()

	var a = newTestNetromNode("Q1TEST-7", "QNODEA")
	var b = newTestNetromNode("Q2TEST-7", "QNODEB")
	a.link.self, a.link.peer = a.netromNode, b.netromNode
	b.link.self, b.link.peer = b.netromNode, a.netromNode

	a.broadcastNodes()
	b.broadcastNodes()
	b.heardUI(0, a.ui[0])
	a.heardUI(0, b.ui[0])

	return a, b
}

// exchange passes frames between two nodes until they fall quiet.
func exchange(t *testing.T, a *testNetromNode, b *testNetromNode) {
	t.Helper()

	for range 100 {
		if a.link.deliver()+b.link.deliver() == 0 {
			return
		}
	}

	t.Fatal("the two nodes never went quiet")
}

func TestNetromNodeBroadcasts(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")

	a.broadcastNodes()

	require.Len(t, a.ui, 1)

	var pp = a.ui[0]
	assert.Equal(t, "NODES", pp.AddrWithSSID(ax25.Destination))
	assert.Equal(t, "Q1TEST-7", pp.AddrWithSSID(ax25.Source))
	assert.Equal(t, ax25.PIDNetROM, pp.PID())
	assert.Equal(t, append([]byte{0xFF}, "QNODEA"...), pp.Info())
}

func TestNetromNodeHearsBroadcasts(t *testing.T) {
	var a, b = joinedNetromNodes(t)

	var hop, ok = a.router.nextHop("Q2TEST-7")
	require.True(t, ok)
	assert.Equal(t, "Q2TEST-7", hop)

	_, ok = b.router.nextHop("Q1TEST-7")
	assert.True(t, ok)
}

func TestNetromNodeIgnoresOtherUIFrames(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")
	var b = newTestNetromNode("Q2TEST-7", "QNODEB")
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

	a.heardUI(1, nodes)
	a.heardUI(0, digipeated)
	a.heardUI(0, toSomeoneElse)

	var _, ok = a.router.nextHop("Q2TEST-7")
	assert.False(t, ok)
}

// Frames go to a neighbour over a connected link, which is asked for the
// first time and not again while it is up or on its way.  Until it is up,
// frames wait: the data link throws away data for a link it is still making.
func TestNetromNodeConnectsToNeighbours(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")

	a.sendToNeighbour("Q2TEST-7", []byte("one"))
	a.sendToNeighbour("Q2TEST-7", []byte("two"))
	assert.Equal(t, []string{"Q2TEST-7"}, a.link.connects)
	assert.Empty(t, a.link.frames, "nothing sent before the link is up")

	a.LinkEstablished(0, netromClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Equal(t, [][]byte{[]byte("one"), []byte("two")}, a.link.queue, "then what was waiting, in order")

	a.sendToNeighbour("Q2TEST-7", []byte("three"))
	assert.Len(t, a.link.connects, 1)
	assert.Len(t, a.link.frames, 3)

	a.LinkTerminated(0, netromClient, "Q2TEST-7", "Q1TEST-7", false)
	a.sendToNeighbour("Q2TEST-7", []byte("four"))
	assert.Len(t, a.link.connects, 2, "and again once it has gone down")
	assert.Len(t, a.link.frames, 3)

	a.LinkTerminated(0, netromClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkEstablished(0, netromClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Len(t, a.link.frames, 3, "what was waiting for a link that failed is dropped")

	for range 2 * netromMaxPending {
		a.LinkTerminated(0, netromClient, "Q2TEST-7", "Q1TEST-7", false)
	}

	for range 2 * netromMaxPending {
		a.sendToNeighbour("Q2TEST-7", []byte("x"))
	}

	a.LinkEstablished(0, netromClient, "Q2TEST-7", "Q1TEST-7", false)
	assert.Len(t, a.link.frames, 3+netromMaxPending, "and only so much waits")
}

// A neighbour connecting to us makes the link up for our traffic too.
func TestNetromNodeUsesAnIncomingLink(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")

	a.LinkEstablished(0, netromClient, "Q2TEST-7", "Q1TEST-7", true)
	a.sendToNeighbour("Q2TEST-7", []byte("one"))

	assert.Empty(t, a.link.connects)
	assert.Len(t, a.link.frames, 1)
}

func TestNetromNodeForwards(t *testing.T) {
	var a, _ = joinedNetromNodes(t)

	var f = new(netromFrame)
	f.origin = "Q3TEST-7"
	f.destination = "Q2TEST-7"
	f.ttl = 5
	f.opcode = netromOpDiscReq

	a.route(f)
	require.Equal(t, []string{"Q2TEST-7"}, a.link.frames, "sent on towards its destination")

	var sent, err = decodeNetromFrame(a.link.queue[0])
	require.NoError(t, err)
	assert.Equal(t, byte(4), sent.ttl, "a hop older")

	f.ttl = 1
	a.route(f)
	assert.Len(t, a.link.frames, 1, "but not once its time is up")

	f.ttl = 5
	f.destination = "Q9TEST"
	a.route(f)
	assert.Len(t, a.link.frames, 1, "nor where there is no route")
}

func TestNetromNodeCircuitEndToEnd(t *testing.T) {
	var a, b = joinedNetromNodes(t)

	require.True(t, b.agwRegister(0, 1, "Q2TEST-7"), "B's client takes its incoming circuits")

	require.True(t, a.agwConnect(0, 0, "Q1TEST", "QNODEB", ax25.PIDNetROM), "by alias")
	exchange(t, a, b)

	assert.Equal(t, []string{"QNODEB"}, a.agw.established)
	assert.Equal(t, []string{"Q1TEST"}, b.agw.established)

	require.True(t, a.agwSend(0, 0, "Q1TEST", "QNODEB", []byte("hello B")))
	require.True(t, b.agwSend(0, 1, "Q2TEST-7", "Q1TEST", []byte("hello A")))
	exchange(t, a, b)

	assert.Equal(t, [][]byte{[]byte("hello B")}, b.agw.data)
	assert.Equal(t, [][]byte{[]byte("hello A")}, a.agw.data)
	assert.Equal(t, []int{ax25.PIDNetROM}, a.agw.pids)

	require.True(t, a.agwDisconnect(0, 0, "Q1TEST", "QNODEB"))
	exchange(t, a, b)

	assert.Equal(t, []string{"QNODEB"}, a.agw.terminated)
	assert.Equal(t, []string{"Q1TEST"}, b.agw.terminated)
}

// Asking again for a circuit the client has already leaves it be, rather
// than telling the client it has gone.
func TestNetromNodeDuplicateConnect(t *testing.T) {
	var a, b = joinedNetromNodes(t)

	require.True(t, b.agwRegister(0, 1, "Q2TEST-7"))
	require.True(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	require.True(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Empty(t, a.agw.terminated)
	assert.Len(t, a.circuits.circuits, 1)
	assert.True(t, a.agwSend(0, 0, "Q1TEST", "Q2TEST-7", []byte("still here")))
}

func TestNetromNodeRefusesWithNoListener(t *testing.T) {
	var a, b = joinedNetromNodes(t)

	require.True(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)

	assert.Empty(t, a.agw.established)
	assert.Equal(t, []string{"Q2TEST-7"}, a.agw.terminated)
	assert.Empty(t, b.agw.established)
}

func TestNetromNodeConnectWithNoRoute(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")

	require.True(t, a.agwConnect(0, 0, "Q1TEST", "QNODEX", ax25.PIDNetROM))
	assert.Equal(t, []string{"QNODEX"}, a.agw.terminated, "told at once")
	assert.Empty(t, a.link.connects)
}

// What is not NET/ROM's is left for AX.25.
func TestNetromNodeLeavesAX25Alone(t *testing.T) {
	var a, _ = joinedNetromNodes(t)

	assert.False(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", 0xF0), "another PID")
	assert.False(t, a.agwConnect(1, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM), "another channel")
	assert.False(t, a.agwSend(0, 0, "Q1TEST", "Q2TEST", []byte("x")), "no such circuit")
	assert.False(t, a.agwDisconnect(0, 0, "Q1TEST", "Q2TEST"), "no such circuit")
	assert.False(t, a.agwRegister(0, 0, "Q1TEST"), "another callsign")
	assert.False(t, a.agwRegister(1, 0, "Q1TEST-7"), "the node's callsign, on another channel")
	assert.Empty(t, a.link.connects)
}

// The review of the first implementation found a departed client's circuits
// left open.
func TestNetromNodeClientGone(t *testing.T) {
	var a, b = joinedNetromNodes(t)

	require.True(t, b.agwRegister(0, 1, "Q2TEST-7"))
	require.True(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)
	require.Len(t, b.agw.established, 1)

	a.agwClientGone(0)
	exchange(t, a, b)

	assert.Equal(t, []string{"Q1TEST"}, b.agw.terminated, "the far end is told")
	assert.Empty(t, a.agw.terminated, "the client that left is not")
	assert.Empty(t, a.circuits.circuits)

	b.agwClientGone(1)

	var _, _, ok = b.acceptCircuit("Q1TEST", "Q1TEST-7")
	assert.False(t, ok, "nor does it take circuits any more")
}

func TestNetromNodeFailedLink(t *testing.T) {
	var a, _ = joinedNetromNodes(t)

	a.LinkTerminated(0, netromClient, "Q2TEST-7", "Q1TEST-7", true)
	a.LinkTerminated(0, netromClient, "Q2TEST-7", "Q1TEST-7", true)

	var _, ok = a.router.nextHop("Q2TEST-7")
	assert.False(t, ok, "a neighbour that keeps failing is passed over")

	a.LinkEstablished(0, netromClient, "Q2TEST-7", "Q1TEST-7", true)

	_, ok = a.router.nextHop("Q2TEST-7")
	assert.True(t, ok, "until it is heard from")
}

func TestNetromNodeIgnoresOtherPIDs(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")

	assert.NotPanics(t, func() { a.RecConnData(0, netromClient, "Q2TEST-7", "Q1TEST-7", 0xF0, []byte("text")) })
	assert.NotPanics(t, func() { a.RecConnData(0, netromClient, "Q2TEST-7", "Q1TEST-7", ax25.PIDNetROM, []byte("junk")) })
}

func TestNetromInitChecksTheChannel(t *testing.T) {
	var testCases = []struct {
		name   string
		medium medium_e
	}{
		{"an unconfigured channel", MEDIUM_NONE},
		{"an IGate channel", MEDIUM_IGATE},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var audio = new(AudioConfig)
			audio.chan_medium[0] = tc.medium

			netromInit(t.Context(), audio, testNetromConfig("Q1TEST-7", "QNODEA"), AX25_N1_PACLEN_DEFAULT)

			assert.Nil(t, theNetromNode.Load())
		})
	}

	t.Run("no audio configuration", func(t *testing.T) {
		netromInit(t.Context(), nil, testNetromConfig("Q1TEST-7", "QNODEA"), AX25_N1_PACLEN_DEFAULT)
		assert.Nil(t, theNetromNode.Load())
	})

	t.Run("not enabled", func(t *testing.T) {
		var audio = new(AudioConfig)
		audio.chan_medium[0] = MEDIUM_RADIO

		netromInit(t.Context(), audio, defaultNetromConfig(), AX25_N1_PACLEN_DEFAULT)
		assert.Nil(t, theNetromNode.Load())
	})
}

func TestNetromInitStartsANode(t *testing.T) {
	setupTestEnv(t)

	var audio = new(AudioConfig)
	audio.chan_medium[6] = MEDIUM_NETTNC

	var cfg = testNetromConfig("Q1TEST-7", "QNODEA")
	cfg.channel = 6

	t.Cleanup(func() {
		theNetromNode.Store(nil)
		setInternalLinkClient(netromClient, nil)
	})

	transmitQueue.Init(audio)

	var registration = dlqAppended(func() { netromInit(t.Context(), audio, cfg, AX25_N1_PACLEN_DEFAULT) })

	var node = theNetromNode.Load()
	require.NotNil(t, node, "an NCHANNEL can carry NET/ROM")
	assert.Same(t, node, linkClientFor(netromClient))

	require.NotNil(t, registration, "the node answers for its callsign on the data link")
	assert.Equal(t, DLQ_REGISTER_CALLSIGN, registration._type)
	assert.Equal(t, "Q1TEST-7", registration.addrs[0])
	assert.Equal(t, 6, registration._chan)
	assert.Equal(t, netromClient, registration.client)
}

// useNetromNode makes a node the running one for the length of the test.
func useNetromNode(t *testing.T, n *netromNode) {
	t.Helper()

	theNetromNode.Store(n)
	t.Cleanup(func() { theNetromNode.Store(nil) })
}

func agwCommand(kind byte, from string, to string, pid byte, data []byte) *AGWPEMessage {
	var cmd = new(AGWPEMessage)
	cmd.Header.DataKind = kind
	cmd.Header.Portx = 0
	cmd.Header.PID = pid
	copy(cmd.Header.CallFrom[:], from)
	copy(cmd.Header.CallTo[:], to)
	cmd.Data = data
	cmd.Header.DataLen = uint32(len(data))

	return cmd
}

func agwServerForNetrom() *AGWServer {
	var s = new(AGWServer)
	s.audioConfigP = new(AudioConfig)
	s.audioConfigP.chan_medium[0] = MEDIUM_RADIO

	return s
}

// The AGW server hands the node what is NET/ROM's, and the data link
// everything else, as before.
func TestAGWServerRoutesNetromRequests(t *testing.T) {
	var a, b = joinedNetromNodes(t)
	useNetromNode(t, a.netromNode)

	require.True(t, b.agwRegister(0, 1, "Q2TEST-7"))

	var s = agwServerForNetrom()

	t.Run("a connect with the NET/ROM PID opens a circuit", func(t *testing.T) {
		var item = dlqAppended(func() {
			s.handleClientCommand(0, agwCommand('c', "Q1TEST", "Q2TEST-7", ax25.PIDNetROM, nil))
		})
		assert.Nil(t, item, "not an AX.25 connect")
		assert.Len(t, a.circuits.circuits, 1)
	})

	exchange(t, a, b)
	require.Len(t, a.agw.established, 1)

	t.Run("data on the circuit goes over it", func(t *testing.T) {
		var item = dlqAppended(func() {
			s.handleClientCommand(0, agwCommand('D', "Q1TEST", "Q2TEST-7", 0xF0, []byte("hi")))
		})
		assert.Nil(t, item)

		exchange(t, a, b)
		assert.Equal(t, [][]byte{[]byte("hi")}, b.agw.data)
	})

	t.Run("data for anything else is AX.25", func(t *testing.T) {
		var item = dlqAppended(func() {
			s.handleClientCommand(0, agwCommand('D', "Q1TEST", "Q3TEST", 0xF0, []byte("hi")))
		})
		require.NotNil(t, item)
		assert.Equal(t, DLQ_XMIT_DATA_REQUEST, item._type)
	})

	t.Run("an ordinary connect is AX.25", func(t *testing.T) {
		var item = dlqAppended(func() {
			s.handleClientCommand(0, agwCommand('C', "Q1TEST", "Q3TEST", 0, nil))
		})
		require.NotNil(t, item)
		assert.Equal(t, DLQ_CONNECT_REQUEST, item._type)
	})

	t.Run("disconnecting the circuit closes it", func(t *testing.T) {
		var item = dlqAppended(func() {
			s.handleClientCommand(0, agwCommand('d', "Q1TEST", "Q2TEST-7", 0, nil))
		})
		assert.Nil(t, item)

		exchange(t, a, b)
		assert.Equal(t, []string{"Q2TEST-7"}, a.agw.terminated)
	})
}

// Registering the node's callsign offers to take its incoming circuits; it
// must not reach the data link, which would then hand the node's neighbour
// links to the client.
func TestAGWServerRegistersTheNodeCallsignWithTheNode(t *testing.T) {
	var a = newTestNetromNode("Q1TEST-7", "QNODEA")
	useNetromNode(t, a.netromNode)

	var s = agwServerForNetrom()
	var client = setupClientPipe(t, s)

	var reply = asyncReply(client)
	var item = dlqAppended(func() { s.handleClientCommand(0, agwCommand('X', "Q1TEST-7", "", 0, nil)) })
	assert.Nil(t, item)
	require.Equal(t, []byte{1}, (<-reply).Data, "reported as a success")

	var got, _, ok = a.acceptCircuit("Q2TEST", "Q2TEST-7")
	assert.True(t, ok)
	assert.Equal(t, 0, got)

	reply = asyncReply(client)
	item = dlqAppended(func() { s.handleClientCommand(0, agwCommand('X', "Q1TEST", "", 0, nil)) })
	<-reply
	require.NotNil(t, item, "any other callsign is registered with the data link")
	assert.Equal(t, DLQ_REGISTER_CALLSIGN, item._type)

	item = dlqAppended(func() { s.handleClientCommand(0, agwCommand('x', "Q1TEST-7", "", 0, nil)) })
	assert.Nil(t, item)

	_, _, ok = a.acceptCircuit("Q2TEST", "Q2TEST-7")
	assert.False(t, ok)
}

func TestAGWServerClosesADepartedClientsCircuits(t *testing.T) {
	var a, b = joinedNetromNodes(t)
	useNetromNode(t, a.netromNode)

	require.True(t, b.agwRegister(0, 1, "Q2TEST-7"))
	require.True(t, a.agwConnect(0, 0, "Q1TEST", "Q2TEST-7", ax25.PIDNetROM))
	exchange(t, a, b)
	require.Len(t, a.circuits.circuits, 1)

	var s = agwServerForNetrom()
	var server, other = net.Pipe()
	t.Cleanup(func() { other.Close() })
	s.clients[0].conn = server

	dlqAppended(func() { s.detachClient(0, server) })

	exchange(t, a, b)
	assert.Empty(t, a.circuits.circuits)
	assert.Equal(t, []string{"Q1TEST"}, b.agw.terminated)
}
