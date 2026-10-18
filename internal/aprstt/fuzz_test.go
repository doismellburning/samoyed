// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aprstt

import (
	"slices"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/testutils"
)

// ttFuzzMaxSequence bounds what FuzzTTSequence tries.  A tone sequence is
// capped at MaxMsgLen buttons, but one in a received packet's info field is
// not, so leave room for a few times that.
const ttFuzzMaxSequence = 4 * MaxMsgLen

// FuzzTTSequence covers the APRStt gateway's handling of a touch tone
// sequence.  Anyone on frequency reaches it with DTMF, and with any packet
// whose info field starts with 't'.  It goes past parsing to where the parsed
// state is used: the user table, the APRStt 3 suffix lookup, the environment
// for the TTCMD script, and the object reports for the client applications,
// the IGate and the radio, which are stubbed.  A known user is heard first, so
// that a suffix or an abbreviation has someone to find.
func FuzzTTSequence(f *testing.F) {
	testutils.FuzzQuietly(f)

	for _, testCase := range ttTestCases {
		f.Add(testCase.toneseq)
	}

	// Each of these used to crash.
	f.Add("A1234BA1#") // Two key overlay that doesn't translate.
	f.Add("A#")        // Callsign field with nothing after the A.
	f.Add("C#")        // Comment field with nothing after the C.
	f.Add("AB15#")     // Symbol field with only one digit.
	f.Add("B*A277#")   // Location field matching a pattern of a lone B.
	f.Add("1#")        // Macro that expands to itself.

	var ttlocs = append(slices.Clone(aprs_tt_test_config),
		&Loc{Type: LocPoint, Pattern: "B", Point: PointLoc{Lat: 12.75, Lon: 56.75}}, //nolint:exhaustruct_v5
		&Loc{Type: LocMacro, Pattern: "1", Macro: MacroLoc{Definition: "1"}},        //nolint:exhaustruct_v5
	)

	f.Fuzz(func(t *testing.T, msg string) {
		// Both callers check for at least one character first.
		if msg == "" || len(msg) > ttFuzzMaxSequence {
			t.Skip()
		}

		var cfg Config
		cfg.Locs = ttlocs
		cfg.ObjSendToApp = 1
		cfg.ObjSendToIG = 1
		cfg.ObjXmitVia = "WIDE2-1"
		cfg.RetainTime = 80 * 60
		cfg.NumXmits = 1
		cfg.Status[1] = "/off duty"

		var discard = func(int, *ax25.Packet) {}
		var transmit = func(int, int, *ax25.Packet) {}

		var g = NewGateway(ttMyCalls("Q1TEST"), &cfg, nil, discard, discard, nil, transmit, nil, 0)

		g.Sequence(t.Context(), 0, "A9A2B42A7A7C71#") // WB4APR/7
		g.Sequence(t.Context(), 0, msg)

		// The radio's reports are due straight away.
		g.users.background()
	})
}
