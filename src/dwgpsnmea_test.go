package direwolf

import (
	"testing"

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
			errContains: "Missing GPS checksum",
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
		assert.InDelta(t, 42.618733, result.Lat, 0.0001)
		assert.InDelta(t, -71.347222, result.Lon, 0.001)
		assert.InDelta(t, 5.07, result.Knots, 0.001)
		assert.InDelta(t, 291.42, result.Course, 0.01)
	})

	t.Run("void status returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gprmc("$GPRMC,001431.00,V,,,,,,,121015,,,N*7C", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_NO_FIX, result.Fix)
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
		assert.InDelta(t, 42.618750, result.Lat, 0.0001)
		assert.InDelta(t, -71.347212, result.Lon, 0.001)
		assert.InDelta(t, 33.5, result.Alt, 0.001)
	})

	t.Run("fix field zero returns no fix", func(t *testing.T) {
		// Example from source code comments.
		var result = dwgpsnmea_gpgga("$GPGGA,001429.00,,,,,0,00,99.99,,,,,,*68", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_NO_FIX, result.Fix)
	})

	t.Run("bad checksum returns error", func(t *testing.T) {
		var result = dwgpsnmea_gpgga("$GPGGA,003518.710,4237.1250,N,07120.8327,W,1,03,5.9,33.5,M,-33.5,M,,0000*00", true)

		require.NotNil(t, result)
		assert.Equal(t, DWFIX_ERROR, result.Fix)
	})
}
