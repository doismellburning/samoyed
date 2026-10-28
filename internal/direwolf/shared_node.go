// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"slices"
	"sync"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/node"
	"github.com/sirupsen/logrus"
)

// sharedNode lets the node shell and the NET/ROM node answer to one
// callsign and alias, as a BPQ or Linux node does: users and neighbouring
// nodes alike connect to the one node.
//
// The data link tells a link's events to the client that registered the
// callsign it was made to, or that asked for it, and keeps a client's
// links apart from another's.  So the shell and the NET/ROM node share one
// client number, the shell registers the callsign and alias for both, and
// sharedNode, standing between them and the data link, hands each link's
// events to whichever of them it belongs to.
//
// A link either of them asks for is its own.  A link made to the node is the
// shell's to begin with, so a user is greeted straight away, until something
// arrives on it: if that is a NET/ROM frame (PID 0xCF), on a link to NET/ROM's
// callsign on one of its channels, the link is a neighbour's, and passes to the
// NET/ROM node, the shell forgetting it; anything else, and it is the shell's
// for good.  A neighbour is so greeted as a user is, and NET/ROM ignores the
// greeting.  NET/ROM asking for such a link before anything has arrived on it
// takes it over likewise.
//
// The shell connects its users onward to other nodes over NET/ROM circuits,
// which the NET/ROM node reports to whoever holds them: sharedNode's
// circuitReporter hands the shell's to the shell, and the AGW clients' to
// them.
type sharedNode struct {
	link node.DataLink

	// netromChannels are the channels NET/ROM runs on, and netromCall the
	// callsign it sends from.  The shell can listen on other channels, and
	// answers to the alias too, where a link is the shell's whatever arrives
	// on it.
	netromChannels []int
	netromCall     string

	// mu guards what follows.
	mu sync.Mutex

	// shell and netrom are set once both are made, which is after this,
	// since each is handed its own side of it: the cycle in the wiring.
	// The AGW server can already drive NET/ROM by then, so they are only
	// read under mu.
	shell  linkClients
	netrom linkClients

	// shellCircuits is told about the shell's circuits.
	shellCircuits linkClients

	links map[sharedLinkKey]*sharedLink
}

// sharedLinkKey identifies a link, as the data link does.
type sharedLinkKey struct {
	channel    int
	ownCall    string
	remoteCall string
}

// linkOwner is which of the shell and the NET/ROM node a link belongs to.
type linkOwner int

const (
	ownerShell linkOwner = iota
	ownerNetROM
)

// sharedLink is what sharedNode knows of a link.
type sharedLink struct {
	owner linkOwner

	// undecided says the link was made to the node and nothing has
	// arrived on it yet, so it may yet turn out to be a neighbour's.
	undecided bool
}

// newSharedNode stands between link and a shell and NET/ROM node sharing
// their names, NET/ROM running on netromChannels as netromCall.
func newSharedNode(link node.DataLink, netromChannels []int, netromCall string) *sharedNode {
	var s = new(sharedNode)
	s.link = link
	s.netromChannels = slices.Clone(netromChannels)
	s.netromCall = netromCall
	s.shell = noLinkClients{}
	s.netrom = noLinkClients{}
	s.shellCircuits = noLinkClients{}
	s.links = make(map[sharedLinkKey]*sharedLink)

	return s
}

// attach hands the shared node the shell, what to tell about the shell's
// circuits, and the NET/ROM node, which closes the cycle.
func (s *sharedNode) attach(shell linkClients, shellCircuits linkClients, netrom linkClients) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shell = shell
	s.shellCircuits = shellCircuits
	s.netrom = netrom
}

// circuitReporter is what the NET/ROM node reports circuits to: routed by
// client number as links are, so the shell's, nodeClient's, go to the shell,
// and the AGW clients' to agw, which may be nil for there being none.  No
// circuit is netromClient's: sharing the shell's names, the NET/ROM node is
// nodeClient too.
func (s *sharedNode) circuitReporter(agw linkClients) *linkClientRouter {
	var shell = new(sharedNodeShellCircuits)
	shell.shared = s

	return newLinkClientRouter(agw, shell, nil)
}

