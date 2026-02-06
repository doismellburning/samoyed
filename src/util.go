package direwolf

import (
	"bytes"
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
