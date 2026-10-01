// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
)

// Test that decode_aprs does not panic on an AX.25 UI frame with an empty
// information field, as sent by linbpq ID broadcasts (issue #504).
func Test_decode_aprs_empty_info(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>ID:", true)
	assert.NotNil(t, pp)

	// Must not panic, and must return a populated struct.
	var A = aprsDecoder.Decode(pp, true)
	assert.NotNil(t, A)
	assert.Equal(t, "AX.25 UI frame with empty information field", A.DataTypeDesc)
	assert.Equal(t, "Q1TEST", A.Src)
	assert.Equal(t, "ID", A.Dest)
}

// !DAO! adds a digit of resolution to a position that has already been
// decoded.  When the position was unknown, the addition used to be applied to
// the G_UNKNOWN sentinel anyway, giving -999999.00015, which is not G_UNKNOWN
// and so was taken for a real position by everything downstream.  An unknown
// longitude must stay unknown.
func Test_decode_aprs_dao_does_not_invent_a_position(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APRS:!4237.14N/xxxxx.83W-Hi!W99!", true)
	assert.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)

	assert.True(t, A.Lon.IsNothing(), "longitude should still be unknown, got %v", A.Lon)

	// The latitude, which did decode, still gets its extra DAO digit:
	// 42 degrees 37.14 minutes, plus 9 ten-thousandths of a minute.
	var lat, ok = A.Lat.Get()
	assert.True(t, ok)
	assert.InDelta(t, 42.619+9.0/60000.0, lat, 0.0000001)
}

// The zero value of every optional field means "unknown", so a struct built
// anywhere other than decode_aprs (beacon.go does this) does not start out
// claiming a position off the coast of Africa.
func Test_decode_aprs_zero_value_has_no_position(t *testing.T) {
	var A Decoded

	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
	assert.Equal(t, maybe.Nothing[float64](), A.Lon)
	assert.Equal(t, maybe.Nothing[float64](), A.SpeedMPH)
	assert.Equal(t, maybe.Nothing[int](), A.Power)
}

// A weather report that stops in the middle of its fields used to run the
// decoder off the end of the information field and panic, taking the whole
// program down with it - from a received packet, so anyone within earshot
// could do it.  Each of these is truncated at a different point.
func Test_decode_aprs_truncated_weather(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, info := range []string{
		"!4903.50N/07201.75W_",            // nothing at all after the symbol
		"!4903.50N/07201.75W_22",          // part way through the wind direction
		"!4903.50N/07201.75W_220/004",     // wind, then nothing
		"!4903.50N/07201.75W_220/004g005", // ends after a complete field
		"!4903.50N/07201.75W_220/004g00",  // ends part way through a field
		"!4903.50N/07201.75W_c220s004g005t077r000p000P000h50b099",
	} {
		var pp = ax25.FromText("Q1TEST>APRS:"+info, true)
		assert.NotNil(t, pp)

		// Must not panic, and must still report the position.
		var A = aprsDecoder.Decode(pp, true)

		var lat, ok = A.Lat.Get()
		assert.True(t, ok, "%s", info)
		assert.InDelta(t, 49.0583333, lat, 0.0000001, "%s", info)
	}

	// What did arrive before the truncation is still decoded.
	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:!4903.50N/07201.75W_220/004g005", true), true)
	assert.Equal(t, `wind 4.6 mph, direction 220, gust 5, ""`, A.Weather)
}

// A positionless weather report was decoded by binary.Decode into a struct
// with a fixed 99-byte comment field.  Any report shorter than the whole 108
// bytes - which is very nearly all of them - failed the decode and left the
// struct zeroed, so the weather was thrown away and the station type came out
// as a hundred NUL bytes.
func Test_decode_aprs_positionless_weather(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:_10090556c220s004g005t077r000p000P000h50b09900wRSW", true), true)

	assert.Equal(t, "Positionless Weather Report", A.DataTypeDesc)
	assert.Equal(t,
		`wind 4.0 mph, direction 220, gust 5, temperature 77, `+
			`rain 0.00 in last hour, rain 0.00 in last 24 hours, rain 0.00 since midnight, `+
			`humidity 50, barometer 29.24, "wRSW"`,
		A.Weather)
}

// A positionless weather report with nothing after its timestamp has no
// weather data to decode, and must say so rather than read off the end.
func Test_decode_aprs_positionless_weather_truncated(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, info := range []string{"_", "_1009", "_10090556", "_10090556c2"} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:"+info, true), true)
		assert.Equal(t, "Positionless Weather Report", A.DataTypeDesc, "%s", info)
	}
}

