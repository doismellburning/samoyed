// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package direwolf

import "golang.org/x/sys/unix"

// The ioctls for reading and writing a terminal's settings are spelled
// differently on each platform - Linux has TCGETS/TCSETS where the BSDs, macOS
// among them, have TIOCGETA/TIOCSETA - so the ones a test needs live here
// rather than in the test that uses them.
const (
	tcGetAttr = unix.TCGETS
	tcSetAttr = unix.TCSETS
)
