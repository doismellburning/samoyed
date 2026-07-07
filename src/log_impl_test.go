// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import "testing"

// Regression test for a bug where heard[:4] and heard[4] were indexed
// before checking len(heard) == 5, causing a panic whenever the heard
// station's callsign was shorter than 4 characters.
func TestLogRRBitsShortHeardDoesNotPanic(t *testing.T) {
	t.Parallel()

	var pp = ax25_from_text("Q1TEST>APRS,Q2TEST*,AB*:test", true)
	if pp == nil {
		t.Fatal("failed to parse test packet")
	}

	if ax25_get_heard(pp) < AX25_REPEATER_2 {
		t.Fatal("test packet did not set up heard station at or beyond AX25_REPEATER_2")
	}

	var A decode_aprs_t

	log_rr_bits(&A, pp)
}
