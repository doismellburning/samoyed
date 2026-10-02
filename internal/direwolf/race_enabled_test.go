//go:build race

// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

// raceEnabled reports whether the tests were built with the race detector,
// which also turns on the runtime's checkptr checks.
const raceEnabled = true
