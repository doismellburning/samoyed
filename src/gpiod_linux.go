package direwolf

import (
	gpiocdev "github.com/warthog618/go-gpiocdev"
)

func RequestGPIODLine(chipName string, lineNumber int, initialState int) (*gpiocdev.Line, error) {

	return gpiocdev.RequestLine(chipName, lineNumber, gpiocdev.AsOutput(initialState))
}
