// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// A NET/ROM node.
//
// The node ties the pieces together: it broadcasts NODES and learns routes
// from its neighbours' broadcasts (netrom_router.go), holds connected-mode
// AX.25 links to its neighbours over which layer 3 frames travel, forwards
// frames for other nodes a hop nearer their destination, and hands frames
// addressed to it to its transport circuits (netrom_circuit.go), which it
// offers to AGW clients.
//
// Between neighbours NET/ROM rides on ordinary AX.25 connections, as the
// Linux and BPQ implementations have it: the node is an internal client of
// the data link (linkclient.go), registered for its own callsign so that
// other nodes can connect to it, and connecting to a neighbour itself the
// first time it has something to send there, holding what it has to send
// until the link is made.  Only NODES broadcasts go out as UI frames.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/sirupsen/logrus"
)

const (
	netromDefaultTTL           = 16
	netromDefaultNodesInterval = time.Hour
	netromDefaultQuality       = 192
	netromDefaultMinQuality    = 10
)

// netromConfig is what the NETROM directive configures.
type netromConfig struct {
	enabled       bool
	channel       int
	callsign      string
	alias         string
	ttl           byte
	nodesInterval time.Duration
	quality       byte // Assumed for the link to any neighbour.
	minQuality    byte // Below which a route is not worth keeping.
}

// defaultNetromConfig is a disabled configuration with the defaults filled
// in, for the directive to start from.
func defaultNetromConfig() netromConfig {
	return netromConfig{
		enabled:       false,
		channel:       0,
		callsign:      "",
		alias:         "",
		ttl:           netromDefaultTTL,
		nodesInterval: netromDefaultNodesInterval,
		quality:       netromDefaultQuality,
		minQuality:    netromDefaultMinQuality,
	}
}

// netromLinkLayer is what the node asks of the connected-mode data link;
// *DataLinkQueue is the real one.
type netromLinkLayer interface {
	ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int)
	XmitDataRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int, data []byte)
	RegisterCallsign(addr string, channel int, client int)
}

// netromLinkState is how far a link to a neighbour has got.
type netromLinkState int

const (
	netromLinkDown netromLinkState = iota
	netromLinkConnecting
	netromLinkUp
)

// netromMaxPending bounds the frames held for a neighbour while the link to
// it is being made.  Beyond that the circuits' retransmission will send them
// again anyway.
const netromMaxPending = 32

// netromNeighbourLink is a link to a neighbour, and what is waiting for it.
type netromNeighbourLink struct {
	state   netromLinkState
	pending [][]byte
}

type netromNode struct {
	cfg      netromConfig
	router   *netromRouter
	circuits *netromCircuitManager

	linkLayer netromLinkLayer
	sendUI    func(channel int, pp *ax25.Packet)
	agw       func() linkClient // Where circuit events are reported.

	mu        sync.Mutex
	links     map[string]*netromNeighbourLink // By neighbour callsign.
	listeners []int                           // AGW clients taking incoming circuits.
}

// theNetromNode is the node, when one is configured.
var theNetromNode atomic.Pointer[netromNode] //nolint:gochecknoglobals

func newNetromNode(cfg netromConfig, linkLayer netromLinkLayer, sendUI func(int, *ax25.Packet), clock netromClock) *netromNode {
	var n = new(netromNode)
	n.cfg = cfg
	n.router = newNetromRouter(cfg.callsign, cfg.quality, cfg.minQuality)
	n.circuits = newNetromCircuitManager(cfg.callsign, cfg.ttl, n, clock)
	n.linkLayer = linkLayer
	n.sendUI = sendUI
	n.agw = func() linkClient { return linkClientFor(0) }
	n.links = make(map[string]*netromNeighbourLink)

	return n
}

// netromMaxFrame is the longest layer 3 frame the node sends: the headers
// and a full INFO frame's data.
const netromMaxFrame = netromHeaderLen + netromMaxInfo

