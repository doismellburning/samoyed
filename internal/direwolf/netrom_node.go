// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node's NET/ROM: what joins internal/netrom's Router to the AX.25 data
// link and to the node shell.  NODES broadcasts are tapped from the received
// frames and sent as UI frames; NET/ROM packets travel to and from neighbours
// over connected AX.25 links with the NET/ROM PID, which the shell hands over
// as they come and go, and which NET/ROM opens itself as it needs them.  A
// user reaches the shell over a circuit to the node, and the shell connects
// users onward over circuits to other nodes.
//
// The Router is not safe for concurrent use, so it is only ever touched from
// NET/ROM's own goroutine.  Everything that arrives from elsewhere - the
// shell, the receive path, a question from the web interface - is queued for
// that goroutine as a function to run.  The queue has no limit, so nothing
// queueing work ever waits on the goroutine doing it, and the shell and
// NET/ROM can call each other freely.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/doismellburning/samoyed/internal/metrics"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/doismellburning/samoyed/internal/nodeevents"
	"github.com/sirupsen/logrus"
)

// neighbourPending is the most NET/ROM packets kept for a neighbour while the
// link to it is being opened.
const neighbourPending = 64

// neighbourLink is an AX.25 link to a neighbour node, carrying NET/ROM.
type neighbourLink struct {
	up       bool
	everUp   bool
	incoming bool
	pending  [][]byte // Packets waiting for the link to come up.
}

// workQueue is a queue of functions to run, without limit.
type workQueue struct {
	mu    sync.Mutex
	items []func()
	ready chan struct{}
}

func newWorkQueue() *workQueue {
	var q = new(workQueue)
	q.ready = make(chan struct{}, 1)

	return q
}

func (q *workQueue) push(f func()) {
	q.mu.Lock()
	q.items = append(q.items, f)
	q.mu.Unlock()

	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// take returns everything queued so far.
func (q *workQueue) take() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()

	var items = q.items
	q.items = nil

	return items
}

// netromNode runs a NET/ROM Router for the node.
type netromNode struct {
	cfg    netrom.Config
	router *netrom.Router
	link   node.DataLink
	sendUI dwcore.FrameSender
	client int
	shell  *node.Node

	work *workQueue

	// links are the AX.25 links to neighbours, by channel and neighbour.
	links map[netrom.NeighbourKey]*neighbourLink

	// events is told what happens; nil for nobody.
	events *nodeevents.Bus

	// heard is when each station was last heard on a NET/ROM port, so it is
	// published as heard only the first time in a while.
	heard map[netrom.NeighbourKey]time.Time

	// snapshot is what the shell can read of the routing table from any
	// goroutine, refreshed whenever the table changes.
	snapMu     sync.Mutex
	nodes      []netrom.Destination
	neighbours []netrom.Neighbour

	now func() time.Time
}

// newNetromNode returns NET/ROM for the node shell, asking link for its AX.25
// links as client and sending UI frames with sendUI.  Nothing happens until
// run.
func newNetromNode(cfg netrom.Config, shell *node.Node, link node.DataLink, client int, sendUI dwcore.FrameSender) (*netromNode, error) {
	var n = new(netromNode)
	n.link = link
	n.client = client
	n.sendUI = sendUI
	n.shell = shell
	n.work = newWorkQueue()
	n.links = make(map[netrom.NeighbourKey]*neighbourLink)
	n.heard = make(map[netrom.NeighbourKey]time.Time)
	n.now = time.Now

	var r, err = netrom.NewRouter(cfg, n, n.now())
	if err != nil {
		return nil, err
	}

	n.router = r
	n.cfg = r.Config()
	r.OnEvent = n.routerEvent

	// Users reach the shell over NET/ROM by connecting to the node itself.
	var lerr = r.Listen(n.cfg.Call, n.cfg.Alias, 0, n.acceptCircuit)
	if lerr != nil {
		return nil, lerr
	}

	n.refresh()

	return n, nil
}

// run is NET/ROM's goroutine, which runs until ctx is cancelled.
func (n *netromNode) run(ctx context.Context) {
	var ticker = time.NewTicker(time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-n.work.ready:
			for _, f := range n.work.take() {
				f()
			}
		case <-ticker.C:
			n.router.Tick(n.now())
		}
	}
}

// do queues f to run on NET/ROM's goroutine.
func (n *netromNode) do(f func()) {
	n.work.push(f)
}

