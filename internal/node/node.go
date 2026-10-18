// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package node is a packet node's command shell: a station connects to the
// node's callsign or alias and types commands at it, one per line.
//
// The node is a client of the AX.25 data link, as an AGW application is.  It
// registers its callsigns for incoming connections, is told when a link to
// one of them comes up or goes down and what arrives on it, and asks the data
// link to send its replies.  A user can also reach it, and it can connect a
// user onward, over a connection other than one of its own AX.25 links - a
// NET/ROM circuit, say - which is handed to it as a Conn.
package node

import (
	"bytes"
	"fmt"
	"maps"
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

// Conn is a connection the node can talk to a user over, or connect a user
// onward on, other than one of its own AX.25 links: a NET/ROM circuit, say.
// Its methods may be called from any goroutine.
type Conn interface {
	// Send sends data to the far end.
	Send(data []byte)

	// Close disconnects.
	Close()
}

// peer is one end of a conversation the node is part of: an AX.25 link it is
// answering or has opened, or a Conn.  Each is comparable, so it can key the
// node's maps.
type peer interface {
	// send sends data to the far end, with the protocol ID pid where that
	// means anything.
	send(n *Node, pid int, data []byte)

	// disconnect ends the conversation.
	disconnect(n *Node)

	// name is who is at the far end, for messages.
	name() string

	// where says the channel and the node's own address an AX.25 link is
	// on, or -1 and "" for a Conn.
	where() (int, string)
}

// linkKey identifies a link the node is answering, as the data link does.
type linkKey struct {
	channel    int
	ownCall    string
	remoteCall string
}

func (k linkKey) send(n *Node, pid int, data []byte) {
	n.link.XmitDataRequest(addrs(k), 2, k.channel, n.client, pid, data)
}

func (k linkKey) disconnect(n *Node) {
	n.link.DisconnectRequest(addrs(k), 2, k.channel, n.client)
}

func (k linkKey) name() string { return k.remoteCall }

func (k linkKey) where() (int, string) { return k.channel, k.ownCall }

// connPeer is a Conn the node talks over.  It is a pointer, so each is a peer
// of its own however its far end is named.
type connPeer struct {
	mu     sync.Mutex
	conn   Conn // Nil until a downlink's Conn is known.
	closed bool // Close was asked for before conn was known.
	remote string
}

func (c *connPeer) send(_ *Node, _ int, data []byte) {
	c.mu.Lock()
	var conn = c.conn
	c.mu.Unlock()

	if conn != nil {
		conn.Send(data)
	}
}

func (c *connPeer) disconnect(*Node) {
	c.mu.Lock()
	var conn = c.conn
	c.closed = conn == nil
	c.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
}

func (c *connPeer) name() string { return c.remote }

func (c *connPeer) where() (int, string) { return -1, "" }

// setConn sets the Conn a downlink travels on, once it is known, closing it
// straight away if the downlink was given up on in the meantime.
func (c *connPeer) setConn(conn Conn) {
	c.mu.Lock()
	c.conn = conn
	var closed = c.closed
	c.mu.Unlock()

	if closed {
		conn.Close()
	}
}

// NetROM is the node's NET/ROM, as far as the shell needs it: it hears about
// the AX.25 links that carry NET/ROM and what arrives on them, answers NODES
// and ROUTES, and connects users onward to other nodes.  The shell calls it
// without holding its own lock, possibly from the data link's goroutine, so
// it must not wait on the shell.
type NetROM interface {
	// IsNeighbour says whether call, heard on channel, is a neighbour node
	// rather than a user.
	IsNeighbour(channel int, call string) bool

	// LinkUp and LinkDown hear about a link that carries, or might carry,
	// NET/ROM: one a neighbour opened, or one NET/ROM asked for itself.
	LinkUp(channel int, ownCall string, remoteCall string, incoming bool)
	LinkDown(channel int, ownCall string, remoteCall string, timeout bool)

	// Packet takes a NET/ROM packet that arrived on a link.
	Packet(channel int, ownCall string, remoteCall string, data []byte)

	// Nodes and Neighbours are what NET/ROM knows, for NODES and ROUTES.
	Nodes() []netrom.Destination
	Neighbours() []netrom.Neighbour

	// Knows says whether name, a callsign or an alias, is a node NET/ROM
	// can connect to.
	Knows(name string) bool

	// Connect connects user onward to the node dest over NET/ROM, telling
	// events how it goes, and returns the connection.
	Connect(dest string, user string, events Downlink) Conn
}

// Downlink is told how a connection onward that NET/ROM makes is going.
type Downlink interface {
	// Connected says the far end answered.
	Connected()

	// Received hands over what the far end sent.
	Received(data []byte)

	// Closed says the connection has gone: failure says why one that never
	// came up failed.
	Closed(failure string)
}

// Sender is somewhere to send a user text.
type Sender interface {
	Send(data []byte)
}

// Application is something a user can start from the node's shell, by a
// command of its own, such as a chat server.
type Application interface {
	// Name is the command that starts it, such as "CHAT".
	Name() string

	// Description says what it is, for HELP.
	Description() string

	// Open starts a session for user, sending to out.  The application
	// calls exit when the user leaves it, to go back to the node.  Open is
	// called without the node's lock held, and exit may be called from any
	// goroutine, Open's own included.
	Open(user string, out Sender, exit func()) AppSession
}

// AppSession is one user's time in an application.  Its methods are called
// without the node's lock held.
type AppSession interface {
	// Input hands over what the user sent, as it came.
	Input(data []byte)

	// Close says the user has gone altogether.
	Close()
}

// appRun is a user's time in an application, from the shell's side.
type appRun struct {
	name string

	// session is what Open returned, or nil until it has.
	session AppSession

	// closed says the user went while Open was still under way, so the
	// session it returns is to be closed at once.
	closed bool
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
	downlink peer

	// connected says downlink is up, and what the user sends goes to it.
	connected bool

	// cancelled says the user gave up on downlink before it came up.
	cancelled bool

	// app is the application the user is in, or nil.
	app *appRun
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
	sessions map[peer]*session

	// downlinks are the links users have asked to be connected onward on,
	// each to the user's own link.
	downlinks map[peer]peer

	// netrom is the node's NET/ROM, or nil for none.  It is set before
	// Start and not changed after.
	netrom NetROM

	// observer is told what happens to sessions, or is nil.  It is set
	// before Start and not changed after.
	observer func(SessionEvent)

	// apps are the applications users can start, which are added before
	// Start and not changed after.
	apps []Application

	// services are the applications stations can connect to directly, by
	// their own callsigns, which are added before Start and not changed
	// after.
	services map[string]Service

	// serviceSessions are the stations connected to a service, and
	// appLinks the links applications opened, both guarded by mu.
	serviceSessions map[linkKey]*appRun
	appLinks        map[linkKey]*appLink
}

// SessionEventKind says what a SessionEvent is about.
type SessionEventKind int

const (
	SessionStarted SessionEventKind = iota // A user connected to the node.
	SessionEnded                           // A user went.
	DownlinkUp                             // A user was connected onward.
	DownlinkDown                           // A user's connection onward went, or failed.
)

// SessionEvent is something that happened to a user's session, for anyone
// watching the node.
type SessionEvent struct {
	Kind SessionEventKind

	// User is who the session is for, on Channel at Local, the address of
	// the node's they connected to - or -1 and "" over a Conn.
	User    string
	Channel int
	Local   string

	// Downlink is who the user was connected onward to, and Failure why a
	// connection onward that never came up failed.
	Downlink string
	Failure  string
}

// SetObserver has f told what happens to users' sessions, without the node's
// lock held.  It must be called before Start.
func (n *Node) SetObserver(f func(SessionEvent)) {
	n.observer = f
}

// observe tells the observer, if there is one, what happened to the session
// on key.
func (n *Node) observe(kind SessionEventKind, key peer, downlink string, failure string) {
	if n.observer == nil {
		return
	}

	var channel, local = key.where()

	n.observer(SessionEvent{Kind: kind, User: key.name(), Channel: channel, Local: local, Downlink: downlink, Failure: failure})
}

// SessionInfo is a user's session, as Sessions reports it.
type SessionInfo struct {
	User      string
	Channel   int    // -1 over a Conn.
	Local     string // The node's address the user connected to, or "" over a Conn.
	Downlink  string // Who the user is connected onward to, or "".
	Connected bool   // Whether the connection onward is up.
	App       string // The application the user is in, or "".
}

// Sessions reports every user's session.
func (n *Node) Sessions() []SessionInfo {
	n.mu.Lock()
	defer n.mu.Unlock()

	var list = make([]SessionInfo, 0, len(n.sessions))

	for key, s := range n.sessions {
		var channel, local = key.where()

		var info = SessionInfo{User: key.name(), Channel: channel, Local: local, Downlink: "", Connected: s.connected, App: ""}
		if s.downlink != nil {
			info.Downlink = s.downlink.name()
		}

		if s.app != nil {
			info.App = s.app.name
		}

		list = append(list, info)
	}

	for key, run := range n.serviceSessions {
		list = append(list, SessionInfo{User: key.remoteCall, Channel: key.channel, Local: key.ownCall, Downlink: "", Connected: false, App: run.name})
	}

	slices.SortFunc(list, func(a, b SessionInfo) int {
		if a.Channel != b.Channel {
			return a.Channel - b.Channel
		}

		return strings.Compare(a.User, b.User)
	})

	return list
}

// AddApplication lets users start a, by its name.  It must be called before
// Start, and a's name must not be one of the shell's own commands.
func (n *Node) AddApplication(a Application) {
	n.apps = append(n.apps, a)
}

// SetNetROM gives the node NET/ROM.  It must be called before Start.
func (n *Node) SetNetROM(nr NetROM) {
	n.netrom = nr
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
	n.sessions = make(map[peer]*session)
	n.downlinks = make(map[peer]peer)
	n.services = make(map[string]Service)
	n.serviceSessions = make(map[linkKey]*appRun)
	n.appLinks = make(map[linkKey]*appLink)

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

		for _, call := range slices.Sorted(maps.Keys(n.services)) {
			n.link.RegisterCallsign(call, port.Channel, n.client)
		}
	}
}

