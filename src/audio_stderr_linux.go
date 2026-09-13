//go:build linux

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"golang.org/x/sys/unix"
)

// dupOntoStderr makes file descriptor 2 a copy of fd.  Linux has dup2 only on
// some architectures (arm64, for one, has just dup3), so use dup3 throughout.
func dupOntoStderr(fd int) error {
	return unix.Dup3(fd, unix.Stderr, 0)
}
