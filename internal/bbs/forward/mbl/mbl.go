// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mbl is forwarding mail with another BBS in the plain MBL/RLI style,
// one message at a time, as BBSes without FBB's batched protocol do and those
// with it fall back to.  A Machine takes the other BBS's lines one at a time
// and says what to send in answer, like the user shell, so a forwarding
// session is the same loop around it.
//
// Whichever BBS made the connection - the calling one - sends first.  Each
// message is offered with a proposal ("SP Q1TEST @ Q1BBS < Q2TEST $BID"),
// which the other side answers OK or NO; an accepted one follows as its
// title, its body, and /EX.  After each message, and after each NO, the
// receiving side says it is ready for the next: with "F>" if it made the
// connection, or with its prompt if it was called.  A sender with nothing
// more to send hands the turn over with "F>" when it was answered with a
// prompt, and ends the exchange with "*** DONE" when it was answered with
// "F>".
//
// That is the exchange as seen forwarding with LinBPQ 6.0.26.1 both ways;
// FBB and XRouter are still to be checked (#1152).
package mbl

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	// accept is the answer to a proposal for a message wanted.
	accept = "OK"

	// refuse is the answer to a proposal for a message not wanted.
	refuse = "NO"

	// handOver gives the other side its turn to send.
	handOver = "F>"

	// done ends the exchange: the side saying it has nothing more.
	done = "*** DONE"

	// endOfMessage ends a message's body.
	endOfMessage = "/EX"
)

// store is what forwarding needs of the message store.
type store interface {
	// SeenBID says whether a message ID has been seen before.
	SeenBID(ctx context.Context, bid string) (bool, error)

	// AcceptForwarded stores a message sent by the partner from, routing it
	// on but never back to from.
	AcceptForwarded(ctx context.Context, m mail.NewMessage, from string) (mail.Message, error)

	// QueueFor is the numbers of the messages waiting for partner.
	QueueFor(ctx context.Context, partner string) ([]int, error)

	// Get is message n, if there is one.
	Get(ctx context.Context, n int) (maybe.Maybe[mail.Message], error)

	// MarkForwarded and MarkRefused take message n off partner's queue,
	// sent or turned down.
	MarkForwarded(ctx context.Context, n int, partner string) error
	MarkRefused(ctx context.Context, n int, partner string) error
}

// Config is what a Machine needs to know about this BBS.
type Config struct {
	// Call is the BBS's callsign, for the R: line it puts on each message it
	// takes.
	Call string

	// HRoute is the rest of the BBS's hierarchical address, for its R: line.
	HRoute maybe.Maybe[string]

	// Prompt is the BBS's prompt, which tells the other side it is ready.
	Prompt string

	// SID is the BBS's system ID.
	SID sid.SID

	// Now is the time, for R: lines.  Left nil, it is time.Now.
	Now func() time.Time
}

// state is what the machine is waiting for.
type state int

const (
	// Receiving: a proposal, F> or *** DONE.
	stateProposal state = iota
	stateTitle
	stateBody

	// Sending: the answer to a proposal, then the other side saying it is
	// ready after a message, or after refusing one.
	stateAnswer
	statePrompt
	stateRefused

	// Calling, before the exchange begins: the other side's SID and
	// prompt, then its prompt again once it has ours.
	stateGreeting
	stateSIDTaken
)

// Machine is one forwarding session with a partner.
type Machine struct {
	partner string
	calling bool
	mode    sid.Mode
	store   store
	cfg     Config
	state   state

	// heardSID is whether the calling side has had the other's SID.
	heardSID bool

	// turns is how many turns at sending this side has had.
	turns int

	// incoming is the message being received, as far as it has got.
	incoming *mail.NewMessage
	body     []string

	// queue is what is still to be sent this turn, and sending the message
	// being sent.
	queue   []int
	sending int
}

// NewAnswering is the session for a partner that has connected to this BBS
// and sent its SID: it sends first, and this side answers.
func NewAnswering(partner string, theirs sid.SID, st store, cfg Config) *Machine {
	var m = newMachine(partner, st, cfg)

	m.mode = sid.Negotiate(cfg.SID, theirs)
	m.state = stateProposal

	return m
}

// NewCalling is the session for a partner this BBS has connected to: it waits
// for the other side's SID and prompt, sends its own, and then sends first.
func NewCalling(partner string, st store, cfg Config) *Machine {
	var m = newMachine(partner, st, cfg)

	m.calling = true
	m.state = stateGreeting

	return m
}

func newMachine(partner string, st store, cfg Config) *Machine {
	var m = new(Machine)

	m.partner = baseCall(partner)
	m.store = st
	m.cfg = cfg

	return m
}

