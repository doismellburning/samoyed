// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package node is a packet node's user-facing side: the command shell a
// station gets on connecting to the node, by AX.25 or over NET/ROM, from
// which it can list what the node knows, connect onwards, or start one of
// the node's applications.
//
// Nothing here is safe for concurrent use.  The node runs everything - every
// shell, every application, every connection's events - on one goroutine, as
// the NET/ROM Router does, so none of it needs locks; and none of it may
// block.
package node

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/netrom"
)

// Sender is somewhere to send a user text.
type Sender interface {
	Send(data []byte)
}

// Conn is one end of a connection: the user's link to the node, or a link the
// node opened onwards on the user's behalf.
type Conn interface {
	Sender

	// Close disconnects.
	Close()
}

// Port is one of the node's ports, as the PORTS command lists it.
type Port struct {
	Number      int
	Description string
}

// Backend is what a Shell asks the rest of the node for.
type Backend interface {
	Ports() []Port
	Nodes() []netrom.Destination
	Neighbours() []netrom.Neighbour
	Heard(port int) []HeardStation

	// ConnectNode opens a NET/ROM circuit from user to the node dest, a
	// callsign or an alias, telling events what happens on it.
	ConnectNode(dest string, user string, events Downlink) (Conn, error)

	// ConnectAX25 opens an AX.25 link from user to call on port, by way of
	// the digipeaters via, telling events what happens on it.
	ConnectAX25(port int, call string, via []string, user string, events Downlink) (Conn, error)
}

// Downlink is told what happens on a connection a Shell opened onwards.
type Downlink interface {
	Connected()
	Received(data []byte)
	Closed(err error)
}

// Application is something a user can start from the node's shell, as a
// command of its own.
type Application interface {
	// Name is the command that starts it, such as "CHAT".
	Name() string

	// Description says what it is, for HELP.
	Description() string

	// Open starts a session for user, sending to out, calling exit when the
	// user leaves the application to go back to the node.
	Open(user string, out Sender, exit func()) AppSession
}

// AppSession is one user's time in an application.
type AppSession interface {
	// Input hands over what the user typed.
	Input(data []byte)

	// Close says the user has gone altogether.
	Close()
}

// Config is what every Shell on a node shares.
type Config struct {
	Call  string // The node's callsign.
	Alias string // Its alias.
	Info  string // What the INFO command shows.

	// IdleTimeout disconnects a user who has done nothing for this long; 0
	// for never.
	IdleTimeout time.Duration

	Applications []Application
}

// prompt prefixes what the node itself says, as other node software does,
// so a user several hops away can tell which node is speaking.
func (c Config) prompt() string {
	if c.Alias == "" {
		return c.Call + "} "
	}

	return c.Alias + ":" + c.Call + "} "
}

type shellState int

const (
	stateCommand    shellState = iota // At the node's prompt.
	stateConnecting                   // Waiting for an onward connection to open.
	stateConnected                    // Passing everything to and from an onward connection.
	stateApp                          // In an application.
	stateClosed                       // The user has gone.
)

// maxLine is the most of a line kept: anything longer is taken as a line once
// it gets that far, so a user who never ends a line cannot run the node out of
// memory.
const maxLine = 256

// Shell is one user's session with the node.
type Shell struct {
	cfg  Config
	be   Backend
	user string
	up   Conn

	state      shellState
	line       []byte
	down       Conn
	downName   string
	held       [][]byte // What the user sent while the onward connection opened.
	app        AppSession
	lastActive time.Time
}

// NewShell returns a shell for user, who reached the node over up, starting at
// now.  Nothing is sent until Start.
func NewShell(cfg Config, be Backend, user string, up Conn, now time.Time) *Shell {
	var s = new(Shell)
	s.cfg = cfg
	s.be = be
	s.user = user
	s.up = up
	s.lastActive = now

	return s
}

// User returns whose shell this is.
func (s *Shell) User() string { return s.user }