// routerEvent logs what the Router says happened, keeping the shell's view of
// the table up to date.
func (n *netromNode) routerEvent(e netrom.Event) {
	var ev = n.event(nodeevents.RoutesChanged)

	switch e.Kind {
	case netrom.EventRoutesChanged:
		n.refresh()
		logrus.WithFields(logrus.Fields{
			"destinations": len(n.nodes),
			"neighbours":   len(n.neighbours),
		}).Debug("NET/ROM routes changed")
		metrics.SetNetROMDestinations(len(n.nodes))

		ev.Destinations = len(n.nodes)
		ev.Neighbours = len(n.neighbours)
	case netrom.EventCircuitUp:
		logrus.WithFields(logrus.Fields{"remote": e.Circuit.Remote, "user": e.Circuit.User}).Info("NET/ROM circuit up")

		ev.Kind = nodeevents.CircuitUp
	case netrom.EventCircuitDown:
		logrus.WithFields(logrus.Fields{"remote": e.Circuit.Remote, "user": e.Circuit.User, "error": e.Err}).Info("NET/ROM circuit down")

		ev.Kind = nodeevents.CircuitDown
		ev.Error = failureText(e.Err)
	}

	if e.Kind != netrom.EventRoutesChanged {
		metrics.SetNetROMCircuits(len(n.router.Circuits()))

		ev.Local = e.Circuit.Local
		ev.Remote = e.Circuit.Remote
		ev.User = e.Circuit.User
		ev.Incoming = e.Circuit.Incoming
	}

	n.events.Publish(ev)
}

// event returns an event of kind about this node, now.
func (n *netromNode) event(kind nodeevents.Kind) nodeevents.Event {
	return newEvent(kind, n.cfg.Call, n.now())
}

// linkEvent publishes a neighbour link coming up or going down.
func (n *netromNode) linkEvent(kind nodeevents.Kind, key netrom.NeighbourKey, incoming bool, failure string) {
	var ev = n.event(kind)
	var port = key.Port
	ev.Port = &port
	ev.Local = n.cfg.Call
	ev.Remote = key.Call
	ev.Role = "neighbour"
	ev.Incoming = incoming
	ev.Error = failure
	n.events.Publish(ev)
}

// heardQuiet is how long a station must go unheard to count as heard afresh.
const heardQuiet = 15 * time.Minute

// maxHeard is the most stations the heard map holds before it starts again.
const maxHeard = 1000

// heardStation notes a station heard directly, publishing it if it is the
// first time in a while.  It runs on NET/ROM's goroutine.
func (n *netromNode) heardStation(key netrom.NeighbourKey, now time.Time) {
	var last, ok = n.heard[key]
	if ok && now.Sub(last) < heardQuiet {
		n.heard[key] = now

		return
	}

	if len(n.heard) >= maxHeard {
		clear(n.heard)
	}

	n.heard[key] = now

	var ev = n.event(nodeevents.Heard)
	var port = key.Port
	ev.Port = &port
	ev.Remote = key.Call
	n.events.Publish(ev)
}

// refresh copies the routing table for the shell.  It runs on NET/ROM's
// goroutine.
func (n *netromNode) refresh() {
	var nodes = n.router.Table().Destinations()
	var neighbours = n.router.Table().Neighbours()

	n.snapMu.Lock()
	n.nodes = nodes
	n.neighbours = neighbours
	n.snapMu.Unlock()
}

// heardFrame takes in a received frame, keeping it if it is a NODES broadcast
// from a station heard directly.  It is called on the receive goroutine for
// every frame received; a nil netromNode takes none.
func (n *netromNode) heardFrame(channel int, pp *ax25.Packet) {
	if n == nil || pp.NumAddr() < 2 {
		return
	}

	var _, onPort = n.cfg.PortConfig(channel)
	if !onPort {
		return
	}

	// A frame that came by way of a digipeater was not heard from its sender.
	var direct = pp.NumAddr() == 2 || pp.Heard() == ax25.Source

	var nodes = pp.NumAddr() == 2 && pp.Control() == ax25.UIFrame && pp.PID() == ax25.PIDNetROM &&
		pp.AddrWithSSID(ax25.Destination) == netrom.NodesDestination

	if !direct {
		return
	}

	var from = pp.AddrWithSSID(ax25.Source)

	var info []byte
	if nodes {
		info = append([]byte(nil), pp.Info()...)
	}

	n.do(func() {
		var now = n.now()

		n.heardStation(neighbourKey(channel, from), now)

		if nodes {
			n.router.HeardNodes(channel, from, info, now)
		}
	})
}

