//go:build !linux

package direwolf

import (
	"fmt"
	"os"
)

func cm108_find_ptt(_ string) string {
	return ""
}

func cm108_set_gpio_pin(_ string, _ int, _ int) int {
	return 0
}

func CM108Main() {
	fmt.Fprintf(os.Stderr, "CM108 GPIO PTT is only supported on Linux.\n")
	os.Exit(1)
}
