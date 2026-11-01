// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package routing

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// self is the BBS doing the routing.
func self() Self { return Self{Call: "Q1BBS"} }

// partners are a BBS in the same region, one for the rest of the country,
// one for Europe, and a default route.
func partners() []Partner {
	return []Partner{
		{Call: "Q2BBS", Covers: []string{"#77.GBR.EURO"}, Floods: []string{"GBR", "#77"}},
		{Call: "Q3BBS-1", Covers: []string{"GBR.EURO"}, Floods: []string{"GBR", "EURO", "WW"}},
		{Call: "Q4BBS", Covers: []string{"EURO", "Q8BBS"}, Floods: []string{"EURO", "WW"}},
		{Call: "Q5BBS", Covers: []string{Wildcard}, Floods: []string{Wildcard}},
	}
}

func message(typ mail.Type, to string, body string) mail.NewMessage {
	var address, err = mail.ParseAddress(to)
	if err != nil {
		panic(err)
	}

	var m = new(mail.NewMessage)

	m.Type = typ
	m.From = "Q9TEST"
	m.To = address
	m.Body = []byte(body)

	return *m
}

func forwardedTo(calls ...string) Decision {
	return Decision{Local: false, Partners: calls, Held: maybe.Nothing[string]()}
}

func TestRoutePersonalMail(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name string
		to   string
		want Decision
	}{
		{"for a user here", "Q1TEST", Local()},
		{"for a user here, said in full", "Q1TEST@Q1BBS.#77.GBR.EURO", Local()},
		{"for this BBS with an SSID-less match", "Q1TEST@q1bbs", Local()},
		{"in the same region", "Q1TEST@Q6BBS.#77.GBR.EURO", forwardedTo("Q2BBS")},
		{"elsewhere in the country", "Q1TEST@Q6BBS.#12.GBR.EURO", forwardedTo("Q3BBS")},
		{"elsewhere in Europe", "Q1TEST@Q6BBS.FRA.EURO", forwardedTo("Q4BBS")},
		{"elsewhere in the world", "Q1TEST@Q6BBS.USA.NOAM", forwardedTo("Q5BBS")},
		{"for a partner itself, which beats its region", "Q1TEST@Q3BBS.#77.GBR.EURO", forwardedTo("Q3BBS")},
		{"for a BBS a partner covers by name", "Q1TEST@Q8BBS.#77.GBR.EURO", forwardedTo("Q4BBS")},
		{"for a bare BBS nobody names", "Q1TEST@Q6BBS", forwardedTo("Q5BBS")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Route(message(mail.Personal, tt.to, "Hi.\r"), maybe.Nothing[string](), self(), partners()))
		})
	}
}

func TestRouteTrafficIsRoutedAsPersonalMail(t *testing.T) {
	t.Parallel()

	assert.Equal(t, forwardedTo("Q3BBS"), Route(message(mail.Traffic, "Q1TEST@Q6BBS.GBR.EURO", ""), maybe.Nothing[string](), self(), partners()))
}

func TestRouteHoldsWhatNobodyCovers(t *testing.T) {
	t.Parallel()

	var got = Route(message(mail.Personal, "Q1TEST@Q6BBS.USA.NOAM", ""), maybe.Nothing[string](), self(), partners()[:3])

	assert.False(t, got.Local)
	assert.Empty(t, got.Partners)
	assert.Equal(t, maybe.Just("no partner covers Q6BBS.USA.NOAM"), got.Held)

	var none = Route(message(mail.Personal, "Q1TEST@Q6BBS", ""), maybe.Nothing[string](), self(), nil)
	assert.Equal(t, maybe.Just("no partner covers Q6BBS"), none.Held)
}

func TestRouteNeverSendsMailBackTheWayItCame(t *testing.T) {
	t.Parallel()

	// It came from Q2BBS, so the region's mail goes to the next best fit.
	var fromOrigin = Route(message(mail.Personal, "Q1TEST@Q6BBS.#77.GBR.EURO", ""), maybe.Just("Q2BBS-4"), self(), partners())
	assert.Equal(t, forwardedTo("Q3BBS"), fromOrigin)

	// Q3BBS and Q4BBS have had it already, by its R: lines.
	var body = "R:261101/1000Z @:Q4BBS.FRA.EURO\rR:261101/0900Z @:Q3BBS.GBR.EURO\rHi.\r"
	var fromPath = Route(message(mail.Personal, "Q1TEST@Q6BBS.#12.GBR.EURO", body), maybe.Nothing[string](), self(), partners())
	assert.Equal(t, forwardedTo("Q5BBS"), fromPath)

	// With nobody else, it is held.
	var nowhere = Route(message(mail.Personal, "Q1TEST@Q6BBS.#12.GBR.EURO", body), maybe.Just("Q5BBS"), self(), partners())
	assert.True(t, nowhere.Held.IsJust())
}

func TestRouteTiesGoToTheFirstPartner(t *testing.T) {
	t.Parallel()

	var twins = []Partner{
		{Call: "Q2BBS", Covers: []string{"GBR.EURO"}, Floods: nil},
		{Call: "Q3BBS", Covers: []string{"GBR.EURO"}, Floods: nil},
	}

	assert.Equal(t, forwardedTo("Q2BBS"), Route(message(mail.Personal, "Q1TEST@Q6BBS.GBR.EURO", ""), maybe.Nothing[string](), self(), twins))
}

func TestRouteBulletins(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name string
		to   string
		want []string
	}{
		{"for the country", "ALL@GBR", []string{"Q2BBS", "Q3BBS", "Q5BBS"}},
		{"for the world", "KEPS@WW", []string{"Q3BBS", "Q4BBS", "Q5BBS"}},
		{"for the region", "CLUB@#77", []string{"Q2BBS", "Q5BBS"}},
		{"for an area nobody floods but the wildcard", "ALL@FRA", []string{"Q5BBS"}},
		{"with no distribution", "ALL", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got = Route(message(mail.Bulletin, tt.to, ""), maybe.Nothing[string](), self(), partners())

			assert.True(t, got.Local, "a bulletin is always kept here too")
			assert.Equal(t, tt.want, got.Partners)
			assert.True(t, got.Held.IsNothing())
		})
	}
}

func TestRouteBulletinsSkipWhereTheyHaveBeen(t *testing.T) {
	t.Parallel()

	var body = "R:261101/0900Z @:Q3BBS.GBR.EURO\rNews.\r"

	var got = Route(message(mail.Bulletin, "ALL@GBR", body), maybe.Just("Q2BBS"), self(), partners())
	assert.Equal(t, []string{"Q5BBS"}, got.Partners)
}

func TestLocal(t *testing.T) {
	t.Parallel()

	var d = Local()

	require.True(t, d.Local)
	assert.Empty(t, d.Partners)
	assert.True(t, d.Held.IsNothing())
}
