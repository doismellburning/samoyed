// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAddress(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		in   string
		want Address
	}{
		{in: "Q1TEST", want: Address{To: "Q1TEST", At: maybe.Nothing[string]()}},
		{in: " q1test ", want: Address{To: "Q1TEST", At: maybe.Nothing[string]()}},
		{in: "Q1TEST@Q1BBS", want: Address{To: "Q1TEST", At: maybe.Just("Q1BBS")}},
		{in: "q1test@q1bbs.#77.gbr.euro", want: Address{To: "Q1TEST", At: maybe.Just("Q1BBS.#77.GBR.EURO")}},
		{in: "ALL@WW", want: Address{To: "ALL", At: maybe.Just("WW")}},
	}

	for _, tt := range tests {
		var got, err = ParseAddress(tt.in)
		require.NoError(t, err, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
	}
}

func TestParseAddressRefusesNonsense(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "@Q1BBS", "Q1TEST@", "TOOLONGX", "Q1-TEST", "Q1TEST@Q1 BBS", "Q1TEST@" + strings.Repeat("A", MaxAtLen+1)} {
		var _, err = ParseAddress(in)
		require.ErrorIs(t, err, ErrBadAddress, "%q", in)
	}
}

func TestAddressString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Q1TEST", Address{To: "Q1TEST", At: maybe.Nothing[string]()}.String())
	assert.Equal(t, "Q1TEST@Q1BBS", Address{To: "Q1TEST", At: maybe.Just("Q1BBS")}.String())
}

func TestGenerateMID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1_Q1BBS", GenerateMID(1, "q1bbs-3"))
	assert.Equal(t, "123456_Q1BBS", GenerateMID(123456, "Q1BBS"))
	assert.Equal(t, "1234567_Q1BB", GenerateMID(1234567, "Q1BBS"), "cut to %d characters", MaxMIDLen)
}

func TestParseTypeAndStatus(t *testing.T) {
	t.Parallel()

	for _, typ := range []Type{Personal, Bulletin, Traffic} {
		var got, err = ParseType(strings.ToLower(typ.String()))
		require.NoError(t, err)
		assert.Equal(t, typ, got)
	}

	var _, typeErr = ParseType("X")
	require.Error(t, typeErr)

	for _, status := range []Status{New, Read, Forwarded, Killed, Held} {
		var got, err = ParseStatus(status.String())
		require.NoError(t, err)
		assert.Equal(t, status, got)
	}

	var _, statusErr = ParseStatus("X")
	require.Error(t, statusErr)
}

func validMessage() NewMessage {
	var m = new(NewMessage)

	m.Type = Personal
	m.From = "Q2TEST"
	m.To = Address{To: "Q1TEST", At: maybe.Nothing[string]()}
	m.Title = "Hello"
	m.Body = []byte("Hello there.\r")

	return *m
}

func TestValidate(t *testing.T) {
	t.Parallel()

	require.NoError(t, validMessage().Validate())

	var tests = map[string]func(m *NewMessage){
		"bad type":     func(m *NewMessage) { m.Type = 'X' },
		"no sender":    func(m *NewMessage) { m.From = "" },
		"long sender":  func(m *NewMessage) { m.From = "Q2TESTLONG" },
		"no addressee": func(m *NewMessage) { m.To = Address{To: "", At: maybe.Nothing[string]()} },
		"long title":   func(m *NewMessage) { m.Title = strings.Repeat("x", MaxTitleLen+1) },
		"empty MID":    func(m *NewMessage) { m.MID = maybe.Just("") },
		"long MID":     func(m *NewMessage) { m.MID = maybe.Just(strings.Repeat("1", MaxMIDLen+1)) },
	}

	for name, spoil := range tests {
		var m = validMessage()
		spoil(&m)
		require.Error(t, m.Validate(), name)
	}
}

func FuzzParseAddress(f *testing.F) {
	f.Add("Q1TEST@Q1BBS.#77.GBR.EURO")
	f.Add("ALL@WW")
	f.Add("@@")

	f.Fuzz(func(t *testing.T, in string) {
		var a, err = ParseAddress(in)
		if err != nil {
			return
		}

		if a.To == "" || len(a.To) > MaxToLen {
			t.Fatalf("To %q out of bounds", a.To)
		}

		var again, againErr = ParseAddress(a.String())
		if againErr != nil || again != a {
			t.Fatalf("%q does not survive a round trip: %v, %v", a.String(), again, againErr)
		}
	})
}
