// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Examples from utm2ll.c source, checked against direwolf Debian package

func Example_main() { //nolint:testableexamples
	main()
	// TODO Can we do a partial match in the style of Python doctests?
}

func Example_main_1() {
	os.Args = []string{"utm2ll", "19T", "306130", "4726010"}

	main()
	// Output: from UTM, latitude = 42.662139, longitude = -71.365553
}

func Example_main_2() {
	os.Args = []string{"utm2ll", "19TCH06132600"}

	main()
	// Output:
	// from MGRS, latitude = 42.662049, longitude = -71.365550
}

func Example_main_3() {
	os.Args = []string{"utm2ll", "19t", "306130", "4726010"} // Handle lowercase letter

	main()
	// Output: from UTM, latitude = 42.662139, longitude = -71.365553
}

func Example_main_southern() {
	os.Args = []string{"utm2ll", "19C", "306130", "4726010"}

	main()
	// Output: from UTM, latitude = -47.590322, longitude = -71.578679
}

// runMainEnv, when set, has the test binary run main with the arguments it
// holds instead of the tests, so a test can see main exit.
const runMainEnv = "SAMOYED_UTM2LL_RUN_MAIN"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(runMainEnv); ok {
		os.Args = append([]string{"utm2ll"}, strings.Fields(args)...)

		main()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// runMain runs main in a process of its own, returning what it printed and
// its exit status.
func runMain(t *testing.T, args ...string) (string, int) {
	t.Helper()

	var cmd = exec.CommandContext(t.Context(), os.Args[0]) //nolint:gosec
	cmd.Env = append(os.Environ(), runMainEnv+"="+strings.Join(args, " "))

	var out, err = cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)

		return string(out), exitErr.ExitCode()
	}

	return string(out), 0
}

// A position that can't be converted is reported, but isn't a usage error.
func Test_main_unconvertible(t *testing.T) {
	var testCases = map[string]struct {
		args []string
		want string
	}{
		"UTM":  {[]string{"19", "99999999", "1"}, "Conversion from UTM failed:\neasting out of range"},
		"MGRS": {[]string{"ZZZ"}, "Conversion from MGRS failed:\nInvalid MGRS string"},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var out, status = runMain(t, tc.args...)

			assert.Equal(t, 0, status)
			assert.Contains(t, out, tc.want)
		})
	}
}

func Test_main_invalid(t *testing.T) {
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
			var out, status = runMain(t, tc.args...)

			assert.Equal(t, 1, status)
			assert.Contains(t, out, tc.want)
			assert.Contains(t, out, "Usage:\n\tutm2ll  zone  easting  northing\n")
		})
	}
}
