// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package maybe

import "fmt"

// Format renders the value held by m with the fmt verb format, or as nothing
// if m is Nothing - so an absent reading can print as "unknown" rather than as
// a plausible-looking number.
func Format[T any](format, nothing string, m Maybe[T]) string {
	return Fold(nothing, func(value T) string {
		return fmt.Sprintf(format, value)
	}, m)
}