// sharedNodeShellCircuits tells the shell about its circuits, through
// whatever attach was handed for them, which comes after the NET/ROM node
// reporting them is made.
type sharedNodeShellCircuits struct {
	shared *sharedNode
}

func (c *sharedNodeShellCircuits) circuits() linkClients { //nolint:ireturn // whatever attach was handed
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()

	return c.shared.shellCircuits
}

func (c *sharedNodeShellCircuits) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	c.circuits().LinkEstablished(channel, client, remoteCall, ownCall, incoming)
}

func (c *sharedNodeShellCircuits) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	c.circuits().LinkTerminated(channel, client, remoteCall, ownCall, timeout)
}

func (c *sharedNodeShellCircuits) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	c.circuits().RecConnData(channel, client, remoteCall, ownCall, pid, data)
}

func (c *sharedNodeShellCircuits) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	c.circuits().OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
}

// clients are the shell and the NET/ROM node, together.
func (s *sharedNode) clients() (linkClients, linkClients) { //nolint:ireturn // the two it was given
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.shell, s.netrom
}

func (s *sharedNode) clientFor(owner linkOwner) linkClients { //nolint:ireturn // choosing between them is the point
	var shell, netrom = s.clients()

	if owner == ownerNetROM {
		return netrom
	}

	return shell
}

// isNetROMLink says whether key is a link NET/ROM could have: to its
// callsign, on one of its channels.
func (s *sharedNode) isNetROMLink(key sharedLinkKey) bool {
	return slices.Contains(s.netromChannels, key.channel) && key.ownCall == s.netromCall
}

// ownerOf is who link key belongs to: the shell, if it is none of theirs,
// which ignores a link it does not know.
func (s *sharedNode) ownerOf(key sharedLinkKey) linkOwner {
	s.mu.Lock()
	defer s.mu.Unlock()

	if l, ok := s.links[key]; ok {
		return l.owner
	}

	return ownerShell
}

// The data link reports on the shared client's links.

func (s *sharedNode) LinkEstablished(channel int, client int, remoteCall string, ownCall string, incoming bool) {
	var key = sharedLinkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	s.mu.Lock()

	var l, ok = s.links[key]
	if !ok {
		// One made to the node: a user's, until it shows otherwise.
		l = new(sharedLink)
		l.owner = ownerShell
		l.undecided = incoming
		s.links[key] = l
	}

	var owner = l.owner

	s.mu.Unlock()

	s.clientFor(owner).LinkEstablished(channel, client, remoteCall, ownCall, incoming)
}

func (s *sharedNode) LinkTerminated(channel int, client int, remoteCall string, ownCall string, timeout bool) {
	var key = sharedLinkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	s.mu.Lock()

	var owner = ownerShell
	if l, ok := s.links[key]; ok {
		owner = l.owner
	}

	delete(s.links, key)

	s.mu.Unlock()

	s.clientFor(owner).LinkTerminated(channel, client, remoteCall, ownCall, timeout)
}

func (s *sharedNode) RecConnData(channel int, client int, remoteCall string, ownCall string, pid int, data []byte) {
	var key = sharedLinkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	s.mu.Lock()

	var owner = ownerShell
	var handOver bool

	if l, ok := s.links[key]; ok {
		handOver = l.undecided && pid == ax25.PIDNetROM && s.isNetROMLink(key)
		l.undecided = false

		if handOver {
			l.owner = ownerNetROM
		}

		owner = l.owner
	}

	s.mu.Unlock()

	if handOver {
		logrus.WithFields(logrus.Fields{"channel": channel, "neighbour": remoteCall}).Debug("Node: link is from a neighbouring NET/ROM node")
		s.handOver(channel, client, remoteCall, ownCall)
	}

	s.clientFor(owner).RecConnData(channel, client, remoteCall, ownCall, pid, data)
}

// handOver tells the shell and NET/ROM that a link that was the shell's is
// NET/ROM's.
func (s *sharedNode) handOver(channel int, client int, remoteCall string, ownCall string) {
	var shell, netrom = s.clients()

	// The shell takes it as the user gone, and says nothing.
	shell.LinkTerminated(channel, client, remoteCall, ownCall, false)
	netrom.LinkEstablished(channel, client, remoteCall, ownCall, true)
}

