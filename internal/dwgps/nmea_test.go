// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwgps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		{
			// Minutes must be under 60, and 59.9999 is, so this is a perfectly
			// ordinary position rather than something to reject.
			name:     "just under sixty minutes",
			str:      "8959.9999",
			hemi:     'N',
			expected: 89.999998,
			delta:    0.000001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LatitudeFromNMEA(tt.str, tt.hemi)
			require.NoError(t, err, "latitude should parse")
			assert.InDelta(t, tt.expected, result, tt.delta, "latitude should match")
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
		// Only the first byte used to be checked, so a non-digit elsewhere in
		// the degrees was subtracted from '0' anyway, and unparseable minutes
		// were silently taken as zero - leaving a corrupted field looking like
		// a plausible whole-degree position.
		{"non-digit in degrees", "4:21.6060", 'N'},
		{"non-digit in minutes", "42X1.6060", 'N'},
		{"non-digit after decimal point", "4221.60X0", 'N'},
		{"signed minutes", "42-1.6060", 'N'},
		// Minutes are sixtieths, so 60 of them is the next degree up and the
		// sender has made a mistake.  These used to come back as 43 and 43.67.
		{"sixty minutes", "4260.0000", 'N'},
		{"minutes beyond sixty", "4299.9999", 'N'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LatitudeFromNMEA(tt.str, tt.hemi)
			assert.Error(t, err, "should return an error for invalid input")
		})
	}

	t.Run("invalid_hemisphere", func(t *testing.T) {
		var _, err = LatitudeFromNMEA("4221.6060", 'X')
		assert.ErrorContains(t, err, "should be N or S",
			"a hemisphere that is neither N nor S should be reported")
	})

	t.Run("out_of_range", func(t *testing.T) {
		var _, err = LatitudeFromNMEA("9500.0000", 'N')
		assert.ErrorContains(t, err, "not in range of 0 to 90",
			"a latitude beyond 90 degrees should be reported")
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
		{
			name:     "just under sixty minutes",
			str:      "17959.9999",
			hemi:     'E',
			expected: 179.999998,
			delta:    0.000001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LongitudeFromNMEA(tt.str, tt.hemi)
			require.NoError(t, err, "longitude should parse")
			assert.InDelta(t, tt.expected, result, tt.delta, "longitude should match")
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
		{"non-digit in degrees", "15:12.5580", 'E'},
		{"non-digit in minutes", "151X2.5580", 'E'},
		{"non-digit after decimal point", "15112.55X0", 'E'},
		{"signed minutes", "151-2.5580", 'E'},
		{"sixty minutes", "15160.0000", 'E'},
		{"minutes beyond sixty", "15199.9999", 'E'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LongitudeFromNMEA(tt.str, tt.hemi)
			assert.Error(t, err, "should return an error for invalid input")
		})
	}

	t.Run("out_of_range", func(t *testing.T) {
		var _, err = LongitudeFromNMEA("18500.0000", 'E')
		assert.ErrorContains(t, err, "not in range of 0 to 180",
			"a longitude beyond 180 degrees should be reported")
	})

	t.Run("invalid_hemisphere", func(t *testing.T) {
		var _, err = LongitudeFromNMEA("15112.5580", 'X')
		assert.ErrorContains(t, err, "should be E or W",
			"a hemisphere that is neither E nor W should be reported")
		// Test passes if it doesn't panic
	})
}
