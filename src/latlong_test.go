package direwolf

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLatitudeToNMEA tests conversion of latitude to NMEA format
func TestLatitudeToNMEA(t *testing.T) {
	tests := []struct {
		name        string
		lat         float64
		expectedStr string
		expectedHem string
	}{
		{
			name:        "north latitude middle value",
			lat:         42.3601,
			expectedStr: "4221.6060",
			expectedHem: "N",
		},
		{
			name:        "south latitude",
			lat:         -33.8688,
			expectedStr: "3352.1280",
			expectedHem: "S",
		},
		{
			name:        "zero latitude",
			lat:         0.0,
			expectedStr: "0000.0000",
			expectedHem: "N",
		},
		{
			name:        "north pole",
			lat:         90.0,
			expectedStr: "9000.0000",
			expectedHem: "N",
		},
		{
			name:        "south pole",
			lat:         -90.0,
			expectedStr: "9000.0000",
			expectedHem: "S",
		},
		{
			name:        "small positive latitude",
			lat:         0.0166666666, // 1 minute
			expectedStr: "0001.0000",
			expectedHem: "N",
		},
		{
			name:        "small negative latitude",
			lat:         -0.0166666666,
			expectedStr: "0001.0000",
			expectedHem: "S",
		},
		{
			name:        "unknown latitude returns empty",
			lat:         G_UNKNOWN,
			expectedStr: "",
			expectedHem: "",
		},
		{
			name:        "latitude with rounding edge case",
			lat:         45.99999,
			expectedStr: "4559.9994", // NMEA has more precision than APRS format
			expectedHem: "N",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, hem := latitude_to_nmea(tt.lat)
			assert.Equal(t, tt.expectedStr, str, "latitude string should match")
			assert.Equal(t, tt.expectedHem, hem, "hemisphere should match")
		})
	}
}

// TestLatitudeToNMEABounds tests latitude bounds checking
func TestLatitudeToNMEABounds(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
	}{
		{"latitude below -90 clamped", -100.0},
		{"latitude above 90 clamped", 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, hem := latitude_to_nmea(tt.lat)
			// Should clamp to valid range
			assert.NotEmpty(t, str, "should return a string even for out of bounds")
			assert.NotEmpty(t, hem, "should return hemisphere even for out of bounds")
			assert.Equal(t, "9000.0000", str, "should clamp to 90 degrees")
		})
	}
}

// TestLongitudeToNMEA tests conversion of longitude to NMEA format
func TestLongitudeToNMEA(t *testing.T) {
	tests := []struct {
		name        string
		lon         float64
		expectedStr string
		expectedHem string
	}{
		{
			name:        "east longitude middle value",
			lon:         151.2093,
			expectedStr: "15112.5580",
			expectedHem: "E",
		},
		{
			name:        "west longitude",
			lon:         -71.0589,
			expectedStr: "07103.5340",
			expectedHem: "W",
		},
		{
			name:        "zero longitude",
			lon:         0.0,
			expectedStr: "00000.0000",
			expectedHem: "E",
		},
		{
			name:        "180 longitude",
			lon:         180.0,
			expectedStr: "18000.0000",
			expectedHem: "E",
		},
		{
			name:        "-180 longitude",
			lon:         -180.0,
			expectedStr: "18000.0000",
			expectedHem: "W",
		},
		{
			name:        "small positive longitude",
			lon:         0.0166666666, // 1 minute
			expectedStr: "00001.0000",
			expectedHem: "E",
		},
		{
			name:        "small negative longitude",
			lon:         -0.0166666666,
			expectedStr: "00001.0000",
			expectedHem: "W",
		},
		{
			name:        "unknown longitude returns empty",
			lon:         G_UNKNOWN,
			expectedStr: "",
			expectedHem: "",
		},
		{
			name:        "longitude with rounding edge case",
			lon:         45.99999,
			expectedStr: "04559.9994", // NMEA has more precision than APRS format
			expectedHem: "E",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, hem := longitude_to_nmea(tt.lon)
			assert.Equal(t, tt.expectedStr, str, "longitude string should match")
			assert.Equal(t, tt.expectedHem, hem, "hemisphere should match")
		})
	}
}