// Start is what to send when the session begins: for the answering side, its
// prompt, taking the partner's SID.
func (m *Machine) Start() []action.Action {
	if m.calling {
		return nil
	}

	return []action.Action{m.prompt()}
}

// Line acts on one line from the partner.
func (m *Machine) Line(ctx context.Context, line string) []action.Action {
	switch m.state {
	case stateGreeting:
		return m.greeting(line)
	case stateSIDTaken:
		if isPrompt(line) {
			return m.startTurn(ctx)
		}

		return nil
	case stateTitle:
		return m.title(line)
	case stateBody:
		return m.bodyLine(ctx, line)
	case stateAnswer:
		return m.answer(ctx, line)
	case statePrompt:
		return m.sent(ctx, line)
	case stateRefused:
		if isReady(line) {
			return m.next(ctx)
		}

		return nil
	default:
		return m.proposal(ctx, strings.TrimSpace(line))
	}
}

func (m *Machine) prompt() action.Prompt {
	return action.Prompt{Text: m.cfg.Prompt}
}

func (m *Machine) now() time.Time {
	if m.cfg.Now != nil {
		return m.cfg.Now()
	}

	return time.Now()
}

// greeting waits, on calling, for the other side's SID and then its prompt,
// and answers with this BBS's SID.  Something that prompts without a SID
// isn't a BBS that forwards.
func (m *Machine) greeting(line string) []action.Action {
	var theirs, sidErr = sid.Parse(strings.TrimSpace(line))
	if sidErr == nil {
		m.mode = sid.Negotiate(m.cfg.SID, theirs)
		m.heardSID = true

		return nil
	}

	if !isPrompt(line) {
		return nil
	}

	if !m.heardSID {
		return []action.Action{action.Disconnect{}}
	}

	m.state = stateSIDTaken

	return []action.Action{action.Output{Lines: []string{m.cfg.SID.String()}}}
}

// proposal acts on a line while receiving: a message offered, the turn handed
// over, or the end of the exchange.  Anything else - the other side's own
// prompt, say - is passed over.
func (m *Machine) proposal(ctx context.Context, line string) []action.Action {
	switch {
	case strings.EqualFold(line, handOver):
		return m.startTurn(ctx)
	case strings.EqualFold(line, done):
		return []action.Action{action.Disconnect{}}
	case line == "" || isPrompt(line) || !strings.EqualFold(line[:1], "S"):
		return nil
	}

	var offered, parseErr = ParseProposal(line)
	if parseErr != nil || offered.Validate() != nil {
		return m.refuse()
	}

	if bid, ok := offered.MID.Get(); ok {
		var seen, seenErr = m.store.SeenBID(ctx, bid)
		if seenErr != nil || seen {
			return m.refuse()
		}
	}

	m.incoming = &offered
	m.body = nil
	m.state = stateTitle

	return []action.Action{action.Output{Lines: []string{accept}}}
}

func (m *Machine) refuse() []action.Action {
	return append([]action.Action{action.Output{Lines: []string{refuse}}}, m.ready()...)
}

// ready is what the receiving side says when it is ready for the next
// message: F> if it made the connection, its prompt if it was called.
func (m *Machine) ready() []action.Action {
	if m.calling {
		return []action.Action{action.Output{Lines: []string{handOver}}}
	}

	return []action.Action{m.prompt()}
}

// title takes an accepted message's title, cut to fit.
func (m *Machine) title(line string) []action.Action {
	var title = strings.TrimSpace(line)

	m.incoming.Title = title[:min(len(title), mail.MaxTitleLen)]
	m.state = stateBody

	return nil
}

// bodyLine takes a line of the message's body, or its end: /EX on a line of
// its own, or a Ctrl-Z, which can also end a line of text.
func (m *Machine) bodyLine(ctx context.Context, line string) []action.Action {
	var text, hasCtrlZ = strings.CutSuffix(strings.TrimRight(line, " "), "\x1a")

	if !hasCtrlZ && !strings.EqualFold(strings.TrimSpace(line), endOfMessage) {
		m.body = append(m.body, line)

		return nil
	}

	if hasCtrlZ && text != "" {
		m.body = append(m.body, text)
	}

	var body strings.Builder

	for _, l := range m.body {
		body.WriteString(l + "\r")
	}

	var msg = *m.incoming

	msg.Body = mail.Stamp([]byte(body.String()), m.cfg.Call, m.cfg.HRoute, m.now())

	m.incoming = nil
	m.body = nil
	m.state = stateProposal

	var _, err = m.store.AcceptForwarded(ctx, msg, m.partner)
	if err != nil && !errors.Is(err, mail.ErrDuplicateBID) {
		// Saying so would tell the other side the message was taken, so it
		// would never be sent again.  Hanging up leaves it queued there.
		return []action.Action{action.Output{Lines: []string{"*** Error: the message couldn't be stored"}}, action.Disconnect{}}
	}

	// A message whose ID turned up from elsewhere meanwhile has been taken
	// already, which is as good as taking it.
	return m.ready()
}

