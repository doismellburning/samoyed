// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

//go:build !linux

package ptt

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

func requestGPIODLine(chipName string, lineNumber int, initialState int) (*dummyGpiodOutputLine, error) {
	return nil, errors.New("GPIOD not supported on non-Linux operating systems")
}