// TestLongitudeToNMEABounds tests longitude bounds checking
func TestLongitudeToNMEABounds(t *testing.T) {
	tests := []struct {
		name string
		lon  float64
	}{
		{"longitude below -180 clamped", -200.0},
		{"longitude above 180 clamped", 200.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str, hem := longitude_to_nmea(tt.lon)
			// Should clamp to valid range
			assert.NotEmpty(t, str, "should return a string even for out of bounds")
			assert.NotEmpty(t, hem, "should return hemisphere even for out of bounds")
			assert.Equal(t, "18000.0000", str, "should clamp to 180 degrees")
		})
	}
}

// TestLatitudeFromNMEA tests parsing NMEA latitude format
func TestLatitudeFromNMEA(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		hemi     byte
		expected float64
		delta    float64 // tolerance for floating point comparison
	}{
		{
			name:     "north latitude",
			str:      "4221.6060",
			hemi:     'N',
			expected: 42.3601,
			delta:    0.0001,
		},
		{
			name:     "south latitude",
			str:      "3352.1280",
			hemi:     'S',
			expected: -33.8688,
			delta:    0.0001,
		},
		{
			name:     "zero latitude",
			str:      "0000.0000",
			hemi:     'N',
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "north pole",
			str:      "9000.0000",
			hemi:     'N',
			expected: 90.0,
			delta:    0.0001,
		},
		{
			name:     "south pole",
			str:      "9000.0000",
			hemi:     'S',
			expected: -90.0,
			delta:    0.0001,
		},
		{
			name:     "two decimal places",
			str:      "4221.60",
			hemi:     'N',
			expected: 42.36,
			delta:    0.01,
		},
		{
			name:     "three decimal places",
			str:      "4221.606",
			hemi:     'N',
			expected: 42.3601,
			delta:    0.001,
		},
		{
			name:     "zero hemisphere treated as north",
			str:      "4221.6060",
			hemi:     0,
			expected: 42.3601,
			delta:    0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := latitude_from_nmea(tt.str, tt.hemi)
			if tt.expected == G_UNKNOWN {
				assert.InDelta(t, float64(G_UNKNOWN), result, 0.0001, "should return G_UNKNOWN")
			} else {
				assert.InDelta(t, tt.expected, result, tt.delta, "latitude should match")
			}
		})
	}
}

// TestLatitudeFromNMEAErrors tests error cases for NMEA latitude parsing
func TestLatitudeFromNMEAErrors(t *testing.T) {
	tests := []struct {
		name string
		str  string
		hemi byte
	}{
		{"too short", "123", 'N'},
		{"empty string", "", 'N'},
		{"no decimal point", "422160", 'N'},
		{"decimal in wrong position", "42.216060", 'N'},
		{"non-digit at start", "X221.6060", 'N'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := latitude_from_nmea(tt.str, tt.hemi)
			// Should return G_UNKNOWN for invalid input
			assert.InDelta(t, float64(G_UNKNOWN), result, 0.0001, "should return G_UNKNOWN for invalid input")
		})
	}

	// Note: Invalid hemisphere prints error but still processes the data,
	// so we just check it doesn't panic
	t.Run("invalid_hemisphere", func(t *testing.T) {
		_ = latitude_from_nmea("4221.6060", 'X')
		// Test passes if it doesn't panic
	})
}

// TestLongitudeFromNMEA tests parsing NMEA longitude format
func TestLongitudeFromNMEA(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		hemi     byte
		expected float64
		delta    float64
	}{
		{
			name:     "east longitude",
			str:      "15112.5580",
			hemi:     'E',
			expected: 151.2093,
			delta:    0.0001,
		},
		{
			name:     "west longitude",
			str:      "07103.5340",
			hemi:     'W',
			expected: -71.0589,
			delta:    0.0001,
		},
		{
			name:     "zero longitude",
			str:      "00000.0000",
			hemi:     'E',
			expected: 0.0,
			delta:    0.0001,
		},
		{
			name:     "180 longitude east",
			str:      "18000.0000",
			hemi:     'E',
			expected: 180.0,
			delta:    0.0001,
		},
		{
			name:     "180 longitude west",
			str:      "18000.0000",
			hemi:     'W',
			expected: -180.0,
			delta:    0.0001,
		},
		{
			name:     "two decimal places",
			str:      "15112.55",
			hemi:     'E',
			expected: 151.2092,
			delta:    0.01,
		},
		{
			name:     "three decimal places",
			str:      "15112.558",
			hemi:     'E',
			expected: 151.2093,
			delta:    0.001,
		},
		{
			name:     "zero hemisphere treated as east",
			str:      "15112.5580",
			hemi:     0,
			expected: 151.2093,
			delta:    0.0001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := longitude_from_nmea(tt.str, tt.hemi)
			if tt.expected == G_UNKNOWN {
				assert.InDelta(t, float64(G_UNKNOWN), result, 0.0001, "should return G_UNKNOWN")
			} else {
				assert.InDelta(t, tt.expected, result, tt.delta, "longitude should match")
			}
		})
	}
}

