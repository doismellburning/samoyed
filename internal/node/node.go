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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/mheard"
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

	// Info is what INFO tells a user about the node, or empty for a
	// line saying what it is.  It may run to several lines.
	Info string
}

// Port is a channel the node accepts connections on.
type Port struct {
	Channel int

	// Description is what PORTS says the channel is.
	Description string
}

// linkKey identifies a link the node is answering, as the data link does.
type linkKey struct {
	channel    int
	ownCall    string
	remoteCall string
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

// Node is the node shell.
type Node struct {
	config Config
	link   DataLink
	heard  HeardStations

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

// New makes a node that talks to link as client, and lists the stations heard
// says it has heard.  Nothing is registered with the data link until Start.
func New(config Config, link DataLink, heard HeardStations, client int) *Node {
	var n = new(Node)

	n.config = config
	n.link = link
	n.heard = heard
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
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

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
		n.send(key, []byte(n.prompt()+"Welcome to Samoyed\r"))
	}
}

// LinkTerminated is told a link has gone down.  A user's ends their session;
// a downlink's returns its user to the node.
func (n *Node) LinkTerminated(channel int, _ int, remoteCall string, ownCall string, timeout bool) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

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
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	var lines [][]byte

	n.mu.Lock()

	if up, ok := n.downlinks[key]; ok {
		var s = n.downlinkUserLocked(key)

		// Not for a user who gave up on it, and is back at the node.
		var forward = s != nil && !s.cancelled
		n.mu.Unlock()

		if forward {
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
func commands() []command {
	return []command{
		{names: byeNames(), help: "Disconnect", run: (*Node).bye},
		{names: []string{"CONNECT", "C"}, help: "Connect to a station: C [port] callsign", run: (*Node).connect},
		{names: []string{"HELP", "H", "?"}, help: "List the commands", run: (*Node).help},
		{names: []string{"INFO", "I"}, help: "About this node", run: (*Node).info},
		{names: []string{"PORTS", "P"}, help: "List the ports", run: (*Node).ports},
		{names: []string{"MHEARD", "MH"}, help: "List the stations heard", run: (*Node).mheard},
		{names: []string{"NODES", "N"}, help: "List the nodes known", run: (*Node).noNetROM},
		{names: []string{"ROUTES", "R"}, help: "List the routes known", run: (*Node).noNetROM},
	}
}

// command carries out one command line for the user on the link key names,
// whose session is s, and says whether to go on to another.
func (n *Node) command(key linkKey, s *session, line string) bool {
	var words = strings.Fields(line)
	var name = strings.ToUpper(words[0])

	for _, c := range commands() {
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

// connect connects the user onward, to the station named on the port named.
// The port can be left out when there is only one.
func (n *Node) connect(key linkKey, s *session, args []string) bool {
	var channel, call, problem = n.connectArgs(args)
	if problem != "" {
		n.send(key, []byte(n.prompt()+problem+"\r"))

		return true
	}

	var down = linkKey{channel: channel, ownCall: n.config.Address, remoteCall: call}

	n.mu.Lock()

	// The data link has one link for each pair of stations on a channel, so
	// a station already connected to the node, or connected to from it, is
	// not one it can connect to again.
	var _, isUser = n.sessions[down]
	var _, isDownlink = n.downlinks[down]

	if isUser || isDownlink {
		n.mu.Unlock()
		n.send(key, []byte(n.prompt()+"Busy - "+call+" is already connected to this node\r"))

		return true
	}

	n.downlinks[down] = key
	s.downlink = &down
	s.cancelled = false

	// Anything after CONNECT in the frame was sent before the user could
	// know what it would do, so it is not kept for later.
	s.line = s.line[:0]
	s.overlong = false
	n.mu.Unlock()

	n.send(key, []byte(fmt.Sprintf("%sTrying %s on port %d\r", n.prompt(), call, channel)))
	n.link.ConnectRequest(addrs(down), 2, down.channel, n.client, ax25.PIDNoLayer3)

	return false
}

// connectArgs makes sense of what was typed after CONNECT: the channel and the
// callsign to connect to, or what is wrong with them.
func (n *Node) connectArgs(args []string) (int, string, string) {
	const usage = "Usage: C [port] callsign"

	var channel int

	switch len(args) {
	case 1:
		if len(n.config.Ports) != 1 {
			return 0, "", usage
		}

		channel = n.config.Ports[0].Channel
	case 2:
		var number, err = strconv.Atoi(args[0])
		if err != nil {
			return 0, "", usage
		}

		if !slices.ContainsFunc(n.config.Ports, func(p Port) bool { return p.Channel == number }) {
			return 0, "", fmt.Sprintf("No port %d - type P for the ports", number)
		}

		channel = number
	default:
		return 0, "", usage
	}

	var call, ok = Address(args[len(args)-1])
	if !ok {
		return 0, "", "Invalid callsign " + args[len(args)-1]
	}

	if call == n.config.Address || call == n.config.Alias {
		return 0, "", "That is this node"
	}

	return channel, call, ""
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
	if announce {
		s.connected = true

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
		n.send(up, []byte(n.prompt()+"Connected to "+key.remoteCall+"\r"))
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

	if s != nil {
		wasConnected, cancelled = s.connected, s.cancelled
		s.downlink = nil
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
		message = "Gave up on " + key.remoteCall
	case wasConnected:
		message = "Disconnected from " + key.remoteCall
	case timeout:
		message = "Failure with " + key.remoteCall + " - no answer"
	default:
		message = "Failure with " + key.remoteCall + " - busy"
	}

	n.send(up, []byte(n.prompt()+message+"\r"))
}

// help lists the commands.
func (n *Node) help(key linkKey, _ *session, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Commands:\r")

	for _, c := range commands() {
		fmt.Fprintf(&b, "  %s - %s\r", strings.Join(c.names, ", "), c.help)
	}

	n.send(key, []byte(b.String()))

	return true
}

// info tells the user about the node.
func (n *Node) info(key linkKey, _ *session, _ []string) bool {
	var text = n.config.Info
	if text == "" {
		text = n.identity() + " is a Samoyed node"
	}

	// However the configuration file ended its lines, a packet terminal
	// wants CR.
	text = strings.ReplaceAll(text, "\r\n", "\r")
	text = strings.ReplaceAll(text, "\n", "\r")
	text = strings.TrimRight(text, "\r")

	n.send(key, []byte(n.prompt()+text+"\r"))

	return true
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

// noNetROM answers the commands that list what NET/ROM knows, of which
// there is nothing yet.
func (n *Node) noNetROM(key linkKey, _ *session, _ []string) bool {
	n.send(key, []byte(n.prompt()+"This node does not do NET/ROM yet\r"))

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
// came.
func (n *Node) forward(key linkKey, pid int, data []byte) {
	n.link.XmitDataRequest(addrs(key), 2, key.channel, n.client, pid, data)
}

// disconnect ends the link key names.  The session goes when the data link
// says the link has.
func (n *Node) disconnect(key linkKey) {
	n.link.DisconnectRequest(addrs(key), 2, key.channel, n.client)
}
