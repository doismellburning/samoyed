// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package node is a packet node's command shell: a station connects to the
// node's callsign or alias and types commands at it, one per line.
//
// The node is a client of the AX.25 data link, as an AGW application is.  It
// registers its callsigns for incoming connections, is told when a link to
// one of them comes up or goes down and what arrives on it, and asks the data
// link to send its replies.
package node

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/mheard"
	"github.com/doismellburning/samoyed/internal/netrom"
)

// maxLineLen is the longest command line the shell will hold on to.  Anyone
// on frequency can connect and send whatever they like, so a line that runs
// on is cut off rather than buffered without limit.
const maxLineLen = 256

// DataLink is the AX.25 data link, as far as the node needs it: where it
// registers its callsigns and sends its requests for the links it is
// answering.  *direwolf.DataLinkQueue is one.
type DataLink interface {
	RegisterCallsign(addr string, channel int, client int)
	ConnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int)
	DisconnectRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int)
	XmitDataRequest(addrs [ax25.MaxAddrs]string, numAddr int, channel int, client int, pid int, xdata []byte)
}

// Config is what the node is and where it listens.
type Config struct {
	// Address is the node's address - its callsign, with any SSID - as
	// the data link writes addresses: upper case, and no "-0".
	Address string

	// Alias is a second name the node answers to, or empty for none.
	Alias string

	// Ports are the channels the node accepts connections on.
	Ports []Port

	// InfoMessage is what INFO tells a user about the node, or empty for a
	// line saying what it is.  It may run to several lines.
	InfoMessage string

	// ConnectMessage is what a user is told on connecting, or empty for a
	// welcome.  It may run to several lines.
	ConnectMessage string
}

// Port is a channel the node accepts connections on.
type Port struct {
	Channel int

	// Description is what PORTS says the channel is.
	Description string
}

// linkKey identifies a link the node is answering, as the data link does,
// or a NET/ROM circuit it has opened for a user, as the NET/ROM node does.
type linkKey struct {
	channel    int
	ownCall    string
	remoteCall string

	// circuit says this is a NET/ROM circuit, whose own end is the user's
	// callsign and far end a node's, rather than an AX.25 link.
	circuit bool
}

// session is one connected user.
type session struct {
	// line is what has arrived of the command being typed.
	line []byte

	// overlong is set once line has outgrown maxLineLen, so the rest of
	// that line is thrown away rather than taken as a fresh command.
	overlong bool

	// leaving is set once the user has asked to go.  The session lasts
	// until the data link says the link is down, and anything that
	// arrives in the meantime is ignored.
	leaving bool

	// downlink is the link the user asked to be connected onward on, or
	// nil when they are talking to the node.
	downlink *linkKey

	// downlinkName is what the user is told downlink goes to: the station's
	// callsign, or a node's alias and callsign.
	downlinkName string

	// connected says downlink is up, and what the user sends goes to it.
	connected bool

	// cancelled says the user gave up on downlink before it came up.
	cancelled bool
}

// maxHeard is the most stations MH lists.
const maxHeard = 20

// HeardStations lists up to limit stations heard over the radio, most
// recently heard first.  (*mheard.DB).HeardRF is one.
type HeardStations func(limit int) []mheard.Heard

// NetROM is the NET/ROM node, as far as the node needs it: what it knows
// now - the nodes it can reach and the neighbours it reaches them through -
// and the circuits a user can be connected onward over.  The node is told
// what happens on its circuits through Circuits.  *netrom.Node is one.
type NetROM interface {
	Status() netrom.Status
	Connect(channel int, client int, ownCall string, remoteCall string, pid int) bool
	Send(channel int, client int, ownCall string, remoteCall string, data []byte) bool
	Disconnect(channel int, client int, ownCall string, remoteCall string) bool
}

// Node is the node shell.
type Node struct {
	config Config
	link   DataLink
	heard  HeardStations

	// netrom is the NET/ROM node NODES and ROUTES list, and CONNECT
	// reaches other nodes through, or nil for none.
	netrom NetROM

	// client is the client number the data link knows the node by.
	client int

	// now is the time, which a test can stop.
	now func() time.Time

	// mu guards sessions and downlinks.  The data link calls in on its
	// own goroutine; the lock keeps the node safe from any other caller
	// too.
	mu       sync.Mutex
	sessions map[linkKey]*session

	// downlinks are the links users have asked to be connected onward on,
	// each to the user's own link.
	downlinks map[linkKey]linkKey
}