// TestLongitudeFromNMEAErrors tests error cases for NMEA longitude parsing
func TestLongitudeFromNMEAErrors(t *testing.T) {
	tests := []struct {
		name string
		str  string
		hemi byte
	}{
		{"too short", "12345", 'E'},
		{"empty string", "", 'E'},
		{"no decimal point", "1511255", 'E'},
		{"decimal in wrong position", "151.125580", 'E'},
		{"non-digit at start", "X5112.5580", 'E'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := longitude_from_nmea(tt.str, tt.hemi)
			assert.InDelta(t, float64(G_UNKNOWN), result, 0.0001, "should return G_UNKNOWN for invalid input")
		})
	}

	// Note: Invalid hemisphere prints error but still processes the data,
	// so we just check it doesn't panic
	t.Run("invalid_hemisphere", func(t *testing.T) {
		_ = longitude_from_nmea("15112.5580", 'X')
		// Test passes if it doesn't panic
	})
}

// TestNMEARoundTrip tests that converting to/from NMEA preserves values
func TestNMEARoundTrip(t *testing.T) {
	tests := []struct {
		name string
		lat  float64
		lon  float64
	}{
		{"Boston coordinates", 42.3601, -71.0589},
		{"Sydney coordinates", -33.8688, 151.2093},
		{"Equator prime meridian", 0.0, 0.0},
		{"North pole", 90.0, 0.0},
		{"South pole", -90.0, 0.0},
		{"International date line", 0.0, 180.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to NMEA
			latStr, latHem := latitude_to_nmea(tt.lat)
			lonStr, lonHem := longitude_to_nmea(tt.lon)

			// Convert back
			lat := latitude_from_nmea(latStr, latHem[0])
			lon := longitude_from_nmea(lonStr, lonHem[0])

			// Check round trip (NMEA has 4 decimal places for minutes, about 0.00002 degree precision)
			assert.InDelta(t, tt.lat, lat, 0.0001, "latitude should survive round trip")
			assert.InDelta(t, tt.lon, lon, 0.0001, "longitude should survive round trip")
		})
	}
}

// TestGridSquareEdgeCases tests Maidenhead grid square conversion edge cases
func TestGridSquareEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		grid      string
		expectErr bool
		minLat    float64
		maxLat    float64
		minLon    float64
		maxLon    float64
	}{
		{
			name:      "2 character grid",
			grid:      "BL",
			expectErr: false,
			minLat:    15.0,
			maxLat:    35.0,
			minLon:    -160.0,
			maxLon:    -140.0,
		},
		{
			name:      "4 character grid",
			grid:      "BL11",
			expectErr: false,
			minLat:    20.49,
			maxLat:    21.51,
			minLon:    -157.01,
			maxLon:    -156.99,
		},
		{
			name:      "6 character grid",
			grid:      "BL11BH",
			expectErr: false,
			minLat:    21.31,
			maxLat:    21.32,
			minLon:    -157.88,
			maxLon:    -157.87,
		},
		{
			name:      "lowercase should work",
			grid:      "bl11bh",
			expectErr: false,
			minLat:    21.31,
			maxLat:    21.32,
			minLon:    -157.88,
			maxLon:    -157.87,
		},
		{ //nolint: exhaustruct
			name:      "odd number of characters fails",
			grid:      "BL1",
			expectErr: true,
		},
		{ //nolint: exhaustruct
			name:      "empty string fails",
			grid:      "",
			expectErr: true,
		},
		{ //nolint: exhaustruct
			name:      "too many pairs fails",
			grid:      "BL11BH16OO66XX",
			expectErr: true,
		},
		{ //nolint: exhaustruct
			name:      "invalid first character",
			grid:      "ZZ11",
			expectErr: true,
		},
		{ //nolint: exhaustruct
			name:      "invalid second pair character",
			grid:      "BLA1",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lat, lon, err := ll_from_grid_square(tt.grid)

			if tt.expectErr {
				assert.Error(t, err, "should return error for invalid input")
			} else {
				require.NoError(t, err, "should not return error for valid input")
				assert.GreaterOrEqual(t, lat, tt.minLat, "latitude should be >= min")
				assert.LessOrEqual(t, lat, tt.maxLat, "latitude should be <= max")
				assert.GreaterOrEqual(t, lon, tt.minLon, "longitude should be >= min")
				assert.LessOrEqual(t, lon, tt.maxLon, "longitude should be <= max")
			}
		})
	}
}

