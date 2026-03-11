//go:build !linux

package direwolf

func cm108_find_ptt(_ string) string {
	return ""
}

func cm108_set_gpio_pin(_ string, _ int, _ int) int {
	return -1
}