// New makes a node that talks to link as client, lists the stations heard
// says it has heard, and lists and connects through the NET/ROM node netrom,
// which may be nil for none.  Nothing is registered with the data link until
// Start.
func New(config Config, link DataLink, heard HeardStations, netrom NetROM, client int) *Node {
	var n = new(Node)

	n.config = config
	n.link = link
	n.heard = heard
	n.netrom = netrom
	n.client = client
	n.now = time.Now
	n.sessions = make(map[linkKey]*session)
	n.downlinks = make(map[linkKey]linkKey)

	return n
}

// Start registers the node's callsign, and its alias if it has one, for
// incoming connections on each of its channels.
func (n *Node) Start() {
	for _, port := range n.config.Ports {
		n.link.RegisterCallsign(n.config.Address, port.Channel, n.client)

		if n.config.Alias != "" {
			n.link.RegisterCallsign(n.config.Alias, port.Channel, n.client)
		}
	}
}

// LinkEstablished is told a link has come up.  A station that connected to
// us gets a session and a greeting; a user we were connecting onward is told
// they are through.
func (n *Node) LinkEstablished(channel int, _ int, remoteCall string, ownCall string, incoming bool) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall, circuit: false}

	n.mu.Lock()

	// A downlink the far end resets is said to be established as though
	// the far end had connected to us.
	if _, isDownlink := n.downlinks[key]; isDownlink || !incoming {
		n.mu.Unlock()
		n.downlinkEstablished(key)

		return
	}

	// The data link says so again when the far end resets a link that is
	// already up, which leaves the session as it was.
	var _, reset = n.sessions[key]
	if !reset {
		n.sessions[key] = new(session)
	}

	n.mu.Unlock()

	if !reset {
		var text = n.config.ConnectMessage
		if text == "" {
			text = "Welcome to Samoyed"
		}

		n.send(key, []byte(n.prompt()+terminalText(text)+"\r"))
	}
}

// LinkTerminated is told a link has gone down.  A user's ends their session;
// a downlink's returns its user to the node.
func (n *Node) LinkTerminated(channel int, _ int, remoteCall string, ownCall string, timeout bool) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall, circuit: false}

	n.mu.Lock()

	if _, ok := n.downlinks[key]; ok {
		n.mu.Unlock()
		n.downlinkTerminated(key, timeout)

		return
	}

	var s = n.sessions[key]
	delete(n.sessions, key)

	var down *linkKey
	if s != nil {
		down = s.downlink
	}

	n.mu.Unlock()

	// A user who goes takes their downlink with them.
	if down != nil {
		n.disconnect(*down)
	}
}

// RecConnData is handed what a station sent.  What a user sends the node is
// taken a line at a time; what they send once connected onward goes to the
// downlink as it is, and what comes back on the downlink goes to them.
func (n *Node) RecConnData(channel int, _ int, remoteCall string, ownCall string, pid int, data []byte) {
	n.received(linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall, circuit: false}, pid, data)
}

// received is RecConnData for the link or circuit key names.
func (n *Node) received(key linkKey, pid int, data []byte) {
	var lines [][]byte

	n.mu.Lock()

	if up, ok := n.downlinks[key]; ok {
		var s = n.downlinkUserLocked(key)

		// Not for a user who gave up on it, and is back at the node.
		var forward = s != nil && !s.cancelled
		n.mu.Unlock()

		if forward {
			// A circuit's data is the user's own, whatever the PID it
			// travelled under between the nodes.
			if key.circuit {
				pid = ax25.PIDNoLayer3
			}

			n.forward(up, pid, data)
		}

		return
	}

	var s = n.sessions[key]

	if s != nil && s.downlink != nil && s.connected {
		var down = *s.downlink
		n.mu.Unlock()

		n.forward(down, pid, data)

		return
	}

	if s != nil && s.downlink != nil {
		// Still coming up: a line typed now gives up on it.
		var down = *s.downlink

		lines = s.take(data)

		var cancel = len(lines) > 0 && !s.cancelled
		if cancel {
			s.cancelled = true
		}

		n.mu.Unlock()

		if cancel {
			n.disconnect(down)
		}

		if slices.ContainsFunc(lines, isBye) {
			n.bye(key, s, nil)
		}

		return
	}

	if s != nil && !s.leaving {
		lines = s.take(data)
	}

	n.mu.Unlock()

	for _, line := range lines {
		if line == nil {
			n.send(key, []byte(n.prompt()+"Line too long\r"))

			continue
		}

		if !n.command(key, s, string(line)) {
			return
		}
	}
}

