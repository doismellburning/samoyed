// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package netrom

// A NET/ROM node.
//
// The node ties the pieces together: it broadcasts NODES and learns routes
// from its neighbours' broadcasts (router.go), holds connected-mode AX.25
// links to its neighbours over which layer 3 frames travel, forwards frames
// for other nodes a hop nearer their destination, and hands frames addressed
// to it to its transport circuits (circuit.go), which it offers to client
// applications.
//
// Between neighbours NET/ROM rides on ordinary AX.25 connections: the node is
// a client of the data link, registered for its own callsign so that other
// nodes can connect to it, and connecting to a neighbour itself the first
// time it has something to send there, holding what it has to send until the
// link is made.  Only NODES broadcasts go out as UI frames.

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwcore"
	"github.com/sirupsen/logrus"
)

const (
	DefaultTTL           = 16
	DefaultNodesInterval = time.Hour
	DefaultQuality       = 192
	DefaultMinQuality    = 10
)

// Config is a node's settings.
type Config struct {
	Channels      []int         // The channels the node runs on.
	Callsign      string        // The node's AX.25 address, as NormaliseCallsign gives it.
	Alias         string        // The short name other nodes know it by.
	TTL           byte          // How many nodes a frame it originates may cross.
	NodesInterval time.Duration // Between NODES broadcasts.
	Quality       byte          // Assumed for the link to any neighbour.
	MinQuality    byte          // Below which a route is not worth keeping.
}

// DefaultConfig is a configuration with the defaults filled in, for the
// settings to start from.
func DefaultConfig() Config {
	return Config{
		Channels:      nil,
		Callsign:      "",
		Alias:         "",
		TTL:           DefaultTTL,
		NodesInterval: DefaultNodesInterval,
		Quality:       DefaultQuality,
		MinQuality:    DefaultMinQuality,
	}
}

// MaxFrame is the longest layer 3 frame the node sends: the headers and a
// full INFO frame's data.  An AX.25 v2.0 link splits anything longer than
// its PACLEN over several I frames, which NET/ROM, expecting a frame in each,
// cannot put back together.
const MaxFrame = headerLen + maxInfo

// DataLink is what the node asks of the connected-mode data link.
// *direwolf.DataLinkQueue is one.
type DataLink interface {
	RegisterCallsign(addr string, channel int, client int)
	ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int)
	XmitDataRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int, xdata []byte)
}

// Reporter is told what happens on circuits, for the client applications
// that hold them: a circuit coming up or going down, and data arriving on it.
// *direwolf.AGWServer is one.
type Reporter interface {
	LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool)
	LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool)
	RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte)

	// OutstandingFramesReply answers Outstanding: how many frames are still
	// to go out or be acknowledged.  Mind the order of the callsigns: our
	// own comes first here.
	OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int)
}

// noReporter is the Reporter for when there are no client applications.
type noReporter struct{}

func (noReporter) LinkEstablished(int, int, string, string, bool)       {}
func (noReporter) LinkTerminated(int, int, string, string, bool)        {}
func (noReporter) RecConnData(int, int, string, string, int, []byte)    {}
func (noReporter) OutstandingFramesReply(int, int, string, string, int) {}

// linkState is how far a link to a neighbour has got.
type linkState int

const (
	neighbourDown linkState = iota
	neighbourConnecting
	neighbourUp
)

// maxPending bounds the frames held for a neighbour while the link to it is
// being made.  Beyond that the circuits' retransmission will send them again
// anyway.
const maxPending = 32

// neighbourLink is a link to a neighbour, and what is waiting for it.
type neighbourLink struct {
	state   linkState
	pending [][]byte
}

// Node is a NET/ROM node.  It is safe for concurrent use.
type Node struct {
	cfg      Config
	router   *router
	circuits *circuitManager

	link    DataLink
	send    dwcore.FrameSender
	clients Reporter
	client  int // What the data link knows the node's own links by.

	mu        sync.Mutex
	links     map[neighbour]*neighbourLink
	listeners []listener // Clients taking incoming circuits.
}

