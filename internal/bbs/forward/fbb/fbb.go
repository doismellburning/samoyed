// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fbb is forwarding mail with another BBS in FBB's own protocol,
// uncompressed: what two BBSes do when both have F in their SIDs.  It moves
// mail in blocks, a proposal of up to five messages answered all at once, so
// that a slow link turns round as seldom as it can.  Like the MBL machine, a
// Machine takes the other side's lines one at a time and says what to send.
//
// Whichever BBS made the connection sends its SID and its first proposal as
// soon as it has the other's prompt.  A proposal is up to five lines
//
//	FB <type> <from> <@bbs> <to> <bid> <size>
//
// ended by "F> HH", HH being a checksum.  The other side answers with an FS
// line, a sign for each message - + wanted, - not, = later - and the
// messages wanted follow in order, each its title, its text, and a Ctrl-Z.
// Then the other side takes its turn: its own proposal, or FF if it has
// nothing, which also says it had everything sent to it.  FQ, from a side
// with nothing to send after FF, ends the exchange.
//
// That is the protocol as F6FBB's documentation describes it ("FBB forward
// protocol", appendix 9 of FBB's manual).  LinBPQ 6.0.26.1 turns it away
// uncompressed, so it is only for partners running FBB itself, for now.
package fbb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
)

const (
	// MaxProposal is the most messages one proposal offers.
	MaxProposal = 5

	// MaxBlockBytes is roughly the most text one proposal offers: it stops
	// short of going over, though it always offers at least one message.
	MaxBlockBytes = 10000

	endProposal = "F>"
	noMore      = "FF"
	quit        = "FQ"
	ctrlZ       = "\x1a"
)

// store is what forwarding needs of the message store.
type store interface {
	SeenBID(ctx context.Context, bid string) (bool, error)
	AcceptForwarded(ctx context.Context, m mail.NewMessage, from string) (mail.Message, error)
	QueueFor(ctx context.Context, partner string) ([]int, error)
	Get(ctx context.Context, n int) (maybe.Maybe[mail.Message], error)
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

	// SID is the BBS's system ID.
	SID sid.SID

	// Now is the time, for R: lines.  Left nil, it is time.Now.
	Now func() time.Time
}

// state is what the machine is waiting for.
type state int

const (
	// The other side's proposal, FF or FQ.
	stateProposal state = iota

	// The answer to this side's proposal.
	stateAnswer

	// A message the other side is sending: its title, then its text.
	stateTitle
	stateBody
)

// Machine is one forwarding session with a partner.
type Machine struct {
	partner string
	calling bool
	store   store
	cfg     Config
	state   state

	// offers and offerLines are the other side's proposal so far.
	offers     []Offer
	offerLines []string

	// wanted is what this side accepted of it, still to come, and body the
	// text of the first of them so far.
	wanted []mail.NewMessage
	body   []string

	// proposed is what this side offered in its last proposal, sent what
	// it sent of that and awaits the other side's turn to acknowledge, and
	// deferred what the other side wants later, not this session.
	proposed []int
	sent     []int
	deferred []int
}

// NewAnswering is the session for a partner that has connected to this BBS
// and sent its SID: it sends its first proposal straight after.
func NewAnswering(partner string, st store, cfg Config) *Machine {
	return newMachine(partner, false, st, cfg)
}

// NewCalling is the session for a partner this BBS has connected to, once the
// partner has greeted it with its SID and prompt (see forward.Calling).
func NewCalling(partner string, st store, cfg Config) *Machine {
	return newMachine(partner, true, st, cfg)
}

func newMachine(partner string, calling bool, st store, cfg Config) *Machine {
	var m = new(Machine)

	m.partner = baseCall(partner)
	m.calling = calling
	m.store = st
	m.cfg = cfg
	m.state = stateProposal

	return m
}

// Start is what to send when the session begins: for the calling side, its
// SID and first proposal.  The answering side has greeted the partner
// already, and waits for its proposal.
func (m *Machine) Start(ctx context.Context) []action.Action {
	if !m.calling {
		return nil
	}

	var turn = m.turn(ctx)

	return append([]action.Action{output(m.cfg.SID.String())}, turn...)
}

// Line acts on one line from the partner.
func (m *Machine) Line(ctx context.Context, line string) []action.Action {
	switch m.state {
	case stateAnswer:
		return m.answer(ctx, strings.TrimSpace(line))
	case stateTitle:
		return m.title(line)
	case stateBody:
		return m.bodyLine(ctx, line)
	default:
		return m.proposal(ctx, strings.TrimSpace(line))
	}
}

func output(lines ...string) action.Output {
	return action.Output{Lines: lines}
}

