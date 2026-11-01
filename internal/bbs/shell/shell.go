// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package shell is the BBS's user commands: listing, reading, sending and
// killing messages, in the MBL/RLI style FBB and its kin use.  A Machine
// takes the user's lines one at a time and says what to do in answer; it
// does no I/O itself, so a session is a loop around it, and a test is a
// transcript.
package shell

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// Action is something to do in answer to a line; see package action.
type Action = action.Action

// Output is lines to send the user.
type Output = action.Output

// Prompt is the prompt to send the user, when the machine is ready for its
// next command.
type Prompt = action.Prompt

// Disconnect is ending the session.
type Disconnect = action.Disconnect

// mailbox is what the shell needs of the message store.
type mailbox interface {
	Accept(ctx context.Context, m mail.NewMessage, bbsCall string) (mail.Message, error)
	Get(ctx context.Context, n int) (maybe.Maybe[mail.Message], error)
	List(ctx context.Context, q mail.ListQuery) ([]mail.Summary, error)
	SetStatus(ctx context.Context, n int, status mail.Status) error
}

// Config is what a Machine needs to know about the BBS.
type Config struct {
	// Call is the BBS's callsign, which messages sent here get their IDs
	// from.
	Call string

	// Prompt is what the user is prompted with for each command.
	Prompt string

	// HRoute is the rest of the BBS's hierarchical address, such as
	// "#77.GBR.EURO", for the R: line it puts on messages sent here.
	HRoute maybe.Maybe[string]

	// Now is the time, for the R: line.  Left nil, it is time.Now.
	Now func() time.Time
}

// state is what the machine is waiting for.
type state int

const (
	stateCommand state = iota
	stateTitle
	stateBody
)

// draft is a message being sent, as far as it has got.
type draft struct {
	message mail.NewMessage
	body    []string
}

// Machine is one user's session.
type Machine struct {
	peer  transport.Peer
	box   mailbox
	cfg   Config
	state state
	draft *draft
}

// New makes a session for peer.
func New(peer transport.Peer, box mailbox, cfg Config) *Machine {
	var m = new(Machine)

	m.peer = peer
	m.box = box
	m.cfg = cfg

	return m
}

// Start is what to send when the session begins.
func (m *Machine) Start() []Action {
	return []Action{m.prompt()}
}

// Line acts on one line from the user.
func (m *Machine) Line(ctx context.Context, line string) []Action {
	switch m.state {
	case stateTitle:
		return m.title(line)
	case stateBody:
		return m.bodyLine(ctx, line)
	default:
		return m.command(ctx, strings.TrimSpace(line))
	}
}

func (m *Machine) prompt() Prompt {
	return Prompt{Text: m.cfg.Prompt}
}

// say is output followed by the prompt.
func (m *Machine) say(lines ...string) []Action {
	return []Action{Output{Lines: lines}, m.prompt()}
}

// command runs one command.
func (m *Machine) command(ctx context.Context, line string) []Action {
	var name, args, _ = strings.Cut(line, " ")

	args = strings.TrimSpace(args)

	switch strings.ToUpper(name) {
	case "":
		return []Action{m.prompt()}
	case "B", "BYE":
		return []Action{Output{Lines: []string{"73, and thanks for calling."}}, Disconnect{}}
	case "H", "HELP", "?":
		return m.say(helpLines()...)
	case "L":
		return m.list(ctx, args, nil)
	case "LM":
		return m.list(ctx, "", func(q *mail.ListQuery) { q.To = maybe.Just(baseCall(m.peer.Call)) })
	case "LB":
		return m.list(ctx, args, func(q *mail.ListQuery) { q.Types = []mail.Type{mail.Bulletin} })
	case "LP":
		return m.list(ctx, args, func(q *mail.ListQuery) { q.Types = []mail.Type{mail.Personal} })
	case "L<":
		return m.list(ctx, "", func(q *mail.ListQuery) { q.From = maybe.Just(args) })
	case "L>":
		return m.list(ctx, "", func(q *mail.ListQuery) { q.To = maybe.Just(args) })
	case "R":
		return m.read(ctx, args, false)
	case "RH":
		return m.read(ctx, args, true)
	case "S", "SP":
		return m.send(mail.Personal, args)
	case "SB":
		return m.send(mail.Bulletin, args)
	case "ST":
		return m.send(mail.Traffic, args)
	case "K":
		return m.kill(ctx, args)
	default:
		return m.say(fmt.Sprintf("Unknown command %q.  Type H for help.", name))
	}
}

// helpLines is what H says.
func helpLines() []string {
	return []string{
		"B          Bye: disconnect.",
		"H or ?     This help.",
		"L          List messages; L n lists from number n up, L n-m from n to m.",
		"LM         List messages to you.",
		"LB, LP     List bulletins, or personal messages.",
		"L< call    List messages from call.  L> call: to call.",
		"R n        Read message n.  R n m ... reads several; RH n shows headers.",
		"S call     Send a personal message (SP too); SB area for a bulletin,",
		"           ST for NTS traffic.  Add @bbs for one elsewhere, $bid for an ID.",
		"K n        Kill message n.",
	}
}

