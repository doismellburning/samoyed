// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package aprs

import (
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/dwutil"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/testutils"
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

// Mic-E speed is usually sent with 800 knots added, which the decoder takes
// off again - but it used to do the sum in a byte, which wrapped long before
// it got there, so 12 knots came out as 44.  The expected values are what Dire
// Wolf's decode_aprs makes of the same packets.
func Test_decode_aprs_mic_e_speed_and_course(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		monitor string
		knots   float64
		course  float64
	}{
		{"Q1TEST>T2SP0W:`c_Vm6hk/`\"49}_%", 12, 276},
		{"Q1TEST>S32U6T:`(_fn\"Oj/", 20, 251},
	} {
		var A = aprsDecoder.Decode(ax25.FromText(tc.monitor, true), true)

		var speed, speedOK = A.SpeedMPH.Get()
		assert.True(t, speedOK, "%s", tc.monitor)
		assert.InDelta(t, dwutil.DW_KNOTS_TO_MPH(tc.knots), speed, 0.001, "%s", tc.monitor)
		assert.Equal(t, maybe.Just(tc.course), A.Course, "%s", tc.monitor)
	}
}

// Compressed positions, from the examples in chapter 9 of the APRS 1.0.1
// spec.  The base 91 sums used to be done in a byte, which wrapped, so every
// one of these came out somewhere near the North Pole on the date line.
func Test_decode_aprs_compressed_position(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, info := range []string{
		"!/5L!!<*e7>7P[",
		"=/5L!!<*e7>7P[",
		"@092345z/5L!!<*e7>7P[",
		"/092345z/5L!!<*e7>7P[",
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+info, true), true)

		var lat, latOK = A.Lat.Get()
		assert.True(t, latOK, "%s", info)
		assert.InDelta(t, 49.5, lat, 0.00001, "%s", info)

		var lon, lonOK = A.Lon.Get()
		assert.True(t, lonOK, "%s", info)
		assert.InDelta(t, -72.75, lon, 0.00001, "%s", info)

		assert.Equal(t, byte('/'), A.SymbolTable, "%s", info)
		assert.Equal(t, byte('>'), A.SymbolCode, "%s", info)
		assert.Equal(t, maybe.Just(88.0), A.Course, "%s", info)

		var speed, speedOK = A.SpeedMPH.Get()
		assert.True(t, speedOK, "%s", info)
		assert.InDelta(t, dwutil.DW_KNOTS_TO_MPH(36.2), speed, 0.1, "%s", info) // 1.08^47 - 1 knots, to one place
	}
}

// Both Ultimeter formats, with the readings Dire Wolf's decode_aprs gives.
// The fields were read with C's "%4hx", which Go's fmt doesn't know, so
// neither format ever gave any weather.
func Test_decode_aprs_ultimeter(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		info    string
		weather string
	}{
		{
			"$ULTW0000000001110B6E27F4FFF3897B0001035E004E04DD00030000",
			"wind 0.0 mph, direction 0, temperature 27.3, barometer 30.21, humidity 86",
		},
		{
			"!!00000066013D000028710166--------0158053201200210",
			"wind 0.0 mph, direction 143, temperature 31.7",
		},
		// Temperatures below zero are two's complement: 0xff9c is -10.0.
		{
			"$ULTW00000000FF9C0B6E27F4FFF3897B0001035E004E04DD00030000",
			"wind 0.0 mph, direction 0, temperature -10.0, barometer 30.21, humidity 86",
		},
		{
			"!!00000066FF9C000028710166--------0158053201200210",
			"wind 0.0 mph, direction 143, temperature -10.0",
		},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, "Ultimeter", A.DataTypeDesc, "%s", tc.info)
		assert.Equal(t, tc.weather, A.Weather, "%s", tc.info)
	}
}

// An object's name, live/killed indicator and timestamp come before its
// position, but were only read along with a human-readable one, which is six
// bytes longer than a compressed one - so a compressed object lost its name.
func Test_decode_aprs_compressed_object(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:;Q2TEST   *092345z/5L!!<*e7>7P[", true), true)

	assert.Equal(t, "Object", A.DataTypeDesc)
	assert.Equal(t, "Q2TEST", A.Name)

	var lat, latOK = A.Lat.Get()
	assert.True(t, latOK)
	assert.InDelta(t, 49.5, lat, 0.00001)
}

// A report that stops before the end of its position has no position.  One
// that stopped short of a human-readable position used to be read as a
// compressed one instead, which put it somewhere nobody mentioned.
func Test_decode_aprs_short_position(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, info := range []string{
		"!4903.50N/07201.75W", // no symbol code
		"=4903.50N",
		"@092345z4903.50N/07201.75W",
		"/092345z/5L!!<*e7",
		"@0923",
		";Q2TEST   *092345z4903.50N/07201.75W",
		";Q2TEST   *092345z/5L!",
		";Q2TEST",
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+info, true), true)

		assert.Equal(t, maybe.Nothing[float64](), A.Lat, "%s", info)
		assert.Equal(t, maybe.Nothing[float64](), A.Lon, "%s", info)
	}

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:;Q2TEST   _092345z4903.50N", true), true)
	assert.Equal(t, "Killed Object", A.DataTypeDesc)
	assert.Equal(t, "Q2TEST", A.Name)
}

