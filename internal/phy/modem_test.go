// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModemString(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		m    Modem
		want string
	}{
		{AFSK, "AFSK"},
		{Baseband, "BASEBAND"},
		{Scramble, "SCRAMBLE"},
		{QPSK, "QPSK"},
		{PSK8, "8PSK"},
		{Off, "OFF"},
		{QAM16, "16QAM"},
		{QAM64, "64QAM"},
		{AIS, "AIS"},
		{EAS, "EAS"},
		{BPSK, "BPSK"},
		{Modem(42), "modem_t(42)"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.m.String())
	}
}

// G3RUH scrambles the line for 9600 baud, and the NinoTNC's BPSK does too;
// no other modem does.
func TestModemLineScrambled(t *testing.T) {
	t.Parallel()

	for _, m := range []Modem{AFSK, Baseband, Scramble, QPSK, PSK8, Off, QAM16, QAM64, AIS, EAS, BPSK} {
		assert.Equal(t, m == Scramble || m == BPSK, m.LineScrambled(), m.String())
	}
}