// fail tells the partner what went wrong and hangs up.  Whatever it hadn't
// acknowledged stays queued, to be sent again next time.
func fail(format string, args ...any) []action.Action {
	return []action.Action{output("*** " + fmt.Sprintf(format, args...)), action.Disconnect{}}
}

func (m *Machine) now() time.Time {
	if m.cfg.Now != nil {
		return m.cfg.Now()
	}

	return time.Now()
}

// proposal acts on a line while waiting for the other side's turn: a line
// of its proposal, its end, FF or FQ.  Anything else - a prompt, a comment -
// is passed over.
func (m *Machine) proposal(ctx context.Context, line string) []action.Action {
	var word, _, _ = strings.Cut(strings.ToUpper(line), " ")

	switch word {
	case "FB":
		var offer, err = ParseFB(line)
		if err != nil {
			return fail("Error: %v", err)
		}

		if len(m.offers) == MaxProposal {
			return fail("Error: more than %d messages proposed", MaxProposal)
		}

		m.offers = append(m.offers, offer)
		m.offerLines = append(m.offerLines, line)

		return nil
	case endProposal:
		return m.endOfProposal(ctx, line)
	case noMore:
		var ackErr = m.acknowledged(ctx)
		if ackErr != nil {
			return fail("Error: %v", ackErr)
		}

		return m.turnAfterFF(ctx)
	case quit:
		var ackErr = m.acknowledged(ctx)
		if ackErr != nil {
			return fail("Error: %v", ackErr)
		}

		return []action.Action{action.Disconnect{}}
	default:
		return nil
	}
}

// endOfProposal answers the other side's proposal, once its F> has come.
func (m *Machine) endOfProposal(ctx context.Context, line string) []action.Action {
	if len(m.offers) == 0 {
		return fail("Error: an empty proposal")
	}

	var _, sum, hasSum = strings.Cut(strings.TrimSpace(line), " ")
	if hasSum {
		var want, err = strconv.ParseUint(strings.TrimSpace(sum), 16, 8)
		if err != nil || byte(want&0xff) != Checksum(m.offerLines) {
			return fail("Checksum error")
		}
	}

	// The other side's turn says it had everything this side sent.
	var ackErr = m.acknowledged(ctx)
	if ackErr != nil {
		return fail("Error: %v", ackErr)
	}

	var signs strings.Builder

	m.wanted = nil

	for i, offer := range m.offers {
		var answer, err = m.decide(ctx, offer, m.offers[:i])
		if err != nil {
			return fail("Error: %v", err)
		}

		signs.WriteByte(byte(answer))

		if answer == Accept {
			m.wanted = append(m.wanted, offer.Message)
		}
	}

	m.offers = nil
	m.offerLines = nil

	var actions = []action.Action{output("FS " + signs.String())}

	if len(m.wanted) > 0 {
		m.state = stateTitle
		m.body = nil

		return actions
	}

	return append(actions, m.turn(ctx)...)
}

// decide is whether this side wants offer: not if its ID has been seen, or is
// offered twice in one proposal, or it isn't a message that can be stored.
func (m *Machine) decide(ctx context.Context, offer Offer, earlier []Offer) (Answer, error) {
	var storable = offer.Message.Validate() == nil
	if !storable {
		return Refuse, nil
	}

	var bid = maybe.FromMaybe("", offer.Message.MID)

	var again = slices.ContainsFunc(earlier, func(o Offer) bool { return maybe.FromMaybe("", o.Message.MID) == bid })
	if again {
		return Refuse, nil
	}

	var seen, err = m.store.SeenBID(ctx, bid)
	if err != nil {
		return Refuse, err
	}

	if seen {
		return Refuse, nil
	}

	return Accept, nil
}

// title takes the title of the message coming, cut to fit.
func (m *Machine) title(line string) []action.Action {
	var title = strings.TrimSpace(line)

	m.wanted[0].Title = title[:min(len(title), mail.MaxTitleLen)]
	m.state = stateBody

	return nil
}

// bodyLine takes a line of the message's text, or its end: a Ctrl-Z, which
// can also end a line of text.
func (m *Machine) bodyLine(ctx context.Context, line string) []action.Action {
	var text, hasCtrlZ = strings.CutSuffix(strings.TrimRight(line, " "), ctrlZ)

	if !hasCtrlZ {
		m.body = append(m.body, line)

		return nil
	}

	if text != "" {
		m.body = append(m.body, text)
	}

	var body strings.Builder

	for _, l := range m.body {
		body.WriteString(l + "\r")
	}

	var msg = m.wanted[0]

	msg.Body = mail.Stamp([]byte(body.String()), m.cfg.Call, m.cfg.HRoute, m.now())

	m.wanted = m.wanted[1:]
	m.body = nil

	var _, err = m.store.AcceptForwarded(ctx, msg, m.partner)
	if err != nil && !errors.Is(err, mail.ErrDuplicateBID) {
		// Taking the turn would tell the other side the message had been
		// taken; hanging up leaves it queued there.
		return fail("Error: a message couldn't be stored")
	}

	if len(m.wanted) > 0 {
		m.state = stateTitle

		return nil
	}

	return m.turn(ctx)
}