// OutstandingFramesReply is an answer to a question the node never asks.
func (n *Node) OutstandingFramesReply(int, int, string, string, int) {}

// take adds data to what has arrived of the current line, and returns the
// lines it completes, leaving out blank ones.  Either CR or LF ends a line, so
// CR LF does too.  A line that ran past maxLineLen, and was dropped, is nil.
func (s *session) take(data []byte) [][]byte {
	var lines [][]byte

	for _, b := range data {
		if b != '\r' && b != '\n' {
			if len(s.line) < maxLineLen {
				s.line = append(s.line, b)
			} else {
				s.overlong = true
			}

			continue
		}

		if s.overlong {
			lines = append(lines, nil)
		} else if line := bytes.TrimSpace(s.line); len(line) > 0 {
			lines = append(lines, bytes.Clone(line))
		}

		s.line = s.line[:0]
		s.overlong = false
	}

	return lines
}

// command is one of the shell's commands.
type command struct {
	// names are what the command can be typed as, the full name first.
	names []string

	// help says what it does, for HELP.
	help string

	// run carries it out for the user on the link key names, whose
	// session is s, with the words typed after its name.  It says whether
	// to go on to whatever else the user has sent: not once they have
	// asked to go, or to be connected onward.
	run func(n *Node, key linkKey, s *session, args []string) bool
}

// commands are the shell's commands, in the order HELP lists them.
func (n *Node) commands() []command {
	var connectHelp = "Connect to a station: C [port] callsign"
	if n.netrom != nil {
		connectHelp += ", or a node: C node"
	}

	return []command{
		{names: byeNames(), help: "Disconnect", run: (*Node).bye},
		{names: []string{"CONNECT", "C"}, help: connectHelp, run: (*Node).connect},
		{names: []string{"HELP", "H", "?"}, help: "List the commands", run: (*Node).help},
		{names: []string{"INFO", "I"}, help: "About this node", run: (*Node).info},
		{names: []string{"PORTS", "P"}, help: "List the ports", run: (*Node).ports},
		{names: []string{"MHEARD", "MH"}, help: "List the stations heard", run: (*Node).mheard},
		{names: []string{"NODES", "N"}, help: "List the nodes known: N [node] for its routes", run: (*Node).nodes},
		{names: []string{"ROUTES", "R"}, help: "List the neighbouring nodes", run: (*Node).routes},
	}
}

// command carries out one command line for the user on the link key names,
// whose session is s, and says whether to go on to another.
func (n *Node) command(key linkKey, s *session, line string) bool {
	var words = strings.Fields(line)
	var name = strings.ToUpper(words[0])

	for _, c := range n.commands() {
		if slices.Contains(c.names, name) {
			return c.run(n, key, s, words[1:])
		}
	}

	n.send(key, []byte(n.prompt()+"Invalid command - type ? for help\r"))

	return true
}

// bye disconnects.
func (n *Node) bye(key linkKey, s *session, _ []string) bool {
	n.mu.Lock()
	s.leaving = true
	n.mu.Unlock()

	n.disconnect(key)

	return false
}

// byeNames are what BYE can be typed as.
func byeNames() []string {
	return []string{"BYE", "B", "QUIT", "Q"}
}

// isBye says whether line is a BYE command.
func isBye(line []byte) bool {
	var words = strings.Fields(string(line))

	return len(words) > 0 && slices.Contains(byeNames(), strings.ToUpper(words[0]))
}

