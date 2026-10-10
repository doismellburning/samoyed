// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

// The node's shells: what joins internal/node's Shell to the links and
// circuits users reach it over, and to the links and circuits it opens
// onwards for them.  Everything here runs on the node's goroutine.

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/netrom"
	"github.com/doismellburning/samoyed/internal/node"
)

// startShell gives the user on the AX.25 link s a shell.
func (n *netromNode) startShell(s *axSession) {
	var shell = node.NewShell(n.shellCfg, n, s.key.remote, axConn{n: n, s: s}, n.now())
	s.shell = shell
	n.shells[shell] = true
	shell.Start()
}

// acceptCircuit gives the user on an incoming NET/ROM circuit a shell.
func (n *netromNode) acceptCircuit(c *netrom.Circuit) netrom.CircuitHandler { //nolint:ireturn // netrom.Acceptor's signature.
	var h = new(circuitShell)
	h.n = n
	h.shell = node.NewShell(n.shellCfg, n, c.User(), circuitConn{c: c}, n.now())
	n.shells[h.shell] = true

	return h
}

// circuitShell passes what happens on a user's circuit to their shell.
type circuitShell struct {
	n     *netromNode
	shell *node.Shell
}

func (h *circuitShell) Connected(*netrom.Circuit) { h.shell.Start() }

func (h *circuitShell) Received(_ *netrom.Circuit, data []byte) {
	h.shell.Input(data, h.n.now())
}

func (h *circuitShell) Closed(*netrom.Circuit, error) {
	delete(h.n.shells, h.shell)
	h.shell.Closed()
}

// axConn is a node.Conn over an AX.25 link.
type axConn struct {
	n *netromNode
	s *axSession
}

func (c axConn) Send(data []byte) {
	for len(data) > 0 {
		var chunk = data[:min(len(data), maxSendChunk)]
		data = data[len(chunk):]
		c.n.links.XmitDataRequest(c.s.addrs, c.s.numAddr, c.s.key.port, c.n.client, ax25.PIDNoLayer3, append([]byte(nil), chunk...))
	}
}

func (c axConn) Close() {
	c.n.links.DisconnectRequest(c.s.addrs, c.s.numAddr, c.s.key.port, c.n.client)
}

// circuitConn is a node.Conn over a NET/ROM circuit.
type circuitConn struct {
	c *netrom.Circuit
}

func (c circuitConn) Send(data []byte) { _ = c.c.Write(data) }
func (c circuitConn) Close()           { c.c.Close() }

// circuitDownlink passes what happens on a circuit a shell opened onwards to
// the shell.
type circuitDownlink struct {
	events node.Downlink
}

func (d circuitDownlink) Connected(*netrom.Circuit)               { d.events.Connected() }
func (d circuitDownlink) Received(_ *netrom.Circuit, data []byte) { d.events.Received(data) }
func (d circuitDownlink) Closed(_ *netrom.Circuit, err error)     { d.events.Closed(err) }

// Ports lists the node's ports, for its shells.
func (n *netromNode) Ports() []node.Port { return n.ports }

// Nodes lists the destinations the node knows, for its shells.
func (n *netromNode) Nodes() []netrom.Destination { return n.router.Table().Destinations() }

// Neighbours lists the node's neighbours, for its shells.
func (n *netromNode) Neighbours() []netrom.Neighbour { return n.router.Table().Neighbours() }

// Heard lists the stations heard on port, or on all ports for a negative one,
// for the node's shells.
func (n *netromNode) Heard(port int) []node.HeardStation { return n.heard.Stations(port) }

// ConnectNode opens a NET/ROM circuit onwards for a shell.
func (n *netromNode) ConnectNode(dest string, user string, events node.Downlink) (node.Conn, error) { //nolint:ireturn // node.Backend's signature.
	var c, err = n.router.Connect(dest, user, circuitDownlink{events: events}, n.now())
	if err != nil {
		return nil, err
	}

	return circuitConn{c: c}, nil
}

var errBadPort = errors.New("no such port")

// ConnectAX25 opens an AX.25 link onwards for a shell, from the user's
// callsign with its SSID turned around - SSID n becomes 15-n - so the link is
// told apart from any the user has of their own.
func (n *netromNode) ConnectAX25(port int, call string, via []string, user string, events node.Downlink) (node.Conn, error) { //nolint:ireturn // node.Backend's signature.
	if !slices.ContainsFunc(n.ports, func(p node.Port) bool { return p.Number == port }) {
		return nil, fmt.Errorf("%w: %d", errBadPort, port)
	}

	if len(via) > ax25.MaxRepeaters {
		return nil, fmt.Errorf("too many digipeaters, %d at most", ax25.MaxRepeaters)
	}

	var to, err = netrom.NormaliseCall(call)
	if err != nil {
		return nil, err
	}

	var digis = make([]string, 0, len(via))

	for _, v := range via {
		var d, derr = netrom.NormaliseCall(v)
		if derr != nil {
			return nil, derr
		}

		digis = append(digis, d)
	}

	var own, oerr = downlinkCall(user)
	if oerr != nil {
		return nil, oerr
	}

	var key = axKey{port: port, own: own, remote: to}
	if _, busy := n.sessions[key]; busy {
		return nil, errors.New("already connected")
	}

	var s = n.newSession(key, digis)
	s.downlink = events
	n.links.ConnectRequest(s.addrs, s.numAddr, port, n.client, ax25.PIDNoLayer3)

	return axConn{n: n, s: s}, nil
}

// downlinkCall returns the callsign a link opened onwards for user comes from.
func downlinkCall(user string) (string, error) {
	var call, err = netrom.NormaliseCall(user)
	if err != nil {
		return "", err
	}

	var base, ssid, _ = strings.Cut(call, "-")

	var n = 0
	if ssid != "" {
		_, _ = fmt.Sscanf(ssid, "%d", &n)
	}

	return netrom.NormaliseCall(fmt.Sprintf("%s-%d", base, 15-n))
}

// nodePorts describes the channels in use, as the node's shells list them.
// The IGate channel carries nothing a node user could connect over, so it is
// left out.
func nodePorts(audio *RadioConfig) []node.Port {
	var ports []node.Port

	for ch := range MAX_TOTAL_CHANS {
		var description string

		switch audio.chan_medium[ch] {
		case MEDIUM_RADIO:
			description = fmt.Sprintf("Radio, %d baud", audio.achan[ch].baud)
		case MEDIUM_NETTNC:
			description = "Network TNC"
		case MEDIUM_AXUDP:
			description = fmt.Sprintf("AXUDP, UDP port %d", audio.axudp_port[ch])
		case MEDIUM_NONE, MEDIUM_IGATE:
			continue
		default:
			continue
		}

		ports = append(ports, node.Port{Number: ch, Description: description})
	}

	return ports
}
