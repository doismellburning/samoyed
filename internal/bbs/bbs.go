// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/node"
	"github.com/sirupsen/logrus"
)

// Partner is a BBS this one forwards with.
type Partner struct {
	// Call is the partner BBS's callsign.
	Call string

	// Node, if set, is the NET/ROM node or application to connect to, by
	// callsign or alias.  Otherwise the partner is reached over AX.25, at
	// Call on Port, by way of Via.
	Node string
	Port int
	Via  []string

	// Script is lines to send once connected, before the partner's SID: to
	// get from a node's shell to its BBS, say.
	Script []string

	// Routes say which mail goes this way: a pattern matches an @ field
	// that has it as one of its parts - a BBS callsign, "#HANTS", "GBR" - and
	// "*" matches everything.  Personal mail for the partner itself always
	// goes to it.
	Routes []string

	// Bulletins says whether bulletins whose distribution Routes match are
	// sent to the partner.
	Bulletins bool

	// Interval is how often to connect to the partner, whether or not there
	// is mail for it, to collect any it has.  Zero is only when there is
	// mail for it.
	Interval time.Duration
}

// matches says whether the @ field at is routed to p.
func (p *Partner) matches(at string) bool {
	var parts = strings.Split(strings.ToUpper(at), ".")

	for _, r := range p.Routes {
		if r == "*" || slices.ContainsFunc(parts, func(part string) bool { return strings.EqualFold(part, r) }) {
			return true
		}
	}

	return false
}

// Dialer opens a connection to a partner, telling events what happens on it.
type Dialer func(p *Partner, events node.Downlink) (node.Conn, error)

// Config is how a BBS runs.
type Config struct {
	// Call is the BBS's callsign, and HRoute where it is, hierarchically:
	// "#HANTS.GBR.EURO", say.
	Call   string
	HRoute string

	// Version goes in the BBS's SID.
	Version string

	Partners []Partner

	// Dial connects to partners; nil for no forwarding out.
	Dial Dialer

	// Now tells the time; nil for time.Now.
	Now func() time.Time

	// OnMessage, if set, is told of each new message.
	OnMessage func(m *Message)
}

// soon is how long after mail for a partner arrives the BBS connects to it,
// so a burst of messages goes in one session.
const soon = 30 * time.Second

// fwdIdle is how long a forwarding session may go quiet before it is given
// up on.
const fwdIdle = 5 * time.Minute

// partnerState is how forwarding with one partner is going.
type partnerState struct {
	session  *forwarder
	next     time.Time
	lastTime time.Time
	lastErr  error
}

// BBS is the bulletin board.
type BBS struct {
	cfg      Config
	flags    string // What the SID offers.
	store    *Store
	partners map[string]*partnerState
	sessions map[*userSession]bool
}

// New returns a BBS keeping its messages in store.
func New(cfg Config, store *Store) (*BBS, error) {
	if baseCall(cfg.Call) == "" {
		return nil, errors.New("bbs: no callsign")
	}

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	cfg.Version = strings.ReplaceAll(cfg.Version, "-", ".")
	if cfg.Version == "" {
		cfg.Version = "0"
	}

	var b = new(BBS)
	b.cfg = cfg
	b.flags = defaultFlags
	b.store = store
	b.partners = make(map[string]*partnerState)
	b.sessions = make(map[*userSession]bool)

	var now = cfg.Now()

	for i := range cfg.Partners {
		var p = &cfg.Partners[i]
		p.Call = strings.ToUpper(p.Call)

		if baseCall(p.Call) == "" {
			return nil, errors.New("bbs: partner with no callsign")
		}

		if _, dup := b.partners[p.Call]; dup {
			return nil, fmt.Errorf("bbs: partner %s given twice", p.Call)
		}

		b.partners[p.Call] = &partnerState{session: nil, next: now.Add(soon), lastTime: time.Time{}, lastErr: nil}
	}

	return b, nil
}