// connect connects the user onward: to the station named on the port named,
// or, with no port, over a NET/ROM circuit to the node named.  The port can
// be left out for a station when there is only one, and the name is not a
// node's.
func (n *Node) connect(key linkKey, s *session, args []string) bool {
	var down, name, problem = n.connectArgs(key.remoteCall, args)
	if problem != "" {
		n.send(key, []byte(n.prompt()+problem+"\r"))

		return true
	}

	n.mu.Lock()

	// The data link has one link for each pair of stations on a channel, so
	// a station already connected to the node, or connected to from it, is
	// not one it can connect to again.
	var _, isUser = n.sessions[down]
	var _, isDownlink = n.downlinks[down]

	if isUser || isDownlink {
		n.mu.Unlock()
		var busy = "Busy - " + name + " is already connected to this node"
		if down.circuit {
			busy = "Busy - you already have a circuit to " + name
		}

		n.send(key, []byte(n.prompt()+busy+"\r"))

		return true
	}

	n.downlinks[down] = key
	s.downlink = &down
	s.downlinkName = name
	s.cancelled = false

	// Anything after CONNECT in the frame was sent before the user could
	// know what it would do, so it is not kept for later.
	s.line = s.line[:0]
	s.overlong = false
	n.mu.Unlock()

	if !down.circuit {
		n.send(key, []byte(fmt.Sprintf("%sTrying %s on port %d\r", n.prompt(), name, down.channel)))
		n.link.ConnectRequest(addrs(down), 2, down.channel, n.client, ax25.PIDNoLayer3)

		return false
	}

	n.send(key, []byte(n.prompt()+"Trying "+name+"\r"))

	// The NET/ROM node says how it went through Circuits, unless the
	// circuit was not its to open at all.
	if !n.netrom.Connect(down.channel, n.client, down.ownCall, down.remoteCall, ax25.PIDNetROM) {
		n.downlinkTerminated(down, false)
	}

	return false
}

// connectArgs makes sense of what was typed after CONNECT by user: the link
// or circuit to connect them on, and what to call where it goes, or what is
// wrong with them.
func (n *Node) connectArgs(user string, args []string) (linkKey, string, string) {
	var usage = "Usage: C [port] callsign"
	if n.netrom != nil {
		usage += ", or C node"
	}

	var none linkKey

	var channel int

	switch len(args) {
	case 1:
		if down, name, ok := n.nodeCircuit(user, args[0]); ok {
			return down, name, ""
		}

		if len(n.config.Ports) != 1 {
			if n.netrom != nil {
				return none, "", "No such node " + strings.ToUpper(args[0]) + " - C port callsign for a station"
			}

			return none, "", usage
		}

		channel = n.config.Ports[0].Channel
	case 2:
		var number, err = strconv.Atoi(args[0])
		if err != nil {
			return none, "", usage
		}

		if !slices.ContainsFunc(n.config.Ports, func(p Port) bool { return p.Channel == number }) {
			return none, "", fmt.Sprintf("No port %d - type P for the ports", number)
		}

		channel = number
	default:
		return none, "", usage
	}

	var call, ok = Address(args[len(args)-1])
	if !ok {
		return none, "", "Invalid callsign " + args[len(args)-1]
	}

	if call == n.config.Address || call == n.config.Alias {
		return none, "", "That is this node"
	}

	return linkKey{channel: channel, ownCall: n.config.Address, remoteCall: call, circuit: false}, call, ""
}

// nodeCircuit is the NET/ROM circuit to connect user to the node name names,
// by alias or callsign, and what to call that node, if NET/ROM knows it.  The
// user's own callsign is the circuit's own end, as the CONNECT REQUEST names
// whose circuit it is.  A circuit goes wherever the route to its node does,
// whatever channel it is asked for on, so it is asked for on NET/ROM's first.
func (n *Node) nodeCircuit(user string, name string) (linkKey, string, bool) {
	var none linkKey

	if n.netrom == nil {
		return none, "", false
	}

	var status = n.netrom.Status()

	var d, ok = findNode(status, name)
	if !ok || len(status.Channels) == 0 {
		return none, "", false
	}

	return linkKey{channel: status.Channels[0], ownCall: user, remoteCall: d.Callsign, circuit: true}, nodeName(d), true
}