// listener is a client's registration of the node callsign on a channel,
// to take incoming circuits there.
type listener struct {
	client  int
	channel int
}

// New makes a node from cfg.  It reaches its neighbours through link, as
// client number client, and sends its NODES broadcasts through send; clients
// is told about circuits and may be nil; clock runs its timers.  Nothing
// happens until Start.
func New(cfg Config, link DataLink, send dwcore.FrameSender, clients Reporter, clock Clock, client int) *Node {
	var n = new(Node)
	n.cfg = cfg
	n.cfg.Channels = slices.Clone(cfg.Channels)
	n.router = newRouter(cfg.Callsign, cfg.Quality, cfg.MinQuality)
	n.circuits = newCircuitManager(cfg.Callsign, cfg.TTL, n, clock)
	n.link = link
	n.send = send
	n.client = client
	n.links = make(map[neighbour]*neighbourLink)

	n.clients = clients
	if n.clients == nil {
		n.clients = noReporter{}
	}

	return n
}

// Start registers the node's callsign with the data link on each of its
// channels, so that its neighbours can connect to it, and broadcasts NODES
// now and every interval after until ctx is cancelled.
func (n *Node) Start(ctx context.Context) {
	for _, channel := range n.cfg.Channels {
		n.link.RegisterCallsign(n.cfg.Callsign, channel, n.client)
	}

	go n.run(ctx)

	logrus.WithFields(logrus.Fields{"channels": n.cfg.Channels, "callsign": n.cfg.Callsign, "alias": n.cfg.Alias}).Info("NET/ROM node started")
}