// Start greets the user.
func (s *Shell) Start() {
	s.say(fmt.Sprintf("Welcome to %s, %s.  Type ? for a list of commands.", s.name(), s.user))
}

func (s *Shell) name() string {
	if s.cfg.Alias == "" {
		return s.cfg.Call
	}

	return s.cfg.Alias + ":" + s.cfg.Call
}

// say sends the user lines from the node, each with its prompt.
func (s *Shell) say(lines ...string) {
	var b strings.Builder

	for i, l := range lines {
		if i == 0 {
			b.WriteString(s.cfg.prompt())
		}

		b.WriteString(l)
		b.WriteString("\r")
	}

	s.up.Send([]byte(b.String()))
}

// Input takes what the user sent.
func (s *Shell) Input(data []byte, now time.Time) {
	s.lastActive = now

	switch s.state {
	case stateConnected:
		s.down.Send(data)
	case stateConnecting:
		s.held = append(s.held, append([]byte(nil), data...))
	case stateApp:
		s.app.Input(data)
	case stateCommand:
		s.commandInput(data)
	case stateClosed:
	}
}

func (s *Shell) commandInput(data []byte) {
	for i, c := range data {
		if c == '\r' || c == '\n' {
			var line = string(s.line)
			s.line = s.line[:0]

			if strings.TrimSpace(line) != "" {
				s.command(line)
			}

			if s.state != stateCommand {
				// Whatever came after the line is for where it took the
				// user, if anywhere.
				var rest = data[i+1:]
				if c == '\r' && len(rest) > 0 && rest[0] == '\n' {
					rest = rest[1:]
				}

				if len(rest) > 0 && s.state != stateClosed {
					s.Input(rest, s.lastActive)
				}

				return
			}

			continue
		}

		s.line = append(s.line, c)
		if len(s.line) >= maxLine {
			s.commandInput([]byte{'\r'})
		}
	}
}

// command runs one command line.
func (s *Shell) command(line string) {
	var words = strings.Fields(line)
	var verb = strings.ToUpper(words[0])
	var args = words[1:]

	switch verb {
	case "?", "H", "HELP":
		s.help()
	case "B", "BYE", "Q", "QUIT":
		s.state = stateClosed
		s.up.Close()
	case "C", "CONNECT":
		s.connect(args)
	case "N", "NODES":
		s.nodes(args)
	case "R", "ROUTES":
		s.routes()
	case "MH":
		s.heard(args)
	case "P", "PORTS":
		s.ports()
	case "I", "INFO":
		s.info()
	default:
		var i = slices.IndexFunc(s.cfg.Applications, func(a Application) bool { return strings.EqualFold(a.Name(), verb) })
		if i < 0 {
			s.say("Invalid command.  Type ? for a list of commands.")

			return
		}

		s.startApp(s.cfg.Applications[i])
	}
}

func (s *Shell) help() {
	var lines = []string{
		"Commands:",
		"  C <node>                  Connect to a node",
		"  C <port> <call> [V digis] Connect to a station on a port",
		"  N [node]                  List nodes, or the routes to one",
		"  R                         List neighbours",
		"  MH [port]                 List stations heard",
		"  P                         List ports",
		"  I                         Information about this node",
		"  B                         Disconnect",
	}

	for _, a := range s.cfg.Applications {
		lines = append(lines, fmt.Sprintf("  %-25s %s", strings.ToUpper(a.Name()), a.Description()))
	}

	s.say(lines...)
}

func (s *Shell) info() {
	if strings.TrimSpace(s.cfg.Info) == "" {
		s.say("No information about this node.")

		return
	}

	var lines = strings.Split(strings.TrimRight(strings.ReplaceAll(s.cfg.Info, "\r\n", "\n"), "\n"), "\n")
	s.say(lines...)
}

func nodeName(d netrom.Destination) string {
	if d.Alias == "" {
		return d.Call
	}

	return d.Alias + ":" + d.Call
}

