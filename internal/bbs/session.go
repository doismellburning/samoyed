// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/node"
)

// maxUserLine is the longest line a user's session takes.
const maxUserLine = 256

// maxCompose is the most a user may write in one message.
const maxCompose = 64 * 1024

// listLength is how many messages L lists.
const listLength = 20

// userIdle is how long a user connected straight to the BBS may do nothing.
// One who came through the node's shell is the shell's to time out.
const userIdle = 15 * time.Minute

// Name is the shell command that starts the BBS.
func (b *BBS) Name() string { return "BBS" }

// Description says what it is, for the shell's help.
func (b *BBS) Description() string { return "Read and send mail and bulletins" }

// Open starts a session for user, who came from the node's shell.
func (b *BBS) Open(user string, out node.Sender, exit func()) node.AppSession { //nolint:ireturn // node.Application's signature.
	return b.open(user, out, exit, false)
}

// OpenDirect starts a session for user, who connected straight to the BBS
// over conn.
func (b *BBS) OpenDirect(user string, conn node.Conn) node.AppSession { //nolint:ireturn // As Open.
	return b.open(user, conn, conn.Close, true)
}

func (b *BBS) open(user string, out node.Sender, exit func(), direct bool) *userSession {
	var s = new(userSession)
	s.b = b
	s.user = strings.ToUpper(user)
	s.out = out
	s.exit = exit
	s.direct = direct
	s.lastActive = b.now()
	b.sessions[s] = true

	// The SID first, so a partner BBS knows what it has reached.
	s.send(b.sid().String())

	var unread = 0

	for _, m := range b.store.All() {
		if m.personal() && sameStation(m.To, s.user) && !m.Read {
			unread++
		}
	}

	s.send(fmt.Sprintf("Hello %s, this is the %s BBS.  You have %d unread message(s).  Type H for help.", s.user, baseCall(b.cfg.Call), unread))
	s.prompt()

	return s
}

// userSession is one station's session with the BBS: a user, or a partner BBS
// come to forward.
type userSession struct {
	b      *BBS
	user   string
	out    node.Sender
	exit   func()
	direct bool

	line       []byte
	compose    *composing
	fwd        *forwarder
	lastActive time.Time
	gone       bool
	commands   int
}

// composing is a message being written.
type composing struct {
	msg      Message
	gotTitle bool
	body     strings.Builder
}

func (s *userSession) send(lines ...string) {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\r")
	}

	s.out.Send([]byte(b.String()))
}

func (s *userSession) prompt() {
	s.send(fmt.Sprintf("de %s>", baseCall(s.b.cfg.Call)))
}

// Input takes what the station sent.
func (s *userSession) Input(data []byte) {
	s.lastActive = s.b.now()

	if s.fwd != nil {
		s.fwd.Input(data)

		return
	}

	for i, c := range data {
		if s.gone {
			return
		}

		if c == '\r' || c == '\n' {
			if c == '\n' && len(s.line) == 0 {
				continue
			}

			var line = string(s.line)
			s.line = s.line[:0]
			s.handle(line)

			if s.fwd != nil {
				// Whatever followed the SID is the forwarding's.
				s.fwd.Input(data[i+1:])

				return
			}

			continue
		}

		s.line = append(s.line, c)
		if len(s.line) >= maxUserLine {
			s.Input([]byte{'\r'})
		}
	}
}

// Close says the station has gone.
func (s *userSession) Close() {
	s.gone = true
	delete(s.b.sessions, s)

	if s.fwd != nil {
		s.fwd.Close()
	}
}

func (s *userSession) tick(now time.Time) {
	if s.direct && s.fwd == nil && !s.gone && now.Sub(s.lastActive) >= userIdle {
		s.send("Disconnecting: idle too long.")
		s.leave()
	}
}

// leave ends the session: back to the node's shell, or off the air for a
// station connected straight to the BBS.
func (s *userSession) leave() {
	s.gone = true
	delete(s.b.sessions, s)
	s.exit()
}