// run broadcasts NODES now and every interval after, until ctx is cancelled.
func (n *Node) run(ctx context.Context) {
	var ticker = time.NewTicker(n.cfg.NodesInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		n.broadcastNodes()

		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

// broadcastNodes tells the neighbours on each channel what this node can
// reach, then ages the routing table.  Every channel hears the same: a route
// is offered back even to the neighbours it goes through, who ignore it,
// as split horizon has them do.
func (n *Node) broadcastNodes() {
	var frames, err = EncodeNodes(n.cfg.Alias, n.router.broadcastEntries())
	if err != nil {
		logrus.WithError(err).Error("NET/ROM: cannot build NODES broadcast")

		return
	}

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = NodesCallsign
	addrs[ax25.Source] = n.cfg.Callsign

	for _, channel := range n.cfg.Channels {
		for _, info := range frames {
			var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, info)
			if pp != nil {
				n.send(channel, dwcore.PrioLow, pp)
			}
		}
	}

	n.router.age()

	logrus.WithFields(logrus.Fields{"channels": n.cfg.Channels, "frames": len(frames)}).Debug("NET/ROM: NODES broadcast")
}

// HeardUI takes a received UI frame, which is a NODES broadcast if it is
// anything to NET/ROM.  One that came through a digipeater is not from a
// neighbour, and is ignored.
func (n *Node) HeardUI(channel int, pp *ax25.Packet) {
	if !n.onChannel(channel) || pp.NumAddr() != 2 || pp.PID() != ax25.PIDNetROM {
		return
	}

	if !strings.EqualFold(pp.AddrNoSSID(ax25.Destination), NodesCallsign) {
		return
	}

	var from, err = NormaliseCallsign(pp.AddrWithSSID(ax25.Source))
	if err != nil {
		return
	}

	var nodes, decodeErr = DecodeNodes(pp.Info())
	if decodeErr != nil {
		logrus.WithError(decodeErr).WithField("from", from).Debug("NET/ROM: ignoring NODES broadcast")

		return
	}

	n.router.heardNodes(neighbour{channel: channel, callsign: from}, nodes)
}

// The data link reports on our links to neighbours.

func (n *Node) LinkEstablished(channel int, _ int, remoteCall string, _ string, incoming bool) {
	var nb, ok = n.neighbourOn(channel, remoteCall)
	if !ok {
		return
	}

	var pending = n.linkUp(nb)

	n.router.neighbourHeard(nb)

	logrus.WithFields(logrus.Fields{"channel": channel, "neighbour": remoteCall, "incoming": incoming, "pending": pending}).Debug("NET/ROM: link to neighbour up")
}

func (n *Node) LinkTerminated(channel int, _ int, remoteCall string, _ string, timeout bool) {
	var nb, ok = n.neighbourOn(channel, remoteCall)
	if !ok {
		return
	}

	n.linkDown(nb)

	if timeout {
		n.router.neighbourFailed(nb)
	}

	logrus.WithFields(logrus.Fields{"channel": channel, "neighbour": remoteCall, "timeout": timeout}).Debug("NET/ROM: link to neighbour down")
}

func (n *Node) RecConnData(channel int, _ int, remoteCall string, _ string, pid int, data []byte) {
	if pid != ax25.PIDNetROM {
		return
	}

	var nb, ok = n.neighbourOn(channel, remoteCall)
	if !ok {
		return
	}

	n.router.neighbourHeard(nb)

	var f, err = DecodeFrame(data)
	if err != nil {
		logrus.WithError(err).WithField("neighbour", nb.callsign).Debug("NET/ROM: ignoring undecodable frame")

		return
	}

	// The neighbour is evidently a way back to where the frame came from,
	// which may be somewhere no broadcast has told us of yet: a neighbour
	// that connects before we hear its NODES, or a node further off that
	// is not advertised.  Without this, the answer would have nowhere to go.
	n.router.heardFrom(f.origin, nb)

	n.route(f, data)
}

func (n *Node) OutstandingFramesReply(int, int, string, string, int) {}

// neighbourOn is the neighbour that is call, as the data link writes it, on
// channel, if that is one of the node's channels.
func (n *Node) neighbourOn(channel int, call string) (neighbour, bool) {
	var normalised, err = NormaliseCallsign(call)
	if err != nil || !n.onChannel(channel) {
		var none neighbour

		return none, false
	}

	return neighbour{channel: channel, callsign: normalised}, true
}

// onChannel says whether the node runs on channel.
func (n *Node) onChannel(channel int) bool {
	return slices.Contains(n.cfg.Channels, channel)
}

// linkTo returns the link to nb, making a record of it if there is none.
// n.mu must be held.
func (n *Node) linkTo(nb neighbour) *neighbourLink {
	var l, ok = n.links[nb]
	if !ok {
		l = new(neighbourLink)
		n.links[nb] = l
	}

	return l
}

// linkUp marks the link to neighbour up and sends what was waiting for it,
// saying how much that was.  It sends with the lock held, so that a frame
// sent to the neighbour meanwhile cannot get ahead of those waiting; the
// data link only queues them.
func (n *Node) linkUp(nb neighbour) int {
	n.mu.Lock()
	defer n.mu.Unlock()

	var l = n.linkTo(nb)
	l.state = neighbourUp

	var pending = l.pending
	l.pending = nil

	for _, data := range pending {
		n.xmit(nb, data)
	}

	return len(pending)
}

// linkDown marks the link to nb down, dropping what was waiting for it.
func (n *Node) linkDown(nb neighbour) {
	n.mu.Lock()
	defer n.mu.Unlock()

	var l = n.linkTo(nb)
	l.state = neighbourDown
	l.pending = nil
}

// route takes a layer 3 frame from a neighbour, f decoded from raw: ours if
// it is addressed to us, otherwise to be passed on a hop nearer its
// destination while its time to live lasts.  A frame passed on is the one
// received, byte for byte but for the time to live, rather than what we make
// of it, so that nothing another implementation put there is lost on the
// way through.
func (n *Node) route(f *Frame, raw []byte) {
	if f.destination == n.cfg.Callsign {
		n.circuits.rx(f)

		return
	}

	var log = logrus.WithFields(logrus.Fields{"origin": f.origin, "destination": f.destination})

	if f.ttl <= 1 {
		log.Trace("NET/ROM: time to live expired")

		return
	}

	var neighbour, ok = n.router.nextHop(f.destination)
	if !ok {
		log.Trace("NET/ROM: no route, frame not forwarded")

		return
	}

	var forwarded = slices.Clone(raw)
	forwarded[ttlOffset]--

	n.sendToNeighbour(neighbour, forwarded)
}

// sendLayer3 sends a frame on its way to its destination node, through the
// neighbour the routing table says is best.
func (n *Node) sendLayer3(f *Frame) {
	var log = logrus.WithFields(logrus.Fields{"destination": f.destination, "opcode": f.opcode.String()})

	var neighbour, ok = n.router.nextHop(f.destination)
	if !ok {
		log.Trace("NET/ROM: no route, frame dropped")

		return
	}

	var data, err = f.encode()
	if err != nil {
		log.WithError(err).Error("NET/ROM: cannot encode frame")

		return
	}

	n.sendToNeighbour(neighbour, data)
}

// sendToNeighbour sends a layer 3 frame over the link to nb.  If the link
// is not up, the frame waits while it is made: the data link would throw
// away data for a link we have asked for and it has not yet made.
func (n *Node) sendToNeighbour(nb neighbour, data []byte) {
	n.mu.Lock()

	var l = n.linkTo(nb)

	var state = l.state

	switch state {
	case neighbourUp:
		// With the lock held, as linkUp sends, to keep the order.
		n.xmit(nb, data)
	case neighbourDown:
		l.state = neighbourConnecting

		fallthrough
	case neighbourConnecting:
		if len(l.pending) < maxPending {
			l.pending = append(l.pending, data)
		}
	}

	n.mu.Unlock()

	if state == neighbourDown {
		var addrs [ax25.MaxAddrs]string
		addrs[ax25.Source] = n.cfg.Callsign
		addrs[ax25.Destination] = nb.callsign

		n.link.ConnectRequest(addrs, 2, nb.channel, n.client, ax25.PIDNetROM)
	}
}

// xmit hands a frame to the data link for a link that is up.
func (n *Node) xmit(nb neighbour, data []byte) {
	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = n.cfg.Callsign
	addrs[ax25.Destination] = nb.callsign

	n.link.XmitDataRequest(addrs, 2, nb.channel, n.client, ax25.PIDNetROM, data)
}

// The circuits ask the node, and report to it.

// acceptCircuit hands an incoming circuit to the lowest numbered client
// taking them, on the lowest numbered channel it registered on, so that the
// choice is the same each time.
func (n *Node) acceptCircuit(user string, originNode string) (listener, string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(n.listeners) == 0 {
		logrus.WithFields(logrus.Fields{"user": user, "node": originNode}).Info("NET/ROM: refusing circuit - no client has registered the node callsign")

		var none listener

		return none, "", false
	}

	var first = slices.MinFunc(n.listeners, func(a, b listener) int {
		return cmp.Or(cmp.Compare(a.client, b.client), cmp.Compare(a.channel, b.channel))
	})

	return first, n.cfg.Callsign, true
}

func (n *Node) circuitEstablished(c circuitID, incoming bool) {
	n.clients.LinkEstablished(c.channel, c.client, c.remoteCall, c.ownCall, incoming)
}

func (n *Node) circuitTerminated(c circuitID, timeout bool) {
	n.clients.LinkTerminated(c.channel, c.client, c.remoteCall, c.ownCall, timeout)
}

func (n *Node) circuitData(c circuitID, data []byte) {
	n.clients.RecConnData(c.channel, c.client, c.remoteCall, c.ownCall, ax25.PIDNetROM, data)
}

// What client applications ask of the node.  Each reports whether the
// request was the node's to handle; anything else is plain AX.25.

// Register takes a client's registration of the node callsign on one of
// the node's channels, which is how a client offers to take incoming
// circuits, hearing of them on that channel.  It is not passed on to the
// data link, where the node itself answers for that callsign.
func (n *Node) Register(channel int, client int, call string) bool {
	if !n.isNodeCall(channel, call) {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	var l = listener{client: client, channel: channel}
	if !slices.Contains(n.listeners, l) {
		n.listeners = append(n.listeners, l)
	}

	return true
}

// Unregister withdraws a client's registration of the node callsign.
func (n *Node) Unregister(channel int, client int, call string) bool {
	if !n.isNodeCall(channel, call) {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	var gone = listener{client: client, channel: channel}
	n.listeners = slices.DeleteFunc(n.listeners, func(l listener) bool { return l == gone })

	return true
}

func (n *Node) isNodeCall(channel int, call string) bool {
	var normalised, err = NormaliseCallsign(call)

	return err == nil && n.onChannel(channel) && normalised == n.cfg.Callsign
}

// Connect opens a circuit for a connect request with the NET/ROM PID on
// one of the node's channels, which the client then has the circuit on,
// whichever channel its frames go out on.  The destination may be a node's
// callsign or alias.
func (n *Node) Connect(channel int, client int, ownCall string, remoteCall string, pid int) bool {
	if !n.onChannel(channel) || pid != ax25.PIDNetROM {
		return false
	}

	var id = circuitID{channel: channel, client: client, ownCall: ownCall, remoteCall: remoteCall}

	var log = logrus.WithFields(logrus.Fields{"channel": channel, "client": client, "user": ownCall, "destination": remoteCall})

	var _, err = NormaliseCallsign(ownCall)
	if err != nil {
		log.WithError(err).Error("NET/ROM: cannot open a circuit for that user")
		n.circuitTerminated(id, false)

		return true
	}

	var destination, ok = n.router.resolve(remoteCall)
	if ok {
		_, ok = n.router.nextHop(destination)
	}

	if !ok {
		log.Info("NET/ROM: no route to that node")
		n.circuitTerminated(id, false)

		return true
	}

	var connectErr = n.circuits.connect(id, destination)

	switch {
	case errors.Is(connectErr, errCircuitExists):
		// The client has this circuit already, and it carries on.
		log.Info("NET/ROM: circuit already open")

		return true
	case errors.Is(connectErr, errCircuitOnOtherChannel):
		// That one carries on, and this one is refused: the client asked
		// for what it cannot have, which is no fault of ours.
		log.Info("NET/ROM: circuit already open on another channel")
		n.circuitTerminated(id, false)

		return true
	case connectErr != nil:
		log.WithError(connectErr).Error("NET/ROM: cannot open circuit")
		n.circuitTerminated(id, false)

		return true
	}

	log.WithField("node", destination).Debug("NET/ROM: opening circuit")

	return true
}

// Send sends data on the client's circuit, if it has one by those
// callsigns on one of the node's channels.
func (n *Node) Send(channel int, client int, ownCall string, remoteCall string, data []byte) bool {
	return n.onChannel(channel) &&
		n.circuits.send(circuitID{channel: channel, client: client, ownCall: ownCall, remoteCall: remoteCall}, data) == nil
}

// Disconnect closes the client's circuit, if it has one by those
// callsigns on one of the node's channels.
func (n *Node) Disconnect(channel int, client int, ownCall string, remoteCall string) bool {
	return n.onChannel(channel) &&
		n.circuits.disconnect(circuitID{channel: channel, client: client, ownCall: ownCall, remoteCall: remoteCall}) == nil
}

// Outstanding answers, through the Reporter, how many of the client's
// frames on its circuit by those callsigns are still to go out or be
// acknowledged, if it has such a circuit on one of the node's channels.  A
// client pacing itself by the answer keeps the circuit's queue from
// growing.
func (n *Node) Outstanding(channel int, client int, ownCall string, remoteCall string) bool {
	if !n.onChannel(channel) {
		return false
	}

	var count, ok = n.circuits.outstanding(circuitID{channel: channel, client: client, ownCall: ownCall, remoteCall: remoteCall})
	if !ok {
		return false
	}

	n.clients.OutstandingFramesReply(channel, client, ownCall, remoteCall, count)

	return true
}

// ClientGone closes a departed client's circuits and forgets its
// registration.
func (n *Node) ClientGone(client int) {
	n.circuits.dropClient(client)

	n.mu.Lock()
	defer n.mu.Unlock()

	n.listeners = slices.DeleteFunc(n.listeners, func(l listener) bool { return l.client == client })
}
