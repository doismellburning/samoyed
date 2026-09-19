// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
)

// Test that decode_aprs does not panic on an AX.25 UI frame with an empty
// information field, as sent by linbpq ID broadcasts (issue #504).
func Test_decode_aprs_empty_info(t *testing.T) {
	deviceIDData = NewDeviceIDData()

	var pp = AX25FromText("Q1TEST>ID:", true)
	assert.NotNil(t, pp)

	// Must not panic, and must return a populated struct.
	var A = decode_aprs(pp, true, "")
	assert.NotNil(t, A)
	assert.Equal(t, "AX.25 UI frame with empty information field", A.g_data_type_desc)
	assert.Equal(t, "Q1TEST", A.g_src)
	assert.Equal(t, "ID", A.g_dest)
}

// !DAO! adds a digit of resolution to a position that has already been
// decoded.  When the position was unknown, the addition used to be applied to
// the G_UNKNOWN sentinel anyway, giving -999999.00015, which is not G_UNKNOWN
// and so was taken for a real position by everything downstream.  An unknown
// longitude must stay unknown.
func Test_decode_aprs_dao_does_not_invent_a_position(t *testing.T) {
	deviceIDData = NewDeviceIDData()

	var pp = AX25FromText("Q1TEST>APRS:!4237.14N/xxxxx.83W-Hi!W99!", true)
	assert.NotNil(t, pp)

	var A = decode_aprs(pp, true, "")

	assert.True(t, A.g_lon.IsNothing(), "longitude should still be unknown, got %v", A.g_lon)

	// The latitude, which did decode, still gets its extra DAO digit:
	// 42 degrees 37.14 minutes, plus 9 ten-thousandths of a minute.
	var lat, ok = A.g_lat.Get()
	assert.True(t, ok)
	assert.InDelta(t, 42.619+9.0/60000.0, lat, 0.0000001)
}

// The zero value of every optional field means "unknown", so a struct built
// anywhere other than decode_aprs (beacon.go does this) does not start out
// claiming a position off the coast of Africa.
func Test_decode_aprs_zero_value_has_no_position(t *testing.T) {
	var A decode_aprs_t

	assert.Equal(t, maybe.Nothing[float64](), A.g_lat)
	assert.Equal(t, maybe.Nothing[float64](), A.g_lon)
	assert.Equal(t, maybe.Nothing[float64](), A.g_speed_mph)
	assert.Equal(t, maybe.Nothing[int](), A.g_power)
}

// The bridges to the subsystems that still use the sentinel must not turn an
// absent value into a real one: a GPS fix carries no speed until it has one,
// and wrapping G_UNKNOWN in Just would log -999999 MPH as though it had been
// measured.
func Test_decode_aprs_sentinel_bridges(t *testing.T) {
	assert.Equal(t, maybe.Nothing[float64](), unlessUnknown(float64(G_UNKNOWN)))
	assert.Equal(t, maybe.Nothing[int](), unlessUnknown(int(G_UNKNOWN)))
	assert.Equal(t, maybe.Just(0.0), unlessUnknown(0.0))

	assert.InDelta(t, float64(G_UNKNOWN), orUnknown(maybe.Nothing[float64]()), 0.001)
	assert.Equal(t, G_UNKNOWN, orUnknown(maybe.Nothing[int]()))
	assert.InDelta(t, 0.0, orUnknown(maybe.Just(0.0)), 0.001)

	// A conversion of an unknown value stays unknown rather than becoming a
	// number that no longer looks like the sentinel.
	assert.Equal(t, maybe.Nothing[float64](), maybe.Fmap(DW_KNOTS_TO_MPH, unlessUnknown(float64(G_UNKNOWN))))
}

// A weather report that stops in the middle of its fields used to run the
// decoder off the end of the information field and panic, taking the whole
// program down with it - from a received packet, so anyone within earshot
// could do it.  Each of these is truncated at a different point.
func Test_decode_aprs_truncated_weather(t *testing.T) {
	deviceIDData = NewDeviceIDData()

	for _, info := range []string{
		"!4903.50N/07201.75W_",            // nothing at all after the symbol
		"!4903.50N/07201.75W_22",          // part way through the wind direction
		"!4903.50N/07201.75W_220/004",     // wind, then nothing
		"!4903.50N/07201.75W_220/004g005", // ends after a complete field
		"!4903.50N/07201.75W_220/004g00",  // ends part way through a field
		"!4903.50N/07201.75W_c220s004g005t077r000p000P000h50b099",
	} {
		var pp = AX25FromText("Q1TEST>APRS:"+info, true)
		assert.NotNil(t, pp)

		// Must not panic, and must still report the position.
		var A = decode_aprs(pp, true, "")

		var lat, ok = A.g_lat.Get()
		assert.True(t, ok, "%s", info)
		assert.InDelta(t, 49.0583333, lat, 0.0000001, "%s", info)
	}

	// What did arrive before the truncation is still decoded.
	var A = decode_aprs(AX25FromText("Q1TEST>APRS:!4903.50N/07201.75W_220/004g005", true), true, "")
	assert.Equal(t, `wind 4.6 mph, direction 220, gust 5, ""`, A.g_weather)
}
