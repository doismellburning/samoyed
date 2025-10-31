package direwolf

import "time"

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