// canSee says whether the user may see a message: bulletins are for all,
// personal mail and traffic only for whoever it is from or to - or the sysop.
func (m *Machine) canSee(s mail.Summary) bool {
	return s.Type == mail.Bulletin || m.peer.Authenticated || m.isParty(s)
}

// isParty says whether the user sent or is to receive a message.
func (m *Machine) isParty(s mail.Summary) bool {
	return strings.EqualFold(s.From, m.peer.Call) || strings.EqualFold(s.To.To, baseCall(m.peer.Call))
}

// baseCall is a callsign without its SSID, as messages are addressed.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}

// list lists messages, narrowed by a number range in args and by narrow.
func (m *Machine) list(ctx context.Context, args string, narrow func(q *mail.ListQuery)) []Action {
	var q = new(mail.ListQuery)

	if args != "" {
		var lowest, highest, err = parseRange(args)
		if err != nil {
			return m.say(err.Error())
		}

		q.Lowest = maybe.Just(lowest)
		q.Highest = highest
	}

	if narrow != nil {
		narrow(q)
	}

	var summaries, err = m.box.List(ctx, *q)
	if err != nil {
		return m.say("Sorry, the messages can't be listed just now.")
	}

	var lines = []string{"Msg#  TS Size  To     @BBS     From      Date   Title"}

	for _, s := range summaries {
		if m.canSee(s) {
			lines = append(lines, strings.TrimRight(fmt.Sprintf("%-5d %s%s %5d %-6s %-8s %-9s %s %s",
				s.Number, s.Type, s.Status, s.Size, s.To.To, maybe.FromMaybe("", s.To.At), s.From, s.Created.Format("02-Jan"), s.Title), " "))
		}
	}

	if len(lines) == 1 {
		return m.say("No messages.")
	}

	return m.say(lines...)
}

// parseRange reads "n" (n and up) or "n-m".
func parseRange(args string) (int, maybe.Maybe[int], error) {
	var from, to, isRange = strings.Cut(args, "-")

	var lowest, lowErr = strconv.Atoi(strings.TrimSpace(from))
	if lowErr != nil || lowest < 1 {
		return 0, maybe.Nothing[int](), fmt.Errorf("%q is not a message number", from)
	}

	if !isRange {
		return lowest, maybe.Nothing[int](), nil
	}

	var highest, highErr = strconv.Atoi(strings.TrimSpace(to))
	if highErr != nil || highest < lowest {
		return 0, maybe.Nothing[int](), fmt.Errorf("%q is not a message number range", args)
	}

	return lowest, maybe.Just(highest), nil
}

// read shows each message numbered in args, with its R: lines in full if
// headers, or otherwise reduced to the BBSes it came through.
func (m *Machine) read(ctx context.Context, args string, headers bool) []Action {
	var fields = strings.Fields(args)
	if len(fields) == 0 {
		return m.say("Read which message?  R n")
	}

	var lines []string

	for _, field := range fields {
		lines = append(lines, m.readOne(ctx, field, headers)...)
	}

	return m.say(lines...)
}

func (m *Machine) readOne(ctx context.Context, field string, headers bool) []string {
	var n, convErr = strconv.Atoi(field)
	if convErr != nil {
		return []string{fmt.Sprintf("%q is not a message number.", field)}
	}

	var found, err = m.box.Get(ctx, n)
	if err != nil {
		return []string{fmt.Sprintf("Sorry, message %d can't be read just now.", n)}
	}

	var msg, ok = found.Get()
	if !ok || msg.Status == mail.Killed || !m.canSee(msg.Summary) {
		return []string{fmt.Sprintf("No message %d.", n)}
	}

	// Personal mail is read once its addressee has seen it.
	if msg.Type == mail.Personal && msg.Status == mail.New && strings.EqualFold(msg.To.To, baseCall(m.peer.Call)) {
		_ = m.box.SetStatus(ctx, n, mail.Read)
	}

	var lines = []string{
		"Msg #" + strconv.Itoa(msg.Number) + "  " + msg.Type.String() + "  ID " + msg.MID,
		"From:  " + msg.From,
		"To:    " + msg.To.String(),
		"Date:  " + msg.Created.Format("02-Jan-06 15:04Z"),
		"Title: " + msg.Title,
	}

	var rlines, rest = mail.SplitRLines(msg.Body)

	switch {
	case headers:
		lines = append(lines, rlines...)
	case len(rlines) > 0:
		lines = append(lines, "Path:  "+strings.Join(mail.Path(msg.Body), " "))
	}

	lines = append(lines, "")

	var body = strings.TrimSuffix(string(rest), "\r")
	if body != "" || len(rest) > 0 {
		lines = append(lines, strings.Split(body, "\r")...)
	}

	return append(lines, "")
}