func (s *userSession) handle(line string) {
	if s.compose != nil {
		s.composeLine(line)

		return
	}

	line = strings.TrimSpace(line)
	if line == "" {
		s.prompt()

		return
	}

	s.commands++

	// A partner BBS answers our SID with its own, before any command.
	if _, ok := ParseSID(line); ok && s.commands == 1 {
		s.startForwarding(line)

		return
	}

	var words = strings.Fields(line)
	var verb = strings.ToUpper(words[0])
	var args = words[1:]

	switch verb {
	case "H", "?", "HELP":
		s.send(
			"Commands:",
			"  L [n]         List messages, or those from number n",
			"  LM            List your mail",
			"  LB            List bulletins",
			"  R n           Read message n",
			"  S call[@bbs]  Send a personal message",
			"  SB to@dist    Send a bulletin",
			"  K n           Kill a message of yours",
			"  I             About this BBS",
			"  B             Leave",
		)
	case "L":
		s.list(args, func(*Message) bool { return true })
	case "LM":
		s.list(args, func(m *Message) bool { return m.personal() && sameStation(m.To, s.user) })
	case "LB":
		s.list(args, func(m *Message) bool { return !m.personal() })
	case "R":
		s.read(args)
	case "S", "SP":
		s.startCompose(args, TypePersonal)

		return
	case "SB":
		s.startCompose(args, TypeBulletin)

		return
	case "K":
		s.kill(args)
	case "I":
		s.info()
	case "B", "BYE", "Q", "QUIT":
		s.leave()

		return
	default:
		s.send("Unknown command.  Type H for help.")
	}

	s.prompt()
}

func (s *userSession) startForwarding(sidLine string) {
	var p = s.b.partner(s.user)
	if p == nil {
		s.send("*** " + s.user + " is not a forwarding partner of this BBS")
		s.leave()

		return
	}

	var st = s.b.partners[p.Call]
	if st.session != nil {
		s.send("*** Already forwarding with " + p.Call)
		s.leave()

		return
	}

	var f = newForwarder(s.b, p, s.out, s.leave, false)
	st.session = f
	s.fwd = f
	f.startAnswering(sidLine)
}

func (s *userSession) list(args []string, want func(*Message) bool) {
	var from = 0

	if len(args) > 0 {
		var n, err = strconv.Atoi(args[0])
		if err != nil {
			s.send("Not a message number: " + args[0])

			return
		}

		from = n
	}

	var visible []*Message

	for _, m := range s.b.store.All() {
		if m.Number >= from && m.visibleTo(s.user) && want(m) {
			visible = append(visible, m)
		}
	}

	if from == 0 && len(visible) > listLength {
		visible = visible[len(visible)-listLength:]
	}

	if len(visible) == 0 {
		s.send("No messages.")

		return
	}

	var lines = []string{"Msg#  TS   Size To      @BBS    From    Date   Subject"}

	// Newest first, as BBSes list.
	for _, v := range slices.Backward(visible) {
		var m = v

		var status = "N"
		switch {
		case m.Read:
			status = "Y"
		case len(m.Forwarded) > 0 && len(m.Forward) == 0:
			status = "F"
		}

		lines = append(lines, fmt.Sprintf("%-5d %s%s %6d %-7s %-7s %-7s %s %s",
			m.Number, m.Type, status, m.Size(), m.To, atBBS(m.At), m.From, m.Date.UTC().Format("02-Jan"), m.Subject))
	}

	s.send(lines...)
}