// turn is this side's turn, having had everything the other side sent: its
// proposal, or FF if it has nothing.
func (m *Machine) turn(ctx context.Context) []action.Action {
	var lines, err = m.propose(ctx)
	if err != nil {
		return fail("Error: %v", err)
	}

	m.state = stateProposal

	if len(lines) == 0 {
		return []action.Action{output(noMore)}
	}

	m.state = stateAnswer

	return []action.Action{output(append(lines, fmt.Sprintf("%s %02X", endProposal, Checksum(lines)))...)}
}

// turnAfterFF is this side's turn after the other side has said FF: its
// proposal, or, with nothing to send either, FQ, which ends the exchange.
func (m *Machine) turnAfterFF(ctx context.Context) []action.Action {
	var lines, err = m.propose(ctx)
	if err != nil {
		return fail("Error: %v", err)
	}

	if len(lines) == 0 {
		return []action.Action{output(quit), action.Disconnect{}}
	}

	m.state = stateAnswer

	return []action.Action{output(append(lines, fmt.Sprintf("%s %02X", endProposal, Checksum(lines)))...)}
}

// propose picks what to offer next from the partner's queue - up to
// MaxProposal messages, within MaxBlockBytes - and returns their FB lines.
func (m *Machine) propose(ctx context.Context) ([]string, error) {
	var queue, err = m.store.QueueFor(ctx, m.partner)
	if err != nil {
		return nil, fmt.Errorf("the forwarding queue couldn't be read: %w", err)
	}

	m.proposed = nil

	var lines []string
	var total = 0

	for _, n := range queue {
		if len(m.proposed) == MaxProposal || slices.Contains(m.deferred, n) {
			continue
		}

		var found, getErr = m.store.Get(ctx, n)
		if getErr != nil {
			return nil, fmt.Errorf("message %d couldn't be read: %w", n, getErr)
		}

		var msg, ok = found.Get()
		if !ok || msg.Status == mail.Killed {
			continue
		}

		if len(m.proposed) > 0 && total+len(msg.Body) > MaxBlockBytes {
			break
		}

		total += len(msg.Body)
		m.proposed = append(m.proposed, n)
		lines = append(lines, FBFor(msg, m.partner))
	}

	return lines, nil
}

// answer acts on the other side's FS: sends what it wants, and marks what it
// doesn't.  Anything before the FS - a prompt, a comment - is passed over.
func (m *Machine) answer(ctx context.Context, line string) []action.Action {
	if !strings.HasPrefix(strings.ToUpper(line), "FS") {
		return nil
	}

	var answers, err = ParseFS(line, len(m.proposed))
	if err != nil {
		return fail("Error: %v", err)
	}

	var lines []string

	m.sent = nil

	for i, a := range answers {
		var n = m.proposed[i]

		switch a {
		case Accept:
			var text, getErr = m.messageText(ctx, n)
			if getErr != nil {
				return fail("Error: %v", getErr)
			}

			lines = append(lines, text...)
			m.sent = append(m.sent, n)
		case Defer:
			m.deferred = append(m.deferred, n)
		case Refuse, Reject, Hold, FormatError:
			var markErr = m.store.MarkRefused(ctx, n, m.partner)
			if markErr != nil {
				return fail("Error: the forwarding queue couldn't be updated")
			}
		}
	}

	m.proposed = nil
	m.state = stateProposal

	if len(lines) == 0 {
		return nil
	}

	return []action.Action{output(lines...)}
}

// messageText is message n as sent: its title, its text, and a Ctrl-Z.
func (m *Machine) messageText(ctx context.Context, n int) ([]string, error) {
	var found, err = m.store.Get(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("message %d couldn't be read: %w", n, err)
	}

	var msg, ok = found.Get()
	if !ok {
		return nil, fmt.Errorf("message %d has gone", n)
	}

	var lines = []string{msg.Title}

	var body = strings.TrimSuffix(string(msg.Body), "\r")
	if body != "" {
		lines = append(lines, strings.Split(body, "\r")...)
	}

	return append(lines, ctrlZ), nil
}

// acknowledged marks what this side sent as forwarded, the other side's turn
// having come to say it had it all.
func (m *Machine) acknowledged(ctx context.Context) error {
	for _, n := range m.sent {
		var err = m.store.MarkForwarded(ctx, n, m.partner)
		if err != nil {
			return fmt.Errorf("the forwarding queue couldn't be updated: %w", err)
		}
	}

	m.sent = nil

	return nil
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}
