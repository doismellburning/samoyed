package direwolf

import (
	"testing"

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

// --- dwgpsnmea_gprmc ---

func Test_dwgpsnmea_gprmc(t *testing.T) {
	t.Run("active fix with position, speed, and course", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*7F", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_2D, result.Fix)
		assert.InDelta(t, 42.618733, maybe.FromJust(result.Lat), 0.0001)
		assert.InDelta(t, -71.347222, maybe.FromJust(result.Lon), 0.001)
		assert.InDelta(t, 5.07, maybe.FromJust(result.Knots), 0.001)
		assert.InDelta(t, 291.42, maybe.FromJust(result.Course), 0.01)
	})

	t.Run("empty course field leaves course unknown", func(t *testing.T) {
		// Stationary: speed is reported but the course field is empty.
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,0.00,,160614,,,A*6F", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_2D, result.Fix)
		assert.Equal(t, maybe.Just(0.0), result.Knots)
		assert.Equal(t, maybe.Nothing[float64](), result.Course)
	})

	t.Run("negative speed is an error", func(t *testing.T) {
		// Speed over ground is a magnitude, so a receiver cannot have measured
		// this and the sentence is no more usable than one whose speed field
		// isn't a number.
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,-5.07,291.42,160614,,,A*52", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
		assert.Equal(t, maybe.Nothing[float64](), result.Knots)
	})

	t.Run("non-finite speed is an error", func(t *testing.T) {
		// ParseFloat is happy to read these, and int(NaN) is not defined.
		for _, field := range []string{"inf", "NaN"} {
			var sentence = "$GPRMC,003413.710,A,4237.1240,N,07120.8333,W," + field + ",291.42,160614,,,A*02"
			var result = dwgpsnmea_gprmc(sentence, true)

			require.NotNil(t, result)
			assert.Equal(t, DWFIX_ERROR, result.Fix, field)
			assert.Equal(t, maybe.Nothing[float64](), result.Knots, field)
		}
	})

	t.Run("course outside a circle stays unknown", func(t *testing.T) {
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,361.00,160614,,,A*77", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_2D, result.Fix)
		assert.Equal(t, maybe.Nothing[float64](), result.Course)

		// 360 is the same direction as 0 and receivers do send it.
		var wrapped = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,360.00,160614,,,A*76", true)

		require.NotNil(t, wrapped)
		assert.Equal(t, maybe.Just(360.0), wrapped.Course)
	})

	t.Run("void status returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gprmc("$GPRMC,001431.00,V,,,,,,,121015,,,N*7C", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_NO_FIX, result.Fix)
	})

	t.Run("unparseable latitude leaves position unknown", func(t *testing.T) {
		// latitude_from_nmea returns an error for a field it can't parse; that
		// must not reach the caller as a position, and a sentence carrying one
		// is as unusable as a sentence with no latitude field at all.
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,X237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*13", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("out of range latitude leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,9537.1240,N,07120.8333,W,5.07,291.42,160614,,,A*75", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("bad hemisphere leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,X,07120.8333,W,5.07,291.42,160614,,,A*69", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("bad checksum returns error", func(t *testing.T) {
		var result = dwgpsnmea_gprmc("$GPRMC,003413.710,A,4237.1240,N,07120.8333,W,5.07,291.42,160614,,,A*00", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})
}

// --- dwgpsnmea_gpgga ---

func Test_dwgpsnmea_gpgga(t *testing.T) {
	t.Run("valid 3D fix with altitude", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*5B", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_3D, result.Fix)
		assert.InDelta(t, 42.618750, maybe.FromJust(result.Lat), 0.0001)
		assert.InDelta(t, -71.347212, maybe.FromJust(result.Lon), 0.001)
		assert.InDelta(t, 33.5, maybe.FromJust(result.Alt), 0.001)
	})

	t.Run("2D fix leaves altitude unknown", func(t *testing.T) {
		// Example from source code comments: altitude field is empty.
		var result = dwgpsnmea_gpgga("$GPGGA,212407.000,4237.1505,N,07120.8602,W,0,00,,,M,,M,,*58", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Alt)
	})

	t.Run("an implausible altitude is still an altitude", func(t *testing.T) {
		// -999999 was the G_UNKNOWN sentinel, and a value a receiver would
		// never send, but it is a number and nothing here filters numbers for
		// plausibility.  This pins that deliberate choice so a sentinel-shaped
		// special case cannot creep back in unnoticed.
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,-999999,M,-33.5,M,,0000*6D", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_3D, result.Fix)
		assert.Equal(t, maybe.Just(-999999.0), result.Alt)
	})

	t.Run("non-finite altitude is an error", func(t *testing.T) {
		// Unlike -999999, these do not survive conversion to the integer feet
		// of an /A= field.
		for _, field := range []string{"NaN", "inf"} {
			var sentence = "$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9," + field + ",M,-33.5,M,,0000*21"
			var result = dwgpsnmea_gpgga(sentence, true)

			require.NotNil(t, result)
			assert.Equal(t, DWFIX_ERROR, result.Fix, field)
			assert.Equal(t, maybe.Nothing[float64](), result.Alt, field)
		}
	})

	t.Run("fix field zero returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gpgga("$GPGGA,001429.00,,,,,0,00,99.99,,,,,,*68", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_NO_FIX, result.Fix)
	})

	// GPGGA runs the same error-handling path as GPRMC, so it needs the same
	// cases: a field that is present but unusable must leave the position
	// unknown and the fix in error, whatever is wrong with it.

	t.Run("unparseable latitude leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,X237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*37", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("out of range latitude leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,9537.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*51", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("sixty minutes of latitude leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4260.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*59", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("bad hemisphere leaves position unknown", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4237.1250,X,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*4D", true)

		require.NotNil(t, result)
		assert.Equal(t, maybe.Nothing[float64](), result.Lat)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})

	t.Run("bad checksum returns error", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*00", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})
}
