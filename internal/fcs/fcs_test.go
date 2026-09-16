// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fcs

import "testing"

// 0x906e is the published CRC-16/X-25 check value for "123456789".
func TestCalcCheckValue(t *testing.T) {
	var got = Calc([]byte("123456789"))

	if got != 0x906e {
		t.Errorf("Calc(\"123456789\") = %#04x, want %#04x", got, 0x906e)
	}
}

func TestCRC16SeededWithFFFFMatchesCalc(t *testing.T) {
	var data = []byte("Q1TEST>Q2TEST:hello")

	var got = CRC16(data, 0xffff)

	var want = Calc(data)

	if got != want {
		t.Errorf("CRC16(data, 0xffff) = %#04x, Calc(data) = %#04x", got, want)
	}
}

func TestCRC16AccumulatedIsNotTheCRCOfTheConcatenation(t *testing.T) {
	var region1 = []byte("Q1TEST")
	var region2 = []byte("Q2TEST")

	var accumulated = CRC16(region2, CRC16(region1, 0xffff))

	var concatenated = Calc(append(append([]byte{}, region1...), region2...))

	if accumulated == concatenated {
		t.Errorf("accumulated CRC16 = %#04x, matching the CRC of the concatenation", accumulated)
	}
}
