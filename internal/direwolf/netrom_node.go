// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node: what joins internal/netrom's Router and internal/node's shells to
// the AX.25 link layer.  NODES broadcasts are tapped from the received frames
// and sent as UI frames; NET/ROM packets travel to and from neighbours over
// connected AX.25 links with the NET/ROM PID, which the node opens as it needs
// them and also accepts from neighbours that open them first.  A station that
// connects to the node and is not a neighbour gets a shell.
//
// The Router and the shells are not safe for concurrent use, so they are only
// ever touched from the node's own goroutine: what arrives from elsewhere -
// the link layer's callbacks, a received frame, a question from the web
// interface - is handed to that goroutine as a function to run.

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/sirupsen/logrus"
)

// linkRequester is what the node asks the link layer for.  *DataLinkQueue is
// one.
type linkRequester interface {
	ConnectRequest(addrs [ax25.MaxAddrs]string, num_addr int, channel int, client int, pid int)
	DisconnectRequest(addrs [ax25.MaxAddrs]string, num_addr int, channel int, client int)
	XmitDataRequest(addrs [ax25.MaxAddrs]string, num_addr int, channel int, client int, pid int, xdata []byte)
	RegisterCallsign(addr string, channel int, client int)
}

// neighbourPending is the most NET/ROM packets kept for a neighbour while the
// link to it is being opened.
const neighbourPending = 64

// maxSendChunk is the most user data the node hands the link layer at once,
// so that what it sends fits an information field without the link layer
// having to segment it, which few stations can reassemble.
const maxSendChunk = 236

// axKey names an AX.25 link the node has: the link layer tells them apart by
// channel and both callsigns.
type axKey struct {
	port   int
	own    string
	remote string
}

// axSession is a connected AX.25 link the node has with another station.
// It is one of: a link to a neighbour node, carrying NET/ROM; a user's link
// to the node, with a shell; or a link the node opened onwards for a shell.
type axSession struct {
	key      axKey
	addrs    [ax25.MaxAddrs]string
	numAddr  int
	up       bool
	everUp   bool
	incoming bool
	pending  [][]byte // NET/ROM packets waiting for the link to come up.

	shell    *node.Shell   // For a user connected to the node.
	downlink node.Downlink // For a link opened onwards for a shell.
}

// netromNode runs a NET/ROM Router, and the shells of the users connected to
// it, over the AX.25 link layer.
type netromNode struct {
	cfg    netrom.Config
	router *netrom.Router
	links  linkRequester
	sendUI func(channel int, pp *ax25.Packet)
	client int

	shellCfg node.Config
	ports    []node.Port
	heard    *node.HeardList
	shells   map[*node.Shell]bool

	// work carries what is to be run on the node's goroutine.
	work chan func()

	sessions map[axKey]*axSession

	now func() time.Time
}

// newNetromNode returns a node for cfg, asking links for its AX.25 links and
// sending UI frames with sendUI, giving its users shells configured by
// shellCfg, listing ports for them.  Nothing happens until start.
func newNetromNode(cfg netrom.Config, shellCfg node.Config, ports []node.Port, links linkRequester, sendUI func(channel int, pp *ax25.Packet)) (*netromNode, error) {
	var n = new(netromNode)
	n.links = links
	n.sendUI = sendUI
	n.work = make(chan func(), 1024)
	n.sessions = make(map[axKey]*axSession)
	n.shells = make(map[*node.Shell]bool)
	n.heard = node.NewHeardList(heardMax)
	n.ports = ports
	n.now = time.Now

	var r, err = netrom.NewRouter(cfg, n, n.now())
	if err != nil {
		return nil, err
	}

	n.router = r
	n.cfg = r.Config()
	r.OnEvent = n.logEvent

	n.shellCfg = shellCfg
	n.shellCfg.Call = n.cfg.Call
	n.shellCfg.Alias = n.cfg.Alias

	// Users reach the shell over NET/ROM by connecting to the node itself.
	var lerr = r.Listen(n.cfg.Call, n.cfg.Alias, 0, n.acceptCircuit)
	if lerr != nil {
		return nil, lerr
	}

	return n, nil
}

// heardMax is how many stations the node's heard list remembers.
const heardMax = 200

// start registers the node's callsign and alias with the link layer as client,
// so neighbours and users can connect to it, then runs the node until ctx is
// cancelled.
func (n *netromNode) start(ctx context.Context, client int) {
	n.client = client

	for _, p := range n.cfg.Ports {
		n.links.RegisterCallsign(n.cfg.Call, p.Port, client)

		// An alias that is also a valid callsign can be connected to as one.
		var _, aliasErr = netrom.NormaliseCall(n.cfg.Alias)
		if n.cfg.Alias != "" && aliasErr == nil {
			n.links.RegisterCallsign(n.cfg.Alias, p.Port, client)
		}
	}

	go n.run(ctx)
}