// SendPacket is the Router handing a packet to a neighbour, over the link to
// it, which is opened if there is none.
func (n *netromNode) SendPacket(to netrom.NeighbourKey, packet []byte) {
	var l, ok = n.links[to]
	if !ok {
		l = new(neighbourLink)
		n.links[to] = l
	}

	if l.up {
		n.link.XmitDataRequest(n.addrs(to), 2, to.Port, n.client, ax25.PIDNetROM, packet)

		return
	}

	if len(l.pending) >= neighbourPending {
		return // The far end has its own retries; better to drop than hoard.
	}

	l.pending = append(l.pending, packet)

	if len(l.pending) == 1 {
		n.link.ConnectRequest(n.addrs(to), 2, to.Port, n.client, ax25.PIDNoLayer3)
	}
}

// BroadcastNodes is the Router sending a NODES broadcast.
func (n *netromNode) BroadcastNodes(port int, info []byte) {
	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = netrom.NodesDestination
	addrs[ax25.Source] = n.cfg.Call

	var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, info)
	if pp == nil {
		return
	}

	n.sendUI(port, dwcore.PrioLow, pp)
}

// addrs are the addresses of the node's link to a neighbour.
func (n *netromNode) addrs(to netrom.NeighbourKey) [ax25.MaxAddrs]string {
	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = n.cfg.Call
	addrs[ax25.Destination] = to.Call

	return addrs
}

// neighbourKey names the neighbour at the far end of a link.
func neighbourKey(channel int, remoteCall string) netrom.NeighbourKey {
	var call, err = netrom.NormaliseCall(remoteCall)
	if err != nil {
		call = remoteCall
	}

	return netrom.NeighbourKey{Port: channel, Call: call}
}

// IsNeighbour says whether call, on channel, is a neighbour node.  The shell
// asks, from any goroutine.
func (n *netromNode) IsNeighbour(channel int, call string) bool {
	var key = neighbourKey(channel, call)

	n.snapMu.Lock()
	defer n.snapMu.Unlock()

	return slices.ContainsFunc(n.neighbours, func(nb netrom.Neighbour) bool { return nb.NeighbourKey == key })
}

// LinkUp is the shell handing over a link that carries NET/ROM.
func (n *netromNode) LinkUp(channel int, _ string, remoteCall string, incoming bool) {
	var key = neighbourKey(channel, remoteCall)

	n.do(func() {
		var l, ok = n.links[key]
		if !ok {
			l = new(neighbourLink)
			n.links[key] = l
		}

		if !l.up {
			n.linkEvent(nodeevents.LinkUp, key, incoming, "")
		}

		l.up = true
		l.everUp = true
		l.incoming = l.incoming || incoming

		for _, p := range l.pending {
			n.link.XmitDataRequest(n.addrs(key), 2, key.Port, n.client, ax25.PIDNetROM, p)
		}

		l.pending = nil
	})
}

// LinkDown is the shell saying a link that carried NET/ROM has gone, or could
// not be opened.
func (n *netromNode) LinkDown(channel int, _ string, remoteCall string, timeout bool) {
	var key = neighbourKey(channel, remoteCall)

	n.do(func() {
		var l, ok = n.links[key]
		if !ok {
			return
		}

		delete(n.links, key)

		if l.everUp {
			var failure string
			if timeout {
				failure = "timeout"
			}

			n.linkEvent(nodeevents.LinkDown, key, l.incoming, failure)
		}

		if !l.everUp && len(l.pending) > 0 {
			logrus.WithFields(logrus.Fields{
				"channel":   key.Port,
				"neighbour": key.Call,
				"timeout":   timeout,
			}).Info("NET/ROM: could not reach neighbour")
			n.router.NeighbourFailed(key, n.now())
		}
	})
}

// Packet is the shell handing over a NET/ROM packet.
func (n *netromNode) Packet(channel int, _ string, remoteCall string, data []byte) {
	var key = neighbourKey(channel, remoteCall)
	var copied = append([]byte(nil), data...)

	n.do(func() { n.router.ReceivePacket(key, copied, n.now()) })
}

// Nodes is the routing table's destinations, for the shell.
func (n *netromNode) Nodes() []netrom.Destination {
	n.snapMu.Lock()
	defer n.snapMu.Unlock()

	return slices.Clone(n.nodes)
}