// netromInit starts the configured node, if there is one.  Its NODES
// broadcasts run until ctx is cancelled.  paclen is the configured maximum
// AX.25 information field.
func netromInit(ctx context.Context, audio *AudioConfig, cfg netromConfig, paclen int) {
	if !cfg.enabled {
		return
	}

	var log = logrus.WithFields(logrus.Fields{"channel": cfg.channel, "callsign": cfg.callsign, "alias": cfg.alias})

	// The config parser can only bound the channel number: the NETROM line
	// may come before whatever configures the channel.
	if audio == nil || cfg.channel < 0 || cfg.channel >= MAX_TOTAL_CHANS {
		log.Error("NET/ROM: no such channel, so no NET/ROM node was started")

		return
	}

	switch audio.chan_medium[cfg.channel] {
	case MEDIUM_RADIO, MEDIUM_NETTNC:
	case MEDIUM_IGATE:
		log.Error("NET/ROM: an IGate channel cannot carry NET/ROM, so no NET/ROM node was started")

		return
	default:
		log.Error("NET/ROM: the channel is not configured as a radio channel or an NCHANNEL, so no NET/ROM node was started")

		return
	}

	// An AX.25 v2.0 link splits anything longer than PACLEN over several
	// I frames, which NET/ROM, expecting a frame in each, cannot put back
	// together.
	if paclen < netromMaxFrame {
		log.WithField("paclen", paclen).Warnf("NET/ROM: PACLEN should be at least %d, or longer NET/ROM frames will be split and lost", netromMaxFrame)
	}

	var n = newNetromNode(cfg, dataLinkQueue, func(channel int, pp *ax25.Packet) {
		transmitQueue.Append(channel, TQ_PRIO_1_LO, pp)
	}, realNetromClock{})

	setInternalLinkClient(netromClient, n)
	dataLinkQueue.RegisterCallsign(cfg.callsign, cfg.channel, netromClient)
	theNetromNode.Store(n)

	go n.run(ctx)

	log.Info("NET/ROM node started")
}