// TestCompressedFormatEdgeCases tests compressed format edge cases
func TestCompressedFormatEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		lat      float64
		lon      float64
		latCheck func(string) bool
		lonCheck func(string) bool
	}{
		{
			name: "equator prime meridian",
			lat:  0.0,
			lon:  0.0,
			latCheck: func(s string) bool {
				return len(s) == 4
			},
			lonCheck: func(s string) bool {
				return len(s) == 4
			},
		},
		{
			name: "north pole",
			lat:  90.0,
			lon:  0.0,
			latCheck: func(s string) bool {
				return s == "!!!!" // All minimum values
			},
			lonCheck: func(s string) bool {
				return len(s) == 4
			},
		},
		{
			name: "south pole",
			lat:  -90.0,
			lon:  0.0,
			latCheck: func(s string) bool {
				return s == "{{!!" // All maximum values
			},
			lonCheck: func(s string) bool {
				return len(s) == 4
			},
		},
		{
			name: "international date line east",
			lat:  0.0,
			lon:  180.0,
			latCheck: func(s string) bool {
				return len(s) == 4
			},
			lonCheck: func(s string) bool {
				return s == "{{!!" // Maximum longitude
			},
		},
		{
			name: "international date line west",
			lat:  0.0,
			lon:  -180.0,
			latCheck: func(s string) bool {
				return len(s) == 4
			},
			lonCheck: func(s string) bool {
				return s == "!!!!" // Minimum longitude
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			latStr := latitude_to_comp_str(tt.lat)
			lonStr := longitude_to_comp_str(tt.lon)

			assert.True(t, tt.latCheck(latStr), "latitude compressed format should pass check")
			assert.True(t, tt.lonCheck(lonStr), "longitude compressed format should pass check")
		})
	}
}

// TestCoordinateDistanceSymmetry tests that distance is symmetric
func TestCoordinateDistanceSymmetry(t *testing.T) {
	tests := []struct {
		name string
		lat1 float64
		lon1 float64
		lat2 float64
		lon2 float64
	}{
		{"Boston to Sydney", 42.3601, -71.0589, -33.8688, 151.2093},
		{"Equator points", 0.0, 0.0, 0.0, 90.0},
		{"Same latitude", 45.0, 45.0, 45.0, 135.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d1 := ll_distance_km(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			d2 := ll_distance_km(tt.lat2, tt.lon2, tt.lat1, tt.lon1)

			assert.InDelta(t, d1, d2, 0.001, "distance should be symmetric")
		})
	}
}

// TestBearingAntipodal tests bearing to antipodal points
func TestBearingAntipodal(t *testing.T) {
	// Bearing from a point to its antipode (opposite side of Earth)
	// should be consistent
	lat := 45.0
	lon := 45.0

	antiLat := -lat

	antiLon := lon + 180.0
	if antiLon > 180.0 {
		antiLon -= 360.0
	}

	bearing := ll_bearing_deg(lat, lon, antiLat, antiLon)

	// Bearing to antipode could be any direction (ambiguous), but should be valid
	assert.GreaterOrEqual(t, bearing, 0.0, "bearing should be >= 0")
	assert.Less(t, bearing, 360.0, "bearing should be < 360")
}

