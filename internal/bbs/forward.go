// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

// A forwarding session with a partner BBS, in the FBB protocol: each side in
// turn proposes up to five messages, the other answers which it wants, and
// those are sent; a side with nothing to propose says FF, and when neither
// has anything left, FQ ends it.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/lzhuf"
	"github.com/doismellburning/samoyed/internal/node"
)

type fwdState int

const (
	fwdWaitSID     fwdState = iota // Waiting for the partner's SID (and, calling, its prompt).
	fwdWaitAnswers                 // Proposed; waiting for FS.
	fwdWaitPropose                 // The partner's turn: waiting for proposals, FF or FQ.
	fwdReceiving                   // Receiving the messages accepted.
	fwdDone
)

// maxMessageSize is the largest message taken from a partner.
const maxMessageSize = 100 * 1024

// maxFwdLine is the longest line taken in a forwarding session.
const maxFwdLine = 1024

// offer is a message proposed to the partner, and what will be sent if it is
// accepted.
type offer struct {
	msg      *Message
	prop     proposal
	payload  []byte
	answered answer
}

// forwarder is one forwarding session.
type forwarder struct {
	b       *BBS
	partner *Partner
	out     node.Sender
	hangup  func()
	caller  bool

	state  fwdState
	proto  protocol
	sidOK  bool
	line   []byte
	block  []string   // The partner's proposal lines so far.
	props  []proposal // The partner's proposals in this block.
	recv   []proposal // Accepted, still to be received.
	reader *blockReader

	// A plain message being received.
	plainTitle   string
	plainBody    strings.Builder
	plainStarted bool

	offers []*offer
	tried  map[int]bool // Messages proposed this session, not to be proposed again.

	received, sent int
	lastActive     time.Time
	err            error
}

func newForwarder(b *BBS, partner *Partner, out node.Sender, hangup func(), caller bool) *forwarder {
	var f = new(forwarder)
	f.b = b
	f.partner = partner
	f.out = out
	f.hangup = hangup
	f.caller = caller
	f.tried = make(map[int]bool)
	f.lastActive = b.now()

	return f
}

func (f *forwarder) send(lines ...string) {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\r")
	}

	f.out.Send([]byte(b.String()))
}

// fail ends the session over a protocol error, telling the partner why.
func (f *forwarder) fail(err error) {
	if f.state == fwdDone {
		return
	}

	f.send("*** " + err.Error())
	f.finish(err)
	f.hangup()
}

// finish ends the session.
func (f *forwarder) finish(err error) {
	if f.state == fwdDone {
		return
	}

	f.state = fwdDone
	f.err = err
	f.b.forwardDone(f)
}

// Input takes what the partner sent.
func (f *forwarder) Input(data []byte) {
	f.lastActive = f.b.now()

	for len(data) > 0 && f.state != fwdDone {
		if f.state == fwdReceiving && f.proto.compressed() {
			if f.reader == nil {
				f.reader = new(blockReader)
			}

			var n = f.reader.feed(data)
			data = data[n:]

			switch {
			case f.reader.failure != nil:
				f.fail(f.reader.failure)
			case f.reader.done:
				var r = f.reader
				f.reader = nil
				f.receivedCompressed(r)
			}

			continue
		}

		var c = data[0]
		data = data[1:]

		if c == '\r' || c == '\n' {
			if c == '\n' && len(f.line) == 0 {
				continue // The LF of a CR LF.
			}

			var line = string(f.line)
			f.line = f.line[:0]
			f.handleLine(line)

			continue
		}

		f.line = append(f.line, c)
		if len(f.line) > maxFwdLine {
			f.fail(errors.New("line too long"))
		}
	}
}

func (f *forwarder) handleLine(line string) {
	if f.state == fwdReceiving {
		f.plainLine(line)

		return
	}

	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "***") {
		f.finish(fmt.Errorf("partner said %q", line))
		f.hangup()

		return
	}

	switch f.state {
	case fwdWaitSID:
		f.waitSID(line)
	case fwdWaitPropose:
		f.waitPropose(line)
	case fwdWaitAnswers:
		f.waitAnswers(line)
	case fwdReceiving, fwdDone:
	}
}

// start begins a session the partner opened, whose SID is the line given.
func (f *forwarder) startAnswering(sidLine string) {
	f.waitSID(sidLine)
}