// An ack and a rej are told apart by their subtype, which a rej used to share
// with an ack.
func Test_decode_aprs_ack_and_rej(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		info    string
		subtype MessageSubtype
	}{
		{":Q2TEST   :ack42", MessageSubtypeAck},
		{":Q2TEST   :rej42", MessageSubtypeRej},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, tc.subtype, A.MessageSubtype, "%s", tc.info)
		assert.Equal(t, PacketTypeMessage, A.PacketType, "%s", tc.info)
		assert.Equal(t, "42", A.MessageNumber, "%s", tc.info)
		assert.Equal(t, "Q2TEST", A.Addressee, "%s", tc.info)
	}
}

// A position a hair short of a whole degree used to print as 59.99995 minutes
// or more rounded up to 60, as "W 000°60.0000", rather than carried into the
// degrees.  Floating point on arm64 lands a Maidenhead square's corner there.
func Test_decode_aprs_print_carries_minutes(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = new(Decoded)
	A.DataTypeDesc = "Position"
	A.SymbolCode = ' '
	A.Lat = maybe.Just(51.0 - 1e-9)
	A.Lon = maybe.Just(-(1.0 - 1e-9))

	var output = testutils.CaptureOutput(t, func() { aprsDecoder.Print(A) })

	assert.Equal(t, "Position\nN 51°00.0000, W 001°00.0000\n", output)
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

// assertMaybeInDelta checks that got is present exactly when want is, and then
// that the two are within delta of each other.
func assertMaybeInDelta(t *testing.T, want maybe.Maybe[float64], got maybe.Maybe[float64], delta float64, msgAndArgs ...any) {
	t.Helper()

	var wantValue, wantOK = want.Get()
	var gotValue, gotOK = got.Get()

	if assert.Equal(t, wantOK, gotOK, msgAndArgs...) && wantOK {
		assert.InDelta(t, wantValue, gotValue, delta, msgAndArgs...)
	}
}

// The tests from here on cover the formats one at a time.  Unless they say
// otherwise, the expected values are what Dire Wolf's decode_aprs makes of the
// same packet.

// Mic-E keeps the latitude, the message bits and the longitude offset in the
// destination, and the rest in the first nine bytes of the information part.
// Each of these varies one of them, valid or not.
func Test_decode_aprs_mic_e(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var nothing = maybe.Nothing[float64]()

	for _, tc := range []struct {
		monitor string
		lat     maybe.Maybe[float64]
		lon     maybe.Maybe[float64]
		status  string
		mfr     string
		comment string
	}{
		// Standard message bits, north, west, no offset.
		{"Q1TEST>S32U6T:`(_fn\"Oj/", maybe.Just(33 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Returning", unknownDevice, ""},
		// No message bits is an emergency.  South and east.
		{"Q1TEST>332564:`(_fn\"Oj/>Kenwood TH-D7", maybe.Just(-(33 + 25.64/60)), maybe.Just(12 + 7.74/60), "Emergency", "Kenwood TH-D7A", "Kenwood TH-D7"},
		// A custom message bit.
		{"Q1TEST>C32U6T:`(_fn\"Oj/]=", maybe.Just(23 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Custom-3", "Kenwood TM-D710", ""},
		// A standard and a custom bit together aren't any known message.
		{"Q1TEST>SC2U6T:`(_fn\"Oj/", maybe.Just(32 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Unknown MIC-E Message Type", unknownDevice, ""},
		// K, L and Z are digits of 0, with or without a message bit.
		{"Q1TEST>KLZU6T:`(_fn\"Oj/", maybe.Just(5.64 / 60), maybe.Just(-(12 + 7.74/60)), "Unknown MIC-E Message Type", unknownDevice, ""},
		// The three ranges of degrees with a longitude offset of 100.
		{"Q1TEST>S32UZT:`x_fn\"Oj/", maybe.Just(33 + 25.04/60), maybe.Just(-(2 + 7.74/60)), "Returning", unknownDevice, ""},
		{"Q1TEST>S32UZT:`n_fn\"Oj/", maybe.Just(33 + 25.04/60), maybe.Just(-(102 + 7.74/60)), "Returning", unknownDevice, ""},
		{"Q1TEST>S32UZT:`(_fn\"Oj/", maybe.Just(33 + 25.04/60), maybe.Just(-(112 + 7.74/60)), "Returning", unknownDevice, ""},
		// A longitude offset that is neither, taken as none.
		{"Q1TEST>S32UK6:`(_fn\"Oj/", maybe.Just(33 + 25.06/60), maybe.Just(12 + 7.74/60), "Returning", unknownDevice, ""},
		// Minutes under 10.
		{"Q1TEST>S32U6T:`(Zfn\"Oj/", maybe.Just(33 + 25.64/60), maybe.Just(-(12 + 2.74/60)), "Returning", unknownDevice, ""},
		// Degrees, minutes and hundredths that are out of range.
		{"Q1TEST>S32U6T:`!_fn\"Oj/", maybe.Just(33 + 25.64/60), nothing, "Returning", unknownDevice, ""},
		{"Q1TEST>S32U6T:`(!fn\"Oj/", maybe.Just(33 + 25.64/60), nothing, "Returning", unknownDevice, ""},
		{"Q1TEST>S32U6T:`(Z\x1an\"Oj/", maybe.Just(33 + 25.64/60), nothing, "Returning", unknownDevice, ""},
		// North/south and east/west that are neither: north and east.
		{"Q1TEST>S32A6T:`(_fn\"Oj/", maybe.Just(33 + 20.64/60), maybe.Just(-(12 + 7.74/60)), "Returning", unknownDevice, ""},
		{"Q1TEST>S32U6A:`(_fn\"Oj/", maybe.Just(33 + 25.60/60), maybe.Just(12 + 7.74/60), "Returning", unknownDevice, ""},
		// A latitude digit that isn't any kind of digit counts as 0.
		{"Q1TEST>Sa2U6T:`(_fn\"Oj/", maybe.Just(30 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Returning", unknownDevice, ""},
		// A trailing CR isn't part of the comment, or of the device suffix.
		{"Q1TEST>S32U6T:`(_fn\"Oj/\r", maybe.Just(33 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Returning", unknownDevice, ""},
		{"Q1TEST>S32U6T:'(_fn\"Oj/]\r", maybe.Just(33 + 25.64/60), maybe.Just(-(12 + 7.74/60)), "Returning", "Kenwood TM-D700", ""},
		{"Q1TEST>T2SP0W:`c_Vm6hk/ Comment here", maybe.Just(42 + 30.07/60), maybe.Just(-(71 + 7.58/60)), "In Service", unknownDevice, " Comment here"},
	} {
		// Lenient, as the APRS-IS input is, to get a lower case destination in.
		var A = aprsDecoder.Decode(ax25.FromTextWithStrictness(tc.monitor, ax25.AddrLenient), true)

		assert.Equal(t, "MIC-E", A.DataTypeDesc, "%q", tc.monitor)
		assert.Equal(t, PacketTypePosition, A.PacketType, "%q", tc.monitor)
		assertMaybeInDelta(t, tc.lat, A.Lat, 0.000001, "%q", tc.monitor)
		assertMaybeInDelta(t, tc.lon, A.Lon, 0.000001, "%q", tc.monitor)
		assert.Equal(t, tc.status, A.MicEStatus, "%q", tc.monitor)
		assert.Equal(t, tc.mfr, A.Mfr, "%q", tc.monitor)
		assert.Equal(t, tc.comment, A.Comment, "%q", tc.monitor)
	}
}

// The parts of a Mic-E report after the position: symbol, course, and the
// altitude at the start of the comment.
func Test_decode_aprs_mic_e_symbol_course_altitude(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>T2SP0W:`c_Vm6hk/`\"49}Q1TEST_%", true), true)
	assert.Equal(t, byte('/'), A.SymbolTable)
	assert.Equal(t, byte('k'), A.SymbolCode)
	assert.Equal(t, "Yaesu FTM-400DR", A.Mfr)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_METERS_TO_FEET(34)), A.AltitudeFt, 0.000001)
	assert.Equal(t, "Q1TEST", A.Comment)

	// A symbol table that isn't one falls back to the primary table.
	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>S32U6T:`(_fn\"Oja", true), true)
	assert.Equal(t, byte('/'), A.SymbolTable)
	assert.Equal(t, byte('j'), A.SymbolCode)

	// A course of 0 is unknown, and 360 is north.
	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>S32U6T:`(_fnX\x1cj/", true), true)
	assert.Equal(t, maybe.Nothing[float64](), A.Course)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_KNOTS_TO_MPH(26)), A.SpeedMPH, 0.000001)

	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>S32U6T:`(_fn)Xj/", true), true)
	assert.Equal(t, maybe.Just(0.0), A.Course)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_KNOTS_TO_MPH(21)), A.SpeedMPH, 0.000001)
}

// The many things a "message" can be.
func Test_decode_aprs_message(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		info       string
		desc       string
		addressee  string
		subtype    MessageSubtype
		packetType PacketType
		number     string
		comment    string
	}{
		{
			":Q2TEST   :Hello there{42",
			`APRS Message, number "42", from "Q1TEST" to "Q2TEST"`,
			"Q2TEST", MessageSubtypeMessage, PacketTypeMessage, "42", "Hello there",
		},
		{
			":Q2TEST   :No number",
			`APRS Message, with no number, from "Q1TEST" to "Q2TEST"`,
			"Q2TEST", MessageSubtypeMessage, PacketTypeMessage, "", "No number",
		},
		{
			// A reply-ack: the message's own number, then the one it acks.
			":Q2TEST   :Hello{AB}CD",
			`APRS Message, number "AB", from "Q1TEST" to "Q2TEST", with ACK for "CD"`,
			"Q2TEST", MessageSubtypeMessage, PacketTypeMessage, "AB", "Hello",
		},
		{
			":BLN1     :Bulletin text",
			`Bulletin with identifier "1"`,
			"BLN1", MessageSubtypeBulletin, PacketTypeNone, "", "Bulletin text",
		},
		{
			":BLNA     :Announcement text",
			`Bulletin with identifier "A"`,
			"BLNA", MessageSubtypeBulletin, PacketTypeNone, "", "Announcement text",
		},
		{
			":NWS-WARN :Flood warning",
			`Weather bulletin with identifier "WARN"`,
			"NWS-WARN", MessageSubtypeNWS, PacketTypeNWS, "", "Flood warning",
		},
		{
			":Q2TEST   :?APRSD",
			"Directed Station Query",
			"Q2TEST", MessageSubtypeDirectedQuery, PacketTypeQuery, "", "",
		},
		{
			// The addressee must be padded to 9 characters.
			":Q2TEST:Short addressee",
			"APRS Message",
			"", MessageSubtypeInvalid, PacketTypeNone, "", "",
		},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, tc.desc, A.DataTypeDesc, "%s", tc.info)
		assert.Equal(t, tc.addressee, A.Addressee, "%s", tc.info)
		assert.Equal(t, tc.subtype, A.MessageSubtype, "%s", tc.info)
		assert.Equal(t, tc.packetType, A.PacketType, "%s", tc.info)
		assert.Equal(t, tc.number, A.MessageNumber, "%s", tc.info)
		assert.Equal(t, tc.comment, A.Comment, "%s", tc.info)
	}
}

// Telemetry metadata is sent as messages to the station it describes, and
// shapes how that station's telemetry is shown from then on.
func Test_decode_aprs_telemetry_metadata(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		info    string
		desc    string
		subtype MessageSubtype
	}{
		{":Q1TEST   :PARM.Battery,Temp,Light,B1,B2", `Telemetry Parameter Name for "Q1TEST"`, MessageSubtypeTelemParm},
		{":Q1TEST   :UNIT.Volts,deg.C,lux,on,on", `Telemetry Unit/Label for "Q1TEST"`, MessageSubtypeTelemUnit},
		{":Q1TEST   :EQNS.0,0.1,0,0,1,-40,0,1,0", `Telemetry Equation Coefficients for "Q1TEST"`, MessageSubtypeTelemEqns},
		{":Q1TEST   :BITS.11111111,Balloon project", `Telemetry Bit Sense/Project Name for "Q1TEST"`, MessageSubtypeTelemBits},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, tc.desc, A.DataTypeDesc, "%s", tc.info)
		assert.Equal(t, "Q1TEST", A.Addressee, "%s", tc.info)
		assert.Equal(t, tc.subtype, A.MessageSubtype, "%s", tc.info)
		assert.Equal(t, PacketTypeTelemetry, A.PacketType, "%s", tc.info)
	}

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:T#005,199,000,255,073,123,01101001", true), true)
	assert.Equal(t, "Telemetry", A.DataTypeDesc)
	assert.Equal(t, PacketTypeTelemetry, A.PacketType)
	assert.Equal(t,
		"Balloon project: Seq=5, Battery=19.9 Volts, Temp=-40 deg.C, Light=255 lux, B1=73 on, B2=123 on, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1",
		A.Telemetry)
}

// Raw NMEA sentences straight from a GPS receiver.  Only RMC and GGA carry a
// position for us; anything else is just data of an unknown type.
func Test_decode_aprs_raw_nmea(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:$GPRMC,063909,A,3349.4302,N,11700.3721,W,43.022,89.3,291099,13.6,E*52", true), true)
	assert.Equal(t, "Raw GPS data", A.DataTypeDesc)
	assert.Equal(t, PacketTypePosition, A.PacketType)
	assertMaybeInDelta(t, maybe.Just(33+49.4302/60), A.Lat, 0.000001)
	assertMaybeInDelta(t, maybe.Just(-(117 + 0.3721/60)), A.Lon, 0.000001)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_KNOTS_TO_MPH(43.022)), A.SpeedMPH, 0.000001)
	assertMaybeInDelta(t, maybe.Just(89.3), A.Course, 0.000001)

	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:$GPGGA,102705,5157.9762,N,00029.3256,W,1,04,2.0,75.7,M,47.6,M,,*62", true), true)
	assert.Equal(t, "Raw GPS data", A.DataTypeDesc)
	assertMaybeInDelta(t, maybe.Just(51+57.9762/60), A.Lat, 0.000001)
	assertMaybeInDelta(t, maybe.Just(-29.3256/60), A.Lon, 0.000001)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_METERS_TO_FEET(75.7)), A.AltitudeFt, 0.000001)

	for _, info := range []string{
		"$GPGLL,4916.45,N,12311.12,W,225444,A,*1D",
		"$GPWPL,4916.45,N,12311.12,W,WPTNME*5C",
		"$PGRMZ,1234,f,3*2D",
	} {
		A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+info, true), true)
		assert.Equal(t, `ERROR!!!  Unknown APRS Data Type Indicator "$"`, A.DataTypeDesc, "%s", info)
		assert.Equal(t, maybe.Nothing[float64](), A.Lat, "%s", info)
	}

	// Raw NMEA has no symbol of its own, so the source SSID gives one: 5 is
	// a yacht.
	A = aprsDecoder.Decode(ax25.FromText("Q1TEST-5>APDW17:$GPXYZ,1,2,3", true), true)
	assert.Equal(t, byte('/'), A.SymbolTable)
	assert.Equal(t, byte('Y'), A.SymbolCode)
}

// Formats whose information part is little more than a comment.
func Test_decode_aprs_comment_only_formats(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		info       string
		desc       string
		packetType PacketType
		comment    string
	}{
		{"<IGATE,MSG_CNT=30,LOC_CNT=20", "Station Capabilities", PacketTypeCapabilities, "IGATE,MSG_CNT=30,LOC_CNT=20"},
		{"{Q1qwerty", "User-Defined Data", PacketTypeUserDefined, ""},
		{"{{experimental", "User-Defined Experimental", PacketTypeUserDefined, ""},
		{"{DT2A22A#", "Raw Touch Tone Data", PacketTypeUserDefined, "2A22A#"},
		{"{tt2A22A#", "Raw Touch Tone Data", PacketTypeUserDefined, "2A22A#"},
		{"t2A22A#", "Raw Touch Tone Data", PacketTypeNone, "2A22A#"},
		{"{DMSOS", "Morse Code Data", PacketTypeUserDefined, "SOS"},
		{"{mcSOS", "Morse Code Data", PacketTypeUserDefined, "SOS"},
		{"mSOS", "Morse Code Data", PacketTypeNone, "SOS"},
		{"}garbage", "Third Party Header: Unable to parse payload.", PacketTypeNone, ""},
		{">Plain status", "Status Report", PacketTypeStatus, "Plain status"},
		{">092345zNet Control", "Status Report", PacketTypeStatus, "Net Control"},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, tc.desc, A.DataTypeDesc, "%s", tc.info)
		assert.Equal(t, tc.packetType, A.PacketType, "%s", tc.info)
		assert.Equal(t, tc.comment, A.Comment, "%s", tc.info)
	}
}

// AIS reports carried in Samoyed's own user-defined data type.
func Test_decode_aprs_ais(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	// The sentence is the example from https://www.aggsoft.com/ais-decoder.htm
	// that the AIS package's tests use too.
	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:{DA!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*4E", true), true)
	assert.Equal(t, "AIS 1: Position Report Class A", A.DataTypeDesc)
	assert.Equal(t, PacketTypeUserDefined, A.PacketType)
	assert.Equal(t, "366730000", A.Name)
	assertMaybeInDelta(t, maybe.Just(37.8038033), A.Lat, 0.000001)
	assertMaybeInDelta(t, maybe.Just(-122.3925333), A.Lon, 0.000001)
	assertMaybeInDelta(t, maybe.Just(51.3), A.Course, 0.000001)
	assert.Empty(t, A.Mfr)

	// Not an AIS sentence at all.
	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:{DAx", true), true)
	assert.Equal(t, "AIS", A.DataTypeDesc)
	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
}

// The human-readable latitude and longitude, including position ambiguity,
// where trailing digits are spaces, and the various ways each can be wrong.
func Test_decode_aprs_lat_long(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var lat = maybe.Just(49 + 3.5/60)
	var lon = maybe.Just(-(72 + 1.75/60))
	var nothing = maybe.Nothing[float64]()

	for _, tc := range []struct {
		info string
		lat  maybe.Maybe[float64]
		lon  maybe.Maybe[float64]
	}{
		{"!4903.50N/07201.75W-", lat, lon},
		{"!4903.50S/07201.75E-", maybe.Just(-(49 + 3.5/60)), maybe.Just(72 + 1.75/60)},
		{"!4903.50n/07201.75w-", lat, lon}, // The spec says upper case, but lower case is seen.
		{"!4903.  N/07201.  W-", maybe.Just(49 + 3.0/60), maybe.Just(-(72 + 1.0/60))},
		{"!49  .  N/072  .  W-", maybe.Just(49.0), maybe.Just(-72.0)},
		{"!4903.50X/07201.75W-", nothing, lon},
		{"!49O3.50N/07201.75W-", nothing, lon},
		{"!4903.50N/07201.75X-", lat, nothing},
		{"!4903.50N/072O1.75W-", lat, nothing},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, "Position", A.DataTypeDesc, "%s", tc.info)
		assertMaybeInDelta(t, tc.lat, A.Lat, 0.000001, "%s", tc.info)
		assertMaybeInDelta(t, tc.lon, A.Lon, 0.000001, "%s", tc.info)
	}
}

// The data extensions that can follow a human-readable position, and what
// processComment finds in the comment after them.
func Test_decode_aprs_data_extensions(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var decode = func(info string) *Decoded {
		return aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:!4903.50N/07201.75W"+info, true), true)
	}

	// PHG: power is the square of the digit, height 10 * 2^digit, and the
	// last digit is the direction of a directional antenna, or 0 for omni.
	for _, tc := range []struct {
		info        string
		directivity string
	}{
		{"#PHG5130", "omni"},
		{"#PHG5132WIDE", "E"},
		{"#PHG5138", "N"},
	} {
		var A = decode(tc.info)
		assert.Equal(t, maybe.Just(25), A.Power, "%s", tc.info)
		assert.Equal(t, maybe.Just(20), A.HeightFt, "%s", tc.info)
		assert.Equal(t, maybe.Just(3), A.Gain, "%s", tc.info)
		assert.Equal(t, tc.directivity, A.Directivity, "%s", tc.info)
	}

	assert.Equal(t, "WIDE", decode("#PHG5132WIDE").Comment)

	assertMaybeInDelta(t, maybe.Just(50.0), decode("#RNG0050").RadioRange, 0.000001)

	// DFS: strength, then height and gain like PHG.
	var A = decode(`\DFS2360`)
	assert.Equal(t, maybe.Just(80), A.HeightFt)
	assert.Equal(t, maybe.Just(6), A.Gain)
	assert.Equal(t, "omni", A.Directivity)

	A = decode(">088/036/A=001234")
	assert.Equal(t, maybe.Just(88.0), A.Course)
	assertMaybeInDelta(t, maybe.Just(dwutil.DW_KNOTS_TO_MPH(36)), A.SpeedMPH, 0.000001)
	assert.Equal(t, maybe.Just(1234.0), A.AltitudeFt)

	// The altitude can be anywhere in the comment.  Dire Wolf 1.7 doesn't
	// take a negative one, but newer versions do.
	A = decode("-Comment /A=001234 more")
	assert.Equal(t, maybe.Just(1234.0), A.AltitudeFt)
	assert.Equal(t, "Comment  more", A.Comment)
	assert.Equal(t, maybe.Just(-12.0), decode(">/A=-00012").AltitudeFt)

	// Frequency, tone or DCS, offset and range, in the forms
	// http://www.aprs.org/info/freqspec.txt gives.
	A = decode("-146.520MHz T100 +060")
	assert.Equal(t, maybe.Just(146.52), A.Freq)
	assert.Equal(t, maybe.Just(100.0), A.Tone)
	assert.Equal(t, maybe.Just(600), A.Offset)

	A = decode("-146.520MHz C100 -060 R25m")
	assert.Equal(t, maybe.Just(100.0), A.Tone)
	assert.Equal(t, maybe.Just(-600), A.Offset)
	assert.Equal(t, maybe.Just(25.0), A.RadioRange)

	A = decode("-146.520MHz D023 +600 Rptr")
	assert.Equal(t, maybe.Just(0o23), A.DCS)
	assert.Equal(t, maybe.Just(6000), A.Offset)
	assert.Equal(t, " Rptr", A.Comment)

	assert.Equal(t, maybe.Just(0.0), decode("-146.520MHz Toff").Tone)

	// Not quite the standard form, but recognisable.
	A = decode("-146.52MHz 1234.12MHz")
	assert.Equal(t, maybe.Just(146.52), A.Freq)
	assert.Equal(t, "146.52MHz 1234.12MHz", A.Comment)
}

// !DAO! adds a digit of precision to a position, either as a human-readable
// digit (upper case datum) or base 91 (lower case), or is an APRStt location.
func Test_decode_aprs_dao(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	for _, tc := range []struct {
		dao       string
		lat       float64
		lon       float64
		aprsttLoc string
	}{
		{"!W60!", 49 + 3.506/60, -(72 + 1.75/60), ""},
		{"!w\"<!", 49.058335166666666, -72.02921616666667, ""},
		{"!T  !", 49 + 3.5/60, -(72 + 1.75/60), "APRStt corral location"},
		{"!TB2!", 49 + 3.5/60, -(72 + 1.75/60), "APRStt location B2..."},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:!4903.50N/07201.75W-Hello"+tc.dao, true), true)

		assertMaybeInDelta(t, maybe.Just(tc.lat), A.Lat, 0.0000001, "%s", tc.dao)
		assertMaybeInDelta(t, maybe.Just(tc.lon), A.Lon, 0.0000001, "%s", tc.dao)
		assert.Equal(t, tc.aprsttLoc, A.APRSttLoc, "%s", tc.dao)
		assert.Equal(t, "Hello", A.Comment, "%s", tc.dao)
	}
}

// Timestamps, objects and items around a human-readable position.
func Test_decode_aprs_timestamps_objects_items(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var lat = maybe.Just(49 + 3.5/60)
	var lon = maybe.Just(-(72 + 1.75/60))

	for _, tc := range []struct {
		info       string
		desc       string
		name       string
		packetType PacketType
	}{
		{"/092345z4903.50N/07201.75W>", "Position with time", "", PacketTypePosition},
		{"@092345/4903.50N/07201.75W>", "Position with time", "", PacketTypePosition},
		{"@234517h4903.50N/07201.75W>", "Position with time", "", PacketTypePosition},
		{";LEADER   *092345z4903.50N/07201.75W>", "Object", "LEADER", PacketTypeObject},
		{";LEADER   _092345z4903.50N/07201.75W>", "Killed Object", "LEADER", PacketTypeObject},
		{")AID #2!4903.50N/07201.75WA", "Item", "AID #2", PacketTypeItem},
	} {
		var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:"+tc.info, true), true)

		assert.Equal(t, tc.desc, A.DataTypeDesc, "%s", tc.info)
		assert.Equal(t, tc.name, A.Name, "%s", tc.info)
		assert.Equal(t, tc.packetType, A.PacketType, "%s", tc.info)
		assertMaybeInDelta(t, lat, A.Lat, 0.000001, "%s", tc.info)
		assertMaybeInDelta(t, lon, A.Lon, 0.000001, "%s", tc.info)
	}
}

// The rest of the compressed position examples from chapter 9 of the spec,
// whose last three bytes are an altitude or a radio range rather than course
// and speed.
func Test_decode_aprs_compressed_altitude_and_range(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:!/5L!!<*e7OS]S", true), true)
	assertMaybeInDelta(t, maybe.Just(10004.52), A.AltitudeFt, 0.01)
	assert.Equal(t, maybe.Nothing[float64](), A.Course)

	A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:=/5L!!<*e7>{?!Range", true), true)
	assertMaybeInDelta(t, maybe.Just(20.12531), A.RadioRange, 0.00001)
	assert.Equal(t, "Range", A.Comment)
}

// A status report can start with a Maidenhead locator and a symbol.
func Test_decode_aprs_status_maidenhead(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:>IO91SX/G Status", true), true)
	assert.Equal(t, "Status Report", A.DataTypeDesc)
	assert.Equal(t, "IO91SX", A.Maidenhead)
	assert.Equal(t, byte('/'), A.SymbolTable)
	assert.Equal(t, byte('G'), A.SymbolCode)
	assert.Equal(t, "Status", A.Comment)

	// The locator isn't turned into a latitude and longitude until it's printed.
	assert.Equal(t, maybe.Nothing[float64](), A.Lat)
	assert.Equal(t, maybe.Nothing[float64](), A.Lon)
}

// A third party header carries another station's packet, which is decoded as
// that station's.
func Test_decode_aprs_third_party(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = aprsDecoder.Decode(ax25.FromText("Q1TEST>APDW17:}Q2TEST>APDW17,TCPIP,Q1TEST*:!4903.50N/07201.75W-", true), true)
	assert.True(t, A.HasThirdPartyHeader)
	assert.Equal(t, "Q2TEST", A.Src)
	assert.Equal(t, "Position", A.DataTypeDesc)
	assertMaybeInDelta(t, maybe.Just(49+3.5/60), A.Lat, 0.000001)
}

// Print writes the human-readable form decode_aprs shows.  Apart from the
// device descriptions, which come from a newer tocalls.yaml, and the degree
// signs, which Dire Wolf leaves out, these are all what Dire Wolf prints.
func Test_decode_aprs_print(t *testing.T) {
	for _, tc := range []struct {
		monitor string
		want    string
	}{
		{
			"Q1TEST>APDW17:!4903.50N/07201.75W#PHG5132WIDE",
			"Position, Generic digipeater, WB2OSZ DireWolf, 25 W height(HAAT)=20ft=6m 3dBi E\n" +
				"N 49°03.5000, W 072°01.7500\n" +
				"WIDE\n",
		},
		{
			"Q1TEST>APDW17:!4903.50N/07201.75W-146.520MHz C100 -060 R25m",
			"Position, House, WB2OSZ DireWolf, range=25.0\n" +
				"N 49°03.5000, W 072°01.7500, 146.520 MHz, -600k, PL 100.0\n",
		},
		{
			"Q1TEST>APDW17:!4903.50N/07201.75W-146.520MHz D023 +600 Rptr",
			"Position, House, WB2OSZ DireWolf\n" +
				"N 49°03.5000, W 072°01.7500, 146.520 MHz, +6M, DCS 023\n" +
				"Rptr\n",
		},
		{
			"Q1TEST>APDW17:!4903.50N/07201.75W-146.520MHz Toff",
			"Position, House, WB2OSZ DireWolf\n" +
				"N 49°03.5000, W 072°01.7500, 146.520 MHz, no PL\n",
		},
		{
			"Q1TEST>APDW17:!4903.50X/07201.75W-",
			"Position, House, WB2OSZ DireWolf\n" +
				"Invalid Latitude, W 072°01.7500\n",
		},
		{
			"Q1TEST>APDW17:!4903.50S/07201.75E-Hello!TB2!",
			"Position, House, WB2OSZ DireWolf\n" +
				"S 49°03.5000, E 072°01.7500, APRStt location B2...\n" +
				"Hello\n",
		},
		{
			"Q1TEST>T2SP0W:`c_Vm6hk/`\"49}Q1TEST_%",
			"MIC-E, truck, Yaesu FTM-400DR, In Service\n" +
				"N 42°30.0700, W 071°07.5800, 22 km/h (14 MPH), course 276, alt 34 m (112 ft)\n" +
				"Q1TEST\n",
		},
		{
			"Q1TEST>APDW17:;LEADER   _092345z4903.50N/07201.75W>",
			"Killed Object, \"LEADER\", normal car (side view), WB2OSZ DireWolf\n" +
				"N 49°03.5000, W 072°01.7500\n",
		},
		{
			"Q1TEST>APDW17:>IO91SX/G Status",
			"Status Report, Grid Square (6 digit), WB2OSZ DireWolf\n" +
				"Grid square = IO91SX, N 51°58.7500, W 000°27.5000\n" +
				"Status\n",
		},
		{
			"Q1TEST>APDW17:$ULTW0000000001110B6E27F4FFF3897B0001035E004E04DD00030000",
			"Ultimeter, WB2OSZ DireWolf\n" +
				"wind 0.0 mph, direction 0, temperature 27.3, barometer 30.21, humidity 86\n",
		},
		{
			"Q1TEST>APDW17:T#005,199,000,255,073,123,01101001",
			"Telemetry, WB2OSZ DireWolf\n" +
				"Seq=5, A1=199, A2=0, A3=255, A4=73, A5=123, D1=0, D2=1, D3=1, D4=0, D5=1, D6=0, D7=0, D8=1\n",
		},
		{
			// Newer than Dire Wolf 1.7.
			"Q1TEST>BEACON:>hi",
			"Status Report\n" +
				"Use of \"BEACON\" in the destination field is obsolete.  You can help to improve the quality of APRS signals.\n" +
				"Tell the sender (Q1TEST) to use the proper product identifier from https://github.com/aprsorg/aprs-deviceid \n" +
				"hi\n",
		},
	} {
		// Each with a decoder of its own, so no telemetry metadata carries over.
		var aprsDecoder = NewDecoderFromDataFiles()
		var A = aprsDecoder.Decode(ax25.FromText(tc.monitor, true), true)

		var output = testutils.CaptureOutput(t, func() { aprsDecoder.Print(A) })

		assert.Equal(t, tc.want, output, "%s", tc.monitor)
	}
}

// A grid square is turned into a position when it's printed.
func Test_decode_aprs_print_maidenhead_only(t *testing.T) {
	var aprsDecoder = NewDecoderFromDataFiles()

	var A = new(Decoded)
	A.DataTypeDesc = "Status Report"
	A.SymbolCode = ' '
	A.Maidenhead = "IO91"

	var output = testutils.CaptureOutput(t, func() { aprsDecoder.Print(A) })

	assert.Equal(t, "Status Report\nGrid square = IO91, N 51°30.0000, W 001°00.0000\n", output)
	assertMaybeInDelta(t, maybe.Just(51.5), A.Lat, 0.000001)
	assertMaybeInDelta(t, maybe.Just(-1.0), A.Lon, 0.000001)
}
