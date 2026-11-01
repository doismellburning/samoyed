// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mail

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/doismellburning/samoyed/internal/maybe"
)

// ErrBadRLine says a line isn't an R: line that can be read.
var ErrBadRLine = errors.New("bad R: line")

// rlineTime is the date and time on an R: line, without its Z.
const rlineTime = "060102/1504"

// RLine is one of the lines a BBS adds to the top of a message it takes,
// saying when, and which BBS it is.  Following them down a message gives the
// way it came, newest first, which is how a BBS sees a message has been
// round before.
//
// The form written here, "R:YYMMDD/HHMMZ @:CALL.HROUTE #:n $:MID", is the
// one this BBS believes is usual, but no specification of it has been found:
// it is to be checked against captures from FBB, BPQ and XRouter (#1152).
// Reading is lenient for the same reason.
type RLine struct {
	// When the BBS took the message, in UTC.
	When time.Time

	// BBS is the callsign of the BBS that took it.
	BBS string

	// HRoute is the rest of that BBS's hierarchical address, if given.
	HRoute maybe.Maybe[string]

	// Number is the message's number at that BBS, if given.
	Number maybe.Maybe[int]

	// MID is the message's ID, if given.
	MID maybe.Maybe[string]

	// Rest is anything else on the line, as it was.
	Rest string
}

// String writes the line in the form this BBS adds.
func (r RLine) String() string {
	var line strings.Builder

	line.WriteString("R:" + r.When.UTC().Format(rlineTime) + "Z @:" + r.BBS)

	if hroute, ok := r.HRoute.Get(); ok {
		line.WriteString("." + hroute)
	}

	if number, ok := r.Number.Get(); ok {
		line.WriteString(" #:" + strconv.Itoa(number))
	}

	if mid, ok := r.MID.Get(); ok {
		line.WriteString(" $:" + mid)
	}

	if r.Rest != "" {
		line.WriteString(" " + r.Rest)
	}

	return line.String()
}

// ParseRLine reads an R: line.  It needs the date and time, and the BBS's
// callsign: from an @: field, or failing that the first word after the time
// with anything up to an @ dropped.  Fields it doesn't know are kept in Rest.
func ParseRLine(line string) (RLine, error) {
	var text, isR = strings.CutPrefix(strings.TrimSpace(line), "R:")
	if !isR {
		return RLine{}, fmt.Errorf("%w: %q is not an R: line", ErrBadRLine, line)
	}

	var fields = strings.Fields(text)
	if len(fields) == 0 {
		return RLine{}, fmt.Errorf("%w: %q has no date", ErrBadRLine, line)
	}

	var stamp = strings.TrimSuffix(strings.ToUpper(fields[0]), "Z")

	var when, timeErr = time.Parse(rlineTime, stamp)
	if timeErr != nil {
		return RLine{}, fmt.Errorf("%w: %q has no YYMMDD/HHMM date", ErrBadRLine, line)
	}

	var r = new(RLine)

	r.When = when

	var rest []string
	var unnamed []string

	for _, field := range fields[1:] {
		switch {
		case strings.HasPrefix(field, "@:"):
			r.BBS, r.HRoute = splitAddress(strings.TrimPrefix(field, "@:"))
		case strings.HasPrefix(field, "#:"):
			var n, err = strconv.Atoi(strings.TrimPrefix(field, "#:"))
			if err == nil {
				r.Number = maybe.Just(n)
			} else {
				rest = append(rest, field)
			}
		case strings.HasPrefix(field, "$:"):
			r.MID = maybe.Just(strings.TrimPrefix(field, "$:"))
		default:
			unnamed = append(unnamed, field)
			rest = append(rest, field)
		}
	}

	if r.BBS == "" && len(unnamed) > 0 {
		// An older form: the BBS's address without an @: in front, maybe
		// after a message number and an @.
		var _, address, _ = strings.Cut(unnamed[0], "@")
		if address == "" {
			address = unnamed[0]
		}

		r.BBS, r.HRoute = splitAddress(address)
		rest = removeFirst(rest, unnamed[0])
	}

	if r.BBS == "" || !onlyChars(r.BBS, "-") {
		return RLine{}, fmt.Errorf("%w: %q names no BBS", ErrBadRLine, line)
	}

	r.Rest = strings.Join(rest, " ")

	return *r, nil
}

// splitAddress is a BBS's address split into its callsign and the rest of
// its hierarchical address, if there is any.
func splitAddress(address string) (string, maybe.Maybe[string]) {
	var call, hroute, hasRoute = strings.Cut(strings.ToUpper(address), ".")

	if hasRoute && hroute != "" {
		return call, maybe.Just(hroute)
	}

	return call, maybe.Nothing[string]()
}

// removeFirst is rest without the first of its fields equal to field.
func removeFirst(rest []string, field string) []string {
	for i, f := range rest {
		if f == field {
			return append(rest[:i:i], rest[i+1:]...)
		}
	}

	return rest
}

// SplitRLines splits a message body into the R: lines at its top and the rest
// of it.  Bodies end each line with CR.
func SplitRLines(body []byte) ([]string, []byte) {
	var rlines []string
	var rest = body

	for len(rest) > 0 {
		var line, after, _ = bytes.Cut(rest, []byte("\r"))
		if !bytes.HasPrefix(line, []byte("R:")) {
			break
		}

		rlines = append(rlines, string(line))
		rest = after
	}

	return rlines, rest
}

// Path is the callsigns of the BBSes a message has been through, newest
// first, from the R: lines at the top of its body.  Lines that can't be read
// are passed over.
func Path(body []byte) []string {
	var rlines, _ = SplitRLines(body)
	var path []string

	for _, line := range rlines {
		var r, err = ParseRLine(line)
		if err == nil {
			path = append(path, r.BBS)
		}
	}

	return path
}

// PrependRLine puts r at the top of body.
func PrependRLine(body []byte, r RLine) []byte {
	return append([]byte(r.String()+"\r"), body...)
}