func (b *BBS) now() time.Time { return b.cfg.Now() }

// defaultFlags are what this BBS's SID offers: compressed forwarding, B1 and
// B2, FBB's protocol, hierarchical addresses, and MIDs and BIDs.
const defaultFlags = "B12FHM$"

// sid is this BBS's SID.
func (b *BBS) sid() SID {
	return SID{Software: "SAMOYED", Version: b.cfg.Version, Flags: b.flags}
}

func (b *BBS) partner(call string) *Partner {
	for i := range b.cfg.Partners {
		if sameStation(b.cfg.Partners[i].Call, call) {
			return &b.cfg.Partners[i]
		}
	}

	return nil
}

// route works out which partners m is to go to, other than the one it came
// from.  Personal mail goes one way - to the BBS it is addressed at, if that
// is a partner, or else to the first partner whose routes match; bulletins
// go to every partner taking bulletins whose routes match their
// distribution.  Mail for this BBS goes nowhere.
func (b *BBS) route(m *Message) {
	m.Forward = nil

	var at = strings.ToUpper(m.At)

	var candidates []*Partner

	for i := range b.cfg.Partners {
		var p = &b.cfg.Partners[i]
		if !sameStation(p.Call, m.Origin) && !slices.Contains(m.Forwarded, p.Call) {
			candidates = append(candidates, p)
		}
	}

	if !m.personal() {
		for _, p := range candidates {
			if p.Bulletins && p.matches(at) {
				m.Forward = append(m.Forward, p.Call)
			}
		}

		return
	}

	if at == "" || atBBS(at) == baseCall(b.cfg.Call) {
		return
	}

	for _, p := range candidates {
		if atBBS(at) == baseCall(p.Call) {
			m.Forward = []string{p.Call}

			return
		}
	}

	for _, p := range candidates {
		if p.matches(at) {
			m.Forward = []string{p.Call}

			return
		}
	}
}

// post stores a new message and routes it on.
func (b *BBS) post(m *Message) error {
	b.route(m)

	var err = b.store.Add(m, b.cfg.Call)
	if err != nil {
		return err
	}

	for _, p := range m.Forward {
		var st = b.partners[p]
		if st != nil && st.next.After(b.now().Add(soon)) {
			st.next = b.now().Add(soon)
		}
	}

	if b.cfg.OnMessage != nil {
		b.cfg.OnMessage(m)
	}

	return nil
}

// accept takes a message a partner sent.
func (b *BBS) accept(m *Message) {
	var err = b.post(m)
	if err != nil && !errors.Is(err, ErrDuplicate) {
		logrus.WithError(err).WithField("bid", m.BID).Error("BBS could not store a message")
	}
}

// forwarded notes that m has gone to partner, or need not.
func (b *BBS) forwarded(m *Message, partner string) {
	m.forwardedTo(partner)

	var err = b.store.Update(m)
	if err != nil {
		logrus.WithError(err).WithField("number", m.Number).Error("BBS could not update a message")
	}
}

// pendingFor returns the messages still to go to partner, oldest first, but
// those in tried.
func (b *BBS) pendingFor(partner string, tried map[int]bool) []*Message {
	var list []*Message

	for _, m := range b.store.All() {
		if m.pending(partner) && !tried[m.Number] {
			list = append(list, m)
		}
	}

	return list
}

// forwardDone hears that a forwarding session ended.
func (b *BBS) forwardDone(f *forwarder) {
	var st = b.partners[f.partner.Call]
	if st != nil && st.session == f {
		st.session = nil
		st.lastTime = b.now()
		st.lastErr = f.err
	}

	var entry = logrus.WithFields(logrus.Fields{
		"partner":  f.partner.Call,
		"protocol": f.proto.String(),
		"sent":     f.sent,
		"received": f.received,
	})

	if f.err != nil {
		entry.WithError(f.err).Warn("BBS forwarding ended")
	} else {
		entry.Info("BBS forwarding done")
	}
}

