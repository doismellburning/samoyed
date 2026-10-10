// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The NET/ROM node: what joins internal/netrom's Router to the AX.25 link
// layer.  NODES broadcasts are tapped from the received frames and sent as UI
// frames; NET/ROM packets travel to and from neighbours over connected AX.25
// links with the NET/ROM PID, which the node opens as it needs them and also
// accepts from neighbours that open them first.
//
// The Router is not safe for concurrent use, so it is only ever touched from
// the node's own goroutine: what arrives from elsewhere - the link layer's
// callbacks, a received broadcast, a question from the web interface - is
// handed to that goroutine as a function to run.

import (
	"context"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
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

// axSession is a connected AX.25 link the node has with another station.
type axSession struct {
	key      netrom.NeighbourKey
	up       bool
	everUp   bool
	incoming bool
	pending  [][]byte // NET/ROM packets waiting for the link to come up.
}

// netromNode runs a NET/ROM Router over the AX.25 link layer.
type netromNode struct {
	cfg    netrom.Config
	router *netrom.Router
	links  linkRequester
	sendUI func(channel int, pp *ax25.Packet)
	client int

	// work carries what is to be run on the node's goroutine.
	work chan func()

	sessions map[netrom.NeighbourKey]*axSession

	// onUserData, if set, is handed data arriving on a link with a PID other
	// than NET/ROM's - a user connected to the node itself.
	onUserData func(key netrom.NeighbourKey, data []byte)

	now func() time.Time
}

// newNetromNode returns a node for cfg, asking links for its AX.25 links as
// client and sending UI frames with sendUI.  Nothing happens until run.
func newNetromNode(cfg netrom.Config, links linkRequester, sendUI func(channel int, pp *ax25.Packet)) (*netromNode, error) {
	var n = new(netromNode)
	n.cfg = cfg
	n.links = links
	n.sendUI = sendUI
	n.work = make(chan func(), 1024)
	n.sessions = make(map[netrom.NeighbourKey]*axSession)
	n.now = time.Now

	var r, err = netrom.NewRouter(cfg, n, n.now())
	if err != nil {
		return nil, err
	}

	n.router = r
	n.cfg = r.Config()
	r.OnEvent = n.logEvent

	return n, nil
}

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
			n.router.Tick(n.now())
		}
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

// heardFrame takes in a received frame, keeping it if it is a NODES broadcast
// from a station heard directly on one of the node's ports.  It is called on
// the receive goroutine for every frame received.
func (n *netromNode) heardFrame(channel int, pp *ax25.Packet) {
	if n == nil || pp.NumAddr() != 2 || pp.Control() != ax25.UIFrame || pp.PID() != ax25.PIDNetROM {
		return
	}

	if pp.AddrWithSSID(ax25.Destination) != netrom.NodesDestination {
		return
	}

	var from = pp.AddrWithSSID(ax25.Source)
	var info = append([]byte(nil), pp.Info()...)

	n.do(func() { n.router.HeardNodes(channel, from, info, n.now()) })
}

// SendPacket is the Router handing a packet to a neighbour.
func (n *netromNode) SendPacket(to netrom.NeighbourKey, packet []byte) {
	var s = n.session(to)

	if s.up {
		n.links.XmitDataRequest(n.addrs(to), 2, to.Port, n.client, ax25.PIDNetROM, packet)

		return
	}

	if len(s.pending) >= neighbourPending {
		return // The far end has its own retries; better to drop than hoard.
	}

	s.pending = append(s.pending, packet)

	if len(s.pending) == 1 {
		n.links.ConnectRequest(n.addrs(to), 2, to.Port, n.client, ax25.PIDNoLayer3)
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

	n.sendUI(port, pp)
}

// addrs are the addresses of the node's link to key.
func (n *netromNode) addrs(key netrom.NeighbourKey) [ax25.MaxAddrs]string {
	var addrs [ax25.MaxAddrs]string
	addrs[ax25.Source] = n.cfg.Call
	addrs[ax25.Destination] = key.Call

	return addrs
}

func (n *netromNode) session(key netrom.NeighbourKey) *axSession {
	var s, ok = n.sessions[key]
	if !ok {
		s = new(axSession)
		s.key = key
		n.sessions[key] = s
	}

	return s
}

// linkKey names the link the link layer is telling us about.  A link to the
// node's alias is the same link as one to its callsign.
func linkKey(channel int, remoteCall string) netrom.NeighbourKey {
	var call, err = netrom.NormaliseCall(remoteCall)
	if err != nil {
		call = remoteCall
	}

	return netrom.NeighbourKey{Port: channel, Call: call}
}

// LinkEstablished is the link layer saying a link is up.
func (n *netromNode) LinkEstablished(channel int, _ int, remoteCall string, _ string, incoming bool) {
	var key = linkKey(channel, remoteCall)

	n.do(func() {
		var s = n.session(key)
		s.up = true
		s.everUp = true
		s.incoming = incoming

		for _, p := range s.pending {
			n.links.XmitDataRequest(n.addrs(key), 2, key.Port, n.client, ax25.PIDNetROM, p)
		}

		s.pending = nil
	})
}

// LinkTerminated is the link layer saying a link has gone, or could not be
// opened.
func (n *netromNode) LinkTerminated(channel int, _ int, remoteCall string, _ string, timeout bool) {
	var key = linkKey(channel, remoteCall)

	n.do(func() {
		var s, ok = n.sessions[key]
		if !ok {
			return
		}

		delete(n.sessions, key)

		if !s.everUp && len(s.pending) > 0 {
			logrus.WithFields(logrus.Fields{
				"channel":   key.Port,
				"neighbour": key.Call,
				"timeout":   timeout,
			}).Info("NET/ROM: could not reach neighbour")
			n.router.NeighbourFailed(key, n.now())
		}
	})
}

// RecConnData is the link layer handing over data that arrived on a link.
func (n *netromNode) RecConnData(channel int, _ int, remoteCall string, _ string, pid int, data []byte) {
	var key = linkKey(channel, remoteCall)
	var copied = append([]byte(nil), data...)

	n.do(func() {
		if pid == ax25.PIDNetROM {
			n.router.ReceivePacket(key, copied, n.now())

			return
		}

		if n.onUserData != nil {
			n.onUserData(key, copied)
		}
	})
}

// OutstandingFramesReply is never asked for.
func (n *netromNode) OutstandingFramesReply(int, int, string, string, int) {}

// netrom_init starts the NET/ROM node, if the configuration asks for one,
// taking its AX.25 links through links.  It returns nil for no node.
func netrom_init(ctx context.Context, audio *RadioConfig, misc *misc_config_s, links *linkRouter) (*netromNode, error) {
	if misc.netrom == nil {
		return nil, nil //nolint:nilnil // No node is not an error.
	}

	var cfg, err = netromConfigFor(*misc.netrom, audio)
	if err != nil {
		return nil, err
	}

	var n, nerr = newNetromNode(cfg, dataLinkQueue, func(channel int, pp *ax25.Packet) {
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