func (s *userSession) read(args []string) {
	if len(args) == 0 {
		s.send("Read which message?")

		return
	}

	for _, a := range args {
		var n, err = strconv.Atoi(a)

		var m, ok = s.b.store.Get(n)
		if err != nil || !ok || !m.visibleTo(s.user) {
			s.send("No message " + a + ".")

			continue
		}

		var to = m.To
		if m.At != "" {
			to += "@" + m.At
		}

		s.send(
			"From: "+m.From,
			"To: "+to,
			"Type/Status: "+m.Type,
			"Date/Time: "+m.Date.UTC().Format("02-Jan 15:04Z"),
			"Bid: "+m.BID,
			"Title: "+m.Subject,
			"",
		)
		s.out.Send([]byte(strings.ReplaceAll(m.Body, "\n", "\r")))
		s.send(fmt.Sprintf("[End of message #%d from %s]", m.Number, m.From))

		if m.personal() && sameStation(m.To, s.user) && !m.Read {
			m.Read = true
			_ = s.b.store.Update(m)
		}
	}
}

func (s *userSession) kill(args []string) {
	if len(args) == 0 {
		s.send("Kill which message?")

		return
	}

	for _, a := range args {
		var n, err = strconv.Atoi(a)

		var m, ok = s.b.store.Get(n)
		if err != nil || !ok || !s.mayKill(m) {
			s.send("Cannot kill message " + a + ".")

			continue
		}

		var kerr = s.b.store.Delete(n)
		if kerr != nil {
			s.send("Could not kill message " + a + ".")

			continue
		}

		s.send(fmt.Sprintf("Message %d killed.", n))
	}
}

// mayKill says whether the user may kill m: their own, or personal mail for
// them.
func (s *userSession) mayKill(m *Message) bool {
	return sameStation(m.From, s.user) || (m.personal() && sameStation(m.To, s.user))
}

func (s *userSession) info() {
	var personal, bulletins = 0, 0

	for _, m := range s.b.store.All() {
		if m.personal() {
			personal++
		} else {
			bulletins++
		}
	}

	s.send(fmt.Sprintf("%s BBS, %s.  %d personal messages, %d bulletins.", baseCall(s.b.cfg.Call), s.b.cfg.HRoute, personal, bulletins))
}

func (s *userSession) startCompose(args []string, typ string) {
	if len(args) != 1 {
		s.send("Send to whom?  S call[@bbs], or SB to@distribution")
		s.prompt()

		return
	}

	var to, at = parseAddress(args[0])
	if baseCall(to) == "" || strings.ContainsAny(to+at, " ") || len(to) > 6 {
		s.send("Not an address: " + args[0])
		s.prompt()

		return
	}

	var c = new(composing)
	c.msg = Message{
		Number: 0, Type: typ, From: baseCall(s.user), To: to, At: at, BID: "",
		Subject: "", Body: "", Date: time.Time{}, Read: false,
		Origin: "", Forward: nil, Forwarded: nil,
	}
	s.compose = c
	s.send("Subject:")
}

func (s *userSession) composeLine(line string) {
	var c = s.compose

	if !c.gotTitle {
		var subject = strings.TrimSpace(line)
		if subject == "" {
			s.compose = nil
			s.send("Message cancelled.")
			s.prompt()

			return
		}

		c.msg.Subject = subject
		c.gotTitle = true
		s.send("Enter the message, ending with /EX on a line of its own (/ABORT to cancel):")

		return
	}

	var trimmed = strings.TrimSpace(line)

	var text, _, ended = strings.Cut(line, string(rune(chrSUB)))
	if strings.EqualFold(trimmed, "/EX") {
		text, ended = "", true
	}

	if strings.EqualFold(trimmed, "/ABORT") {
		s.compose = nil
		s.send("Message cancelled.")
		s.prompt()

		return
	}

	if text != "" || !ended {
		c.body.WriteString(text)
		c.body.WriteString("\n")
	}

	if c.body.Len() > maxCompose {
		s.compose = nil
		s.send("Message too long: cancelled.")
		s.prompt()

		return
	}

	if !ended {
		return
	}

	s.compose = nil
	c.msg.Body = c.body.String()
	c.msg.Date = s.b.now().UTC()

	var m = c.msg

	var err = s.b.post(&m)
	if err != nil {
		s.send("Could not save the message: " + err.Error())
	} else {
		s.send(fmt.Sprintf("Message %d saved.", m.Number))
	}

	s.prompt()
}
