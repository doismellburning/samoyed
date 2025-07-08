package direwolf

import "time"

func SLEEP_MS(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
