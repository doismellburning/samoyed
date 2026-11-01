// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRLineString(t *testing.T) {
	t.Parallel()

	var r = new(RLine)

	r.When = time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC)
	r.BBS = "Q1BBS"
	r.HRoute = maybe.Just("#77.GBR.EURO")

	assert.Equal(t, "R:261101/0905Z @:Q1BBS.#77.GBR.EURO", r.String())

	r.Number = maybe.Just(1234)
	r.MID = maybe.Just("1234_Q1BBS")
	r.HRoute = maybe.Nothing[string]()

	assert.Equal(t, "R:261101/0905Z @:Q1BBS #:1234 $:1234_Q1BBS", r.String())
}

func TestParseRLine(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		line string
		want RLine
	}{
		{
			line: "R:261101/0905Z @:Q1BBS.#77.GBR.EURO #:1234 [Somewhere] $:1234_Q1BBS",
			want: RLine{
				When: time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC), BBS: "Q1BBS", HRoute: maybe.Just("#77.GBR.EURO"),
				Number: maybe.Just(1234), MID: maybe.Just("1234_Q1BBS"), Rest: "[Somewhere]",
			},
		},
		{
			line: "R:261101/0905z @:q1bbs",
			want: RLine{
				When: time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC), BBS: "Q1BBS", HRoute: maybe.Nothing[string](),
				Number: maybe.Nothing[int](), MID: maybe.Nothing[string](), Rest: "",
			},
		},
		{
			// No @:, as older BBSes wrote it.
			line: "R:261101/0905 42@Q3BBS.GBR.EURO Town",
			want: RLine{
				When: time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC), BBS: "Q3BBS", HRoute: maybe.Just("GBR.EURO"),
				Number: maybe.Nothing[int](), MID: maybe.Nothing[string](), Rest: "Town",
			},
		},
		{
			line: "R:261101/0905Z Q4BBS",
			want: RLine{
				When: time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC), BBS: "Q4BBS", HRoute: maybe.Nothing[string](),
				Number: maybe.Nothing[int](), MID: maybe.Nothing[string](), Rest: "",
			},
		},
	}

	for _, tt := range tests {
		var got, err = ParseRLine(tt.line)
		require.NoError(t, err, tt.line)
		assert.Equal(t, tt.want, got, tt.line)
	}
}

func TestParseRLineRefusesNonsense(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"", "Hello", "R:", "R:yesterday @:Q1BBS", "R:261101/0905Z", "R:261101/0905Z @:Q1!BBS"} {
		var _, err = ParseRLine(line)
		require.ErrorIs(t, err, ErrBadRLine, "%q", line)
	}
}

func TestRLineRoundTrip(t *testing.T) {
	t.Parallel()

	var r, err = ParseRLine("R:261101/0905Z @:Q1BBS.#77.GBR.EURO #:1234 $:1234_Q1BBS")
	require.NoError(t, err)
	assert.Equal(t, "R:261101/0905Z @:Q1BBS.#77.GBR.EURO #:1234 $:1234_Q1BBS", r.String())
}

func TestSplitRLinesAndPath(t *testing.T) {
	t.Parallel()

	var body = []byte("R:261101/1000Z @:Q1BBS.GBR\rR:261101/0905Z @:Q3BBS\rHello.\rR: not a header down here\r")

	var rlines, rest = SplitRLines(body)
	assert.Equal(t, []string{"R:261101/1000Z @:Q1BBS.GBR", "R:261101/0905Z @:Q3BBS"}, rlines)
	assert.Equal(t, "Hello.\rR: not a header down here\r", string(rest))

	assert.Equal(t, []string{"Q1BBS", "Q3BBS"}, Path(body), "newest first, and only from the top")

	assert.Empty(t, Path([]byte("Hello.\r")))

	var _, afterBlank = SplitRLines([]byte("R:261101/1114Z 4@Q3BPQ.#33.GBR.EURO LinBPQ6.0.26\r\rWaiting.\r\r"))
	assert.Equal(t, "Waiting.\r\r", string(afterBlank), "one blank line after the R: lines is theirs, not the text's")

	var _, noRLines = SplitRLines([]byte("\rHello.\r"))
	assert.Equal(t, "\rHello.\r", string(noRLines), "but a blank line with no R: lines above it is text")
	assert.Equal(t, []string{"Q3BBS"}, Path([]byte("R:garbled\rR:261101/0905Z @:Q3BBS\rHi\r")), "an unreadable line is passed over")
}

