// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mbl

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProposal(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		line string
		typ  mail.Type
		to   string
		from string
		mid  maybe.Maybe[string]
	}{
		{"SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST $123_Q2BBS", mail.Personal, "Q1TEST@Q1BBS.#77.GBR.EURO", "Q2TEST", maybe.Just("123_Q2BBS")},
		{"sp q1test@q1bbs < q2test $abc", mail.Personal, "Q1TEST@Q1BBS", "Q2TEST", maybe.Just("ABC")},
		{"SB ALL @ GBR < Q2TEST", mail.Bulletin, "ALL@GBR", "Q2TEST", maybe.Nothing[string]()},
		{"ST 12345 @ NTSCA < Q2TEST $9_Q2BBS", mail.Traffic, "12345@NTSCA", "Q2TEST", maybe.Just("9_Q2BBS")},
		{"S Q1TEST < Q2TEST", mail.Personal, "Q1TEST", "Q2TEST", maybe.Nothing[string]()},
		{"SP Q1TEST<Q2TEST", mail.Personal, "Q1TEST", "Q2TEST", maybe.Nothing[string]()},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			var got, err = ParseProposal(tt.line)

			require.NoError(t, err)
			assert.Equal(t, tt.typ, got.Type)
			assert.Equal(t, tt.to, got.To.String())
			assert.Equal(t, tt.from, got.From)
			assert.Equal(t, tt.mid, got.MID)
		})
	}
}

func TestParseProposalRefuses(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"", "L", "SX Q1TEST < Q2TEST", "SP Q1TEST", "SP Q1TEST <", "SP < Q2TEST", "SP Q1-TEST < Q2TEST", "SPX Q1TEST < Q2TEST"} {
		var _, err = ParseProposal(line)

		require.ErrorIs(t, err, ErrBadProposal, line)
	}
}

func TestProposalForRoundTrips(t *testing.T) {
	t.Parallel()

	var to, _ = mail.ParseAddress("Q1TEST@Q1BBS.#77.GBR.EURO")

	var msg = new(mail.Message)

	msg.Type = mail.Personal
	msg.To = to
	msg.From = "Q2TEST"
	msg.MID = "5_Q2BBS"

	assert.Equal(t, "SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST $5_Q2BBS", ProposalFor(*msg, true))
	assert.Equal(t, "SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST", ProposalFor(*msg, false))

	var back, err = ParseProposal(ProposalFor(*msg, true))

	require.NoError(t, err)
	assert.Equal(t, msg.To, back.To)
	assert.Equal(t, msg.From, back.From)
	assert.Equal(t, maybe.Just(msg.MID), back.MID)
}

func FuzzParseProposal(f *testing.F) {
	for _, seed := range []string{"SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST $123_Q2BBS", "SB ALL < Q2TEST", "S <", "S", "ST x@y<z $"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		var got, err = ParseProposal(line)
		if err != nil {
			return
		}

		var msg = new(mail.Message)

		msg.Type = got.Type
		msg.To = got.To
		msg.From = got.From
		msg.MID = maybe.FromMaybe("", got.MID)

		var again, againErr = ParseProposal(ProposalFor(*msg, true))

		require.NoError(t, againErr, "a proposal read is one that can be written and read again")
		assert.Equal(t, got.To, again.To)
		assert.Equal(t, got.Type, again.Type)
	})
}
