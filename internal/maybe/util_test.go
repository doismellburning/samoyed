// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package maybe

import "testing"

func TestFormat(t *testing.T) {
	var cases = []struct {
		name string
		got  string
		want string
	}{
		{"Just a float", Format("%.2f", "unknown", Just(1.5)), "1.50"},
		{"Just zero is not Nothing", Format("%.0f", "unknown", Just(0.0)), "0"},
		{"Just an int", Format("%d", "unknown", Just(42)), "42"},
		{"Nothing", Format("%.1f", "unknown", Nothing[float64]()), "unknown"},
		{"Nothing with other text", Format("%d", "-", Nothing[int]()), "-"},
	}

	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}
