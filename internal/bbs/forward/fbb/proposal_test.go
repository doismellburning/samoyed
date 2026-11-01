// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package fbb

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFB(t *testing.T) {
	t.Parallel()

	// The proposal in FBB's documentation, with Q-callsigns.
	var offer, err = ParseFB("FB P Q6FBB Q1GHV.FFPC.FRA.EU Q1MVP 24657_Q6FBB 1345")

	require.NoError(t, err)
	assert.Equal(t, mail.Personal, offer.Message.Type)
	assert.Equal(t, "Q6FBB", offer.Message.From)
	assert.Equal(t, "Q1MVP@Q1GHV.FFPC.FRA.EU", offer.Message.To.String())
	assert.Equal(t, maybe.Just("24657_Q6FBB"), offer.Message.MID)
	assert.Equal(t, 1345, offer.Size)

	var bulletin, bullErr = ParseFB("fb b q6fbb fra fbb 22_456_q6fbb 8548")

	require.NoError(t, bullErr)
	assert.Equal(t, mail.Bulletin, bulletin.Message.Type)
	assert.Equal(t, "FBB@FRA", bulletin.Message.To.String())
	assert.Equal(t, maybe.Just("22_456_Q6FBB"), bulletin.Message.MID)
}

func TestParseFBRefuses(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		"",
		"FB P Q6FBB Q1GHV Q1MVP 24657_Q6FBB",
		"FB P Q6FBB Q1GHV Q1MVP 24657_Q6FBB 1345 extra",
		"FB X Q6FBB Q1GHV Q1MVP 24657_Q6FBB 1345",
		"FB P Q6FBB Q1GHV Q1-MVP 24657_Q6FBB 1345",
		"FB P Q6FBB Q1GHV Q1MVP 24657_Q6FBB -1",
		"FB P Q6FBB Q1GHV Q1MVP 24657_Q6FBB big",
		"FA P Q6FBB Q1GHV Q1MVP 24657_Q6FBB 1345",
	} {
		var _, err = ParseFB(line)

		require.ErrorIs(t, err, ErrBadProposal, line)
	}
}

func TestFBForRoundTrips(t *testing.T) {
	t.Parallel()

	var to, _ = mail.ParseAddress("Q1TEST@Q2BBS.#12.GBR.EURO")

	var msg = new(mail.Message)

	msg.Type = mail.Personal
	msg.From = "Q9TEST"
	msg.To = to
	msg.MID = "5_Q1BBS"
	msg.Body = []byte("Hello.\r")

	var line = FBFor(*msg, "Q2BBS")

	assert.Equal(t, "FB P Q9TEST Q2BBS.#12.GBR.EURO Q1TEST 5_Q1BBS 7", line)

	var offer, err = ParseFB(line)

	require.NoError(t, err)
	assert.Equal(t, msg.To, offer.Message.To)
	assert.Equal(t, 7, offer.Size)

	var local, _ = mail.ParseAddress("Q1TEST")

	msg.To = local

	assert.Equal(t, "FB P Q9TEST Q2BBS Q1TEST 5_Q1BBS 7", FBFor(*msg, "Q2BBS"), "the partner stands in for a missing @")
}

func TestChecksum(t *testing.T) {
	t.Parallel()

	// The sum of every byte, CRs included, and the checksum, is 0 mod 256.
	var lines = []string{"FB P Q6FBB Q1GHV Q1MVP 24657_Q6FBB 1345", "FB B Q6FBB FRA FBB 22_456_Q6FBB 8548"}

	var sum = Checksum(lines)

	for _, line := range lines {
		for i := range len(line) {
			sum += line[i]
		}

		sum += '\r'
	}

	assert.Equal(t, byte(0), sum)
	assert.Equal(t, byte(0), Checksum(nil))
}

func TestParseFS(t *testing.T) {
	t.Parallel()

	var answers, err = ParseFS("FS +-=", 3)

	require.NoError(t, err)
	assert.Equal(t, []Answer{Accept, Refuse, Defer}, answers)

	var v1, v1Err = ParseFS("FS +rhe", 4)

	require.NoError(t, v1Err)
	assert.Equal(t, []Answer{Accept, Reject, Hold, FormatError}, v1)

	for _, bad := range []string{"FS ++", "FS +-+-", "FS +X+", "+-+", "FF"} {
		var _, badErr = ParseFS(bad, 3)

		require.ErrorIs(t, badErr, ErrBadAnswer, bad)
	}
}

func FuzzParseFB(f *testing.F) {
	for _, seed := range []string{"FB P Q6FBB Q1GHV.FFPC.FRA.EU Q1MVP 24657_Q6FBB 1345", "FB B A B C D 0", "FB", ""} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		var offer, err = ParseFB(line)
		if err != nil {
			return
		}

		var msg = new(mail.Message)

		msg.Type = offer.Message.Type
		msg.From = offer.Message.From
		msg.To = offer.Message.To
		msg.MID = maybe.FromMaybe("", offer.Message.MID)
		msg.Body = make([]byte, offer.Size%100000)

		var again, againErr = ParseFB(FBFor(*msg, "Q2BBS"))

		require.NoError(t, againErr, "an FB line read is one that can be written and read again")
		assert.Equal(t, offer.Message.To, again.Message.To)
	})
}
