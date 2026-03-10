//go:build !linux

package direwolf

import (
	"errors"
)

// Basic interface implementation to keep ireturn happy
type dummyGpiodOutputLine struct{}

func (*dummyGpiodOutputLine) SetValue(_ int) error {
	return nil
}

func (*dummyGpiodOutputLine) Close() error {
	return nil
}

func RequestGPIODLine(chipName string, lineNumber int, initialState int) (*dummyGpiodOutputLine, error) {

	return nil, errors.New("GPIOD not supported on non-Linux operating systems")
}
