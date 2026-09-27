//go:build unix && !linux

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"golang.org/x/sys/unix"
)

// dupOntoStderr makes file descriptor 2 a copy of fd.
func dupOntoStderr(fd int) error {
	return unix.Dup2(fd, unix.Stderr)
}
