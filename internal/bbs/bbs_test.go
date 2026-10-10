// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wire is a pair of BBSes, linked in memory: what one sends the other
// receives when pumped.
type wire struct {
	t     *testing.T
	now   time.Time
	queue []func()
}

func (w *wire) pump() {
	for i := 0; len(w.queue) > 0; i++ {
		require.Less(w.t, i, 100000, "the BBSes never went quiet")

		var f = w.queue[0]
		w.queue = w.queue[1:]
		f()
	}
}

// pipeEnd is one end of an in-memory link.
type pipeEnd struct {
	w       *wire
	deliver func([]byte)
	close   func()
	closed  bool
}

func (p *pipeEnd) Send(data []byte) {
	var copied = bytes.Clone(data)

	p.w.queue = append(p.w.queue, func() { p.deliver(copied) })
}

func (p *pipeEnd) Close() {
	if p.closed {
		return
	}

	p.closed = true
	p.w.queue = append(p.w.queue, p.close)
}

// newBBS makes a BBS with flags in its SID, forwarding with partner.
func newBBS(t *testing.T, w *wire, call string, partner Partner, flags string) *BBS {
	t.Helper()

	var store, err = OpenStore(t.TempDir())
	require.NoError(t, err)

	var b, berr = New(Config{
		Call: call, HRoute: "#TEST.GBR.EURO", Version: "1-test", Partners: []Partner{partner},
		Dial: nil, Now: func() time.Time { return w.now }, OnMessage: nil,
	}, store)
	require.NoError(t, berr)

	b.flags = flags

	return b
}

// link lets a dial from one BBS reach the other's sessions, as though over the
// air: the caller connects as its own callsign.
func link(w *wire, from *BBS, to *BBS) {
	from.cfg.Dial = func(_ *Partner, events node.Downlink) (node.Conn, error) {
		var caller = &pipeEnd{w: w, deliver: nil, close: nil, closed: false}
		var answerer = &pipeEnd{w: w, deliver: nil, close: nil, closed: false}

		var session node.AppSession

		w.queue = append(w.queue, func() {
			events.Connected()
			session = to.OpenDirect(from.cfg.Call, answerer)
		})

		caller.deliver = func(data []byte) { session.Input(data) }
		caller.close = func() { session.Close(); events.Closed(nil) }
		answerer.deliver = events.Received
		answerer.close = func() { session.Close(); events.Closed(nil) }

		return caller, nil
	}
}

func partnerFor(call string) Partner {
	return Partner{Call: call, Node: "", Port: 0, Via: nil, Script: nil, Routes: []string{"*"}, Bulletins: true, Interval: 0}
}

func message(typ string, from string, to string, at string, subject string, body string) *Message {
	return &Message{
		Number: 0, Type: typ, From: from, To: to, At: at, BID: "", Subject: subject, Body: body,
		Date: time.Unix(1_700_000_000, 0).UTC(), Read: false, Origin: "", Forward: nil, Forwarded: nil,
	}
}

func pair(t *testing.T, aFlags string, bFlags string) (*wire, *BBS, *BBS) {
	t.Helper()

	var w = &wire{t: t, now: time.Unix(1_800_000_000, 0), queue: nil}
	var a = newBBS(t, w, "Q1BBS", partnerFor("Q2BBS"), aFlags)
	var b = newBBS(t, w, "Q2BBS", partnerFor("Q1BBS"), bFlags)

	link(w, a, b)
	link(w, b, a)

	return w, a, b
}

