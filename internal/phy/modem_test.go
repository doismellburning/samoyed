// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModemString(t *testing.T) {
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
