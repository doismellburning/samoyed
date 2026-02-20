package direwolf

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"runtime"
	"time"
)

func SLEEP_MS(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func SLEEP_SEC(s int) {
	SLEEP_MS(s * 1000)
}

// Because sometimes it's really convenient to have C's ternary ?:
func IfThenElse[T any](x bool, a T, b T) T { //nolint:ireturn
	if x {
		return a
	} else {
		return b
	}
}

// Used for both KISS and AGWPE
const MAX_NET_CLIENTS = 3

// There are several places where we deal with fixed-width byte arrays containing a string.
// For C this was fine, because strings are null-terminated; for Go we want to explicitly drop trailing nulls.
// This takes a slice because I didn't know how to make it take an arbitrary sized array, and didn't see the value.
func ByteArrayToString(b []byte) string {
	return string(bytes.TrimRight(b, "\x00"))
}

func dw_printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
}

func exit(x int) {
	os.Exit(x)
}

// #define ACHAN2ADEV(n) ((n)>>1)
func ACHAN2ADEV(n int) int {
	return n >> 1
}

func ADEVFIRSTCHAN(n int) int {
	return n * 2
}

// #define DW_KNOTS_TO_MPH(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 1.15077945)
func DW_KNOTS_TO_MPH(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 1.15077945
}

// #define DW_MPH_TO_KNOTS(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.868976)
func DW_MPH_TO_KNOTS(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.868976
}

// #define DW_METERS_TO_FEET(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 3.2808399)
func DW_METERS_TO_FEET(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 3.2808399
}

// #define DW_FEET_TO_METERS(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.3048)
func DW_FEET_TO_METERS(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.3048
}

// #define DW_MILES_TO_KM(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 1.609344)
func DW_MILES_TO_KM(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 1.609344
}

// #define DW_MBAR_TO_INHG(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.0295333727)
func DW_MBAR_TO_INHG(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.0295333727
}

// #define DW_KM_TO_MILES(x) ((x) == G_UNKNOWN ? G_UNKNOWN : (x) * 0.621371192)
func DW_KM_TO_MILES(x float64) float64 {
	if x == G_UNKNOWN {
		return G_UNKNOWN
	}

	return x * 0.621371192
}

var retry_text = []string{
	"NONE",
	"SINGLE",
	"DOUBLE",
	"TRIPLE",
	"TWO_SEP",
	"PASSALL",
}

func D2R(d float64) float64 {
	return d * math.Pi / 180
}

func R2D(r float64) float64 {
	return r * 180 / math.Pi
}

// Can't be "assert" because of conflicts with stretchr/testify/assert, but otherwise, it's compatible enough
func Assert(t bool) {
	if !t {
		_, file, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("Assertion failed at %s:%d", file, line))
	}
}