// downlinkUserLocked is the session of the user the downlink key names is
// for, or nil if that user has gone.  The caller holds n.mu.
func (n *Node) downlinkUserLocked(key linkKey) *session {
	var up, ok = n.downlinks[key]
	if !ok {
		return nil
	}

	// A user who went and came back has a new session, which this
	// downlink is not for.
	var s = n.sessions[up]
	if s == nil || s.downlink == nil || *s.downlink != key {
		return nil
	}

	return s
}

// downlinkEstablished is told the downlink key names is up.
func (n *Node) downlinkEstablished(key linkKey) {
	n.mu.Lock()

	var up, isDownlink = n.downlinks[key]

	var s = n.downlinkUserLocked(key)

	// Only the first time: the far end can reset it once it is up.
	var announce = s != nil && !s.connected && !s.cancelled

	var name string

	if announce {
		s.connected = true
		name = s.downlinkName

		// What the user sends goes to the station as it is from here, so
		// part of a line typed while it came up is no command.
		s.line = s.line[:0]
		s.overlong = false
	}

	// Its user gave up on it, or went, while it was coming up - it can
	// come up all the same, the far end's SABM crossing our DISC.
	var unwanted = isDownlink && (s == nil || s.cancelled)

	n.mu.Unlock()

	switch {
	case announce:
		n.send(up, []byte(n.prompt()+"Connected to "+name+"\r"))
	case unwanted:
		n.disconnect(key)
	}
}

// downlinkTerminated is told the downlink key names is down, which returns
// its user to the node.  A downlink that never came up has failed: timeout
// says the station never answered, rather than refusing.
func (n *Node) downlinkTerminated(key linkKey, timeout bool) {
	n.mu.Lock()

	var up = n.downlinks[key]
	var s = n.downlinkUserLocked(key)

	delete(n.downlinks, key)

	var wasConnected, cancelled bool

	var name string

	if s != nil {
		wasConnected, cancelled, name = s.connected, s.cancelled, s.downlinkName
		s.downlink = nil
		s.downlinkName = ""
		s.connected = false
		s.cancelled = false

		// Back at the node, starting a fresh line.
		s.line = s.line[:0]
		s.overlong = false
	}

	n.mu.Unlock()

	if s == nil {
		return
	}

	var message string

	switch {
	case cancelled:
		message = "Gave up on " + name
	case wasConnected:
		message = "Disconnected from " + name
	case timeout:
		message = "Failure with " + name + " - no answer"
	default:
		message = "Failure with " + name + " - busy"
	}

	n.send(up, []byte(n.prompt()+message+"\r"))
}

// help lists the commands.
func (n *Node) help(key linkKey, _ *session, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Commands:\r")

	for _, c := range n.commands() {
		fmt.Fprintf(&b, "  %s - %s\r", strings.Join(c.names, ", "), c.help)
	}

	n.send(key, []byte(b.String()))

	return true
}

// info tells the user about the node.
func (n *Node) info(key linkKey, _ *session, _ []string) bool {
	var text = n.config.InfoMessage
	if text == "" {
		text = n.identity() + " is a Samoyed node"
	}

	n.send(key, []byte(n.prompt()+terminalText(text)+"\r"))

	return true
}

// terminalText is text from the configuration file with its lines ended in
// CR, as a packet terminal wants, however the file ended them, and without a
// last line ending.
func terminalText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")

	return strings.TrimRight(text, "\r")
}

// ports lists the ports, by channel number.
func (n *Node) ports(key linkKey, _ *session, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Ports:\r")

	for _, port := range n.config.Ports {
		fmt.Fprintf(&b, "  %2d %s\r", port.Channel, port.Description)
	}

	n.send(key, []byte(b.String()))

	return true
}

// mheard lists the stations most recently heard over the radio, with the
// port each was heard on and how long ago.
func (n *Node) mheard(key linkKey, _ *session, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Heard:\r")

	var now = n.now()

	for _, h := range n.heard(maxHeard) {
		var ago = max(0, now.Sub(h.LastHeard)).Round(time.Second)

		fmt.Fprintf(&b, "  %-9s %2d %d:%02d:%02d\r", h.Callsign, h.Channel,
			int(ago.Hours()), int(ago.Minutes())%60, int(ago.Seconds())%60)
	}

	n.send(key, []byte(b.String()))

	return true
}

