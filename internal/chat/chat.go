// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package chat is a packet node's chat server: rooms of users connected to
// the node, reached with the shell's CHAT command, each line a user types going
// to everyone else in their room.
//
// It is a node.Application, so, like the rest of the node, it runs on the
// node's one goroutine and is not safe for concurrent use.
package chat

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/doismellburning/samoyed/internal/node"
)

// DefaultRoom is the room users start in, unless configured otherwise.
const DefaultRoom = "General"

// maxRoomName is the longest a room's name may be.
const maxRoomName = 16

// maxLine is the most of a line kept, as in the node's shell.
const maxLine = 256

// Config is how a Server runs.
type Config struct {
	// DefaultRoom is the room users start in.
	DefaultRoom string

	// RateLines is how many lines a user may send in RateWindow before the
	// rest are turned away; zero for no limit.
	RateLines  int
	RateWindow time.Duration

	// Now tells the time; nil for time.Now.
	Now func() time.Time

	// OnJoin and OnLeave, if set, are told of users joining and leaving
	// rooms.
	OnJoin  func(user string, room string)
	OnLeave func(user string, room string)
}

// DefaultConfig returns a configuration with a room to start in and a limit
// generous enough for anyone typing.
func DefaultConfig() Config {
	return Config{
		DefaultRoom: DefaultRoom,
		RateLines:   10,
		RateWindow:  30 * time.Second,
		Now:         nil,
		OnJoin:      nil,
		OnLeave:     nil,
	}
}

// Server is the chat server: its rooms and who is in them.
type Server struct {
	cfg      Config
	sessions map[*session]bool
}

// New returns a server for cfg.
func New(cfg Config) (*Server, error) {
	if cfg.DefaultRoom == "" {
		cfg.DefaultRoom = DefaultRoom
	}

	if !validRoom(cfg.DefaultRoom) {
		return nil, fmt.Errorf("chat: invalid room name %q", cfg.DefaultRoom)
	}

	if cfg.RateLines < 0 || (cfg.RateLines > 0 && cfg.RateWindow <= 0) {
		return nil, fmt.Errorf("chat: invalid rate limit of %d lines in %v", cfg.RateLines, cfg.RateWindow)
	}

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	var s = new(Server)
	s.cfg = cfg
	s.sessions = make(map[*session]bool)

	return s, nil
}

// Name is the shell command that starts the chat server.
func (s *Server) Name() string { return "CHAT" }

// Description says what it is, for the shell's help.
func (s *Server) Description() string { return "Chat with others on this node" }

// Open puts user into the default room.
func (s *Server) Open(user string, out node.Sender, exit func()) node.AppSession { //nolint:ireturn // node.Application's signature.
	var ss = new(session)
	ss.server = s
	ss.user = user
	ss.out = out
	ss.exit = exit
	s.sessions[ss] = true

	ss.send(fmt.Sprintf("*** Welcome to chat, %s.  Type /help for commands, /quit to leave.", user))
	ss.join(s.cfg.DefaultRoom)

	return ss
}

// Room is a room and who is in it, for showing.
type Room struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
}

// Rooms lists the rooms with anyone in them, by name.
func (s *Server) Rooms() []Room {
	var byName = make(map[string]*Room)

	for ss := range s.sessions {
		var key = strings.ToUpper(ss.room)

		var r, ok = byName[key]
		if !ok {
			r = &Room{Name: ss.room, Users: nil}
			byName[key] = r
		}

		r.Users = append(r.Users, ss.user)
	}

	var rooms = make([]Room, 0, len(byName))

	for _, r := range byName {
		slices.Sort(r.Users)
		r.Users = slices.Compact(r.Users)
		rooms = append(rooms, *r)
	}

	slices.SortFunc(rooms, func(a, b Room) int { return strings.Compare(strings.ToUpper(a.Name), strings.ToUpper(b.Name)) })

	return rooms
}

// Announce sends text to everyone in every room, from the node's sysop.
func (s *Server) Announce(text string) {
	for _, ss := range s.sorted() {
		ss.send("*** Announcement: " + text)
	}
}

// sorted returns the sessions in a fixed order, so that what everyone is sent
// does not depend on map order.
func (s *Server) sorted() []*session {
	var list = make([]*session, 0, len(s.sessions))
	for ss := range s.sessions {
		list = append(list, ss)
	}

	slices.SortFunc(list, func(a, b *session) int {
		if c := strings.Compare(a.user, b.user); c != 0 {
			return c
		}

		return a.joined.Compare(b.joined)
	})

	return list
}

// inRoom returns the sessions in room, other than except.
func (s *Server) inRoom(room string, except *session) []*session {
	var list []*session

	for _, ss := range s.sorted() {
		if ss != except && strings.EqualFold(ss.room, room) {
			list = append(list, ss)
		}
	}

	return list
}

func validRoom(name string) bool {
	if name == "" || len(name) > maxRoomName {
		return false
	}

	for _, r := range name {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_') {
			return false
		}
	}

	return true
}

// session is one user's time in the chat server.
type session struct {
	server *Server
	user   string
	out    node.Sender
	exit   func()
	room   string
	joined time.Time
	line   []byte
	recent []time.Time // When the user's latest lines were sent, for the rate limit.
	warned bool
	gone   bool
}

func (ss *session) send(text string) {
	ss.out.Send([]byte(text + "\r"))
}