// run broadcasts NODES now and every interval after, until ctx is cancelled.
func (n *netromNode) run(ctx context.Context) {
	var ticker = time.NewTicker(n.cfg.nodesInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		n.broadcastNodes()

		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

// broadcastNodes tells the neighbours what this node can reach, then ages
// the routing table, as netromd does.
func (n *netromNode) broadcastNodes() {
	var frames, err = encodeNetromNodes(n.cfg.alias, n.router.broadcastEntries())
	if err != nil {
		logrus.WithError(err).Error("NET/ROM: cannot build NODES broadcast")

		return
	}

	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Destination] = netromNodesCallsign
	addrs[ax25.Source] = n.cfg.callsign

	for _, info := range frames {
		var pp = ax25.UFrame(addrs, 2, ax25.CRCmd, ax25.FrameTypeUUI, 0, ax25.PIDNetROM, info)
		if pp != nil {
			n.sendUI(n.cfg.channel, pp)
		}
	}

	n.router.age()

	logrus.WithFields(logrus.Fields{"channel": n.cfg.channel, "frames": len(frames)}).Debug("NET/ROM: NODES broadcast")
}

// heardUI takes a received UI frame, which is a NODES broadcast if it is
// anything to NET/ROM.  One that came through a digipeater is not from a
// neighbour, and is ignored as netromd ignores it.
func (n *netromNode) heardUI(channel int, pp *ax25.Packet) {
	if channel != n.cfg.channel || pp.NumAddr() != 2 || pp.PID() != ax25.PIDNetROM {
		return
	}

	if !strings.EqualFold(pp.AddrNoSSID(ax25.Destination), netromNodesCallsign) {
		return
	}

	var from, err = normaliseNetromCallsign(pp.AddrWithSSID(ax25.Source))
	if err != nil {
		return
	}

	var nodes, decodeErr = decodeNetromNodes(pp.Info())
	if decodeErr != nil {
		logrus.WithError(decodeErr).WithField("from", from).Debug("NET/ROM: ignoring NODES broadcast")

		return
	}

	n.router.heardNodes(from, nodes)
}

// The data link reports on our links to neighbours.

func (n *netromNode) LinkEstablished(_ int, _ int, remoteCall string, _ string, incoming bool) {
	var pending = n.linkUp(remoteCall)

	n.router.neighbourHeard(remoteCall)

	logrus.WithFields(logrus.Fields{"neighbour": remoteCall, "incoming": incoming, "pending": len(pending)}).Debug("NET/ROM: link to neighbour up")

	for _, data := range pending {
		n.xmit(remoteCall, data)
	}
}

func (n *netromNode) LinkTerminated(_ int, _ int, remoteCall string, _ string, timeout bool) {
	n.linkDown(remoteCall)

	if timeout {
		n.router.neighbourFailed(remoteCall)
	}

	logrus.WithFields(logrus.Fields{"neighbour": remoteCall, "timeout": timeout}).Debug("NET/ROM: link to neighbour down")
}

func (n *netromNode) RecConnData(_ int, _ int, remoteCall string, _ string, pid int, data []byte) {
	if pid != ax25.PIDNetROM {
		return
	}

	n.router.neighbourHeard(remoteCall)

	var f, err = decodeNetromFrame(data)
	if err != nil {
		logrus.WithError(err).WithField("neighbour", remoteCall).Debug("NET/ROM: ignoring undecodable frame")

		return
	}

	n.route(f)
}

func (n *netromNode) OutstandingFramesReply(int, int, string, string, int) {}

// link returns the link to neighbour, making a record of it if there is
// none.  n.mu must be held.
func (n *netromNode) link(neighbour string) *netromNeighbourLink {
	var l, ok = n.links[neighbour]
	if !ok {
		l = new(netromNeighbourLink)
		n.links[neighbour] = l
	}

	return l
}

// linkUp marks the link to neighbour up and returns what was waiting for it.
func (n *netromNode) linkUp(neighbour string) [][]byte {
	var call, err = normaliseNetromCallsign(neighbour)
	if err != nil {
		return nil
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	var l = n.link(call)
	l.state = netromLinkUp

	var pending = l.pending
	l.pending = nil

	return pending
}

// linkDown marks the link to neighbour down, dropping what was waiting for
// it.
func (n *netromNode) linkDown(neighbour string) {
	var call, err = normaliseNetromCallsign(neighbour)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	var l = n.link(call)
	l.state = netromLinkDown
	l.pending = nil
}

// route takes a layer 3 frame from a neighbour: ours if it is addressed to
// us, otherwise to be passed on a hop nearer its destination while its time
// to live lasts.
func (n *netromNode) route(f *netromFrame) {
	if f.destination == n.cfg.callsign {
		n.circuits.rx(f)

		return
	}

	if f.ttl <= 1 {
		logrus.WithFields(logrus.Fields{"origin": f.origin, "destination": f.destination}).Trace("NET/ROM: time to live expired")

		return
	}

	var forwarded = *f
	forwarded.ttl--
	n.sendLayer3(&forwarded)
}

// sendLayer3 sends a frame on its way to its destination node, through the
// neighbour the routing table says is best.
func (n *netromNode) sendLayer3(f *netromFrame) {
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

// sendToNeighbour sends a layer 3 frame over the link to neighbour.  If the
// link is not up, the frame waits while it is made: the data link would
// throw away data for a link we have asked for and it has not yet made.
func (n *netromNode) sendToNeighbour(neighbour string, data []byte) {
	n.mu.Lock()

	var l = n.link(neighbour)

	var state = l.state

	switch state {
	case netromLinkUp:
	case netromLinkDown:
		l.state = netromLinkConnecting

		fallthrough
	case netromLinkConnecting:
		if len(l.pending) < netromMaxPending {
			l.pending = append(l.pending, data)
		}
	}

	n.mu.Unlock()

	switch state {
	case netromLinkUp:
		n.xmit(neighbour, data)
	case netromLinkDown:
		var addrs [ax25.MaxAddrs]string
		addrs[OWNCALL] = n.cfg.callsign
		addrs[PEERCALL] = neighbour

		n.linkLayer.ConnectRequest(addrs, 2, n.cfg.channel, netromClient, ax25.PIDNetROM)
	case netromLinkConnecting:
	}
}

// xmit hands a frame to the data link for a link that is up.
func (n *netromNode) xmit(neighbour string, data []byte) {
	var addrs [ax25.MaxAddrs]string
	addrs[OWNCALL] = n.cfg.callsign
	addrs[PEERCALL] = neighbour

	n.linkLayer.XmitDataRequest(addrs, 2, n.cfg.channel, netromClient, ax25.PIDNetROM, data)
}

// The circuits ask the node, and report to it.

func (n *netromNode) acceptCircuit(user string, originNode string) (int, string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(n.listeners) == 0 {
		logrus.WithFields(logrus.Fields{"user": user, "node": originNode}).Info("NET/ROM: refusing circuit - no client has registered the node callsign")

		return 0, "", false
	}

	return slices.Min(n.listeners), n.cfg.callsign, true
}

func (n *netromNode) circuitEstablished(c netromCircuitID, incoming bool) {
	n.agw().LinkEstablished(n.cfg.channel, c.client, c.remoteCall, c.ownCall, incoming)
}

func (n *netromNode) circuitTerminated(c netromCircuitID, timeout bool) {
	n.agw().LinkTerminated(n.cfg.channel, c.client, c.remoteCall, c.ownCall, timeout)
}

func (n *netromNode) circuitData(c netromCircuitID, data []byte) {
	n.agw().RecConnData(n.cfg.channel, c.client, c.remoteCall, c.ownCall, ax25.PIDNetROM, data)
}

// What AGW clients ask of the node.  Each reports whether the request was
// the node's to handle; anything else is plain AX.25.

// agwRegister takes a client's registration of the node callsign on the
// node's channel, which is how a client offers to take incoming circuits.
// It is not passed on to the data link, where the node itself answers for
// that callsign.
func (n *netromNode) agwRegister(channel int, client int, call string) bool {
	if !n.isNodeCall(channel, call) {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if !slices.Contains(n.listeners, client) {
		n.listeners = append(n.listeners, client)
	}

	return true
}

func (n *netromNode) agwUnregister(channel int, client int, call string) bool {
	if !n.isNodeCall(channel, call) {
		return false
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	n.listeners = slices.DeleteFunc(n.listeners, func(c int) bool { return c == client })

	return true
}

func (n *netromNode) isNodeCall(channel int, call string) bool {
	var normalised, err = normaliseNetromCallsign(call)

	return err == nil && channel == n.cfg.channel && normalised == n.cfg.callsign
}

// agwConnect opens a circuit for a connect request with the NET/ROM PID on
// the node's channel.  The destination may be a node's callsign or alias.
func (n *netromNode) agwConnect(channel int, client int, ownCall string, remoteCall string, pid int) bool {
	if channel != n.cfg.channel || pid != ax25.PIDNetROM {
		return false
	}

	var id = netromCircuitID{client: client, ownCall: ownCall, remoteCall: remoteCall}

	var log = logrus.WithFields(logrus.Fields{"client": client, "user": ownCall, "destination": remoteCall})

	var _, err = normaliseNetromCallsign(ownCall)
	if err != nil {
		log.WithError(err).Error("NET/ROM: cannot open a circuit for that user")
		n.circuitTerminated(id, false)

		return true
	}

	var destination, ok = n.router.resolve(remoteCall)
	if !ok {
		log.Info("NET/ROM: no route to that node")
		n.circuitTerminated(id, false)

		return true
	}

	var connectErr = n.circuits.connect(id, destination)

	switch {
	case errors.Is(connectErr, errNetromCircuitExists):
		// The client has this circuit already, and it carries on.
		log.Info("NET/ROM: circuit already open")

		return true
	case connectErr != nil:
		log.WithError(connectErr).Error("NET/ROM: cannot open circuit")
		n.circuitTerminated(id, false)

		return true
	}

	log.WithField("node", destination).Debug("NET/ROM: opening circuit")

	return true
}

// agwSend sends data on the client's circuit, if it has one by those
// callsigns on the node's channel.
func (n *netromNode) agwSend(channel int, client int, ownCall string, remoteCall string, data []byte) bool {
	return channel == n.cfg.channel &&
		n.circuits.send(netromCircuitID{client: client, ownCall: ownCall, remoteCall: remoteCall}, data) == nil
}

// agwDisconnect closes the client's circuit, if it has one by those
// callsigns on the node's channel.
func (n *netromNode) agwDisconnect(channel int, client int, ownCall string, remoteCall string) bool {
	return channel == n.cfg.channel &&
		n.circuits.disconnect(netromCircuitID{client: client, ownCall: ownCall, remoteCall: remoteCall}) == nil
}

// agwClientGone closes a departed client's circuits and forgets its
// registration.
func (n *netromNode) agwClientGone(client int) {
	n.circuits.dropClient(client)

	n.mu.Lock()
	defer n.mu.Unlock()

	n.listeners = slices.DeleteFunc(n.listeners, func(c int) bool { return c == client })
}
