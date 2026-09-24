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

// Examples from ll2utm.c source, checked against direwolf Debian package

func Example_main() { //nolint:testableexamples
	os.Args = []string{"ll2utm"}

	main()
	// TODO Can we do a partial match in the style of Python doctests?
}

func Example_main_1() {
	os.Args = []string{"ll2utm", "42.662139", "-71.365553"}

	main()
	// Output:
	// UTM zone = 19, hemisphere = N, easting = 306130, northing = 4726010
	// MGRS =  19TCH02  19TCH0626  19TCH061260  19TCH06132600  19TCH0613026009
}

// runMainEnv, when set, has the test binary run main with the arguments it
// holds instead of the tests, so a test can see main exit.
const runMainEnv = "SAMOYED_LL2UTM_RUN_MAIN"

func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(runMainEnv); ok {
		os.Args = append([]string{"ll2utm"}, strings.Fields(args)...)

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

func Example_main_southern() {
	os.Args = []string{"ll2utm", "-33.8688", "151.2093"}

	main()
	// Output:
	// UTM zone = 56, hemisphere = S, easting = 334369, northing = 6250948
	// MGRS =  56HLH35  56HLH3450  56HLH343509  56HLH34365094  56HLH3436850948
}

// UTM stops short of the poles, where MGRS carries on.
func Test_main_polar(t *testing.T) {
	var out, status = runMain(t, "85", "10")

	assert.Equal(t, 0, status)
	assert.Contains(t, out, "Conversion to UTM failed:\nlatitude out of range")
	assert.Contains(t, out, "MGRS =  ZAB95  ZAB9652  ZAB964529  ZAB96455298  ZAB9645452981\n")
}

func Test_main_usage(t *testing.T) {
	var out, status = runMain(t)

	assert.Equal(t, 0, status)
	assert.Contains(t, out, "Usage:\n\tll2utm  latitude  longitude\n")
}

func Test_main_invalid(t *testing.T) {
	var testCases = map[string]struct {
		args []string
		want string
	}{
		"latitude":  {[]string{"x", "1"}, `Invalid latitude: strconv.ParseFloat: parsing "x": invalid syntax`},
		"longitude": {[]string{"1", "y"}, `Invalid longitude: strconv.ParseFloat: parsing "y": invalid syntax`},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var out, status = runMain(t, tc.args...)

			assert.Equal(t, 1, status)
			assert.Contains(t, out, tc.want)
			assert.Contains(t, out, "Usage:")
		})
	}
}