// run is the node's goroutine.
func (n *netromNode) run(ctx context.Context) {
	var ticker = time.NewTicker(time.Second)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case f := <-n.work:
			f()
		case <-ticker.C:
			n.tick()
		}
	}
}

func (n *netromNode) tick() {
	var now = n.now()

	n.router.Tick(now)

	for s := range n.shells {
		s.Tick(now)
	}
}

// do runs f on the node's goroutine.  Called from the link layer's goroutine,
// it waits if the node has fallen far behind, rather than lose what arrived.
func (n *netromNode) do(f func()) {
	n.work <- f
}

// logEvent logs what the Router says happened.
func (n *netromNode) logEvent(e netrom.Event) {
	switch e.Kind {
	case netrom.EventRoutesChanged:
		logrus.WithFields(logrus.Fields{
			"destinations": len(n.router.Table().Destinations()),
			"neighbours":   len(n.router.Table().Neighbours()),
		}).Debug("NET/ROM routes changed")
	case netrom.EventCircuitUp:
		logrus.WithFields(logrus.Fields{
			"remote": e.Circuit.Remote,
			"user":   e.Circuit.User,
		}).Info("NET/ROM circuit up")
	case netrom.EventCircuitDown:
		logrus.WithFields(logrus.Fields{
			"remote": e.Circuit.Remote,
			"user":   e.Circuit.User,
			"error":  e.Err,
		}).Info("NET/ROM circuit down")
	}
}

// heardFrame takes in a received frame: every one goes in the heard list, and
// a NODES broadcast from a station heard directly on one of the node's ports
// goes to the Router.  It is called on the receive goroutine for every frame
// received.
func (n *netromNode) heardFrame(channel int, pp *ax25.Packet) {
	if n == nil || pp.NumAddr() < 2 || !n.hasPort(channel) {
		return
	}

	var from = pp.AddrWithSSID(ax25.Source)

	// A frame that came by way of a digipeater was not heard from its sender.
	var direct = pp.NumAddr() == 2 || pp.Heard() == ax25.Source

	var nodes = pp.NumAddr() == 2 && pp.Control() == ax25.UIFrame && pp.PID() == ax25.PIDNetROM &&
		pp.AddrWithSSID(ax25.Destination) == netrom.NodesDestination

	var info []byte
	if nodes {
		info = append([]byte(nil), pp.Info()...)
	}

	n.do(func() {
		var now = n.now()

		if direct {
			n.heard.Heard(channel, from, now)
		}

		if nodes {
			n.router.HeardNodes(channel, from, info, now)
		}
	})
}

// hasPort says whether channel is one of the node's ports.  The ports do not
// change once the node is made, so this is safe from any goroutine.
func (n *netromNode) hasPort(channel int) bool {
	return slices.ContainsFunc(n.ports, func(p node.Port) bool { return p.Number == channel })
}

// SendPacket is the Router handing a packet to a neighbour.
func (n *netromNode) SendPacket(to netrom.NeighbourKey, packet []byte) {
	var key = axKey{port: to.Port, own: n.cfg.Call, remote: to.Call}

	var s, ok = n.sessions[key]
	if !ok {
		s = n.newSession(key, nil)
	}

	if s.up {
		n.links.XmitDataRequest(s.addrs, s.numAddr, key.port, n.client, ax25.PIDNetROM, packet)

		return
	}

	if len(s.pending) >= neighbourPending {
		return // The far end has its own retries; better to drop than hoard.
	}

	s.pending = append(s.pending, packet)

	if len(s.pending) == 1 && !s.incoming {
		n.links.ConnectRequest(s.addrs, s.numAddr, key.port, n.client, ax25.PIDNoLayer3)
	}
}

// newSession starts keeping track of the link key, by way of the digipeaters
// via.
func (n *netromNode) newSession(key axKey, via []string) *axSession {
	var s = new(axSession)
	s.key = key
	s.addrs[ax25.Source] = key.own
	s.addrs[ax25.Destination] = key.remote
	s.numAddr = 2

	for _, v := range via {
		s.addrs[s.numAddr] = v
		s.numAddr++
	}

	n.sessions[key] = s

	return s
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

	n.sendUI(port, pp)
}

// keyOf names the link the link layer is telling us about.
func keyOf(channel int, ownCall string, remoteCall string) axKey {
	return axKey{port: channel, own: normalisedCall(ownCall), remote: normalisedCall(remoteCall)}
}

func normalisedCall(call string) string {
	var c, err = netrom.NormaliseCall(call)
	if err != nil {
		return call
	}

	return c
}

