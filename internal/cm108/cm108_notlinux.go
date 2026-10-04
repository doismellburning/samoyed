// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

//go:build !linux

package cm108

import "errors"

var errNotSupported = errors.New("CM108 GPIO PTT is only supported on Linux")

func FindPTT(_ string) (string, error) {
	return "", errNotSupported
}

func CheckDevice(_ string) error {
	return errNotSupported
}

func SetGPIOPin(_ string, _ int, _ int) error {
	return errNotSupported
}