// LinkEstablished is told a link has come up.  A station that connected to
// us gets a session and a greeting; a user we were connecting onward is told
// they are through.
func (n *Node) LinkEstablished(channel int, _ int, remoteCall string, ownCall string, incoming bool) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	if n.applicationLinkUp(key, incoming) {
		return
	}

	// A link NET/ROM opened, or a neighbour node's, is NET/ROM's, and
	// nobody's session.
	if n.netrom != nil && !n.isUsers(key) && (!incoming || n.netrom.IsNeighbour(channel, remoteCall)) {
		n.netrom.LinkUp(channel, ownCall, remoteCall, incoming)

		return
	}

	n.established(key, incoming)
}

// isUsers says whether key is a user's link or a downlink of one.
func (n *Node) isUsers(key linkKey) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	var _, isSession = n.sessions[key]
	var _, isDownlink = n.downlinks[key]

	return isSession || isDownlink
}

// established is told the conversation with key is under way.
func (n *Node) established(key peer, incoming bool) {
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
		n.observe(SessionStarted, key, "", "")

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
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	if n.applicationLinkDown(key, timeout) {
		return
	}

	if n.netrom != nil && !n.isUsers(key) {
		n.netrom.LinkDown(channel, ownCall, remoteCall, timeout)

		return
	}

	n.terminated(key, failureFor(timeout))
}