func (s *sharedNode) OutstandingFramesReply(channel int, client int, ownCall string, remoteCall string, count int) {
	var owner = s.ownerOf(sharedLinkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall})

	s.clientFor(owner).OutstandingFramesReply(channel, client, ownCall, remoteCall, count)
}

// claimResult is what came of claiming a link for a connect request.
type claimResult int

const (
	// claimNew is a link nobody has: the request goes to the data link.
	claimNew claimResult = iota

	// claimHeld is a link the other has, which the request cannot have.
	claimHeld

	// claimTaken is a link already up, which was the shell's and nothing
	// had yet arrived on, and is NET/ROM's now.
	claimTaken
)

// claim records the link a connect request from owner is for as owner's,
// unless the other already has it.  A link made to the node, that nothing
// has arrived on yet, is NET/ROM's for the asking: it is a neighbour's that
// has yet to send.
func (s *sharedNode) claim(addrs [ax25.MaxAddrs]string, channel int, owner linkOwner) claimResult {
	var key = sharedLinkKey{channel: channel, ownCall: addrs[ax25.Source], remoteCall: addrs[ax25.Destination]}

	s.mu.Lock()
	defer s.mu.Unlock()

	var l, ok = s.links[key]

	switch {
	case !ok:
		l = new(sharedLink)
		l.owner = owner
		s.links[key] = l

		return claimNew
	case l.owner == owner:
		return claimNew
	case owner == ownerNetROM && l.undecided && s.isNetROMLink(key):
		l.owner = ownerNetROM
		l.undecided = false

		return claimTaken
	}

	return claimHeld
}

// shellLink is the data link as the shell sees it.
func (s *sharedNode) shellLink() *sharedNodeShellLink {
	var l = new(sharedNodeShellLink)
	l.DataLink = s.link
	l.shared = s

	return l
}

// netromLink is the data link as the NET/ROM node sees it.
func (s *sharedNode) netromLink() *sharedNodeNetROMLink {
	var l = new(sharedNodeNetROMLink)
	l.DataLink = s.link
	l.shared = s

	return l
}

// sharedNodeShellLink is the data link as the shell sees it: the data link
// itself, but that a link the NET/ROM node has to a station is not one the
// shell can connect a user to, which it is told is busy.
type sharedNodeShellLink struct {
	node.DataLink

	shared *sharedNode
}

func (l *sharedNodeShellLink) ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int) {
	if l.shared.claim(addrs, channel, ownerShell) != claimNew {
		var shell, _ = l.shared.clients()
		shell.LinkTerminated(channel, client, addrs[ax25.Destination], addrs[ax25.Source], false)

		return
	}

	l.DataLink.ConnectRequest(addrs, numAddr, channel, client, pid)
}

// sharedNodeNetROMLink is the data link as the NET/ROM node sees it: the
// data link itself, but that the shell registers the callsign they share,
// and that a link the shell has to a station - a user's - is not one the
// NET/ROM node can have, which it is told has gone down.
type sharedNodeNetROMLink struct {
	node.DataLink

	shared *sharedNode
}

// RegisterCallsign does nothing: the shell has registered the callsign
// for both.
func (l *sharedNodeNetROMLink) RegisterCallsign(string, int, int) {}

func (l *sharedNodeNetROMLink) ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int) {
	var log = logrus.WithFields(logrus.Fields{"channel": channel, "neighbour": addrs[ax25.Destination]})

	switch l.shared.claim(addrs, channel, ownerNetROM) {
	case claimNew:
		l.DataLink.ConnectRequest(addrs, numAddr, channel, client, pid)
	case claimTaken:
		// Up already, so there is nothing to ask the data link for.
		log.Debug("NET/ROM: neighbour connected to the node, taking its link")
		l.shared.handOver(channel, client, addrs[ax25.Destination], addrs[ax25.Source])
	case claimHeld:
		log.Debug("NET/ROM: neighbour is connected to the node shell, not linking to it")

		var _, netrom = l.shared.clients()
		netrom.LinkTerminated(channel, client, addrs[ax25.Destination], addrs[ax25.Source], false)
	}
}