// A weather field of all dots or all spaces is present but says the value is
// unknown, and must not be reported as a reading.  It used to come back as the
// G_UNKNOWN sentinel, which every caller had to remember to test for.
func Test_decode_aprs_weather_unknown_fields(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var known = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:!4903.50N/07201.75W_220/004g005t077r000p000P000h50b09900wRSW", true), true)
	assert.Equal(t,
		`wind 4.6 mph, direction 220, gust 5, temperature 77, `+
			`rain 0.00 in last hour, rain 0.00 in last 24 hours, rain 0.00 since midnight, `+
			`humidity 50, barometer 29.24, "wRSW"`,
		known.Weather)

	// The same report with every optional field blanked out: the fields are
	// still consumed - the station type is found at the end - but none of
	// them is reported.
	var unknown = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:!4903.50N/07201.75W_220/004g...t...r...p...P...h..b.....wRSW", true), true)
	assert.Equal(t, `wind 4.6 mph, direction 220, "wRSW"`, unknown.Weather)
}

// The wind direction and speed of a c000s000-form report are unknown, not
// zero, when their fields are blanked out.  weatherData clears course and
// speedMPH before it returns - the wind belongs on the weather line, not on
// the location line - so the distinction has to be checked at getWeatherData.
func Test_decode_aprs_weather_unknown_wind(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	// The field is there; it just has no value in it.
	var blank, rest, found = getWeatherData([]byte("c...s...g005"), 'c', 3)
	assert.True(t, found)
	assert.Equal(t, maybe.Nothing[float64](), blank)
	assert.Equal(t, "s...g005", string(rest))

	// An all-spaces field says the same thing as an all-dots one.  The two
	// are separate branches of an ||, so one can regress without the other.
	var spaces, afterSpaces, spacesFound = getWeatherData([]byte("c   s004"), 'c', 3)
	assert.True(t, spacesFound)
	assert.Equal(t, maybe.Nothing[float64](), spaces)
	assert.Equal(t, "s004", string(afterSpaces))

	// A field of zeroes is a reading of zero, which is not the same thing.
	var zero, _, _ = getWeatherData([]byte("c000s000"), 'c', 3)
	assert.Equal(t, maybe.Just(0.0), zero)

	// A field that is not there at all is not found, and consumes nothing.
	var missing, untouched, present = getWeatherData([]byte("g005t077"), 'c', 3)
	assert.False(t, present)
	assert.Equal(t, maybe.Nothing[float64](), missing)
	assert.Equal(t, "g005t077", string(untouched))

	// End to end: the blanked-out wind leaves no wind on the weather line,
	// and the fields after it still decode.
	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APRS:!4903.50N/07201.75W_c...s...g005t077wRSW", true), true)
	assert.Equal(t, `, gust 5, temperature 77, "wRSW"`, A.Weather)
}

// An item report whose name runs to the end of the information field has no
// live/killed indicator.  aprsItem used to walk off the end looking for one,
// bringing the program down on a packet that arrived off the air.
func Test_decode_aprs_item_without_live_killed_indicator(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:)Zb00000Zb00001", true)
	assert.NotNil(t, pp)

	// Must not panic.
	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "Item - name not ended by ! or _", A.DataTypeDesc)
	assert.Equal(t, "Zb00000Zb00001", A.Name)
	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
}

// The name is meant to be 3 to 9 characters.  One that isn't was an assertion
// failure, which is to say a crash, rather than something to report.
func Test_decode_aprs_item_with_short_name(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:)AB!4237.14N/07120.83W#", true)
	assert.NotNil(t, pp)

	// Must not panic, and the rest of the item still decodes.
	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "Item", A.DataTypeDesc)
	assert.Equal(t, "AB", A.Name)
	assert.Equal(t, maybe.Just(42.619), A.Lat)
}

// An item that stops before its position has no position, rather than the
// zeroed one binary.Decode leaves behind.
func Test_decode_aprs_item_without_position(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:)ABCDE!", true)
	assert.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "Item", A.DataTypeDesc)
	assert.Equal(t, "ABCDE", A.Name)
	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
	assert.Equal(t, maybe.Nothing[float64](), A.Lon)
}

// A Mic-E destination is really a latitude of six digits, but a lenient parse
// - what the APRS-IS input and samoyed-decode_aprs both use - will accept a
// shorter one, which used to be read off the end of the address.
func Test_decode_aprs_mic_e_short_destination(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromTextWithStrictness("Q1TEST>0:'0000000000000000000", ax25.AddrLenient)
	assert.NotNil(t, pp)

	// Must not panic, and must not claim a position it never read.
	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "MIC-E", A.DataTypeDesc)
	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
	assert.Equal(t, maybe.Nothing[float64](), A.Lon)
}

// A course and speed extension can be the whole of the information field,
// with nothing after it - and then there is no bearing and no NRQ to look at.
func Test_decode_aprs_course_speed_without_bearing(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromTextWithStrictness("Q1TEST>APDW17:!0000.00N/00000.00W/000/000", ax25.AddrLenient)
	assert.NotNil(t, pp)

	// Must not panic, and the course and speed still decode.
	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, maybe.Just(0.0), A.Course)
	assert.Equal(t, maybe.Just(0.0), A.SpeedMPH)
	assert.Empty(t, A.Comment)
}