// failureFor says why a link that never came up failed: timeout says the
// station never answered, rather than refusing.
func failureFor(timeout bool) string {
	if timeout {
		return "no answer"
	}

	return "busy"
}

// terminated is told the conversation with key is over; why says why a
// downlink that never came up failed.
func (n *Node) terminated(key peer, why string) {
	n.mu.Lock()

	if _, ok := n.downlinks[key]; ok {
		n.mu.Unlock()
		n.downlinkTerminated(key, why)

		return
	}

	var s = n.sessions[key]
	delete(n.sessions, key)

	var down peer
	var app AppSession

	if s != nil {
		down = s.downlink
		app = s.endAppLocked()
	}

	n.mu.Unlock()

	if app != nil {
		app.Close()
	}

	if s != nil {
		n.observe(SessionEnded, key, "", "")
	}

	// A user who goes takes their downlink with them.
	if down != nil {
		n.disconnect(down)
	}
}

// RecConnData is handed what a station sent.  What a user sends the node is
// taken a line at a time; what they send once connected onward goes to the
// downlink as it is, and what comes back on the downlink goes to them.
func (n *Node) RecConnData(channel int, _ int, remoteCall string, ownCall string, pid int, data []byte) {
	var key = linkKey{channel: channel, ownCall: ownCall, remoteCall: remoteCall}

	if n.applicationData(key, data) {
		return
	}

	if n.netrom != nil && pid == ax25.PIDNetROM && n.netromPacket(key) {
		n.netrom.Packet(channel, ownCall, remoteCall, data)

		return
	}

	n.received(key, pid, data)
}

