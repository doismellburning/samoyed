// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build darwin

package direwolf

import "golang.org/x/sys/unix"

// See the Linux counterpart: these are the same two ioctls under the names
// the BSDs give them.
const (
	tcGetAttr = unix.TIOCGETA
	tcSetAttr = unix.TIOCSETA
)