func TestForwardingBothWays(t *testing.T) {
	for name, flags := range map[string][2]string{
		"B1":           {"B12FHM$", "B12FHM$"},
		"B2":           {"B12FHM$", "B2FHM$"},
		"B":            {"B12FHM$", "BFHM$"},
		"uncompressed": {"B12FHM$", "FHM$"},
	} {
		t.Run(name, func(t *testing.T) {
			var w, a, b = pair(t, flags[0], flags[1])

			var body = strings.Repeat("Line of text for the message.\n", 50)

			require.NoError(t, a.post(message(TypePersonal, "Q4TEST", "Q5TEST", "Q2BBS.#TEST.GBR.EURO", "For you", body)))
			require.NoError(t, a.post(message(TypeBulletin, "Q4TEST", "ALL", "GBR", "News", "Hello all.\n")))
			require.NoError(t, b.post(message(TypePersonal, "Q5TEST", "Q4TEST", "Q1BBS", "Reply", "Thanks!\n")))

			// Mail for the BBS itself stays put.
			require.NoError(t, a.post(message(TypePersonal, "Q6TEST", "Q4TEST", "", "Local", "Here.\n")))

			require.NoError(t, a.ForwardNow("Q2BBS"))
			w.pump()

			var got = b.store.All()
			require.Len(t, got, 3, "B kept its own and took both of A's")

			var personal = got[1]
			assert.Equal(t, "For you", personal.Subject)
			assert.Equal(t, "Q5TEST", personal.To)
			assert.Equal(t, "Q4TEST", personal.From)
			assert.Equal(t, "Q2BBS.#TEST.GBR.EURO", personal.At)
			assert.Equal(t, "Q1BBS", personal.Origin)
			assert.True(t, strings.HasPrefix(personal.Body, "R:"), "A's R: line is on top")
			assert.True(t, strings.HasSuffix(personal.Body, body))
			assert.Empty(t, personal.Forward, "personal mail for B goes no further")

			var reply = a.store.All()[3]
			assert.Equal(t, "Reply", reply.Subject)
			assert.Equal(t, "Q2BBS", reply.Origin)

			for _, m := range a.store.All()[:2] {
				assert.Empty(t, m.Forward)
				assert.Equal(t, []string{"Q2BBS"}, m.Forwarded)
			}

			assert.Empty(t, a.Partners()[0].LastError)
			assert.Empty(t, b.Partners()[0].LastError)
			assert.False(t, a.Partners()[0].Active)

			// Offered again, they are refused as already there.
			a.store.All()[0].Forward = []string{"Q2BBS"}
			require.NoError(t, a.ForwardNow("Q2BBS"))
			w.pump()
			assert.Len(t, b.store.All(), 3)
			assert.Empty(t, a.store.All()[0].Forward)
		})
	}
}

func TestForwardingRefusesStrangers(t *testing.T) {
	var w = &wire{t: t, now: time.Unix(1_800_000_000, 0), queue: nil}
	var a = newBBS(t, w, "Q1BBS", partnerFor("Q2BBS"), "B12FHM$")
	var stranger = newBBS(t, w, "Q9BBS", partnerFor("Q1BBS"), "B12FHM$")

	link(w, stranger, a)
	require.NoError(t, stranger.post(message(TypeBulletin, "Q9TEST", "ALL", "GBR", "Spam", "x\n")))
	require.NoError(t, stranger.ForwardNow("Q1BBS"))
	w.pump()

	assert.Empty(t, a.store.All())
	assert.Contains(t, stranger.Partners()[0].LastError, "not a forwarding partner")
}

func TestRouting(t *testing.T) {
	var w = &wire{t: t, now: time.Unix(1_800_000_000, 0), queue: nil}

	var store, err = OpenStore(t.TempDir())
	require.NoError(t, err)

	var b, berr = New(Config{
		Call: "Q1BBS", HRoute: "", Version: "", Dial: nil, Now: func() time.Time { return w.now }, OnMessage: nil,
		Partners: []Partner{
			{Call: "Q2BBS", Node: "", Port: 0, Via: nil, Script: nil, Routes: []string{"#NORTH"}, Bulletins: true, Interval: 0},
			{Call: "Q3BBS", Node: "", Port: 0, Via: nil, Script: nil, Routes: []string{"GBR"}, Bulletins: false, Interval: 0},
			{Call: "Q4BBS", Node: "", Port: 0, Via: nil, Script: nil, Routes: []string{"*"}, Bulletins: true, Interval: 0},
		},
	}, store)
	require.NoError(t, berr)

	for _, c := range []struct {
		typ, at, origin string
		want            []string
	}{
		{TypePersonal, "", "", nil},
		{TypePersonal, "Q1BBS.#X.GBR", "", nil},
		{TypePersonal, "Q3BBS", "", []string{"Q3BBS"}},
		{TypePersonal, "Q7BBS.#NORTH.GBR", "", []string{"Q2BBS"}},
		{TypePersonal, "Q7BBS.#SOUTH.GBR", "", []string{"Q3BBS"}},
		{TypePersonal, "Q7BBS.#SOUTH.GBR", "Q3BBS", []string{"Q4BBS"}},
		{TypeBulletin, "GBR", "", []string{"Q4BBS"}},
		{TypeBulletin, "#NORTH", "Q4BBS", []string{"Q2BBS"}},
	} {
		var m = message(c.typ, "Q8TEST", "Q9TEST", c.at, "", "")
		m.Origin = c.origin
		b.route(m)
		assert.Equal(t, c.want, m.Forward, "%+v", c)
	}
}

