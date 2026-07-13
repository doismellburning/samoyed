//go:build !linux

package direwolf

func cm108_find_ptt(_ string) string {
	return ""
}

func CM108SetGPIOPin(_ string, _ int, _ int) int {
	return -1
}
