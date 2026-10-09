// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package direwolf

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/ais"
	"github.com/doismellburning/samoyed/internal/aprs"
	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/testutils"
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
// report to an APRS object used to hand EncodeObject int(G_UNKNOWN + 0.5),
// which is not G_UNKNOWN, so the course was folded back into range and
// transmitted as 82 degrees - a heading nobody reported.  Absence must
// survive all the way into EncodeObject.
func Test_ais_to_object_without_course_or_speed(t *testing.T) {
	var aprsDecoder = aprs.NewDecoderFromDataFiles()

	var sentence = aisPositionReport(t, 1023, 3600)
	var pp = ax25.FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", aprs.UserDefUserID, aprs.UserDefTypeAIS, sentence), true)
	require.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)

	require.True(t, A.Lat.IsJust(), "position should have decoded")
	assert.True(t, A.Course.IsNothing(), "course should be unknown, got %v", A.Course)
	assert.True(t, A.SpeedMPH.IsNothing(), "speed should be unknown, got %v", A.SpeedMPH)

	var course, speed = ais_object_course_speed(A)
	assert.True(t, course.IsNothing(), "course should be unknown, got %v", course)
	assert.True(t, speed.IsNothing(), "speed should be unknown, got %v", speed)

	var info = aprs.EncodeObject("366730000", false, time.Time{},
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
	var aprsDecoder = aprs.NewDecoderFromDataFiles()

	var sentence = aisPositionReport(t, 208, 900) // 20.8 knots, 90 degrees
	var pp = ax25.FromText(fmt.Sprintf("Q1TEST>APRS:{%c%c%s", aprs.UserDefUserID, aprs.UserDefTypeAIS, sentence), true)
	require.NotNil(t, pp)

	var A = aprsDecoder.Decode(pp, true)

	var course, speed = ais_object_course_speed(A)
	assert.Equal(t, maybe.Just(90), course)
	assert.Equal(t, maybe.Just(21), speed)

	var info = aprs.EncodeObject("366730000", false, time.Time{},
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
			var output = testutils.CaptureOutput(t, func() {
				reportConfigCheck("dw.conf", tt.errors, tt.warnings)
			})

			assert.Equal(t, tt.want, output)
		})
	}
}

// TestMheardPosition checks that only a position report's location reaches the
// stations-heard list, so an object report can't overwrite where its sender is
// (Dire Wolf issue 545).
func TestMheardPosition(t *testing.T) {
	var position = new(aprs.Decoder).Decode(ax25.FromText("Q1TEST>APDW17:!4237.14N/07120.83W#", true), true)
	var lat, lon = mheardPosition(position)
	assert.True(t, lat.IsJust())
	assert.True(t, lon.IsJust())

	var object = new(aprs.Decoder).Decode(ax25.FromText("Q1TEST>APDW17:;OBJECT   *111111z4237.14N/07120.83W#", true), true)
	require.True(t, object.Lat.IsJust(), "the object report should carry a location to ignore")

	lat, lon = mheardPosition(object)
	assert.Equal(t, maybe.Nothing[float64](), lat)
	assert.Equal(t, maybe.Nothing[float64](), lon)
}

// teardownExitEnv, when set, has TestTeardownExitReleasesBeforeExiting exit
// the test binary the way startup does when it gives up, rather than run the
// test.
const teardownExitEnv = "SAMOYED_TEST_TEARDOWN_EXIT"

// Startup giving up part way through - a bad option once the PTT is open,
// say - stops its goroutines and releases what it acquired before it ends
// the process, as a stop does.  Ending the process takes the test binary
// with it, so the test runs a copy of itself to do it.
func TestTeardownExitReleasesBeforeExiting(t *testing.T) {
	if os.Getenv(teardownExitEnv) != "" {
		var td = new(teardownList)
		td.stop = func() { fmt.Println("stopped the goroutines") }
		td.add(func() { fmt.Println("released the ptt") })
		td.exit(3)

		return
	}

	var cmd = exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTeardownExitReleasesBeforeExiting$") //nolint:gosec // G204: our own test binary.
	cmd.Env = append(os.Environ(), teardownExitEnv+"=1")

	var out, err = cmd.CombinedOutput()
	var output = string(out)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the copy should have exited with a failure: %s", output)
	assert.Equal(t, 3, exitErr.ExitCode(), "with the code it was given")
	assert.Contains(t, output, "QRT")

	var stopped = strings.Index(output, "stopped the goroutines")
	var released = strings.Index(output, "released the ptt")

	require.GreaterOrEqual(t, stopped, 0, "the goroutines should have been stopped: %s", output)
	require.GreaterOrEqual(t, released, 0, "the PTT should have been released: %s", output)
	assert.Less(t, stopped, released, "the goroutines should be stopped before the PTT is released")
}
