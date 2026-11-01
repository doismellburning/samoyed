// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"slices"
	"strings"
)

// ValidHRoute says whether s is a hierarchical address, such as
// "Q1BBS.#77.GBR.EURO" or "GBR": dot-separated elements, most specific first,
// each of letters, digits, hyphens and underscores, any of them starting
// with a #.
func ValidHRoute(s string) bool {
	if s == "" {
		return false
	}

	for element := range strings.SplitSeq(s, ".") {
		var name = strings.TrimPrefix(element, "#")
		if name == "" || !onlyChars(name, "-_") {
			return false
		}
	}

	return true
}

// AtParts is the address after the @ split into its elements, least specific
// first: Q1TEST@Q1BBS.#77.GBR.EURO gives EURO, GBR, #77, Q1BBS.  It is empty
// for an address with no @.
func (a Address) AtParts() []string {
	var at, ok = a.At.Get()
	if !ok {
		return nil
	}

	var parts = strings.Split(at, ".")

	slices.Reverse(parts)

	return parts
}

// BBS is the first element after the @: for personal mail, the BBS it is
// for; for a bulletin, the area it is for.  It is empty for an address with
// no @.
func (a Address) BBS() string {
	var at, _ = a.At.Get()
	var first, _, _ = strings.Cut(at, ".")

	return first
}
