// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package ptt

import (
	gpiocdev "github.com/warthog618/go-gpiocdev"
)

func requestGPIODLine(chipName string, lineNumber int, initialState int) (*gpiocdev.Line, error) {
	return gpiocdev.RequestLine(chipName, lineNumber, gpiocdev.AsOutput(initialState))
}
