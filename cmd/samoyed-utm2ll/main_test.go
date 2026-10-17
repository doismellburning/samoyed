// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package main

import (
	"os"
	"testing"

	"github.com/doismellburning/samoyed/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	testutils.RunMainIfAsked(main)

	os.Exit(m.Run())
}

// Examples from utm2ll.c source, checked against direwolf Debian package
func Test_main(t *testing.T) {
	t.Parallel()

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"UTM":       {[]string{"19T", "306130", "4726010"}, "from UTM, latitude = 42.662139, longitude = -71.365553\n"},
		"MGRS":      {[]string{"19TCH06132600"}, "from MGRS, latitude = 42.662049, longitude = -71.365550\n"},
		"lowercase": {[]string{"19t", "306130", "4726010"}, "from UTM, latitude = 42.662139, longitude = -71.365553\n"},
		"southern":  {[]string{"19C", "306130", "4726010"}, "from UTM, latitude = -47.590322, longitude = -71.578679\n"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var result = testutils.RunMain(t, "", tc.args...)

			assert.Equal(t, 0, result.Status)
			assert.Equal(t, tc.want, result.Output())
		})
	}
}

// A position that can't be converted is reported, but isn't a usage error.
func Test_main_unconvertible(t *testing.T) {
	t.Parallel()

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"UTM":  {[]string{"19", "99999999", "1"}, "Conversion from UTM failed:\neasting out of range"},
		"MGRS": {[]string{"ZZZ"}, "Conversion from MGRS failed:\nInvalid MGRS string"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var result = testutils.RunMain(t, "", tc.args...)
			var out, status = result.Output(), result.Status

			assert.Equal(t, 0, status)
			assert.Contains(t, out, tc.want)
		})
	}
}

func Test_main_invalid(t *testing.T) {
	t.Parallel()

	var testCases = map[string]struct {
		args []string
		want string
	}{
		"no arguments": {nil, ""},
		"two":          {[]string{"19T", "306130"}, ""},
		"zone":         {[]string{"x", "306130", "4726010"}, "Invalid zone:"},
		"band":         {[]string{"19Z", "306130", "4726010"}, "Latitudinal band must be one of CDEFGHJKLMNPQRSTUVWX."},
		"easting":      {[]string{"19T", "x", "4726010"}, `Invalid easting: strconv.ParseFloat: parsing "x": invalid syntax`},
		"northing":     {[]string{"19T", "306130", "y"}, `Invalid northing: strconv.ParseFloat: parsing "y": invalid syntax`},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var result = testutils.RunMain(t, "", tc.args...)
			var out, status = result.Output(), result.Status

			assert.Equal(t, 1, status)
			assert.Contains(t, out, tc.want)
			assert.Contains(t, out, "Usage:\n\tutm2ll  zone  easting  northing\n")
		})
	}
}