// isNodeCall says whether call is one the node answers to: its callsign or its
// alias.
func (n *netromNode) isNodeCall(call string) bool {
	return call == n.cfg.Call || (n.cfg.Alias != "" && call == n.cfg.Alias)
}

// isNeighbour says whether the station key is talking to is a known
// neighbour node, rather than a user.
func (n *netromNode) isNeighbour(key axKey) bool {
	var _, ok = n.router.Table().Neighbour(netrom.NeighbourKey{Port: key.port, Call: key.remote})

	return ok
}

// LinkEstablished is the link layer saying a link is up.
func (n *netromNode) LinkEstablished(channel int, _ int, remoteCall string, ownCall string, incoming bool) {
	var key = keyOf(channel, ownCall, remoteCall)

	n.do(func() { n.linkUp(key, incoming) })
}

func (n *netromNode) linkUp(key axKey, incoming bool) {
	var s, ok = n.sessions[key]
	if !ok {
		s = n.newSession(key, nil)
	}

	s.up = true
	s.everUp = true
	s.incoming = s.incoming || incoming

	for _, p := range s.pending {
		n.links.XmitDataRequest(s.addrs, s.numAddr, key.port, n.client, ax25.PIDNetROM, p)
	}

	s.pending = nil

	switch {
	case s.downlink != nil:
		s.downlink.Connected()
	case incoming && n.isNodeCall(key.own) && !n.isNeighbour(key):
		n.startShell(s)
	}
}

// LinkTerminated is the link layer saying a link has gone, or could not be
// opened.
func (n *netromNode) LinkTerminated(channel int, _ int, remoteCall string, ownCall string, timeout bool) {
	var key = keyOf(channel, ownCall, remoteCall)

	n.do(func() { n.linkDown(key, timeout) })
}

func (n *netromNode) linkDown(key axKey, timeout bool) {
	var s, ok = n.sessions[key]
	if !ok {
		return
	}

	delete(n.sessions, key)

	if s.shell != nil {
		delete(n.shells, s.shell)
		s.shell.Closed()
	}

	if s.downlink != nil {
		var err error
		if !s.everUp {
			err = errNoAnswer
		}

		s.downlink.Closed(err)
	}

	if !s.everUp && len(s.pending) > 0 {
		logrus.WithFields(logrus.Fields{
			"channel":   key.port,
			"neighbour": key.remote,
			"timeout":   timeout,
		}).Info("NET/ROM: could not reach neighbour")
		n.router.NeighbourFailed(netrom.NeighbourKey{Port: key.port, Call: key.remote}, n.now())
	}
}

var errNoAnswer = errors.New("no answer")

// RecConnData is the link layer handing over data that arrived on a link.
func (n *netromNode) RecConnData(channel int, _ int, remoteCall string, ownCall string, pid int, data []byte) {
	var key = keyOf(channel, ownCall, remoteCall)
	var copied = append([]byte(nil), data...)

	n.do(func() { n.linkData(key, pid, copied) })
}

func (n *netromNode) linkData(key axKey, pid int, data []byte) {
	var s, ok = n.sessions[key]

	if pid == ax25.PIDNetROM {
		if ok && s.shell != nil {
			// A node after all, which we took for a user before we had
			// heard of it.
			delete(n.shells, s.shell)
			s.shell.Closed()
			s.shell = nil
		}

		n.router.ReceivePacket(netrom.NeighbourKey{Port: key.port, Call: key.remote}, data, n.now())

		return
	}

	if !ok {
		return
	}

	switch {
	case s.shell != nil:
		s.shell.Input(data, n.now())
	case s.downlink != nil:
		s.downlink.Received(data)
	}
}

// OutstandingFramesReply is never asked for.
func (n *netromNode) OutstandingFramesReply(int, int, string, string, int) {}

// netrom_init starts the node, if the configuration asks for one, taking its
// AX.25 links through links.  It returns nil for no node.
func netrom_init(ctx context.Context, audio *RadioConfig, misc *misc_config_s, links *linkRouter) (*netromNode, error) {
	if misc.netrom == nil {
		return nil, nil //nolint:nilnil // No node is not an error.
	}

	var cfg, err = netromConfigFor(*misc.netrom, audio)
	if err != nil {
		return nil, err
	}

	var n, nerr = newNetromNode(cfg, misc.node, nodePorts(audio), dataLinkQueue, func(channel int, pp *ax25.Packet) {
		transmitQueue.Append(channel, TQ_PRIO_1_LO, pp)
	})
	if nerr != nil {
		return nil, nerr
	}

	n.start(ctx, links.attach(n))

	logrus.WithFields(logrus.Fields{
		"call":  n.cfg.Call,
		"alias": n.cfg.Alias,
	}).Info("NET/ROM node started")

	return n, nil
}
