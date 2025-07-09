package direwolf

import "time"

func SLEEP_MS(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func SLEEP_SEC(s int) {
	SLEEP_MS(s * 1000)
}
