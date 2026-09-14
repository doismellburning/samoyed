//go:build !unix

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// captureStderrFD has nothing to redirect file descriptor 2 with on platforms
// that aren't Unix, so it just runs fn.
func captureStderrFD(fn func()) string {
	fn()

	return ""
}