// Neighbours is the routing table's neighbours, for the shell.
func (n *netromNode) Neighbours() []netrom.Neighbour {
	n.snapMu.Lock()
	defer n.snapMu.Unlock()

	return slices.Clone(n.neighbours)
}

// Knows says whether name is a node NET/ROM knows a way to.
func (n *netromNode) Knows(name string) bool {
	n.snapMu.Lock()
	defer n.snapMu.Unlock()

	return slices.ContainsFunc(n.nodes, func(d netrom.Destination) bool {
		return strings.EqualFold(d.Call, name) || (d.Alias != "" && strings.EqualFold(d.Alias, name))
	})
}

// circuitConn is a node.Conn over a NET/ROM circuit.  Its methods may be
// called from any goroutine: they queue the work for NET/ROM's.
type circuitConn struct {
	n *netromNode
	c *netrom.Circuit // Set on NET/ROM's goroutine; nil until the circuit exists.
}

func (cc *circuitConn) Send(data []byte) {
	var copied = append([]byte(nil), data...)

	cc.n.do(func() {
		if cc.c != nil {
			_ = cc.c.Write(copied)
		}
	})
}

func (cc *circuitConn) Close() {
	cc.n.do(func() {
		if cc.c != nil {
			cc.c.Close()
		}
	})
}

// Connect connects a user of the shell onward to the node dest.
func (n *netromNode) Connect(dest string, user string, events node.Downlink) node.Conn { //nolint:ireturn // node.NetROM's signature.
	var cc = &circuitConn{n: n, c: nil}

	n.do(func() {
		var c, err = n.router.Connect(dest, user, downlinkEvents{events: events}, n.now())
		if err != nil {
			events.Closed("no route")

			return
		}

		cc.c = c
	})

	return cc
}

// downlinkEvents passes what happens on a circuit the shell asked for back to
// the shell.
type downlinkEvents struct {
	events node.Downlink
}

func (d downlinkEvents) Connected(*netrom.Circuit)               { d.events.Connected() }
func (d downlinkEvents) Received(_ *netrom.Circuit, data []byte) { d.events.Received(data) }
func (d downlinkEvents) Closed(_ *netrom.Circuit, err error)     { d.events.Closed(failureText(err)) }

// failureText says, as the shell does, why a circuit failed.
func failureText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, netrom.ErrTimeout):
		return "no answer"
	case errors.Is(err, netrom.ErrNoRoute):
		return "no route"
	default:
		return "busy"
	}
}

// acceptCircuit gives a user connecting to the node over NET/ROM a session
// with the shell.
func (n *netromNode) acceptCircuit(c *netrom.Circuit) netrom.CircuitHandler { //nolint:ireturn // netrom.Acceptor's signature.
	return &circuitUser{n: n, conn: &circuitConn{n: n, c: c}, user: c.User(), session: nil}
}

// circuitUser passes what happens on a user's circuit to their session.
type circuitUser struct {
	n       *netromNode
	conn    *circuitConn
	user    string
	session *node.Attached
}

func (u *circuitUser) Connected(*netrom.Circuit) {
	u.session = u.n.shell.Attach(u.user, u.conn)
}

func (u *circuitUser) Received(_ *netrom.Circuit, data []byte) {
	if u.session != nil {
		u.session.Received(data)
	}
}

func (u *circuitUser) Closed(*netrom.Circuit, error) {
	if u.session != nil {
		u.session.Closed()
	}
}

// startNetROM gives the node shell NET/ROM, configured by cfg and going by
// the node's address and alias, and starts it running until ctx is cancelled.
// It must be called before the shell starts.
func startNetROM(ctx context.Context, cfg netrom.Config, audio *RadioConfig, nodeConfig node.Config, shell *node.Node,
	link node.DataLink, sendUI dwcore.FrameSender, events *nodeevents.Bus,
) (*netromNode, error) {
	var final, err = netromConfigFor(cfg, audio, nodeConfig)
	if err != nil {
		return nil, err
	}

	var n, nerr = newNetromNode(final, shell, link, nodeClient, sendUI)
	if nerr != nil {
		return nil, nerr
	}

	n.events = events
	shell.SetNetROM(n)

	go n.run(ctx)

	logrus.WithFields(logrus.Fields{"call": n.cfg.Call, "alias": n.cfg.Alias}).Info("NET/ROM started")

	return n, nil
}
