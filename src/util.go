package direwolf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"time"
)

func SLEEP_MS(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func SLEEP_SEC(s int) {
	SLEEP_MS(s * 1000)
}

// sleepCtx sleeps for d, or until ctx is cancelled, whichever comes first.
//
// It reports whether the whole of d elapsed, so a false return says the caller
// is being shut down and should return rather than carry on with whatever it
// woke up to do.  That makes it the replacement for a SLEEP_MS or SLEEP_SEC in
// a long-lived goroutine's loop: such a sleep is where the goroutine spends
// most of its life, and nothing else in the loop can notice a cancellation
// until it finishes.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	var timer = time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// sleepSecCtx is sleepCtx taking whole seconds, as the loops ported from C
// count in whole seconds via SLEEP_SEC.
func sleepSecCtx(ctx context.Context, s int) bool {
	return sleepCtx(ctx, time.Duration(s)*time.Second)
}

// SleepSecCtx is sleepSecCtx for the commands outside this package, which have
// the same problem in their own loops: taking a signal as a context means the
// default "an interrupt ends the process" is gone, so every wait in the loop
// has to be one the interrupt can cut short.
func SleepSecCtx(ctx context.Context, s int) bool {
	return sleepSecCtx(ctx, s)
}

// closeOnDone closes c once ctx is cancelled, and returns a function that
// cancels that arrangement.
//
// A goroutine blocked in Accept or Read cannot see a cancellation at all: it
// is inside a system call, not at the top of its loop.  Closing what it is
// blocked on is what gets it back - the call returns an error, and the
// goroutine then finds ctx.Err() non-nil and returns.  Call the returned
// function, usually deferred, when finished with c, so a long-lived context
// doesn't hold on to something already closed and forgotten.
//
// The two can happen at once: the goroutine can be on its way out for its own
// reasons at the moment of cancellation, and get in before the close.  It then
// closes c itself rather than leaving a socket nobody is watching any more
// open for the rest of the process's life.
//
// That still leaves the reverse order - the call returns, and a cancellation
// arrives after the check above but before the caller looks at ctx.Err() - so
// this is how to interrupt a blocking call, not a promise about who closes c
// in the end.  A caller that owns c closes it on its own cancellation path
// too.
func closeOnDone(ctx context.Context, c io.Closer) func() {
	var stop = context.AfterFunc(ctx, func() {
		_ = c.Close()
	})

	return func() {
		if stop() && ctx.Err() != nil {
			_ = c.Close()
		}
	}
}

// IfThenElse exists because sometimes it's really convenient to have C's ternary ?:.
func IfThenElse[T any](x bool, a T, b T) T {
	if x {
		return a
	} else {
		return b
	}
}

// MAX_NET_CLIENTS is used for both KISS and AGWPE
const MAX_NET_CLIENTS = 3

// ByteArrayToString handles the several places where we deal with fixed-width byte arrays containing a string.
// For C this was fine, because strings are null-terminated; for Go we want to explicitly drop trailing nulls.
// This takes a slice because I didn't know how to make it take an arbitrary sized array, and didn't see the value.
func ByteArrayToString(b []byte) string {
	return string(bytes.TrimRight(b, "\x00"))
}

// dw_printf writes program output, as Dire Wolf's C function of the same name did.
//
// Deprecated: new output should go through logrus, whose entries carry structured
// fields rather than text assembled a piece at a time. The remaining calls are being
// converted gradually - don't add more.
//
// Note that staticcheck's SA1019 will not point this out: it deliberately says nothing
// about a deprecated identifier used within its own package, and every caller is in
// package direwolf.
func dw_printf(format string, a ...any) (int, error) {
	// Can't call variadic functions through cgo, so let's define our own!
	// Fortunately dw_printf doesn't do much
	return fmt.Printf(format, a...)
}

// ACHAN2ADEV is `#define ACHAN2ADEV(n) ((n)>>1)`.
func ACHAN2ADEV(n int) int {
	return n >> 1
}

func ADEVFIRSTCHAN(n int) int {
	return n * 2
}

// The unit conversions below are the Dire Wolf macros of the same names, less
// their `(x) == G_UNKNOWN ? G_UNKNOWN :` guard: a value that might not be there
// is a maybe.Maybe, and maybe.Fmap keeps its absence out of the arithmetic.

// DW_KNOTS_TO_MPH converts knots to miles per hour.
func DW_KNOTS_TO_MPH(x float64) float64 {
	return x * 1.15077945
}

// DW_MPH_TO_KNOTS converts miles per hour to knots.
func DW_MPH_TO_KNOTS(x float64) float64 {
	return x * 0.868976
}

// DW_METERS_TO_FEET converts metres to feet.
func DW_METERS_TO_FEET(x float64) float64 {
	return x * 3.2808399
}

// DW_FEET_TO_METERS converts feet to metres.
func DW_FEET_TO_METERS(x float64) float64 {
	return x * 0.3048
}

// DW_MILES_TO_KM converts miles to kilometres.
func DW_MILES_TO_KM(x float64) float64 {
	return x * 1.609344
}

// DW_MBAR_TO_INHG converts millibars to inches of mercury.
func DW_MBAR_TO_INHG(x float64) float64 {
	return x * 0.0295333727
}

// DW_KM_TO_MILES converts kilometres to miles.
func DW_KM_TO_MILES(x float64) float64 {
	return x * 0.621371192
}

func D2R(d float64) float64 {
	return d * math.Pi / 180
}

func R2D(r float64) float64 {
	return r * 180 / math.Pi
}

// Assert can't be named "assert" because of conflicts with stretchr/testify/assert, but otherwise, it's compatible enough.
func Assert(t bool) {
	if !t {
		_, file, line, _ := runtime.Caller(1)
		panic(fmt.Sprintf("Assertion failed at %s:%d", file, line))
	}
}
