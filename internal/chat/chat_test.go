// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// screen records what one user is sent.
type screen struct {
	b strings.Builder
}

func (s *screen) Send(data []byte) { s.b.Write(data) }

// take returns what has been sent since last asked, a line per element.
func (s *screen) take() []string {
	var text = strings.TrimSuffix(s.b.String(), "\r")
	s.b.Reset()

	if text == "" {
		return nil
	}

	return strings.Split(text, "\r")
}

type user struct {
	screen *screen
	app    node.AppSession
	exited bool
}

func (u *user) say(text string) { u.app.Input([]byte(text + "\r")) }

type clock struct{ now time.Time }

func newServer(t *testing.T, cfg Config) (*Server, *clock) {
	t.Helper()

	var c = &clock{now: time.Unix(1000, 0)}
	cfg.Now = func() time.Time { return c.now }

	var s, err = New(cfg)
	require.NoError(t, err)

	return s, c
}

func (u *user) open(s *Server, call string) *user {
	u.screen = new(screen)
	u.app = s.Open(call, u.screen, func() { u.exited = true })

	return u
}

func TestChatRoom(t *testing.T) {
	var s, _ = newServer(t, DefaultConfig())

	var a = new(user).open(s, "Q1TEST")
	assert.Equal(t, []string{
		"*** Welcome to chat, Q1TEST.  Type /help for commands, /quit to leave.",
		"*** You are in General.",
		"*** In General: Q1TEST",
	}, a.screen.take())

	var b = new(user).open(s, "Q2TEST")
	b.screen.take()
	assert.Equal(t, []string{"*** Q2TEST has joined General."}, a.screen.take())

	a.say("hello")
	assert.Equal(t, []string{"<Q1TEST> hello"}, b.screen.take())
	assert.Empty(t, a.screen.take(), "no echo")

	b.say("/join ragchew")
	assert.Equal(t, []string{"*** Q2TEST has moved to ragchew."}, a.screen.take())
	assert.Equal(t, []string{"*** You are in ragchew.", "*** In ragchew: Q2TEST"}, b.screen.take())

	a.say("anyone?")
	assert.Empty(t, b.screen.take(), "rooms are apart")

	a.say("/rooms")
	assert.Equal(t, []string{"*** Rooms: General (1), ragchew (1)"}, a.screen.take())

	a.say("/join RAGCHEW")
	assert.Equal(t, []string{"*** You are in ragchew.", "*** In ragchew: Q1TEST, Q2TEST"}, a.screen.take(), "a room keeps its first name")

	a.say("/msg q2test psst")
	assert.Contains(t, b.screen.take(), "*Q1TEST* psst")

	a.say("/msg Q9TEST hello?")
	assert.Equal(t, []string{"*** Q9TEST is not in chat."}, a.screen.take())

	b.say("/quit")
	assert.True(t, b.exited)
	assert.Equal(t, []string{"*** Q2TEST has left chat."}, a.screen.take())

	a.app.Close()
	assert.Empty(t, s.Rooms())
}

func TestChatCommands(t *testing.T) {
	var s, _ = newServer(t, DefaultConfig())
	var a = new(user).open(s, "Q1TEST")
	a.screen.take()

	for line, want := range map[string]string{
		"/help":          "*** Commands:",
		"/bogus":         "*** Unknown command.",
		"/join":          "*** Join which room?",
		"/join a b":      "*** A room's name",
		"/join General":  "*** You are already in General.",
		"/who nowhere":   "*** Nobody is in nowhere.",
		"/msg":           "*** Usage:",
		"/msg Q2TEST   ": "*** Usage:",
	} {
		a.say(line)

		var got = a.screen.take()
		require.NotEmpty(t, got, line)
		assert.True(t, strings.HasPrefix(got[0], want), "%s: %q", line, got)
	}
}

func TestChatRateLimit(t *testing.T) {
	var cfg = DefaultConfig()
	cfg.RateLines = 2
	cfg.RateWindow = 10 * time.Second

	var s, c = newServer(t, cfg)
	var a = new(user).open(s, "Q1TEST")
	var b = new(user).open(s, "Q2TEST")
	b.screen.take()
	a.screen.take()

	a.say("one")
	a.say("two")
	a.say("three")
	a.say("four")
	assert.Equal(t, []string{"*** Slow down: that line was not sent."}, a.screen.take(), "warned once")
	assert.Equal(t, []string{"<Q1TEST> one", "<Q1TEST> two"}, b.screen.take())

	c.now = c.now.Add(10 * time.Second)
	a.say("five")
	assert.Equal(t, []string{"<Q1TEST> five"}, b.screen.take())
}

func TestChatAnnounceAndRooms(t *testing.T) {
	var joins, leaves []string

	var cfg = DefaultConfig()
	cfg.OnJoin = func(u, r string) { joins = append(joins, u+" "+r) }
	cfg.OnLeave = func(u, r string) { leaves = append(leaves, u+" "+r) }

	var s, _ = newServer(t, cfg)
	var a = new(user).open(s, "Q1TEST")
	var b = new(user).open(s, "Q1TEST") // The same user, twice.
	a.screen.take()
	b.screen.take()

	assert.Equal(t, []Room{{Name: "General", Users: []string{"Q1TEST"}}}, s.Rooms())

	s.Announce("going down for maintenance")
	assert.Equal(t, []string{"*** Announcement: going down for maintenance"}, a.screen.take())
	assert.Equal(t, []string{"*** Announcement: going down for maintenance"}, b.screen.take())

	b.app.Close()
	b.app.Close() // Twice is harmless.
	assert.Equal(t, []string{"Q1TEST General", "Q1TEST General"}, joins)
	assert.Equal(t, []string{"Q1TEST General"}, leaves)
}

func TestChatLongLine(t *testing.T) {
	var s, _ = newServer(t, Config{DefaultRoom: "", RateLines: 0, RateWindow: 0, Now: nil, OnJoin: nil, OnLeave: nil})
	var a = new(user).open(s, "Q1TEST")
	var b = new(user).open(s, "Q2TEST")
	b.screen.take()

	a.app.Input([]byte(strings.Repeat("x", 2*maxLine)))
	assert.Len(t, b.screen.take(), 2)
}

func TestNewRejects(t *testing.T) {
	for _, cfg := range []Config{
		{DefaultRoom: "no spaces", RateLines: 0, RateWindow: 0, Now: nil, OnJoin: nil, OnLeave: nil},
		{DefaultRoom: "General", RateLines: 1, RateWindow: 0, Now: nil, OnJoin: nil, OnLeave: nil},
		{DefaultRoom: "General", RateLines: -1, RateWindow: time.Second, Now: nil, OnJoin: nil, OnLeave: nil},
	} {
		var _, err = New(cfg)
		require.Error(t, err)
	}
}

func FuzzChat(f *testing.F) {
	f.Add("hello\r/join x\r/who\r/rooms\r/msg Q2TEST hi\r/leave\r/quit\r")

	f.Fuzz(func(t *testing.T, text string) {
		var s, err = New(DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}

		var a = new(user).open(s, "Q1TEST")
		var b = new(user).open(s, "Q2TEST")

		a.app.Input([]byte(text))
		b.app.Input([]byte(text))
		a.app.Close()
		b.app.Close()

		if len(s.Rooms()) != 0 {
			t.Fatal("someone was left behind")
		}
	})
}