func (s *Shell) nodes(args []string) {
	if len(args) > 0 {
		s.routesTo(args[0])

		return
	}

	var lines = []string{"Nodes:"}

	var row strings.Builder

	for i, d := range s.be.Nodes() {
		fmt.Fprintf(&row, "%-19s", nodeName(d))

		if i%4 == 3 {
			lines = append(lines, strings.TrimRight(row.String(), " "))
			row.Reset()
		}
	}

	if row.Len() > 0 {
		lines = append(lines, strings.TrimRight(row.String(), " "))
	}

	s.say(lines...)
}

func (s *Shell) routesTo(name string) {
	var i = slices.IndexFunc(s.be.Nodes(), func(d netrom.Destination) bool {
		return strings.EqualFold(d.Call, name) || (d.Alias != "" && strings.EqualFold(d.Alias, name))
	})
	if i < 0 {
		s.say("Not found: " + strings.ToUpper(name))

		return
	}

	var d = s.be.Nodes()[i]

	var lines = []string{"Routes to " + nodeName(d) + ":"}
	for _, r := range d.Routes {
		lines = append(lines, fmt.Sprintf("  quality %3d  obsolescence %d  port %d  via %s", r.Quality, r.Obsolescence, r.Neighbour.Port, r.Neighbour.Call))
	}

	s.say(lines...)
}

func (s *Shell) routes() {
	var lines = []string{"Neighbours:"}

	for _, n := range s.be.Neighbours() {
		var locked = ""
		if n.Locked {
			locked = " (locked)"
		}

		var name = n.Call
		if n.Alias != "" {
			name = n.Alias + ":" + n.Call
		}

		lines = append(lines, fmt.Sprintf("  port %d  %-19s quality %3d%s", n.Port, name, n.Quality, locked))
	}

	s.say(lines...)
}

func (s *Shell) heard(args []string) {
	var port = -1

	if len(args) > 0 {
		var n, err = strconv.Atoi(args[0])
		if err != nil || n < 0 {
			s.say("Bad port number: " + args[0])

			return
		}

		port = n
	}

	var lines = []string{"Heard:"}
	if port >= 0 {
		lines = []string{fmt.Sprintf("Heard on port %d:", port)}
	}

	// The command came in just now, so its time is the time now.
	for _, h := range s.be.Heard(port) {
		lines = append(lines, fmt.Sprintf("  %-9s port %d  %s ago  %d frames", h.Call, h.Port, since(s.lastActive, h.Last), h.Frames))
	}

	s.say(lines...)
}

// since says how long before now then was, to the second.
func since(now time.Time, then time.Time) string {
	var d = max(now.Sub(then), 0).Round(time.Second)

	var days = int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour

	var h = int(d / time.Hour)
	d -= time.Duration(h) * time.Hour

	var m = int(d / time.Minute)
	d -= time.Duration(m) * time.Minute

	return fmt.Sprintf("%d:%02d:%02d:%02d", days, h, m, int(d/time.Second))
}

func (s *Shell) ports() {
	var lines = []string{"Ports:"}
	for _, p := range s.be.Ports() {
		lines = append(lines, fmt.Sprintf("  %2d %s", p.Number, p.Description))
	}

	s.say(lines...)
}

// connect runs the CONNECT command: to a node by NET/ROM, or to a station on a
// port by AX.25.
func (s *Shell) connect(args []string) {
	if len(args) == 0 {
		s.say("Connect to which node, or which port and station?")

		return
	}

	var port, perr = strconv.Atoi(args[0])
	if perr == nil && len(args) >= 2 {
		s.connectAX25(port, args[1:])

		return
	}

	if len(args) != 1 {
		s.say("Connect to which node, or which port and station?")

		return
	}

	var dest = strings.ToUpper(args[0])
	if strings.EqualFold(dest, s.cfg.Call) || strings.EqualFold(dest, s.cfg.Alias) {
		s.say("You are already connected to " + s.name() + ".")

		return
	}

	s.downName = dest
	s.state = stateConnecting

	var down, err = s.be.ConnectNode(dest, s.user, downlinkEvents{s})
	if err != nil {
		s.state = stateCommand
		s.say(failure(dest, err))

		return
	}

	// The connection can open, or fail, before the backend even returns.
	if s.state == stateConnecting || s.state == stateConnected {
		s.down = down
	}
}