// netromPacket says whether a NET/ROM packet on key is NET/ROM's: all of them
// but those on a user's downlink, which go to the user as they are.  A node
// that connected before it was known as a neighbour was given a session,
// which it now loses, its link going to NET/ROM.
func (n *Node) netromPacket(key linkKey) bool {
	n.mu.Lock()

	if _, isDownlink := n.downlinks[key]; isDownlink {
		n.mu.Unlock()

		return false
	}

	var s, isSession = n.sessions[key]

	var app AppSession

	if isSession && s.downlink == nil {
		delete(n.sessions, key)

		app = s.endAppLocked()
	}

	n.mu.Unlock()

	if app != nil {
		app.Close()
	}

	if isSession && s.downlink == nil {
		n.netrom.LinkUp(key.channel, key.ownCall, key.remoteCall, true)
	}

	return !isSession || s.downlink == nil
}

// received is handed what key sent.
func (n *Node) received(key peer, pid int, data []byte) {
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

	if s != nil && s.app != nil {
		// What arrives while the application is still being opened has
		// nowhere to go yet.
		var app = s.app.session
		n.mu.Unlock()

		if app != nil {
			app.Input(data)
		}

		return
	}

	if s != nil && s.downlink != nil && s.connected {
		var down = s.downlink
		n.mu.Unlock()

		n.forward(down, pid, data)

		return
	}

	if s != nil && s.downlink != nil {
		// Still coming up: a line typed now gives up on it.
		var down = s.downlink

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

	for i, line := range lines {
		if line == nil {
			n.send(key, []byte(n.prompt()+"Line too long\r"))

			continue
		}

		if !n.command(key, s, string(line)) {
			n.handOn(key, s, lines[i+1:])

			return
		}
	}
}

// handOn gives an application the user has just started what else they sent
// along with the command: the lines after it, and the start of the next.
func (n *Node) handOn(key peer, s *session, lines [][]byte) {
	n.mu.Lock()

	if n.sessions[key] != s || s.app == nil || s.app.session == nil {
		n.mu.Unlock()

		return
	}

	var app = s.app.session

	var rest []byte

	for _, line := range lines {
		if line != nil {
			rest = append(append(rest, line...), '\r')
		}
	}

	if !s.overlong {
		rest = append(rest, s.line...)
	}

	s.line = s.line[:0]
	s.overlong = false

	n.mu.Unlock()

	if len(rest) > 0 {
		app.Input(rest)
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
	run func(n *Node, key peer, s *session, args []string) bool
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
		{names: []string{"NODES", "N"}, help: "List the nodes known", run: (*Node).nodes},
		{names: []string{"ROUTES", "R"}, help: "List the routes known", run: (*Node).routes},
	}
}

// command carries out one command line for the user on the link key names,
// whose session is s, and says whether to go on to another.
func (n *Node) command(key peer, s *session, line string) bool {
	var words = strings.Fields(line)
	var name = strings.ToUpper(words[0])

	for _, c := range commands() {
		if slices.Contains(c.names, name) {
			return c.run(n, key, s, words[1:])
		}
	}

	for _, a := range n.apps {
		if strings.EqualFold(a.Name(), name) {
			return n.startApp(key, s, a)
		}
	}

	n.send(key, []byte(n.prompt()+"Invalid command - type ? for help\r"))

	return true
}

// bye disconnects.
func (n *Node) bye(key peer, s *session, _ []string) bool {
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
// The port can be left out when there is only one.  A node NET/ROM knows is
// connected to over NET/ROM, by its callsign or alias alone.
func (n *Node) connect(key peer, s *session, args []string) bool {
	if n.netrom != nil && len(args) == 1 && n.netrom.Knows(args[0]) {
		return n.connectNode(key, s, strings.ToUpper(args[0]))
	}

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
	s.downlink = down
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
func (n *Node) downlinkUserLocked(key peer) *session {
	var up, ok = n.downlinks[key]
	if !ok {
		return nil
	}

	// A user who went and came back has a new session, which this
	// downlink is not for.
	var s = n.sessions[up]
	if s == nil || s.downlink == nil || s.downlink != key {
		return nil
	}

	return s
}

// downlinkEstablished is told the downlink key names is up.
func (n *Node) downlinkEstablished(key peer) {
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
		n.observe(DownlinkUp, up, key.name(), "")
		n.send(up, []byte(n.prompt()+"Connected to "+key.name()+"\r"))
	case unwanted:
		n.disconnect(key)
	}
}

// downlinkTerminated is told the downlink key names is down, which returns
// its user to the node.  A downlink that never came up has failed, for the
// reason why gives.
func (n *Node) downlinkTerminated(key peer, why string) {
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

	var failure string

	switch {
	case cancelled:
		message = "Gave up on " + key.name()
	case wasConnected:
		message = "Disconnected from " + key.name()
	default:
		message = "Failure with " + key.name() + " - " + why
		failure = why
	}

	n.observe(DownlinkDown, up, key.name(), failure)

	n.send(up, []byte(n.prompt()+message+"\r"))
}

// help lists the commands.
func (n *Node) help(key peer, _ *session, _ []string) bool {
	var b strings.Builder

	b.WriteString(n.prompt() + "Commands:\r")

	for _, c := range commands() {
		fmt.Fprintf(&b, "  %s - %s\r", strings.Join(c.names, ", "), c.help)
	}

	for _, a := range n.apps {
		fmt.Fprintf(&b, "  %s - %s\r", strings.ToUpper(a.Name()), a.Description())
	}

	n.send(key, []byte(b.String()))

	return true
}

// info tells the user about the node.
func (n *Node) info(key peer, _ *session, _ []string) bool {
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
func (n *Node) ports(key peer, _ *session, _ []string) bool {
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
func (n *Node) mheard(key peer, _ *session, _ []string) bool {
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

// noNetROM answers the commands that list what NET/ROM knows, for a node
// without NET/ROM.
func (n *Node) noNetROM(key peer) bool {
	n.send(key, []byte(n.prompt()+"This node does not do NET/ROM yet\r"))

	return true
}

// nodes lists the nodes NET/ROM knows, or the routes to the one named.
func (n *Node) nodes(key peer, _ *session, args []string) bool {
	if n.netrom == nil {
		return n.noNetROM(key)
	}

	var known = n.netrom.Nodes()

	var b strings.Builder

	if len(args) > 0 {
		var i = slices.IndexFunc(known, func(d netrom.Destination) bool {
			return strings.EqualFold(d.Call, args[0]) || (d.Alias != "" && strings.EqualFold(d.Alias, args[0]))
		})
		if i < 0 {
			n.send(key, []byte(n.prompt()+"No such node "+strings.ToUpper(args[0])+"\r"))

			return true
		}

		fmt.Fprintf(&b, "%sRoutes to %s:\r", n.prompt(), nodeName(known[i].Alias, known[i].Call))

		for _, r := range known[i].Routes {
			fmt.Fprintf(&b, "  %3d %d %2d %s\r", r.Quality, r.Obsolescence, r.Neighbour.Port, r.Neighbour.Call)
		}

		n.send(key, []byte(b.String()))

		return true
	}

	b.WriteString(n.prompt() + "Nodes:\r")

	// Four to a line, as other nodes list them.
	for row := range slices.Chunk(known, 4) {
		var line strings.Builder
		for _, d := range row {
			fmt.Fprintf(&line, "%-19s", nodeName(d.Alias, d.Call))
		}

		b.WriteString(strings.TrimRight(line.String(), " ") + "\r")
	}

	n.send(key, []byte(b.String()))

	return true
}

// routes lists the neighbouring nodes.
func (n *Node) routes(key peer, _ *session, _ []string) bool {
	if n.netrom == nil {
		return n.noNetROM(key)
	}

	var b strings.Builder

	b.WriteString(n.prompt() + "Routes:\r")

	for _, nb := range n.netrom.Neighbours() {
		var locked = ""
		if nb.Locked {
			locked = " !"
		}

		fmt.Fprintf(&b, "  %2d %-19s %3d%s\r", nb.Port, nodeName(nb.Alias, nb.Call), nb.Quality, locked)
	}

	n.send(key, []byte(b.String()))

	return true
}

// nodeName is how a node is named in lists: ALIAS:CALLSIGN, or the callsign
// alone.
func nodeName(alias string, call string) string {
	if alias == "" {
		return call
	}

	return alias + ":" + call
}

// connectNode connects the user onward to the node dest over NET/ROM.
func (n *Node) connectNode(key peer, s *session, dest string) bool {
	var down = new(connPeer)
	down.remote = dest

	n.mu.Lock()
	n.downlinks[down] = key
	s.downlink = down
	s.cancelled = false
	s.line = s.line[:0]
	s.overlong = false
	n.mu.Unlock()

	n.send(key, []byte(n.prompt()+"Trying "+dest+"\r"))
	down.setConn(n.netrom.Connect(dest, key.name(), netromDownlink{n: n, p: down}))

	return false
}

// netromDownlink passes what happens on a NET/ROM connection onward to the
// shell.
type netromDownlink struct {
	n *Node
	p *connPeer
}

func (d netromDownlink) Connected()           { d.n.established(d.p, false) }
func (d netromDownlink) Received(data []byte) { d.n.received(d.p, ax25.PIDNoLayer3, data) }
func (d netromDownlink) Closed(failure string) {
	if failure == "" {
		failure = "busy"
	}

	d.n.terminated(d.p, failure)
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

// send sends data to the user key names.
func (n *Node) send(key peer, data []byte) {
	n.forward(key, ax25.PIDNoLayer3, data)
}

// forward sends data, with the protocol ID pid, to key: what crosses between
// a user and the station they are connected to goes as it came.
func (n *Node) forward(key peer, pid int, data []byte) {
	key.send(n, pid, data)
}

// disconnect ends the conversation with key.  The session goes when the far
// end says it has.
func (n *Node) disconnect(key peer) {
	key.disconnect(n)
}

// startApp puts the user into the application a.  Whatever else they sent
// with the command is not for the node, so is not gone on to: received hands
// it to the application instead.
func (n *Node) startApp(key peer, s *session, a Application) bool {
	var run = &appRun{name: strings.ToUpper(a.Name()), session: nil, closed: false}

	n.mu.Lock()

	// The user has asked to go, or NET/ROM has taken the link over.
	if s.leaving || n.sessions[key] != s {
		n.mu.Unlock()

		return false
	}

	s.app = run
	n.mu.Unlock()

	var app = a.Open(key.name(), peerSender{n: n, key: key}, func() { n.leaveApp(key, s, run) })

	n.mu.Lock()
	run.session = app
	var closed = run.closed
	n.mu.Unlock()

	if closed {
		app.Close()
	}

	return false
}

// leaveApp brings the user back from the application run to the node.
func (n *Node) leaveApp(key peer, s *session, run *appRun) {
	n.mu.Lock()

	var back = n.sessions[key] == s && s.app == run
	if back {
		s.app = nil
	}

	n.mu.Unlock()

	if back {
		n.send(key, []byte(n.prompt()+"Back at the node\r"))
	}
}

// endAppLocked takes the user out of their application, if they are in one,
// returning the session to close, or nil if there is none yet - Open having
// not returned, and closing what it does instead.  n.mu must be held.
func (s *session) endAppLocked() AppSession { //nolint:ireturn // Whatever the application opened.
	if s.app == nil {
		return nil
	}

	var run = s.app
	s.app = nil
	run.closed = true

	return run.session
}

// peerSender sends to the user on key.
type peerSender struct {
	n   *Node
	key peer
}

func (p peerSender) Send(data []byte) {
	p.n.send(p.key, data)
}

// Attached is a user's session on a Conn handed to the node with Attach.
type Attached struct {
	n *Node
	p *connPeer
}

// Attach gives the user remote, on c, a session with the node, as one who
// connected over AX.25 gets, greeting them.  What arrives on c is to be
// handed to the result's Received, and c going to its Closed.
func (n *Node) Attach(remote string, c Conn) *Attached {
	var p = new(connPeer)
	p.conn = c
	p.remote = remote

	n.established(p, true)

	return &Attached{n: n, p: p}
}

// Received hands the node what the user sent.
func (a *Attached) Received(data []byte) {
	a.n.received(a.p, ax25.PIDNoLayer3, data)
}

// Closed tells the node the user's Conn has gone.
func (a *Attached) Closed() {
	a.n.terminated(a.p, "")
}
