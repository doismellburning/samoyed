//go:build !linux

package direwolf

import "errors"

var errCM108NotSupported = errors.New("CM108 GPIO PTT is only supported on Linux")

func cm108_find_ptt(_ string) (string, error) {
	return "", errCM108NotSupported
}

func CM108CheckDevice(_ string) error {
	return errCM108NotSupported
}

func CM108SetGPIOPin(_ string, _ int, _ int) error {
	return errCM108NotSupported
}
