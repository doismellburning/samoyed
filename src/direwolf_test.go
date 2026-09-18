// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aisPositionReport builds the NMEA sentence for an AIS type 1 position
// report at Boston, with the given raw speed over ground and course over
// ground.  1023 and 3600 respectively are "not available".
func aisPositionReport(t *testing.T, rawSpeed int, rawCourse int) string {
	t.Helper()

	var ais = make([]byte, 21) // 168 bits

	set_field(ais, 0, 6, 1)          // message type
	set_field(ais, 8, 30, 366730000) // MMSI
	set_field(ais, 50, 10, rawSpeed)
	set_field(ais, 61, 28, int(-71.06*600000)) // longitude, minutes/10000
	set_field(ais, 89, 27, int(42.36*600000))  // latitude, minutes/10000
	set_field(ais, 116, 12, rawCourse)

	var nmea, err = AISToNMEA(ais)
	require.NoError(t, err)

	return string(nmea)
}

// An AIS station may report neither speed nor course.  Converting such a
// report to an APRS object used to hand encode_object int(G_UNKNOWN + 0.5),
// which is not G_UNKNOWN, so the course was folded back into range and
// transmitted as 82 degrees - a heading nobody reported.  Absence must
// survive as the sentinel encode_object recognises.
func Test_ais_to_object_without_course_or_speed(t *testing.T) {
	deviceIDData = NewDeviceIDData()

	var sentence = aisPositionReport(t, 1023, 3600)
	var pp = AX25FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", USER_DEF_USER_ID, USER_DEF_TYPE_AIS, sentence), true)
	require.NotNil(t, pp)

	var A = decode_aprs(pp, true, "")

	require.True(t, A.g_lat.IsJust(), "position should have decoded")
	assert.True(t, A.g_course.IsNothing(), "course should be unknown, got %v", A.g_course)
	assert.True(t, A.g_speed_mph.IsNothing(), "speed should be unknown, got %v", A.g_speed_mph)

	var course, speed = ais_object_course_speed(A)
	assert.Equal(t, G_UNKNOWN, course)
	assert.Equal(t, G_UNKNOWN, speed)

	var info = encode_object("366730000", false, time.Time{},
		42.36, -71.06, 0,
		'/', 's',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "",
		course, speed,
		0, 0, 0, "")

	// The course/speed data extension is "ccc/sss" straight after the symbol.
	assert.Equal(t, ";366730000*111111z4221.60N/07103.60Ws", info)
	assert.NotContains(t, info, "082/000")
}

// The same report with a course and speed still gets its data extension, so
// the test above is not passing for want of anything to encode.
func Test_ais_to_object_with_course_and_speed(t *testing.T) {
	deviceIDData = NewDeviceIDData()

	var sentence = aisPositionReport(t, 208, 900) // 20.8 knots, 90 degrees
	var pp = AX25FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", USER_DEF_USER_ID, USER_DEF_TYPE_AIS, sentence), true)
	require.NotNil(t, pp)

	var A = decode_aprs(pp, true, "")

	var course, speed = ais_object_course_speed(A)
	assert.Equal(t, 90, course)
	assert.Equal(t, 21, speed)

	var info = encode_object("366730000", false, time.Time{},
		42.36, -71.06, 0,
		'/', 's',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "",
		course, speed,
		0, 0, 0, "")

	assert.Equal(t, ";366730000*111111z4221.60N/07103.60Ws090/021", info)
}
