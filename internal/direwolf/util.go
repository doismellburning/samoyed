package direwolf

import (
	"fmt"
	"math"
)

// MAX_NET_CLIENTS is used for both KISS and AGWPE
const MAX_NET_CLIENTS = 3

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

func D2R(d float64) float64 {
	return d * math.Pi / 180
}

func R2D(r float64) float64 {
	return r * 180 / math.Pi
}