// Input takes what the user typed.
func (ss *session) Input(data []byte) {
	for _, c := range data {
		if ss.gone {
			return
		}

		if c == '\r' || c == '\n' {
			var line = strings.TrimSpace(string(ss.line))
			ss.line = ss.line[:0]

			if line != "" {
				ss.handle(line)
			}

			continue
		}

		ss.line = append(ss.line, c)
		if len(ss.line) >= maxLine {
			ss.Input([]byte{'\r'})
		}
	}
}

// Close says the user has gone from the node altogether.
func (ss *session) Close() {
	ss.leave("disconnected")
}

// allowed applies the rate limit to a line the user sent now.
func (ss *session) allowed() bool {
	var cfg = ss.server.cfg
	if cfg.RateLines == 0 {
		return true
	}

	var now = cfg.Now()

	ss.recent = slices.DeleteFunc(ss.recent, func(t time.Time) bool { return now.Sub(t) >= cfg.RateWindow })

	if len(ss.recent) >= cfg.RateLines {
		if !ss.warned {
			ss.warned = true
			ss.send("*** Slow down: that line was not sent.")
		}

		return false
	}

	ss.warned = false
	ss.recent = append(ss.recent, now)

	return true
}

func (ss *session) handle(line string) {
	if !strings.HasPrefix(line, "/") {
		if !ss.allowed() {
			return
		}

		for _, other := range ss.server.inRoom(ss.room, ss) {
			other.send(fmt.Sprintf("<%s> %s", ss.user, line))
		}

		return
	}

	var verb, rest, _ = strings.Cut(line[1:], " ")
	rest = strings.TrimSpace(rest)

	switch strings.ToLower(verb) {
	case "help", "h", "?":
		ss.send("*** Commands: /join <room>, /leave, /who [room], /rooms, /msg <user> <text>, /quit")
	case "join", "j":
		ss.cmdJoin(rest)
	case "leave":
		ss.cmdJoin(ss.server.cfg.DefaultRoom)
	case "who", "w":
		ss.cmdWho(rest)
	case "rooms", "r":
		ss.cmdRooms()
	case "msg", "m":
		ss.cmdMsg(rest)
	case "quit", "q", "bye":
		ss.leave("left chat")

		if ss.exit != nil {
			ss.exit()
		}
	default:
		ss.send("*** Unknown command.  Type /help for commands.")
	}
}

func (ss *session) cmdJoin(room string) {
	switch {
	case room == "":
		ss.send("*** Join which room?")
	case !validRoom(room):
		ss.send(fmt.Sprintf("*** A room's name is letters, digits, - and _, up to %d of them.", maxRoomName))
	case strings.EqualFold(room, ss.room):
		ss.send("*** You are already in " + ss.room + ".")
	default:
		ss.part("moved to " + room)
		ss.join(room)
	}
}

func (ss *session) cmdWho(room string) {
	if room == "" {
		room = ss.room
	}

	var users []string
	for _, other := range ss.server.inRoom(room, nil) {
		users = append(users, other.user)
	}

	slices.Sort(users)
	users = slices.Compact(users)

	if len(users) == 0 {
		ss.send("*** Nobody is in " + room + ".")

		return
	}

	ss.send(fmt.Sprintf("*** In %s: %s", room, strings.Join(users, ", ")))
}

func (ss *session) cmdRooms() {
	var parts []string
	for _, r := range ss.server.Rooms() {
		parts = append(parts, fmt.Sprintf("%s (%d)", r.Name, len(r.Users)))
	}

	ss.send("*** Rooms: " + strings.Join(parts, ", "))
}

func (ss *session) cmdMsg(rest string) {
	var to, text, _ = strings.Cut(rest, " ")
	text = strings.TrimSpace(text)

	if to == "" || text == "" {
		ss.send("*** Usage: /msg <user> <text>")

		return
	}

	if !ss.allowed() {
		return
	}

	var sent = false

	for _, other := range ss.server.sorted() {
		if other != ss && strings.EqualFold(other.user, to) {
			other.send(fmt.Sprintf("*%s* %s", ss.user, text))

			sent = true
		}
	}

	if !sent {
		ss.send("*** " + strings.ToUpper(to) + " is not in chat.")
	}
}

// join puts the user into room, telling everyone there.
func (ss *session) join(room string) {
	// A room keeps the name its first user gave it.
	for _, other := range ss.server.inRoom(room, ss) {
		room = other.room

		break
	}

	ss.room = room
	ss.joined = ss.server.cfg.Now()

	for _, other := range ss.server.inRoom(room, ss) {
		other.send(fmt.Sprintf("*** %s has joined %s.", ss.user, room))
	}

	ss.send("*** You are in " + room + ".")
	ss.cmdWho("")

	if ss.server.cfg.OnJoin != nil {
		ss.server.cfg.OnJoin(ss.user, room)
	}
}

// part takes the user out of their room, telling everyone there why.
func (ss *session) part(why string) {
	for _, other := range ss.server.inRoom(ss.room, ss) {
		other.send(fmt.Sprintf("*** %s has %s.", ss.user, why))
	}

	if ss.server.cfg.OnLeave != nil {
		ss.server.cfg.OnLeave(ss.user, ss.room)
	}
}

// leave takes the user out of chat altogether.
func (ss *session) leave(why string) {
	if ss.gone {
		return
	}

	ss.gone = true
	ss.part(why)
	delete(ss.server.sessions, ss)
}
