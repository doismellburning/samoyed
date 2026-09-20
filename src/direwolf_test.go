// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"fmt"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ais"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aisPositionReport builds the NMEA sentence for an AIS type 1 position
// report at Boston, with the given raw speed over ground and course over
// ground.  1023 and 3600 respectively are "not available".
func aisPositionReport(t *testing.T, rawSpeed int, rawCourse int) string {
	t.Helper()

	var payload = make([]byte, 21) // 168 bits

	ais.SetField(payload, 0, 6, 1)          // message type
	ais.SetField(payload, 8, 30, 366730000) // MMSI
	ais.SetField(payload, 50, 10, rawSpeed)
	ais.SetField(payload, 61, 28, int(-71.06*600000)) // longitude, minutes/10000
	ais.SetField(payload, 89, 27, int(42.36*600000))  // latitude, minutes/10000
	ais.SetField(payload, 116, 12, rawCourse)

	var nmea, err = ais.ToNMEA(payload)
	require.NoError(t, err)

	return string(nmea)
}

// An AIS station may report neither speed nor course.  Converting such a
// report to an APRS object used to hand encode_object int(G_UNKNOWN + 0.5),
// which is not G_UNKNOWN, so the course was folded back into range and
// transmitted as 82 degrees - a heading nobody reported.  Absence must
// survive all the way into encode_object.
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
	assert.True(t, course.IsNothing(), "course should be unknown, got %v", course)
	assert.True(t, speed.IsNothing(), "speed should be unknown, got %v", speed)

	var info = encode_object("366730000", false, time.Time{},
		42.36, -71.06, 0,
		'/', 's',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "",
		course, speed,
		maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Nothing[float64](), "")

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
	assert.Equal(t, maybe.Just(90), course)
	assert.Equal(t, maybe.Just(21), speed)

	var info = encode_object("366730000", false, time.Time{},
		42.36, -71.06, 0,
		'/', 's',
		maybe.Nothing[int](), maybe.Nothing[int](), maybe.Nothing[int](), "",
		course, speed,
		maybe.Nothing[float64](), maybe.Nothing[float64](), maybe.Nothing[float64](), "")

	assert.Equal(t, ";366730000*111111z4221.60N/07103.60Ws090/021", info)
}

// --- --config-check summary ---

func Test_reportConfigCheck(t *testing.T) {
	tests := []struct {
		name     string
		errors   int
		warnings int
		want     string
	}{
		{
			name:     "a clean file says so rather than counting nothing",
			errors:   0,
			warnings: 0,
			want:     "\nConfiguration file dw.conf: no problems found.\n",
		},
		{
			name:     "one of each is singular",
			errors:   1,
			warnings: 1,
			want:     "\nConfiguration file dw.conf: 1 error, 1 warning.\n",
		},
		{
			name:     "several of each are plural",
			errors:   3,
			warnings: 2,
			want:     "\nConfiguration file dw.conf: 3 errors, 2 warnings.\n",
		},
		{
			// Warnings do not fail the check, but they are still worth saying
			// out loud - so a file with only warnings is not "no problems".
			name:     "warnings alone are still reported",
			errors:   0,
			warnings: 1,
			want:     "\nConfiguration file dw.conf: 0 errors, 1 warning.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output = CaptureOutput(t, func() {
				reportConfigCheck("dw.conf", tt.errors, tt.warnings)
			})

			assert.Equal(t, tt.want, output)
		})
	}
}
