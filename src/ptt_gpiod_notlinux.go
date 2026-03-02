//go:build !linux

package direwolf

import "errors"

func requestGpiodLine(_ string, _ int, _ int) (gpiodOutputLine, error) {
	return nil, errors.New("GPIOD PTT is only supported on Linux")
}