// TestDestinationZeroDistance tests that zero distance returns same point
func TestDestinationZeroDistance(t *testing.T) {
	lat := 42.3601
	lon := -71.0589
	dist := 0.0
	bearing := 90.0 // arbitrary

	newLat := ll_dest_lat(lat, lon, dist, bearing)
	newLon := ll_dest_lon(lat, lon, dist, bearing)

	assert.InDelta(t, lat, newLat, 0.0001, "zero distance should return same latitude")
	assert.InDelta(t, lon, newLon, 0.0001, "zero distance should return same longitude")
}

// TestLatitudeBoundaryClamping tests that out-of-range values are clamped
func TestLatitudeBoundaryClamping(t *testing.T) {
	tests := []struct {
		name     string
		lat      float64
		expected string
	}{
		{"way below minimum", -200.0, "9000.00S"},
		{"just below minimum", -90.1, "9000.00S"},
		{"valid minimum", -90.0, "9000.00S"},
		{"valid maximum", 90.0, "9000.00N"},
		{"just above maximum", 90.1, "9000.00N"},
		{"way above maximum", 200.0, "9000.00N"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := latitude_to_str(tt.lat, 0)
			assert.Equal(t, tt.expected, result, "should clamp to valid range")
		})
	}
}

// TestLongitudeBoundaryClamping tests that out-of-range values are clamped
func TestLongitudeBoundaryClamping(t *testing.T) {
	tests := []struct {
		name     string
		lon      float64
		expected string
	}{
		{"way below minimum", -200.0, "18000.00W"},
		{"just below minimum", -180.1, "18000.00W"},
		{"valid minimum", -180.0, "18000.00W"},
		{"valid maximum", 180.0, "18000.00E"},
		{"just above maximum", 180.1, "18000.00E"},
		{"way above maximum", 200.0, "18000.00E"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := longitude_to_str(tt.lon, 0)
			assert.Equal(t, tt.expected, result, "should clamp to valid range")
		})
	}
}

// BenchmarkLatitudeConversions benchmarks latitude conversion functions
func BenchmarkLatitudeConversions(b *testing.B) {
	lat := 42.3601

	b.Run("to_str", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = latitude_to_str(lat, 0)
		}
	})

	b.Run("to_comp_str", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = latitude_to_comp_str(lat)
		}
	})

	b.Run("to_nmea", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = latitude_to_nmea(lat)
		}
	})

	b.Run("from_nmea", func(b *testing.B) {
		str := "4221.6060"
		for i := 0; i < b.N; i++ {
			_ = latitude_from_nmea(str, 'N')
		}
	})
}

// BenchmarkDistanceBearing benchmarks distance and bearing calculations
func BenchmarkDistanceBearing(b *testing.B) {
	lat1, lon1 := 42.3601, -71.0589
	lat2, lon2 := -33.8688, 151.2093

	b.Run("distance", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ll_distance_km(lat1, lon1, lat2, lon2)
		}
	})

	b.Run("bearing", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ll_bearing_deg(lat1, lon1, lat2, lon2)
		}
	})

	b.Run("destination", func(b *testing.B) {
		dist := 1000.0

		bearing := 45.0
		for i := 0; i < b.N; i++ {
			_ = ll_dest_lat(lat1, lon1, dist, bearing)
			_ = ll_dest_lon(lat1, lon1, dist, bearing)
		}
	})
}

// TestHaversineFormulaAccuracy tests the accuracy of the haversine implementation
func TestHaversineFormulaAccuracy(t *testing.T) {
	// Test against known distances
	tests := []struct {
		name     string
		lat1     float64
		lon1     float64
		lat2     float64
		lon2     float64
		expected float64 // in km
		delta    float64 // tolerance
	}{
		{
			name:     "London to Paris",
			lat1:     51.5074,
			lon1:     -0.1278,
			lat2:     48.8566,
			lon2:     2.3522,
			expected: 344.0,
			delta:    5.0,
		},
		{
			name:     "New York to Los Angeles",
			lat1:     40.7128,
			lon1:     -74.0060,
			lat2:     34.0522,
			lon2:     -118.2437,
			expected: 3936.0,
			delta:    10.0,
		},
		{
			name:     "Singapore to Tokyo",
			lat1:     1.3521,
			lon1:     103.8198,
			lat2:     35.6762,
			lon2:     139.6503,
			expected: 5312.0,
			delta:    10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			distance := ll_distance_km(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			assert.InDelta(t, tt.expected, distance, tt.delta,
				"distance should match known value within tolerance")
		})
	}
}