// Tick connects to partners when it is time, and gives up on sessions gone
// quiet.
func (b *BBS) Tick(now time.Time) {
	for i := range b.cfg.Partners {
		var p = &b.cfg.Partners[i]
		var st = b.partners[p.Call]

		if st.session != nil {
			if now.Sub(st.session.lastActive) >= fwdIdle {
				st.session.fail(errors.New("timed out"))
			}

			continue
		}

		if now.Before(st.next) {
			continue
		}

		st.next = now.Add(max(p.Interval, time.Hour))
		if p.Interval > 0 {
			st.next = now.Add(p.Interval)
		}

		if p.Interval > 0 || len(b.pendingFor(p.Call, nil)) > 0 {
			var err = b.dial(p)
			if err != nil {
				st.lastErr = err
				logrus.WithError(err).WithField("partner", p.Call).Warn("BBS could not connect to partner")
			}
		}
	}

	for s := range b.sessions {
		s.tick(now)
	}
}

// ErrNoPartner is returned for a partner the BBS does not have.
var ErrNoPartner = errors.New("bbs: no such partner")

// ForwardNow connects to the partner now, if it is not already.
func (b *BBS) ForwardNow(call string) error {
	var p = b.partner(call)
	if p == nil {
		return fmt.Errorf("%w: %s", ErrNoPartner, call)
	}

	if b.partners[p.Call].session != nil {
		return nil
	}

	return b.dial(p)
}

func (b *BBS) dial(p *Partner) error {
	if b.cfg.Dial == nil {
		return errors.New("bbs: no way to connect")
	}

	var link = new(dialLink)

	var f = newForwarder(b, p, link, func() {}, true)
	link.f = f

	var conn, err = b.cfg.Dial(p, link)
	if err != nil {
		return err
	}

	link.conn = conn
	f.hangup = conn.Close
	b.partners[p.Call].session = f

	return nil
}

// dialLink is a connection the BBS opened to a partner.
type dialLink struct {
	f    *forwarder
	conn node.Conn
	held [][]byte // What was sent before the connection was there to send it.
}

func (d *dialLink) Send(data []byte) {
	if d.conn == nil {
		d.held = append(d.held, data)

		return
	}

	d.conn.Send(data)
}

func (d *dialLink) Connected() {
	for _, line := range d.f.partner.Script {
		d.Send([]byte(line + "\r"))
	}
}

func (d *dialLink) Received(data []byte) { d.f.Input(data) }

func (d *dialLink) Closed(err error) {
	if d.f.state != fwdDone {
		if err == nil {
			err = errors.New("link closed")
		}

		d.f.finish(err)
	}
}

// PartnerStatus is how forwarding with a partner is going, for showing.
type PartnerStatus struct {
	Call      string    `json:"call"`
	Pending   int       `json:"pending"`
	Active    bool      `json:"active"`
	Next      time.Time `json:"next"`
	Last      time.Time `json:"last"`
	LastError string    `json:"lastError,omitempty"`
}

// Partners reports on every partner.
func (b *BBS) Partners() []PartnerStatus {
	var list = make([]PartnerStatus, 0, len(b.cfg.Partners))

	for _, p := range b.cfg.Partners {
		var st = b.partners[p.Call]

		var lastErr string
		if st.lastErr != nil {
			lastErr = st.lastErr.Error()
		}

		list = append(list, PartnerStatus{
			Call: p.Call, Pending: len(b.pendingFor(p.Call, nil)), Active: st.session != nil,
			Next: st.next.UTC(), Last: st.lastTime.UTC(), LastError: lastErr,
		})
	}

	return list
}

// Messages returns every message, oldest first.
func (b *BBS) Messages() []*Message { return b.store.All() }

// Kill deletes message n.
func (b *BBS) Kill(n int) error { return b.store.Delete(n) }