// User-defined data is a user ID and a type after the "{", and an information
// field that stops before them is user-defined data we know nothing about
// rather than something to read off the end of.
func Test_decode_aprs_user_defined_without_id(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromTextWithStrictness("Q1TEST>APDW17:{", ax25.AddrLenient)
	assert.NotNil(t, pp)

	// Must not panic.
	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "User-Defined Data", A.DataTypeDesc)
}

// A general query may carry a "footprint" of latitude, longitude and radius.
// The parser used to take the three-field branch when the query had any other
// number of fields, reading off the end of a shorter one (and never decoding a
// well-formed footprint at all).
func Test_decode_aprs_general_query_footprint(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:?APRS? 42.3714,-71.2083,0050", true)
	assert.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "General Query", A.DataTypeDesc)
	assert.Equal(t, "APRS", A.QueryType)
	assert.Equal(t, maybe.Just(42.3714), A.FootprintLat)
	assert.Equal(t, maybe.Just(-71.2083), A.FootprintLon)
	assert.Equal(t, maybe.Just(50.0), A.FootprintRadius)
}

// A general query whose footprint is not three comma-separated fields is
// rejected rather than read off the end (found by fuzzing).
func Test_decode_aprs_general_query_short_footprint(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:?APRS?0", true)
	assert.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)
	assert.Equal(t, "General Query", A.DataTypeDesc)
	assert.Equal(t, "APRS", A.QueryType)
	assert.Equal(t, maybe.Nothing[float64](), A.FootprintLat)
	assert.Equal(t, maybe.Nothing[float64](), A.FootprintLon)
	assert.Equal(t, maybe.Nothing[float64](), A.FootprintRadius)
}

// The APRS message branches used to index the message text without checking
// its length, so a message with nothing after the addressee - or one shorter
// than the "ack"/"rej" they compare against - read off the end (found by
// fuzzing).
func Test_decode_aprs_short_message(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct{ monitor, comment string }{
		{"Q1TEST>APDW17::Q2TEST   :", ""},
		{"Q1TEST>APDW17::Q2TEST   :ab", "ab"},
	} {
		var pp = ax25.FromText(tc.monitor, true)
		assert.NotNil(t, pp)

		var A = aprsDecoder.Decode(pp, true)
		assert.Equal(t, MessageSubtypeMessage, A.MessageSubtype)
		assert.Equal(t, "Q2TEST", A.Addressee)
		assert.Equal(t, tc.comment, A.Comment)
	}
}

// A quiet decode keeps its complaints to itself, but a malformed timestamp
// used to be reported regardless.
func Test_decode_aprs_quiet_bad_timestamp(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var pp = ax25.FromText("Q1TEST>APDW17:@09234Xz4903.50N/07201.75W-", true)
	assert.NotNil(t, pp)

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	aprsDecoder.Decode(pp, true)
	assert.Empty(t, hook.AllEntries())

	// And a decode that isn't quiet does complain, so the hook would see it.
	aprsDecoder.Decode(pp, false)
	assert.Contains(t, hook.LastEntry().Message, "Timestamp must be")
}

// Dire Wolf warned about a comment too long for its 256 byte buffer, but the
// port measured against the still-empty comment string, so it warned about
// every comment at all.
func Test_decode_aprs_comment_length_warning(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var hook = test.NewGlobal()

	t.Cleanup(hook.Reset)

	var pp = ax25.FromText("Q1TEST>APDW17:!4903.50N/07201.75W-A short comment", true)
	assert.NotNil(t, pp)

	aprsDecoder.Decode(pp, false)

	for _, entry := range hook.AllEntries() {
		assert.NotEqual(t, "Comment is extremely long", entry.Message)
	}

	pp = ax25.FromText("Q1TEST>APDW17:!4903.50N/07201.75W-"+strings.Repeat("x", 256), true)
	assert.NotNil(t, pp)

	aprsDecoder.Decode(pp, false)
	assert.Equal(t, "Comment is extremely long", hook.LastEntry().Message)
}

// Telemetry metadata a station sends is kept by the Decoder that
// decoded it, for decoding that station's later telemetry data, and not by
// any other Decoder.
func Test_decode_aprs_telemetry_metadata_per_decoder(t *testing.T) {
	var parm = ax25.FromText("Q1TEST>APRS::Q1TEST   :PARM.Volts", true)
	var data = ax25.FromText("Q1TEST>APRS:T#005,199,000,255,073,123,01101001", true)

	var heard = NewDecoder(nil, nil)
	heard.Decode(parm, true)
	assert.Contains(t, heard.Decode(data, true).Telemetry, "Volts=199")

	var other = NewDecoder(nil, nil)
	assert.NotContains(t, other.Decode(data, true).Telemetry, "Volts")
}
