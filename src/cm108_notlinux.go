//go:build !linux

package direwolf

import "errors"

// ErrCM108Unsupported reports that USB audio adapter GPIO is not available on
// this platform - the implementation is Linux-only, via udev and hidraw.
var ErrCM108Unsupported = errors.New("CM108 GPIO is only supported on Linux")

// cm108_find_ptt is not supported on this platform.
func cm108_find_ptt(_ string) (*CM108Thing, error) {
	return nil, ErrCM108Unsupported
}

// CM108SetGPIOPin is not supported on this platform.
func CM108SetGPIOPin(_ string, _ int, _ int) error {
	return ErrCM108Unsupported
}
