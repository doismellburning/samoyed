//go:build linux

package direwolf

import gpiocdev "github.com/warthog618/go-gpiocdev"

func requestGpiodLine(chipName string, lineNumber int, initialState int) (gpiodOutputLine, error) {
	return gpiocdev.RequestLine(chipName, lineNumber, gpiocdev.AsOutput(initialState))
}
