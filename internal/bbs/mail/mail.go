// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package mail is the BBS's messages: what they are, who they are to, and
// the IDs that keep one message from being taken twice.
package mail

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// Type is what sort of message it is.
type Type byte

const (
	// Personal is to one station.
	Personal Type = 'P'

	// Bulletin is to everyone in a distribution area.
	Bulletin Type = 'B'

	// Traffic is NTS traffic, for delivery outside packet.
	Traffic Type = 'T'
)

func (t Type) String() string { return string(rune(t)) }

// ParseType reads a message type from its letter.
func ParseType(s string) (Type, error) {
	switch strings.ToUpper(s) {
	case "P":
		return Personal, nil
	case "B":
		return Bulletin, nil
	case "T":
		return Traffic, nil
	default:
		return 0, fmt.Errorf("message type %q is not P, B or T", s)
	}
}

// Status is where a message is in its life.
type Status byte

const (
	// New has not been read by whoever it is to.
	New Status = 'N'

	// Read has been.
	Read Status = 'Y'

	// Forwarded has gone on to every BBS it was for.
	Forwarded Status = 'F'

	// Killed has been deleted, and stays only until it expires.
	Killed Status = 'K'

	// Held waits for the sysop.
	Held Status = 'H'
)

func (s Status) String() string { return string(rune(s)) }

// ParseStatus reads a message status from its letter.
func ParseStatus(s string) (Status, error) {
	for _, status := range []Status{New, Read, Forwarded, Killed, Held} {
		if strings.EqualFold(s, status.String()) {
			return status, nil
		}
	}

	return 0, fmt.Errorf("message status %q is not one of NYFKH", s)
}

const (
	// MaxToLen is the longest a message's To can be: a callsign without its
	// SSID, or a bulletin's subject area.
	MaxToLen = 6

	// MaxCallLen is the longest a sender's callsign can be, with its SSID.
	MaxCallLen = 9

	// MaxAtLen is the longest a message's @ can be.
	MaxAtLen = 40

	// MaxMIDLen is the longest a message ID can be.
	MaxMIDLen = 12

	// MaxTitleLen is the longest a message's title can be.
	MaxTitleLen = 80
)

// ErrBadAddress says an address couldn't be read.
var ErrBadAddress = errors.New("bad address")

// ErrDuplicateBID says a message's ID has been seen before, so the message
// is one already taken.
var ErrDuplicateBID = errors.New("message ID already seen")

// Address is who a message is for: a callsign or bulletin area, and
// optionally the BBS, or distribution, after an @.
type Address struct {
	To string
	At maybe.Maybe[string]
}

// ParseAddress reads an address such as "Q1TEST", "Q1TEST@Q1BBS" or
// "ALL@GBR".  It is upper-cased, and checked only for its length and the
// characters in it.
func ParseAddress(s string) (Address, error) {
	var to, at, hasAt = strings.Cut(strings.ToUpper(strings.TrimSpace(s)), "@")

	var address = new(Address)

	if to == "" || len(to) > MaxToLen || !onlyChars(to, "") {
		return Address{}, fmt.Errorf("%w: %q is not a callsign or area of up to %d letters and digits", ErrBadAddress, to, MaxToLen)
	}

	address.To = to

	if hasAt {
		if at == "" || len(at) > MaxAtLen || !onlyChars(at, ".#-_") {
			return Address{}, fmt.Errorf("%w: %q after the @ is not a BBS or area of up to %d characters", ErrBadAddress, at, MaxAtLen)
		}

		address.At = maybe.Just(at)
	}

	return *address, nil
}

func (a Address) String() string {
	return maybe.Format("%s", a.To, maybe.Fmap(func(at string) string { return a.To + "@" + at }, a.At))
}

// onlyChars says whether s is all upper-case letters, digits and extra.
func onlyChars(s string, extra string) bool {
	for _, r := range s {
		var ok = (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune(extra, r)
		if !ok {
			return false
		}
	}

	return true
}

// GenerateMID makes the ID of a message originated here: its number, then the
// BBS's callsign without its SSID, cut to MaxMIDLen.
func GenerateMID(number int, call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")
	var mid = strconv.Itoa(number) + "_" + base

	return mid[:min(len(mid), MaxMIDLen)]
}

// NewMessage is a message as offered: by a user typing it in, or by a
// forwarding partner.
type NewMessage struct {
	Type  Type
	From  string
	To    Address
	Title string
	Body  []byte

	// MID is the message's ID, if it already has one.  One originated
	// here gets one when it is stored.
	MID maybe.Maybe[string]
}

// Validate checks a new message is one that can be stored.
func (m NewMessage) Validate() error {
	var problems []error

	var _, typeErr = ParseType(m.Type.String())
	if typeErr != nil {
		problems = append(problems, typeErr)
	}

	if m.From == "" || len(m.From) > MaxCallLen {
		problems = append(problems, fmt.Errorf("sender %q is not a callsign", m.From))
	}

	if m.To.To == "" {
		problems = append(problems, errors.New("no addressee"))
	}

	if len(m.Title) > MaxTitleLen {
		problems = append(problems, fmt.Errorf("title is %d characters, more than %d", len(m.Title), MaxTitleLen))
	}

	if mid, ok := m.MID.Get(); ok && (mid == "" || len(mid) > MaxMIDLen) {
		problems = append(problems, fmt.Errorf("message ID %q is not 1 to %d characters", mid, MaxMIDLen))
	}

	return errors.Join(problems...)
}

// Summary is a message without its body, as a listing shows it.
type Summary struct {
	Number  int
	MID     string
	Type    Type
	Status  Status
	From    string
	To      Address
	Title   string
	Size    int
	Created time.Time
}

// Message is a stored message.
type Message struct {
	Summary

	Body []byte
}

// ListQuery says which messages to list.  The zero value lists every message
// that hasn't been killed.
type ListQuery struct {
	// Types, if any, are the only types listed.
	Types []Type

	// From and To, if given, are the only sender and addressee listed.
	From maybe.Maybe[string]
	To   maybe.Maybe[string]

	// Lowest and Highest, if given, bound the message numbers listed.
	Lowest  maybe.Maybe[int]
	Highest maybe.Maybe[int]

	// Statuses, if any, are the only statuses listed.  Without them,
	// killed messages are left out.
	Statuses []Status
}
