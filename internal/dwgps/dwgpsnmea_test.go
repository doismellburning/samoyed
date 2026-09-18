// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwgps

import (
	"io"
	"testing"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- remove_checksum ---

func Test_remove_checksum(t *testing.T) {
	tests := []struct {
		name        string
		sent        string
		wantErr     bool
		wantResult  string
		errContains string
	}{
		{
			name:        "valid GPRMC sentence",
			sent:        "$GPRMC,001431.00,V,,,,,,,121015,,,N*7C",
			wantErr:     false,
			wantResult:  "$GPRMC,001431.00,V,,,,,,,121015,,,N",
			errContains: "",
		},
		{
			name:        "valid sentence with position",
			sent:        "$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F",
			wantErr:     false,
			wantResult:  "$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A",
			errContains: "",
		},
		{
			name:        "missing asterisk",
			sent:        "$GPRMC,001431.00",
			wantErr:     true,
			errContains: "missing GPS checksum",
			wantResult:  "",
		},
		{
			name:        "wrong checksum",
			sent:        "$GPRMC,001431.00,V,,,,,,,121015,,,N*00",
			wantErr:     true,
			errContains: "GPS checksum error",
			wantResult:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result, err = remove_checksum(tt.sent, true)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Empty(t, result)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}

// --- ParseGPRMC ---

func Test_ParseGPRMC(t *testing.T) {
	t.Run("active fix with position, speed, and course", func(t *testing.T) {
		// Example from source code comments.
		var result = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F", true)

		require.NotNil(t, result)
		assert.Equal(t, Fix2D, result.Fix)
		assert.InDelta(t, 42.618733, maybe.FromJust(result.Lat), 0.0001)
		assert.InDelta(t, -71.347222, maybe.FromJust(result.Lon), 0.001)
		assert.InDelta(t, 5.07, maybe.FromJust(result.Knots), 0.001)
		assert.InDelta(t, 291.42, maybe.FromJust(result.Course), 0.01)
	})

	t.Run("empty course field leaves course unknown", func(t *testing.T) {
		// Stationary: speed is reported but the course field is empty.
		var result = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,0.00,,160614,,,A*6F", true)

		require.NotNil(t, result)
		assert.Equal(t, Fix2D, result.Fix)
		assert.Equal(t, maybe.Just(0.0), result.Knots)
		assert.Equal(t, maybe.Nothing[float64](), result.Course)
	})

	t.Run("sentinel speed and course stay unknown", func(t *testing.T) {
		// -999999 parses as a float but is the G_UNKNOWN sentinel, so it must
		// not come back as a speed anyone reported.
		var speed = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,-999999,291.42,160614,,,A*4E", true)

		require.NotNil(t, speed)
		assert.Equal(t, maybe.Nothing[float64](), speed.Knots)

		var course = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,-999999,160614,,,A*40", true)

		require.NotNil(t, course)
		assert.Equal(t, maybe.Nothing[float64](), course.Course)
	})

	t.Run("void status returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = ParseGPRMC("$GPRMC,001431.00,V,,,,,,,121015,,,N*7C", true)

		require.NotNil(t, result)
		assert.Equal(t, FixNoFix, result.Fix)
	})

	t.Run("unparseable latitude leaves position unknown", func(t *testing.T) {
		// LatitudeFromNMEA returns an error for a field it can't parse; that
		// must not reach the caller as a position, and a sentence carrying one
		// is as unusable as a sentence with no latitude field at all.
		var result = ParseGPRMC("$GPRMC,003413.710,A,X237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*13", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("out of range latitude leaves position unknown", func(t *testing.T) {
		var result = ParseGPRMC("$GPRMC,003413.710,A,9537.1240,N,07120.8333,W,5.07,291.42,160614,,,A*75", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("bad hemisphere leaves position unknown", func(t *testing.T) {
		var result = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,X,07120.8333,W,5.07,291.42,160614,,,A*69", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("bad checksum returns error", func(t *testing.T) {
		var result = ParseGPRMC("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*00", true)

		require.NotNil(t, result)
		assert.Equal(t, FixError, result.Fix)
	})
}

// --- ParseGPGGA ---

func Test_ParseGPGGA(t *testing.T) {
	t.Run("valid 3D fix with altitude", func(t *testing.T) {
		// Example from source code comments.
		var result = ParseGPGGA("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B", true)

		require.NotNil(t, result)
		assert.Equal(t, Fix3D, result.Fix)
		assert.InDelta(t, 42.618750, maybe.FromJust(result.Lat), 0.0001)
		assert.InDelta(t, -71.347212, maybe.FromJust(result.Lon), 0.001)
		assert.InDelta(t, 33.5, maybe.FromJust(result.Alt), 0.001)
	})

	t.Run("2D fix leaves altitude unknown", func(t *testing.T) {
		// Example from source code comments: altitude field is empty.
		var result = ParseGPGGA("$GPGGA,212407.000,4237.1505,N,07120.8602,W,0,00,,,M,,M,,*58", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Alt)
	})

	t.Run("sentinel altitude stays unknown", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,-999999,M,-33.5,M,,0000*6D", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Alt)
	})

	t.Run("fix field zero returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = ParseGPGGA("$GPGGA,001429.00,,,,,0,00,99.99,,,,,,*68", true)

		require.NotNil(t, result)
		assert.Equal(t, FixNoFix, result.Fix)
	})

	// GPGGA runs the same error-handling path as GPRMC, so it needs the same
	// cases: a field that is present but unusable must leave the position
	// unknown and the fix in error, whatever is wrong with it.

	t.Run("unparseable latitude leaves position unknown", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,X237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*37", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("out of range latitude leaves position unknown", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,9537.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*51", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("sixty minutes of latitude leaves position unknown", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,4260.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*59", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("bad hemisphere leaves position unknown", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,4237.1250,X,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*4D", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, FixError, result.Fix)
	})

	t.Run("bad checksum returns error", func(t *testing.T) {
		var result = ParseGPGGA("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*00", true)

		require.NotNil(t, result)
		assert.Equal(t, FixError, result.Fix)
	})
}

// Test_read_gpsnmea_thread feeds the reader the sentences a GPS receiver would
// send and checks that a location fix comes out the other end.
func Test_read_gpsnmea_thread(t *testing.T) {
	var reader, writer = io.Pipe()

	setData(new(Info))

	var done = make(chan struct{})

	go func() {
		defer close(done)

		read_gpsnmea_thread(reader)
	}()

	t.Cleanup(func() {
		require.NoError(t, writer.Close())
		<-done             // So the thread can't touch the shared data after we've gone.
		setData(new(Info)) // And leave it as we found it, for everyone else.
	})

	// RMC carries course and speed, GGA the position and altitude, so both are
	// needed for a 3D fix.
	var _, writeErr = writer.Write([]byte(
		"$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F\r\n" +
			"$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B\r\n"))
	require.NoError(t, writeErr)

	var info = new(Info)

	var fix Fix

	var deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		fix = Read(info)
		if fix >= Fix3D {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	require.Equal(t, Fix3D, fix, "never got a 3D location fix from the NMEA sentences")

	assert.InDelta(t, 42.618750, maybe.FromJust(info.Lat), 0.000001)
	assert.InDelta(t, -71.347212, maybe.FromJust(info.Lon), 0.000001)
	assert.InDelta(t, 33.5, maybe.FromJust(info.Altitude), 0.001)
	assert.InDelta(t, 5.07, maybe.FromJust(info.SpeedKnots), 0.001)
	assert.InDelta(t, 291.42, maybe.FromJust(info.Track), 0.001)
}