// send starts sending a message of typ to the address in args, with an
// optional $BID after it.
func (m *Machine) send(typ mail.Type, args string) []Action {
	var fields = strings.Fields(args)
	if len(fields) == 0 {
		return m.say("Send to whom?  S call, or SB area for a bulletin.")
	}

	var address, addrErr = mail.ParseAddress(fields[0])
	if addrErr != nil {
		return m.say(fmt.Sprintf("%q is not an address.", fields[0]))
	}

	var d = new(draft)

	d.message.Type = typ
	d.message.From = m.peer.Call
	d.message.To = address

	for _, field := range fields[1:] {
		var bid, isBID = strings.CutPrefix(field, "$")
		if !isBID || bid == "" || len(bid) > mail.MaxMIDLen {
			return m.say(fmt.Sprintf("%q is not a $BID of up to %d characters.", field, mail.MaxMIDLen))
		}

		d.message.MID = maybe.Just(strings.ToUpper(bid))
	}

	m.draft = d
	m.state = stateTitle

	return []Action{Output{Lines: []string{"Title:"}}}
}

// title takes a message's title.
func (m *Machine) title(line string) []Action {
	var title = strings.TrimSpace(line)

	if len(title) > mail.MaxTitleLen {
		return []Action{Output{Lines: []string{fmt.Sprintf("Titles are at most %d characters.  Title:", mail.MaxTitleLen)}}}
	}

	m.draft.message.Title = title
	m.state = stateBody

	return []Action{Output{Lines: []string{"Enter the message.  End it with /EX or Ctrl-Z on a line of its own."}}}
}

// bodyLine takes a line of a message's body, or its end: /EX on a line of
// its own, or a Ctrl-Z, which can also end a line of text.
func (m *Machine) bodyLine(ctx context.Context, line string) []Action {
	var text, hasCtrlZ = strings.CutSuffix(strings.TrimRight(line, " "), "\x1a")

	if !hasCtrlZ && !strings.EqualFold(strings.TrimSpace(line), "/EX") {
		m.draft.body = append(m.draft.body, line)

		return nil
	}

	if hasCtrlZ && text != "" {
		m.draft.body = append(m.draft.body, text)
	}

	var d = m.draft

	m.draft = nil
	m.state = stateCommand

	var body strings.Builder

	for _, l := range d.body {
		body.WriteString(l + "\r")
	}

	d.message.Body = mail.PrependRLine([]byte(body.String()), m.rline())

	var stored, err = m.box.Accept(ctx, d.message, m.cfg.Call)
	if errors.Is(err, mail.ErrDuplicateBID) {
		return m.say("That message ID has been seen before, so the message wasn't saved.")
	}

	if err != nil {
		return m.say("Sorry, the message couldn't be saved: " + err.Error())
	}

	var saved = fmt.Sprintf("Message %d saved, ID %s.", stored.Number, stored.MID)

	if stored.Status == mail.Held {
		return m.say(saved, "It is held for the sysop: this BBS doesn't know where to send it on to.")
	}

	return m.say(saved)
}

// kill kills each message numbered in args that the user may kill: their own,
// to or from them, or any for the sysop.
func (m *Machine) kill(ctx context.Context, args string) []Action {
	var fields = strings.Fields(args)
	if len(fields) == 0 {
		return m.say("Kill which message?  K n")
	}

	var lines []string

	for _, field := range fields {
		lines = append(lines, m.killOne(ctx, field))
	}

	return m.say(lines...)
}

func (m *Machine) killOne(ctx context.Context, field string) string {
	var n, convErr = strconv.Atoi(field)
	if convErr != nil {
		return fmt.Sprintf("%q is not a message number.", field)
	}

	var found, err = m.box.Get(ctx, n)
	if err != nil {
		return fmt.Sprintf("Sorry, message %d can't be killed just now.", n)
	}

	var msg, ok = found.Get()
	if !ok || msg.Status == mail.Killed || !m.canSee(msg.Summary) {
		return fmt.Sprintf("No message %d.", n)
	}

	if !m.peer.Authenticated && !m.isParty(msg.Summary) {
		return fmt.Sprintf("Message %d isn't yours to kill.", n)
	}

	var setErr = m.box.SetStatus(ctx, n, mail.Killed)
	if setErr != nil {
		return fmt.Sprintf("Sorry, message %d can't be killed just now.", n)
	}

	return fmt.Sprintf("Message %d killed.", n)
}

// rline is the R: line for a message taken now.
func (m *Machine) rline() mail.RLine {
	var now = time.Now

	if m.cfg.Now != nil {
		now = m.cfg.Now
	}

	var r = new(mail.RLine)

	r.When = now().UTC()
	r.BBS = baseCall(m.cfg.Call)
	r.HRoute = m.cfg.HRoute

	return *r
}