// TestBearingCalculation tests bearing calculation accuracy
func TestBearingCalculation(t *testing.T) {
	tests := []struct {
		name     string
		lat1     float64
		lon1     float64
		lat2     float64
		lon2     float64
		expected float64 // degrees
		delta    float64
	}{
		{
			name:     "due north",
			lat1:     0.0,
			lon1:     0.0,
			lat2:     1.0,
			lon2:     0.0,
			expected: 0.0,
			delta:    0.1,
		},
		{
			name:     "due east",
			lat1:     0.0,
			lon1:     0.0,
			lat2:     0.0,
			lon2:     1.0,
			expected: 90.0,
			delta:    0.1,
		},
		{
			name:     "due south",
			lat1:     1.0,
			lon1:     0.0,
			lat2:     0.0,
			lon2:     0.0,
			expected: 180.0,
			delta:    0.1,
		},
		{
			name:     "due west",
			lat1:     0.0,
			lon1:     1.0,
			lat2:     0.0,
			lon2:     0.0,
			expected: 270.0,
			delta:    0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bearing := ll_bearing_deg(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			assert.InDelta(t, tt.expected, bearing, tt.delta,
				"bearing should match expected cardinal direction")
		})
	}
}

// TestSmallAngles tests coordinate functions with very small angles
func TestSmallAngles(t *testing.T) {
	// Test with very small differences in coordinates
	lat1, lon1 := 42.0, -71.0
	lat2, lon2 := 42.0001, -71.0001

	dist := ll_distance_km(lat1, lon1, lat2, lon2)
	bearing := ll_bearing_deg(lat1, lon1, lat2, lon2)

	assert.Greater(t, dist, 0.0, "distance should be positive")
	assert.Less(t, dist, 1.0, "distance should be very small")
	assert.GreaterOrEqual(t, bearing, 0.0, "bearing should be valid")
	assert.Less(t, bearing, 360.0, "bearing should be valid")

	// Test round trip
	newLat := ll_dest_lat(lat1, lon1, dist, bearing)
	newLon := ll_dest_lon(lat1, lon1, dist, bearing)

	assert.InDelta(t, lat2, newLat, 0.0001, "round trip latitude should match")
	assert.InDelta(t, lon2, newLon, 0.0001, "round trip longitude should match")
}

// TestAmbiguityLevels tests all ambiguity levels for latitude/longitude
func TestAmbiguityLevels(t *testing.T) {
	lat := 42.3601
	lon := -71.0589

	latTests := []struct {
		ambiguity int
		expected  string
	}{
		{0, "4221.61N"},
		{1, "4221.6 N"},
		{2, "4221.  N"},
		{3, "422 .  N"},
		{4, "42  .  N"},
	}

	for _, tt := range latTests {
		t.Run(fmt.Sprintf("lat_ambiguity_%d", tt.ambiguity), func(t *testing.T) {
			result := latitude_to_str(lat, tt.ambiguity)
			assert.Equal(t, tt.expected, result)
		})
	}

	lonTests := []struct {
		ambiguity int
		expected  string
	}{
		{0, "07103.53W"},
		{1, "07103.5 W"},
		{2, "07103.  W"},
		{3, "0710 .  W"},
		{4, "071  .  W"},
	}

	for _, tt := range lonTests {
		t.Run(fmt.Sprintf("lon_ambiguity_%d", tt.ambiguity), func(t *testing.T) {
			result := longitude_to_str(lon, tt.ambiguity)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNaN tests that NaN inputs don't cause panics
func TestNaN(t *testing.T) {
	nan := math.NaN()

	// These shouldn't panic
	_ = latitude_to_str(nan, 0)
	_ = longitude_to_str(nan, 0)
	_ = latitude_to_comp_str(nan)
	_ = longitude_to_comp_str(nan)
	_, _ = latitude_to_nmea(nan)
	_, _ = longitude_to_nmea(nan)
	_ = ll_distance_km(nan, 0, 0, 0)
	_ = ll_bearing_deg(nan, 0, 0, 0)
}
