// SPDX-FileCopyrightText: 2025 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

import (
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

// The callers of phgDataExtension and compressedPosition only check that at
// least one of power, height and gain was specified, so the others arrive
// absent.  That used to be the G_UNKNOWN sentinel, which reached Sqrt/Log2, and
// the resulting NaN converted to a NUL byte in the middle of the transmitted
// packet.

func Test_phg_data_extension_partially_specified(t *testing.T) {
	var none = maybe.Nothing[int]()
	var some = maybe.Just[int]

	assert.Equal(t, "PHG7368", phgDataExtension(some(50), some(100), some(6), "N"), "all specified")

	assert.Equal(t, "PHG7000", phgDataExtension(some(50), none, none, ""), "power only")
	assert.Equal(t, "PHG0100", phgDataExtension(none, some(20), none, ""), "height only")
	assert.Equal(t, "PHG0060", phgDataExtension(none, none, some(6), ""), "gain only")
	assert.Equal(t, "PHG0008", phgDataExtension(none, none, none, "N"), "direction only")
}

// Gain is a single digit, so anything above 9 dB goes out as 9, as power
// does.  Dire Wolf sent it as 0, claiming no gain at all.
func Test_phg_data_extension_high_gain(t *testing.T) {
	var none = maybe.Nothing[int]()

	assert.Equal(t, "PHG0090", phgDataExtension(none, none, maybe.Just(9), ""))
	assert.Equal(t, "PHG0090", phgDataExtension(none, none, maybe.Just(12), ""))
}

func Test_phg_data_extension_directivity(t *testing.T) {
	var none = maybe.Nothing[int]()

	for dir, want := range map[string]string{
		"":     "PHG0000",
		"omni": "PHG0000",
		"NE":   "PHG0001",
		"E":    "PHG0002",
		"SE":   "PHG0003",
		"S":    "PHG0004",
		"SW":   "PHG0005",
		"W":    "PHG0006",
		"NW":   "PHG0007",
		"N":    "PHG0008",
		"nw":   "PHG0007",
		"Se":   "PHG0003",
	} {
		assert.Equal(t, want, phgDataExtension(none, none, none, dir), dir)
	}
}

func Test_EncodePosition_partially_specified_phg(t *testing.T) {
	var none = maybe.Nothing[int]()
	var some = maybe.Just[int]
	var noFloat = maybe.Nothing[float64]()

	var result = EncodePosition(false, false, 42+34.61/60, -(71 + 26.47/60), 0, none, 'D', '&',
		some(50), none, none, "", none, some(0), noFloat, noFloat, noFloat, "")
	assert.Equal(t, "!4234.61ND07126.47W&PHG7000", result)

	// Compressed positions carry the same three values as a radio range.

	result = EncodePosition(false, true, 42+34.61/60, -(71 + 26.47/60), 0, none, 'D', '&',
		some(50), none, none, "", none, some(0), noFloat, noFloat, noFloat, "")
	assert.NotContains(t, result, "\x00", "compressed radio range")
	assert.Equal(t, EncodePosition(false, true, 42+34.61/60, -(71+26.47/60), 0, none, 'D', '&',
		some(50), some(0), some(0), "", none, some(0), noFloat, noFloat, noFloat, ""), result,
		"absent height/gain should encode as the unspecified defaults")
}

// An explicitly zero frequency spec asks for "Toff" and a zero offset; it is
// not the same as having no frequency spec to send.  The two used to be
// indistinguishable, because a caller with nothing to say passed three zeroes
// and the encoders guarded on all three being non-zero.

func Test_EncodePosition_explicit_zero_frequency_spec(t *testing.T) {
	var none = maybe.Nothing[int]()
	var noFloat = maybe.Nothing[float64]()
	var zero = maybe.Just(0.0)

	assert.Equal(t, "!4234.61ND07126.47W&Toff +000 ",
		EncodePosition(false, false, 42+34.61/60, -(71+26.47/60), 0, none, 'D', '&',
			none, none, none, "", none, maybe.Just(0), zero, zero, zero, ""),
		"explicit zeroes")

	assert.Equal(t, "!4234.61ND07126.47W&",
		EncodePosition(false, false, 42+34.61/60, -(71+26.47/60), 0, none, 'D', '&',
			none, none, none, "", none, maybe.Just(0), noFloat, noFloat, noFloat, ""),
		"nothing to say")
}

// An object report's timestamp is day, hour and minute in UTC, the "z" says
// so.  It was formatted with Go's "03", the 12-hour clock, and in whatever zone
// the time carried, so an evening report from east of Greenwich came out hours
// off and could not be told from a morning one.
func Test_encode_object_timestamp_is_24_hour_utc(t *testing.T) {
	var eastOfGreenwich = time.FixedZone("UTC+1", 60*60)

	var info = EncodeObject("Q1TEST", false, time.Date(2026, 9, 26, 18, 30, 0, 0, eastOfGreenwich),
		42.5, -71.5, 0, '/', '-',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "",
		maybe.Nothing[int](), maybe.Nothing[int](),
		maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Nothing[float64](), "")

	assert.Equal(t, ";Q1TEST   *261730z", info[:18])
}

// The compressed speed byte is 1.08^(s-33) - 1 knots.  An unrealistically fast
// speed must still come out as printable, as the radio range beside it does,
// rather than running past '~' into bytes that aren't ASCII.
func Test_compressed_position_speed_is_printable(t *testing.T) {
	var none = maybe.Nothing[int]()

	var c = compressedPosition('/', '>', 0, 0, none, none, none, maybe.Just(90), maybe.Just(1000000))
	assert.Equal(t, byte('~'), c.S)
}

// The compressed course wraps however many turns it is given, as the
// uncompressed course/speed extension's does.  Wrapping only once let 720
// degrees through as '{', which marks the byte after it as radio range instead
// of speed.
func Test_compressed_position_course_wraps(t *testing.T) {
	var none = maybe.Nothing[int]()

	for _, degrees := range []int{0, 360, 720, -360, -720} {
		var c = compressedPosition('/', '>', 0, 0, none, none, none, maybe.Just(degrees), maybe.Just(10))
		assert.Equal(t, byte('!'), c.C, degrees)
	}

	var c = compressedPosition('/', '>', 0, 0, none, none, none, maybe.Just(-90), maybe.Just(10))
	assert.Equal(t, byte('!'+68), c.C, "-90, as 270 rounds to 272")
}

// checkSymbol complains about each part of a symbol that isn't valid, but
// leaves the encoding to go ahead.
func Test_checkSymbol(t *testing.T) {
	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	checkSymbol('/', '>')
	checkSymbol('\\', '!')
	checkSymbol('A', '~')
	checkSymbol('9', '#')
	assert.Empty(t, hook.AllEntries())

	checkSymbol('a', '>')
	assert.Contains(t, hook.LastEntry().Message, "Symbol table identifier")

	checkSymbol('/', ' ')
	assert.Contains(t, hook.LastEntry().Message, "Symbol code")
}

// What the encoder makes, the decoder reads back, in each hemisphere and in
// both forms.  A compressed position only round trips to within its
// resolution, and a human-readable one to a hundredth of a minute.
func Test_encode_decode_round_trip(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var none = maybe.Nothing[int]()
	var noFreq = maybe.Nothing[float64]()

	for _, tc := range []struct {
		lat float64
		lon float64
	}{
		{42.6190, -71.3472},
		{-33.8568, 151.2153},
		{51.4779, 0.0015},
		{-0.5, -0.5},
	} {
		for _, compressed := range []bool{false, true} {
			var delta = 1.0 / 6000
			if compressed {
				delta = 0.00001
			}

			var info = EncodePosition(false, compressed, tc.lat, tc.lon, 0, none, '/', '>',
				none, none, none, "", maybe.Just(88), maybe.Just(36), noFreq, noFreq, noFreq, "Round trip")

			var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+info, true), true)

			assertMaybeInDelta(t, maybe.Just(tc.lat), A.Lat, delta, "%s", info)
			assertMaybeInDelta(t, maybe.Just(tc.lon), A.Lon, delta, "%s", info)
			assert.Equal(t, byte('/'), A.SymbolTable, "%s", info)
			assert.Equal(t, byte('>'), A.SymbolCode, "%s", info)
			assertMaybeInDelta(t, maybe.Just(88.0), A.Course, 4, "%s", info) // Compressed course is in steps of 4 degrees.
			assert.Equal(t, "Round trip", A.Comment, "%s", info)

			info = EncodeObject("Q2TEST", compressed, time.Time{}, tc.lat, tc.lon, 0, '/', '>',
				none, none, none, "", none, none, noFreq, noFreq, noFreq, "")

			A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+info, true), true)

			assert.Equal(t, "Object", A.DataTypeDesc, "%s", info)
			assert.Equal(t, "Q2TEST", A.Name, "%s", info)
			assertMaybeInDelta(t, maybe.Just(tc.lat), A.Lat, delta, "%s", info)
			assertMaybeInDelta(t, maybe.Just(tc.lon), A.Lon, delta, "%s", info)
		}
	}
}