func (f *forwarder) waitSID(line string) {
	if sid, ok := ParseSID(line); ok {
		f.proto = negotiate(f.b.sid(), sid)
		if f.proto == protoNone {
			f.fail(errors.New("no FBB forwarding in common"))

			return
		}

		f.sidOK = true

		if !f.caller {
			// We announced ourselves first; now it is the caller's turn.
			f.state = fwdWaitPropose
		}

		return
	}

	// Calling, the partner's prompt after its SID is our cue.
	if f.caller && f.sidOK && strings.HasSuffix(line, ">") {
		f.send(f.b.sid().String())
		f.propose()
	}
}

func (f *forwarder) waitPropose(line string) {
	switch {
	case line == "":
	case strings.HasPrefix(line, ";"):
		// A comment, which some systems send.
	case line == "FF":
		if f.hasPending() {
			f.propose()

			return
		}

		f.send("FQ")
		f.finish(nil)
		f.hangup()
	case line == "FQ":
		f.finish(nil)
		f.hangup()
	case strings.HasPrefix(line, "F>"):
		f.answer(line)
	case len(line) >= 2 && line[0] == 'F' && strings.ContainsRune("ABC", rune(line[1])):
		var p, err = parseProposal(line)
		if err != nil {
			f.fail(err)

			return
		}

		f.block = append(f.block, line)
		f.props = append(f.props, p)

		if len(f.props) > maxProposals {
			f.fail(errors.New("too many proposals in one block"))
		}
	default:
		f.fail(fmt.Errorf("unexpected %q", line))
	}
}

// answer answers the proposal block that the F> line ends.
func (f *forwarder) answer(end string) {
	if len(f.props) == 0 {
		f.fail(errors.New("empty proposal block"))

		return
	}

	var given = strings.TrimSpace(strings.TrimPrefix(end, "F>"))
	if given != "" && !strings.EqualFold(given, fmt.Sprintf("%02X", blockChecksum(f.block))) {
		f.fail(errors.New("proposal checksum is wrong"))

		return
	}

	var answers strings.Builder

	f.recv = nil

	for _, p := range f.props {
		var a = f.decide(p)
		answers.WriteByte(byte(a))

		if a == answerAccept {
			f.recv = append(f.recv, p)
		}
	}

	f.props = nil
	f.block = nil
	f.send("FS " + answers.String())

	if len(f.recv) > 0 {
		f.state = fwdReceiving

		return
	}

	f.propose()
}

// decide says whether to take a proposed message.
func (f *forwarder) decide(p proposal) answer {
	var compressed = p.code == 'A' || p.code == 'C'

	switch {
	case f.b.store.HasBID(p.bid):
		return answerHave
	case p.size > maxMessageSize || p.compressedSize > maxMessageSize:
		return answerReject
	case p.code == 'B' && f.proto.compressed():
		return answerReject // A compressed binary file, which a BBS message is not.
	case compressed != f.proto.compressed(), (p.code == 'C') != (f.proto == protoB2):
		return answerError
	case p.code == 'C' && p.msgType != "EM" && p.msgType != "CM":
		return answerReject
	default:
		return answerAccept
	}
}

// plainLine takes a line of an uncompressed message: its title, then its text
// up to a Ctrl-Z.
func (f *forwarder) plainLine(line string) {
	if !f.plainStarted {
		f.plainStarted = true
		f.plainTitle = strings.TrimSpace(line)

		return
	}

	var text, _, ended = strings.Cut(line, string(rune(chrSUB)))

	if !ended {
		f.plainBody.WriteString(line)
		f.plainBody.WriteString("\n")

		if f.plainBody.Len() > maxMessageSize {
			f.fail(errTooBig)
		}

		return
	}

	if text != "" {
		f.plainBody.WriteString(text)
		f.plainBody.WriteString("\n")
	}

	var p = f.recv[0]
	f.recv = f.recv[1:]

	f.take(&Message{
		Number: 0, Type: p.typ, From: p.from, To: p.to, At: p.at, BID: p.bid,
		Subject: f.plainTitle, Body: f.plainBody.String(), Date: f.b.now().UTC(),
		Read: false, Origin: "", Forward: nil, Forwarded: nil,
	})

	f.plainStarted = false
	f.plainTitle = ""
	f.plainBody.Reset()
	f.nextReceive()
}

// receivedCompressed takes a compressed message now all in.
func (f *forwarder) receivedCompressed(r *blockReader) {
	var p = f.recv[0]
	f.recv = f.recv[1:]

	var data, err = lzhuf.DecompressMax(r.data, f.proto.crc(), maxMessageSize)
	if err != nil {
		f.fail(err)

		return
	}

	var m *Message

	if p.code == 'C' {
		m, err = decodeB2F(data)
		if err != nil {
			f.fail(err)

			return
		}
	} else {
		m = &Message{
			Number: 0, Type: p.typ, From: p.from, To: p.to, At: p.at, BID: p.bid,
			Subject: r.title, Body: localBody(string(data)), Date: f.b.now().UTC(),
			Read: false, Origin: "", Forward: nil, Forwarded: nil,
		}
	}

	if m.Date.IsZero() {
		m.Date = f.b.now().UTC()
	}

	f.take(m)
	f.nextReceive()
}