func TestStorePersists(t *testing.T) {
	var dir = t.TempDir()

	var s, err = OpenStore(dir)
	require.NoError(t, err)

	var m = message(TypePersonal, "Q1TEST", "Q2TEST", "", "Hi", "There\n")
	require.NoError(t, s.Add(m, "Q1BBS-1"))
	assert.Equal(t, 1, m.Number)
	assert.Equal(t, "1_Q1BBS", m.BID)
	require.NoError(t, s.Add(message(TypePersonal, "a", "b", "", "", ""), "Q1BBS"))

	var dup = message(TypePersonal, "a", "b", "", "", "")
	dup.BID = "1_q1bbs"
	require.ErrorIs(t, s.Add(dup, "Q1BBS"), ErrDuplicate)

	m.Read = true
	require.NoError(t, s.Update(m))
	require.NoError(t, s.Delete(2))

	var again, rerr = OpenStore(dir)
	require.NoError(t, rerr)
	require.Len(t, again.All(), 1)
	assert.True(t, again.All()[0].Read)
	assert.True(t, again.HasBID("1_Q1BBS"))

	var next = message(TypePersonal, "a", "b", "", "", "")
	require.NoError(t, again.Add(next, "Q1BBS"))
	assert.Equal(t, 2, next.Number, "numbers carry on from the highest left")
}

// screen records what a user is sent.
type screen struct {
	b      strings.Builder
	exited bool
}

func (s *screen) Send(data []byte) { s.b.Write(data) }

func (s *screen) take() string {
	var text = strings.ReplaceAll(s.b.String(), "\r", "\n")
	s.b.Reset()

	return text
}

func TestUserCommands(t *testing.T) {
	var w = &wire{t: t, now: time.Unix(1_800_000_000, 0), queue: nil}
	var b = newBBS(t, w, "Q1BBS", partnerFor("Q2BBS"), "B12FHM$")

	var alice = new(screen)
	var as = b.Open("Q4TEST", alice, func() { alice.exited = true })
	assert.Equal(t, "[SAMOYED-1.test-B12FHM$]\nHello Q4TEST, this is the Q1BBS BBS.  You have 0 unread message(s).  Type H for help.\nde Q1BBS>\n", alice.take())

	var say = func(s node.AppSession, text string) { s.Input([]byte(text + "\r")) }

	say(as, "S Q5TEST")
	say(as, "Lunch")
	say(as, "Are you free?")
	say(as, "/EX")
	assert.Contains(t, alice.take(), "Message 1 saved.")

	say(as, "SB ALL@GBR")
	say(as, "Net tonight")
	say(as, "At 8.\x1a")
	assert.Contains(t, alice.take(), "Message 2 saved.")

	say(as, "S Q6TEST")
	say(as, "Never mind")
	say(as, "/ABORT")
	assert.Contains(t, alice.take(), "Message cancelled.")

	var bob = new(screen)
	var bs = b.Open("Q5TEST-7", bob, func() { bob.exited = true })
	assert.Contains(t, bob.take(), "You have 1 unread message(s).")

	say(bs, "LM")
	var listing = bob.take()
	assert.Contains(t, listing, "Lunch")
	assert.NotContains(t, listing, "Net tonight")

	say(bs, "R 1")
	var read = bob.take()
	assert.Contains(t, read, "From: Q4TEST")
	assert.Contains(t, read, "Are you free?\n")
	assert.True(t, b.store.All()[0].Read)

	var carol = new(screen)
	var cs = b.Open("Q6TEST", carol, func() {})
	carol.take()

	say(cs, "R 1")
	assert.Contains(t, carol.take(), "No message 1.", "personal mail is for its sender and addressee")

	say(cs, "L")
	listing = carol.take()
	assert.Contains(t, listing, "Net tonight")
	assert.NotContains(t, listing, "Lunch")

	say(cs, "K 2")
	assert.Contains(t, carol.take(), "Cannot kill message 2.")

	say(as, "K 2")
	assert.Contains(t, alice.take(), "Message 2 killed.")

	say(as, "bogus")
	assert.Contains(t, alice.take(), "Unknown command.")

	say(as, "B")
	assert.True(t, alice.exited)
}

func TestForwarderSIDNegotiation(t *testing.T) {
	var sid, ok = ParseSID("[BPQ-6.0.24.1-B1FWIHJM$]")
	require.True(t, ok)
	assert.Equal(t, "BPQ", sid.Software)
	assert.Equal(t, "6.0.24.1", sid.Version)

	var ours = SID{Software: "SAMOYED", Version: "1", Flags: "B12FHM$"}
	assert.Equal(t, protoB1, negotiate(ours, sid))

	for flags, want := range map[string]protocol{
		"B2FHM$":  protoB2,
		"BFHM$":   protoB,
		"FHM$":    protoF,
		"HM$":     protoNone,
		"AB1FHMX": protoB1,
	} {
		assert.Equal(t, want, negotiate(ours, SID{Software: "X", Version: "", Flags: flags}), flags)
	}

	for _, line := range []string{"", "[]", "[X]", "BPQ-1-F", "[X-1-F"} {
		var _, isSID = ParseSID(line)
		assert.False(t, isSID, line)
	}
}