func TestPrependRLine(t *testing.T) {
	t.Parallel()

	var r = new(RLine)

	r.When = time.Date(2026, 11, 1, 9, 5, 0, 0, time.UTC)
	r.BBS = "Q1BBS"

	var body = PrependRLine([]byte("R:261101/0800Z @:Q3BBS\rHello.\r"), *r)
	assert.Equal(t, "R:261101/0905Z @:Q1BBS\rR:261101/0800Z @:Q3BBS\rHello.\r", string(body))
	assert.Equal(t, []string{"Q1BBS", "Q3BBS"}, Path(body))
}

func TestValidHRoute(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"Q1BBS", "Q1BBS.#77.GBR.EURO", "GBR", "#NCA.CA.USA.NOAM", "WW"} {
		assert.True(t, ValidHRoute(s), s)
	}

	for _, s := range []string{"", ".", "Q1BBS.", ".GBR", "Q1BBS..GBR", "#", "Q1BBS.#", "Q1 BBS", "Q1BBS.G#BR"} {
		assert.False(t, ValidHRoute(s), s)
	}
}

func TestAtParts(t *testing.T) {
	t.Parallel()

	var address, err = ParseAddress("Q1TEST@Q1BBS.#77.GBR.EURO")
	require.NoError(t, err)

	assert.Equal(t, []string{"EURO", "GBR", "#77", "Q1BBS"}, address.AtParts())
	assert.Equal(t, "Q1BBS", address.BBS())

	var bare, _ = ParseAddress("Q1TEST")
	assert.Empty(t, bare.AtParts())
	assert.Empty(t, bare.BBS())
}

func FuzzParseRLine(f *testing.F) {
	f.Add("R:261101/0905Z @:Q1BBS.#77.GBR.EURO #:1234 [Town] $:1234_Q1BBS")
	f.Add("R:261101/0905 42@Q3BBS.GBR.EURO Town")
	f.Add("R:yesterday")

	f.Fuzz(func(t *testing.T, line string) {
		var r, err = ParseRLine(line)
		if err != nil {
			return
		}

		if r.BBS == "" {
			t.Fatal("parsed with no BBS")
		}

		// What it writes reads back as the same BBS and time.
		var again, againErr = ParseRLine(r.String())
		if againErr != nil || again.BBS != r.BBS || !again.When.Equal(r.When) {
			t.Fatalf("%q does not survive a round trip: %+v, %v", r.String(), again, againErr)
		}

		if strings.ContainsAny(r.String(), "\r\n") {
			t.Fatalf("%q has a line ending in it", r.String())
		}
	})
}

func TestStamp(t *testing.T) {
	t.Parallel()

	var when = time.Date(2026, 11, 1, 9, 5, 0, 0, time.FixedZone("CET", 3600))
	var earlier = []byte("R:261031/1200Z @:Q2BBS.GBR.EURO\rHi.\r")

	var stamped = Stamp(earlier, "q1bbs-3", maybe.Just("#77.GBR.EURO"), when)

	assert.Equal(t, "R:261101/0805Z @:Q1BBS.#77.GBR.EURO\rR:261031/1200Z @:Q2BBS.GBR.EURO\rHi.\r", string(stamped))
	assert.Equal(t, []string{"Q1BBS", "Q2BBS"}, Path(stamped))

	assert.Equal(t, "R:261101/0805Z @:Q1BBS\r\r", string(Stamp(nil, "Q1BBS", maybe.Nothing[string](), when)))
}

// A message's R: lines are kept apart from its text by a blank line, as
// LinBPQ's are, and as XRouter needs: without it, XRouter takes the text for
// more header lines, and doesn't show it.
func TestStampSeparatesTheRLinesFromTheText(t *testing.T) {
	t.Parallel()

	var when = time.Date(2026, 11, 1, 18, 52, 0, 0, time.UTC)

	var originated = Stamp([]byte("Hello from samoyed-bbs.\r"), "Q2BBS", maybe.Just("#77.GBR.EURO"), when)

	assert.Equal(t, "R:261101/1852Z @:Q2BBS.#77.GBR.EURO\r\rHello from samoyed-bbs.\r", string(originated))

	// A message that has been somewhere already has its blank line, so
	// another R: line just goes on top.
	var forwarded = Stamp(originated, "Q5NOD", maybe.Nothing[string](), when.Add(time.Minute))

	assert.Equal(t, "R:261101/1853Z @:Q5NOD\rR:261101/1852Z @:Q2BBS.#77.GBR.EURO\r\rHello from samoyed-bbs.\r", string(forwarded))

	var rlines, rest = SplitRLines(forwarded)

	assert.Len(t, rlines, 2)
	assert.Equal(t, "Hello from samoyed-bbs.\r", string(rest), "the blank line goes with the R: lines")
}