// nodesPerLine is how many nodes NODES lists on a line.
const nodesPerLine = 3

// nodes lists the nodes NET/ROM can reach, by alias, or the routes to the
// one named.
func (n *Node) nodes(key linkKey, _ *session, args []string) bool {
	if n.netrom == nil {
		return n.noNetROM(key)
	}

	var status = n.netrom.Status()

	if len(args) > 0 {
		n.nodeRoutes(key, status, args[0])

		return true
	}

	// By alias, as other nodes list them, and those without one after, by
	// callsign.
	var destinations = slices.Clone(status.Destinations)
	slices.SortFunc(destinations, func(a, b netrom.DestinationStatus) int {
		return cmp.Or(cmp.Compare(noAlias(a), noAlias(b)), cmp.Compare(a.Alias, b.Alias), cmp.Compare(a.Callsign, b.Callsign))
	})

	var names = make([]string, 0, len(destinations))
	for _, d := range destinations {
		names = append(names, nodeName(d))
	}

	var b strings.Builder

	b.WriteString(n.prompt() + "Nodes:\r")

	for line := range slices.Chunk(names, nodesPerLine) {
		for i, name := range line {
			if i == len(line)-1 {
				b.WriteString(name)
			} else {
				fmt.Fprintf(&b, "%-17s ", name)
			}
		}

		b.WriteString("\r")
	}

	n.send(key, []byte(b.String()))

	return true
}

// nodeRoutes lists the routes to the node named, by alias or callsign, best
// first.
func (n *Node) nodeRoutes(key linkKey, status netrom.Status, name string) {
	var d, ok = findNode(status, name)
	if !ok {
		n.send(key, []byte(n.prompt()+"No such node "+strings.ToUpper(name)+"\r"))

		return
	}

	var b strings.Builder

	fmt.Fprintf(&b, "%sRoutes to %s:\r", n.prompt(), nodeName(d))

	for _, r := range d.Routes {
		fmt.Fprintf(&b, "  %-9s quality %3d obsolescence %d\r", r.Neighbour, r.Quality, r.Obsolescence)
	}

	n.send(key, []byte(b.String()))
}

// findNode is the node in status named name, by alias or callsign.
func findNode(status netrom.Status, name string) (netrom.DestinationStatus, bool) {
	var call, _ = Address(name)
	name = strings.ToUpper(name)

	var i = slices.IndexFunc(status.Destinations, func(d netrom.DestinationStatus) bool {
		return d.Callsign == call || (d.Alias != "" && d.Alias == name)
	})
	if i < 0 {
		var none netrom.DestinationStatus

		return none, false
	}

	return status.Destinations[i], true
}

// noAlias is 1 for a node without an alias, which NODES lists after those
// with one, and 0 otherwise.
func noAlias(d netrom.DestinationStatus) int {
	if d.Alias == "" {
		return 1
	}

	return 0
}

// nodeName is how NODES names a node: ALIAS:CALLSIGN, or the callsign alone
// when it has no alias.
func nodeName(d netrom.DestinationStatus) string {
	if d.Alias == "" {
		return d.Callsign
	}

	return d.Alias + ":" + d.Callsign
}

// routes lists the NET/ROM node's neighbours, with whether the link to each
// is up, what it is taken to be worth, and how many nodes can be reached
// through it.
func (n *Node) routes(key linkKey, _ *session, _ []string) bool {
	if n.netrom == nil {
		return n.noNetROM(key)
	}

	var status = n.netrom.Status()

	var b strings.Builder

	b.WriteString(n.prompt() + "Routes:\r")

	for _, nb := range status.Neighbours {
		var count = 0

		for _, d := range status.Destinations {
			if slices.ContainsFunc(d.Routes, func(r netrom.RouteStatus) bool { return r.Neighbour == nb.Callsign }) {
				count++
			}
		}

		var nodes = "nodes"
		if count == 1 {
			nodes = "node"
		}

		var failing string
		if nb.Failing {
			failing = " failing"
		}

		fmt.Fprintf(&b, "  %-9s %-10s quality %3d %d %s%s\r", nb.Callsign, nb.Link, nb.Quality, count, nodes, failing)
	}

	n.send(key, []byte(b.String()))

	return true
}