func TestProposalsAndAnswers(t *testing.T) {
	var p, err = parseProposal("FA P Q4TEST Q2BBS.#TEST Q5TEST 12_Q1BBS 345")
	require.NoError(t, err)
	assert.Equal(t, "FA P Q4TEST Q2BBS.#TEST Q5TEST 12_Q1BBS 345", p.line())

	var c, cerr = parseProposal("FC EM ABCDEF123456 527 123 0")
	require.NoError(t, cerr)
	assert.Equal(t, 123, c.compressedSize)

	for _, bad := range []string{"FA P Q4TEST", "FX P a b c d 1", "FA P a b c d -1", "FC EM x y z", "FB P a b c d x"} {
		var _, perr = parseProposal(bad)
		require.Error(t, perr, bad)
	}

	// FBB's own documentation's example block.
	var line = "FB P F6FBB FC1GHV FC1MVP 24657_F6FBB 1345"
	assert.Equal(t, byte(-(sumOf(line)+'\r')&0xff), blockChecksum([]string{line}))

	var answers, aerr = parseAnswers("FS +-=RHE!123Y", 8)
	require.NoError(t, aerr)
	assert.Equal(t, []answer{answerAccept, answerHave, answerDefer, answerReject, answerHold, answerError, answerAcceptAt, answerAccept}, answers)

	var _, nerr = parseAnswers("FS ++", 3)
	require.Error(t, nerr)

	_, nerr = parseAnswers("FS +?", 2)
	require.Error(t, nerr)
}

func sumOf(s string) int {
	var n = 0
	for i := range len(s) {
		n += int(s[i])
	}

	return n
}

func TestFraming(t *testing.T) {
	var data = bytes.Repeat([]byte{1, 2, 3, 250}, 300)
	var framed = frameCompressed("A title", data)

	var r = new(blockReader)
	var n = r.feed(append(framed, 'x'))
	require.NoError(t, r.failure)
	assert.True(t, r.done)
	assert.Equal(t, len(framed), n, "stops at the end of the message")
	assert.Equal(t, "A title", r.title)
	assert.Equal(t, data, r.data)

	framed[len(framed)-1]++
	r = new(blockReader)
	r.feed(framed)
	require.ErrorIs(t, r.failure, errChecksum)
}

func TestB2FRoundTrip(t *testing.T) {
	var m = message(TypeBulletin, "Q4TEST", "ALL", "GBR", "News", "One\nTwo\n")
	m.BID = "12_Q1BBS"

	var got, err = decodeB2F(encodeB2F(m, "Q1BBS"))
	require.NoError(t, err)
	assert.Equal(t, m.Type, got.Type)
	assert.Equal(t, m.From, got.From)
	assert.Equal(t, m.To, got.To)
	assert.Equal(t, m.At, got.At)
	assert.Equal(t, m.BID, got.BID)
	assert.Equal(t, m.Subject, got.Subject)
	assert.Equal(t, m.Body, got.Body)
	assert.Equal(t, m.Date.Truncate(time.Minute), got.Date, "B2F dates are to the minute")

	for _, bad := range []string{"", "Mid: x\r\n", "Mid: x\r\nFrom: a\r\nTo: b\r\nBody: 99\r\n\r\nshort", "nonsense\r\n\r\n"} {
		var _, berr = decodeB2F([]byte(bad))
		require.Error(t, berr, bad)
	}
}

// FuzzForwarding feeds an answering forwarder whatever a partner might send
// after its SID.
func FuzzForwarding(f *testing.F) {
	var block = []string{"FA P Q4TEST Q1BBS Q5TEST 1_Q2BBS 10"}
	f.Add("[X-1-B1FHM$]\r" + block[0] + "\rF> \r" + string(frameCompressed("t", []byte{1, 2, 3})))
	f.Add("[X-1-FHM$]\rFB P A B C 1_X 5\rF>\rTitle\rText\r\x1a\rFF\rFQ\r")
	f.Add("[X-1-B2FHM$]\rFC EM ABC 10 10 0\rF>\r\x01\x05t\x000\x00\x02\x01x\x04\x88")

	f.Fuzz(func(t *testing.T, input string) {
		var w = &wire{t: t, now: time.Unix(1_800_000_000, 0), queue: nil}

		var store, err = OpenStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}

		var b, berr = New(Config{
			Call: "Q1BBS", HRoute: "", Version: "1", Partners: []Partner{partnerFor("Q2BBS")},
			Dial: nil, Now: func() time.Time { return w.now }, OnMessage: nil,
		}, store)
		if berr != nil {
			t.Fatal(berr)
		}

		_ = b.post(message(TypeBulletin, "Q4TEST", "ALL", "GBR", "x", "y\n"))

		var out = new(screen)
		var s = b.Open("Q2BBS", out, func() {})
		s.Input([]byte(input))
		s.Close()
		b.Tick(w.now.Add(time.Hour))
	})
}