func (f *forwarder) take(m *Message) {
	f.received++
	m.Origin = f.partner.Call
	f.b.accept(m)
}

// nextReceive moves on once a message is in: to the next, or to our turn.
func (f *forwarder) nextReceive() {
	if f.state != fwdReceiving || len(f.recv) > 0 {
		return
	}

	f.propose()
}

func (f *forwarder) hasPending() bool {
	return len(f.b.pendingFor(f.partner.Call, f.tried)) > 0
}

// propose offers the partner what is waiting for it, or says FF.
func (f *forwarder) propose() {
	var msgs = f.b.pendingFor(f.partner.Call, f.tried)

	f.offers = nil

	var size = 0

	for _, m := range msgs {
		if len(f.offers) == maxProposals || (len(f.offers) > 0 && size+m.Size() > maxBlockSize) {
			break
		}

		f.tried[m.Number] = true
		size += m.Size()
		f.offers = append(f.offers, f.makeOffer(m))
	}

	if len(f.offers) == 0 {
		f.send("FF")
		f.state = fwdWaitPropose

		return
	}

	var lines = make([]string, 0, len(f.offers)+1)
	for _, o := range f.offers {
		lines = append(lines, o.prop.line())
	}

	var sum = blockChecksum(lines)
	f.send(append(lines, fmt.Sprintf("F> %02X", sum))...)
	f.state = fwdWaitAnswers
}

// makeOffer prepares m for the partner, with this BBS's R: line on top.
func (f *forwarder) makeOffer(m *Message) *offer {
	var o = new(offer)
	o.msg = m

	var routed = *m
	routed.Body = routeLine(f.b.now(), m.Number, f.b.cfg.Call, f.b.cfg.HRoute) + "\n" + m.Body

	var at = m.At
	if at == "" {
		at = baseCall(f.b.cfg.Call)
	}

	var text = wireBody(routed.Body)

	switch f.proto {
	case protoB2:
		var b2f = encodeB2F(&routed, baseCall(f.b.cfg.Call))
		o.payload = lzhuf.Compress(b2f, true)
		o.prop = proposal{code: 'C', msgType: "EM", bid: m.BID, size: len(b2f), compressedSize: len(o.payload), typ: "", from: "", at: "", to: ""}
	case protoB, protoB1:
		o.payload = lzhuf.Compress([]byte(text), f.proto.crc())
		o.prop = proposal{code: 'A', typ: m.Type, from: m.From, at: at, to: m.To, bid: m.BID, size: len(text), msgType: "", compressedSize: 0}
	case protoF, protoNone:
		o.payload = []byte(subjectLine(m.Subject) + "\r" + text + string(rune(chrSUB)) + "\r")
		o.prop = proposal{code: 'B', typ: m.Type, from: m.From, at: at, to: m.To, bid: m.BID, size: len(text), msgType: "", compressedSize: 0}
	}

	return o
}

// subjectLine is a subject fit to be a line of its own.
func subjectLine(subject string) string {
	subject = strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}

		return r
	}, subject)

	if subject == "" {
		return "(no subject)"
	}

	return subject
}

func (f *forwarder) waitAnswers(line string) {
	if line == "" {
		return
	}

	var answers, err = parseAnswers(line, len(f.offers))
	if err != nil {
		f.fail(err)

		return
	}

	for i, a := range answers {
		var o = f.offers[i]
		o.answered = a

		switch a {
		case answerAccept:
			if f.proto.compressed() {
				f.out.Send(frameCompressed(subjectLine(o.msg.Subject), o.payload))
			} else {
				f.out.Send(o.payload)
			}

			f.sent++
			f.b.forwarded(o.msg, f.partner.Call)
		case answerHave, answerReject, answerHold, answerError:
			// Not to be offered again: the partner has it, or never will.
			f.b.forwarded(o.msg, f.partner.Call)
		case answerDefer, answerAcceptAt:
		}
	}

	f.offers = nil
	f.state = fwdWaitPropose
}

// Close says the link has gone.
func (f *forwarder) Close() {
	if f.state != fwdDone {
		f.finish(errors.New("link closed"))
	}
}
