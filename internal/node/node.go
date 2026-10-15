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

	// mu guards sessions.  The data link calls in on its own goroutine;
	// the lock keeps the node safe from any other caller too.
	mu       sync.Mutex
	sessions map[linkKey]*session
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
// us gets a session and a greeting.
func (n *Node) LinkEstablished(channel int, _ int, remoteCall string, ownCall string, incoming bool) {
	if !incoming {
		return
	}

	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	n.mu.Lock()

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

// LinkTerminated is told a link has gone down, which ends its session.
func (n *Node) LinkTerminated(channel int, _ int, remoteCall string, ownCall string, _ bool) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	n.mu.Lock()
	delete(n.sessions, key)
	n.mu.Unlock()
}

// RecConnData is handed what a user sent, which is taken a line at a time.
func (n *Node) RecConnData(channel int, _ int, remoteCall string, ownCall string, _ int, data []byte) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	var lines [][]byte

	n.mu.Lock()

	var s = n.sessions[key]
	if s != nil && !s.leaving {
		lines = s.take(data)
	}

	n.mu.Unlock()

	for _, line := range lines {
		if line == nil {
			n.send(key, []byte(n.prompt()+"Line too long\r"))

			continue
		}

		if !n.command(key, string(line)) {
			n.mu.Lock()
			s.leaving = true
			n.mu.Unlock()

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

	// run carries it out for the user on the link key names, with the
	// words typed after its name, and says whether the session is still
	// there for another command.
	run func(n *Node, key linkKey, args []string) bool
}

// commands are the shell's commands, in the order HELP lists them.
func commands() []command {
	return []command{
		{names: []string{"BYE", "B", "QUIT", "Q"}, help: "Disconnect", run: (*Node).bye},
		{names: []string{"HELP", "H", "?"}, help: "List the commands", run: (*Node).help},
		{names: []string{"INFO", "I"}, help: "About this node", run: (*Node).info},
		{names: []string{"PORTS", "P"}, help: "List the ports", run: (*Node).ports},
		{names: []string{"MHEARD", "MH"}, help: "List the stations heard", run: (*Node).mheard},
		{names: []string{"NODES", "N"}, help: "List the nodes known", run: (*Node).noNetROM},
		{names: []string{"ROUTES", "R"}, help: "List the routes known", run: (*Node).noNetROM},
	}
}

// command carries out one command line, and says whether the session is
// still there for another.
func (n *Node) command(key linkKey, line string) bool {
	var words = strings.Fields(line)
	var name = strings.ToUpper(words[0])

	for _, c := range commands() {
		if slices.Contains(c.names, name) {
			return c.run(n, key, words[1:])
		}
	}

	n.send(key, []byte(n.prompt()+"Invalid command - type ? for help\r"))

	return true
}

// bye disconnects.
func (n *Node) bye(key linkKey, _ []string) bool {
	n.disconnect(key)

	return false
}

// help lists the commands.
func (n *Node) help(key linkKey, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Commands:\r")

	for _, c := range commands() {
		fmt.Fprintf(&b, "  %s - %s\r", strings.Join(c.names, ", "), c.help)
	}

	n.send(key, []byte(b.String()))

	return true
}

// info tells the user about the node.
func (n *Node) info(key linkKey, _ []string) bool {
	var text = n.config.InfoMessage
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
func (n *Node) ports(key linkKey, _ []string) bool {
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
func (n *Node) mheard(key linkKey, _ []string) bool {
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
func (n *Node) noNetROM(key linkKey, _ []string) bool {
	n.send(key, []byte(n.prompt()+"This node does not do NET/ROM yet\r"))

	return true
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
	n.link.XmitDataRequest(addrs(key), 2, key.channel, n.client, ax25.PIDNoLayer3, data)
}

// disconnect ends the link key names.  The session goes when the data link
// says the link has.
func (n *Node) disconnect(key linkKey) {
	n.link.DisconnectRequest(addrs(key), 2, key.channel, n.client)
}