// noNetROM answers the commands that list what NET/ROM knows, when the node
// runs no NET/ROM.
func (n *Node) noNetROM(key linkKey) bool {
	n.send(key, []byte(n.prompt()+"This node does not do NET/ROM\r"))

	return true
}

// Address is addr written as the data link writes addresses - upper case,
// with no SSID when it is 0 - which is how it must be for the data link to
// recognise a connection to it, or for us to tell one address from another.
// ok is false if it is not a valid address.
func Address(addr string) (string, bool) {
	var call, ssid, _, ok = ax25.ParseAddr(-1, strings.ToUpper(addr), ax25.AddrStrictNoStar)
	if !ok || call == "" {
		return "", false
	}

	if ssid != 0 {
		call += "-" + strconv.Itoa(ssid)
	}

	return call, true
}

// identity is how the node names itself: ALIAS:CALLSIGN, or the callsign
// alone when it has no alias.
func (n *Node) identity() string {
	if n.config.Alias == "" {
		return n.config.Address
	}

	return n.config.Alias + ":" + n.config.Address
}

// prompt heads each reply, as other nodes' do, so a user can tell the node's
// words from those of a station it has connected them to.
func (n *Node) prompt() string {
	return n.identity() + "} "
}

// addrs is the address list for sending on the link key names.
func addrs(key linkKey) [ax25.MaxAddrs]string {
	var a [ax25.MaxAddrs]string

	a[ax25.Source] = key.ownCall
	a[ax25.Destination] = key.remoteCall

	return a
}

// send sends data to the user on the link key names.
func (n *Node) send(key linkKey, data []byte) {
	n.forward(key, ax25.PIDNoLayer3, data)
}

// forward sends data, with the protocol ID pid, on the link key names: what
// crosses between a user and the station they are connected to goes as it
// came.  A circuit has no PID of its own to give it.
func (n *Node) forward(key linkKey, pid int, data []byte) {
	if key.circuit {
		n.netrom.Send(key.channel, n.client, key.ownCall, key.remoteCall, data)

		return
	}

	n.link.XmitDataRequest(addrs(key), 2, key.channel, n.client, pid, data)
}

// disconnect ends the link or circuit key names.  The session goes when the
// data link says the link has, and a downlink when whichever has it says so.
func (n *Node) disconnect(key linkKey) {
	if key.circuit {
		n.netrom.Disconnect(key.channel, n.client, key.ownCall, key.remoteCall)

		return
	}

	n.link.DisconnectRequest(addrs(key), 2, key.channel, n.client)
}

// Circuits is told what happens on the NET/ROM circuits the node opens for
// its users, as the node itself is told about its links: hand it to the
// NET/ROM node to report them to.
func (n *Node) Circuits() CircuitEvents {
	return CircuitEvents{n: n}
}

// CircuitEvents is what the NET/ROM node tells the node about the circuits
// it opened for its users.  It has the methods netrom.Reporter asks for.
type CircuitEvents struct {
	n *Node
}

func circuitKey(channel int, remoteCall string, ownCall string) linkKey {
	return linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall, circuit: true}
}

func (c CircuitEvents) LinkEstablished(channel int, _ int, remoteCall string, ownCall string, _ bool) {
	c.n.downlinkEstablished(circuitKey(channel, remoteCall, ownCall))
}

func (c CircuitEvents) LinkTerminated(channel int, _ int, remoteCall string, ownCall string, timeout bool) {
	c.n.downlinkTerminated(circuitKey(channel, remoteCall, ownCall), timeout)
}

func (c CircuitEvents) RecConnData(channel int, _ int, remoteCall string, ownCall string, pid int, data []byte) {
	c.n.received(circuitKey(channel, remoteCall, ownCall), pid, data)
}

// OutstandingFramesReply is an answer to a question the node never asks.
func (CircuitEvents) OutstandingFramesReply(int, int, string, string, int) {}