// startTurn begins this side's turn at sending, with what is queued for the
// partner.
func (m *Machine) startTurn(ctx context.Context) []action.Action {
	var queue, err = m.store.QueueFor(ctx, m.partner)
	if err != nil {
		return []action.Action{action.Output{Lines: []string{"*** Error: the forwarding queue couldn't be read"}}, action.Disconnect{}}
	}

	m.turns++
	m.queue = queue

	return m.next(ctx)
}

// next offers the next message queued, or ends the turn.
func (m *Machine) next(ctx context.Context) []action.Action {
	for len(m.queue) > 0 {
		var n = m.queue[0]

		m.queue = m.queue[1:]

		var found, err = m.store.Get(ctx, n)
		if err != nil {
			return []action.Action{action.Output{Lines: []string{"*** Error: a message couldn't be read"}}, action.Disconnect{}}
		}

		var msg, ok = found.Get()
		if !ok || msg.Status == mail.Killed {
			continue
		}

		m.sending = n
		m.state = stateAnswer

		return []action.Action{action.Output{Lines: []string{ProposalFor(msg, m.mode.BIDs)}}}
	}

	return m.endTurn()
}

// endTurn hands the turn over, or, if the other side has had one, ends the
// exchange.  The calling side sends first, so has the first turn; the
// answering side's turn, or the calling side's second, is the last.
// LinBPQ, calling in, hands over with F> once this BBS prompts it, and takes
// the *** DONE that ends this BBS's turn.
func (m *Machine) endTurn() []action.Action {
	if m.calling && m.turns == 1 {
		m.state = stateProposal

		return []action.Action{action.Output{Lines: []string{handOver}}}
	}

	return []action.Action{action.Output{Lines: []string{done}}, action.Disconnect{}}
}

// answer acts on the other side's answer to a proposal.  Anything that is
// neither OK nor NO is passed over.
func (m *Machine) answer(ctx context.Context, line string) []action.Action {
	var word = strings.ToUpper(strings.TrimSpace(line))

	switch {
	case strings.HasPrefix(word, accept):
		return m.send(ctx)
	case strings.HasPrefix(word, refuse):
		var err = m.store.MarkRefused(ctx, m.sending, m.partner)
		if err != nil {
			return []action.Action{action.Output{Lines: []string{"*** Error: the forwarding queue couldn't be updated"}}, action.Disconnect{}}
		}

		// The other side says it is ready again before the next is offered.
		m.state = stateRefused

		return nil
	default:
		return nil
	}
}

// send sends the accepted message: its title, its body, and /EX.
func (m *Machine) send(ctx context.Context) []action.Action {
	var found, err = m.store.Get(ctx, m.sending)

	var msg, ok = found.Get()
	if err != nil || !ok {
		return []action.Action{action.Output{Lines: []string{"*** Error: a message couldn't be read"}}, action.Disconnect{}}
	}

	var lines = []string{msg.Title}

	var body = strings.TrimSuffix(string(msg.Body), "\r")
	if body != "" {
		lines = append(lines, strings.Split(body, "\r")...)
	}

	lines = append(lines, endOfMessage)

	m.state = statePrompt

	return []action.Action{action.Output{Lines: lines}}
}

// sent waits for the other side to say it is ready again, which says it has
// taken the message.
func (m *Machine) sent(ctx context.Context, line string) []action.Action {
	if !isReady(line) {
		return nil
	}

	var err = m.store.MarkForwarded(ctx, m.sending, m.partner)
	if err != nil {
		return []action.Action{action.Output{Lines: []string{"*** Error: the forwarding queue couldn't be updated"}}, action.Disconnect{}}
	}

	return m.next(ctx)
}

// isReady says whether line is the other side saying it is ready for the
// next message: its prompt, or F>.
func isReady(line string) bool {
	return isPrompt(line) || strings.EqualFold(strings.TrimSpace(line), handOver)
}

// isPrompt says whether line is a BBS's prompt, which ends in >.
func isPrompt(line string) bool {
	var text = strings.TrimSpace(line)

	return strings.HasSuffix(text, ">") && !strings.EqualFold(text, handOver)
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}