func (s *Shell) connectAX25(port int, args []string) {
	var call = strings.ToUpper(args[0])

	var via []string

	if len(args) > 1 {
		var rest = args[1:]
		if strings.EqualFold(rest[0], "V") || strings.EqualFold(rest[0], "VIA") {
			rest = rest[1:]
		}

		for _, d := range rest {
			for part := range strings.SplitSeq(d, ",") {
				if part != "" {
					via = append(via, strings.ToUpper(part))
				}
			}
		}
	}

	s.downName = call
	s.state = stateConnecting

	var down, err = s.be.ConnectAX25(port, call, via, s.user, downlinkEvents{s})
	if err != nil {
		s.state = stateCommand
		s.say(failure(call, err))

		return
	}

	// The connection can open, or fail, before the backend even returns.
	if s.state == stateConnecting || s.state == stateConnected {
		s.down = down
	}
}

func failure(name string, err error) string {
	return fmt.Sprintf("Failure with %s: %v", name, err)
}

// downlinkEvents passes a downlink's events to its Shell.
type downlinkEvents struct{ s *Shell }

func (d downlinkEvents) Connected()           { d.s.downlinkUp() }
func (d downlinkEvents) Received(data []byte) { d.s.downlinkData(data) }
func (d downlinkEvents) Closed(err error)     { d.s.downlinkDown(err) }

func (s *Shell) downlinkUp() {
	if s.state != stateConnecting {
		return
	}

	s.state = stateConnected
	s.say("Connected to " + s.downName)

	for _, h := range s.held {
		s.down.Send(h)
	}

	s.held = nil
}

func (s *Shell) downlinkData(data []byte) {
	if s.state == stateConnected {
		s.up.Send(data)
	}
}

func (s *Shell) downlinkDown(err error) {
	switch s.state {
	case stateConnecting:
		s.state = stateCommand
		s.down = nil
		s.held = nil

		if err == nil {
			err = errors.New("disconnected")
		}

		s.say(failure(s.downName, err))
	case stateConnected:
		s.state = stateCommand
		s.down = nil
		s.say("Reconnected to " + s.name())
	case stateCommand, stateApp, stateClosed:
	}
}

func (s *Shell) startApp(a Application) {
	s.state = stateApp
	s.app = a.Open(s.user, s.up, s.leaveApp)
}

// leaveApp brings the user back from an application to the node.
func (s *Shell) leaveApp() {
	if s.state != stateApp {
		return
	}

	s.state = stateCommand
	s.app = nil
	s.say("Returned to " + s.name())
}

// Closed says the user has gone: whatever they had open onwards is closed too.
func (s *Shell) Closed() {
	var was = s.state
	s.state = stateClosed

	switch was {
	case stateConnecting, stateConnected:
		if s.down != nil {
			s.down.Close()
		}
	case stateApp:
		s.app.Close()
	case stateCommand, stateClosed:
	}
}

// Tick disconnects a user who has been idle too long.
func (s *Shell) Tick(now time.Time) {
	if s.state == stateClosed || s.cfg.IdleTimeout <= 0 || now.Sub(s.lastActive) < s.cfg.IdleTimeout {
		return
	}

	s.say("Disconnecting: idle too long.")
	s.up.Close()
	s.Closed()
}

// Touch notes activity the shell does not see itself - data arriving from an
// onward connection - so a user reading a long transfer is not idle.
func (s *Shell) Touch(now time.Time) {
	s.lastActive = now
}
