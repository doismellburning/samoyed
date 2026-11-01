// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package fbb

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
)

var (
	// ErrBadProposal says an FB line can't be read.
	ErrBadProposal = errors.New("bad FB line")

	// ErrBadAnswer says an FS line can't be read.
	ErrBadAnswer = errors.New("bad FS line")
)

// Offer is a message as an FB line offers it: everything but its title and
// text, which follow if it is accepted.
type Offer struct {
	Message mail.NewMessage

	// Size is the size of its text, in bytes.
	Size int
}

// ParseFB reads an FB line: "FB <type> <from> <@bbs> <to> <bid> <size>",
// every field needed.
func ParseFB(line string) (Offer, error) {
	var fields = strings.Fields(line)
	if len(fields) != 7 || !strings.EqualFold(fields[0], "FB") {
		return Offer{}, fmt.Errorf("%w: %q is not FB and six fields", ErrBadProposal, line)
	}

	var typ, typeErr = mail.ParseType(fields[1])
	if typeErr != nil {
		return Offer{}, fmt.Errorf("%w: %w", ErrBadProposal, typeErr)
	}

	var to, addrErr = mail.ParseAddress(fields[4] + "@" + fields[3])
	if addrErr != nil {
		return Offer{}, fmt.Errorf("%w: %w", ErrBadProposal, addrErr)
	}

	var size, sizeErr = strconv.Atoi(fields[6])
	if sizeErr != nil || size < 0 {
		return Offer{}, fmt.Errorf("%w: %q is not a size", ErrBadProposal, fields[6])
	}

	var m = new(mail.NewMessage)

	m.Type = typ
	m.From = strings.ToUpper(fields[2])
	m.To = to
	m.MID = maybe.Just(strings.ToUpper(fields[5]))

	return Offer{Message: *m, Size: size}, nil
}

// FBFor is the FB line offering msg to partner.  A message with no @ has the
// partner's call in its place, every field being needed.
func FBFor(msg mail.Message, partner string) string {
	var at = maybe.FromMaybe(partner, msg.To.At)

	return fmt.Sprintf("FB %s %s %s %s %s %d", msg.Type, msg.From, at, msg.To.To, msg.MID, len(msg.Body))
}

// Checksum is the checksum of a proposal's FB lines, as sent after its F>: the
// sum of their bytes, each line ended with a CR, modulo 256, two's
// complemented.  FBB's documentation describes its message checksum that way
// and doesn't spell out the proposal's, so this is a reading of it that is
// still to be checked against another BBS: LinBPQ, the one tried, doesn't
// forward in FBB's protocol uncompressed.
func Checksum(lines []string) byte {
	var sum byte

	for _, line := range lines {
		for i := range len(line) {
			sum += line[i]
		}

		sum += '\r'
	}

	return -sum
}

// Answer is what an FS line says of one message offered.
type Answer byte

const (
	// Accept is wanting the message.
	Accept Answer = '+'

	// Refuse is not wanting it, as one already had.
	Refuse Answer = '-'

	// Defer is not wanting it yet: it is to be offered again another time.
	Defer Answer = '='

	// Reject is refusing it, in version 1 of the protocol.
	Reject Answer = 'R'

	// Hold is the other side holding it for its sysop.
	Hold Answer = 'H'

	// FormatError is the other side finding its FB line wrong.
	FormatError Answer = 'E'
)

// ParseFS reads an FS line, the answer to a proposal of n messages.
func ParseFS(line string, n int) ([]Answer, error) {
	var signs, isFS = strings.CutPrefix(strings.TrimSpace(line), "FS")
	if !isFS {
		return nil, fmt.Errorf("%w: %q is not FS", ErrBadAnswer, line)
	}

	signs = strings.ToUpper(strings.TrimSpace(signs))

	if len(signs) != n {
		return nil, fmt.Errorf("%w: %q answers %d messages, not %d", ErrBadAnswer, line, len(signs), n)
	}

	var answers = make([]Answer, 0, n)

	for i := range len(signs) {
		var a = Answer(signs[i])

		switch a {
		case Accept, Refuse, Defer, Reject, Hold, FormatError:
			answers = append(answers, a)
		default:
			return nil, fmt.Errorf("%w: %q is not one of +-=RHE", ErrBadAnswer, string(signs[i]))
		}
	}

	return answers, nil
}
