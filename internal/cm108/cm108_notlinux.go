// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !linux

package cm108

import "errors"

// ErrUnsupported reports that USB audio adapter GPIO is not available on this
// platform - the implementation is Linux-only, via udev and hidraw.
var ErrUnsupported = errors.New("CM108 GPIO is only supported on Linux")

// Inventory is not supported on this platform.
func Inventory(_ int) ([]*Thing, error) {
	return nil, ErrUnsupported
}

// FindPTT is not supported on this platform.
func FindPTT(_ string) (*Thing, error) {
	return nil, ErrUnsupported
}

// SetGPIOPin is not supported on this platform.
func SetGPIOPin(_ string, _ int, _ int) error {
	return ErrUnsupported
}
