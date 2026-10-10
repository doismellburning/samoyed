// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

import (
	"testing"
)

func FuzzProposal(f *testing.F) {
	f.Add("FA P Q4TEST Q2BBS.#TEST Q5TEST 12_Q1BBS 345")
	f.Add("FC EM ABCDEF123456 527 123 0")

	f.Fuzz(func(t *testing.T, line string) {
		var p, err = parseProposal(line)
		if err != nil {
			return
		}

		var again, aerr = parseProposal(p.line())
		if aerr != nil || again != p {
			t.Fatalf("%q parsed as %+v, which reads back as %+v (%v)", line, p, again, aerr)
		}
	})
}

func FuzzSID(f *testing.F) {
	f.Add("[BPQ-6.0.24.1-B1FWIHJM$]")
	f.Add("[FBB-7.0.10-AB1FHMRX$]")

	f.Fuzz(func(t *testing.T, line string) {
		var sid, ok = ParseSID(line)
		if !ok {
			return
		}

		_ = negotiate(SID{Software: "SAMOYED", Version: "1", Flags: defaultFlags}, sid)
	})
}

func FuzzB2F(f *testing.F) {
	var m = message(TypePersonal, "Q4TEST", "Q5TEST", "Q2BBS", "Hi", "Body\n")
	m.BID = "ABC"
	f.Add(encodeB2F(m, "Q1BBS"))

	f.Fuzz(func(t *testing.T, data []byte) {
		var got, err = decodeB2F(data)
		if err != nil {
			return
		}

		if got.BID == "" || got.From == "" || got.To == "" {
			t.Fatalf("decoded a message missing a header: %+v", got)
		}
	})
}
