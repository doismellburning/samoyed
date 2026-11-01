// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sid is BBS system IDs: the bracketed line, such as
// "[FBB-7.0-ABFHM$]", a BBS sends to say what it is and what it can do, and
// what two BBSes that have swapped them agree to do.
//
// The flags' meanings here are as FBB's documentation gives them, as best
// this package's author recalls it: they are to be checked against captures
// from FBB, BPQ and XRouter (#1152).
package sid

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrNotSID says a line isn't a system ID.
var ErrNotSID = errors.New("not a system ID")

const (
	// FlagBID says the BBS uses $BIDs to tell messages it has had before.
	FlagBID = '$'

	// FlagHierarchical says the BBS understands hierarchical addresses,
	// such as Q1BBS.#77.GBR.EURO.
	FlagHierarchical = 'H'

	// FlagFBB says the BBS can forward in FBB's batched protocol.  This
	// one doesn't yet, so doesn't say so.
	FlagFBB = 'F'
)

// pattern is a system ID: a name, a version and feature flags, in square
// brackets.
var pattern = regexp.MustCompile(`^\[([A-Za-z0-9._]+)-([A-Za-z0-9._]*)-([A-Za-z0-9$]*)\]$`)

// SID is a BBS's system ID.
type SID struct {
	// Name is the BBS software, such as "FBB".
	Name string

	// Version is its version, such as "7.0".
	Version string

	// Flags are what it can do, such as "ABFHM$".
	Flags string
}

// IsSID says whether line is a system ID, as a BBS sends on connecting to
// forward mail.
func IsSID(line string) bool {
	return pattern.MatchString(line)
}

// Parse reads a system ID.
func Parse(line string) (SID, error) {
	var match = pattern.FindStringSubmatch(line)
	if match == nil {
		return SID{}, fmt.Errorf("%w: %q", ErrNotSID, line)
	}

	return SID{Name: match[1], Version: match[2], Flags: match[3]}, nil
}

// Has says whether the SID has flag.  Flags are case-insensitive.
func (s SID) Has(flag rune) bool {
	return strings.ContainsRune(strings.ToUpper(s.Flags), flag)
}

// String writes the SID as it is sent.
func (s SID) String() string {
	return "[" + s.Name + "-" + s.Version + "-" + s.Flags + "]"
}

// Ours is this BBS's system ID, for the given version: hierarchical
// addresses and BIDs, and forwarding in plain MBL/RLI style.
func Ours(major, minor int) SID {
	return SID{Name: "SAMBBS", Version: fmt.Sprintf("%d.%d", major, minor), Flags: string([]rune{FlagHierarchical, FlagBID})}
}

// Mode is what two BBSes that have swapped SIDs agree to do.
type Mode struct {
	// BIDs says both use $BIDs, so a message is offered with its own and
	// refused if the other side has had it.
	BIDs bool
}

// Negotiate is what ours and theirs agree to do: whatever both can.
func Negotiate(ours, theirs SID) Mode {
	return Mode{BIDs: ours.Has(FlagBID) && theirs.Has(FlagBID)}
}
