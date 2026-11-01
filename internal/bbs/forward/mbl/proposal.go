// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package mbl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// ErrBadProposal says a line isn't a proposal that can be read.
var ErrBadProposal = errors.New("bad proposal")

// ParseProposal reads a message offered by the other BBS, such as
// "SP Q1TEST @ Q1BBS.#77.GBR.EURO < Q2TEST $123_Q2BBS": its type, who it is
// to, who it is from and its ID if it has one.  The @ may have spaces round
// it or not, and S on its own is a personal message.  The title and body
// follow on lines of their own.
//
// This is the form FBB's and W0RLI's documentation give, as best recalled;
// it is to be checked against captures (#1152).
func ParseProposal(line string) (mail.NewMessage, error) {
	var fields = strings.Fields(line)
	if len(fields) == 0 || len(fields[0]) > 2 || !strings.EqualFold(fields[0][:1], "S") {
		return mail.NewMessage{}, fmt.Errorf("%w: %q is not S, SP, SB or ST", ErrBadProposal, line)
	}

	var typ = mail.Personal

	if len(fields[0]) == 2 {
		var t, err = mail.ParseType(fields[0][1:])
		if err != nil {
			return mail.NewMessage{}, fmt.Errorf("%w: %w", ErrBadProposal, err)
		}

		typ = t
	}

	var to, from, hasFrom = strings.Cut(strings.Join(fields[1:], " "), "<")
	if !hasFrom {
		return mail.NewMessage{}, fmt.Errorf("%w: %q has no < and sender", ErrBadProposal, line)
	}

	var address, addrErr = mail.ParseAddress(strings.ReplaceAll(to, " ", ""))
	if addrErr != nil {
		return mail.NewMessage{}, fmt.Errorf("%w: %w", ErrBadProposal, addrErr)
	}

	var rest = strings.Fields(from)
	if len(rest) == 0 {
		return mail.NewMessage{}, fmt.Errorf("%w: %q has no sender after the <", ErrBadProposal, line)
	}

	var m = new(mail.NewMessage)

	m.Type = typ
	m.To = address
	m.From = strings.ToUpper(rest[0])
	m.MID = maybe.Nothing[string]()

	for _, field := range rest[1:] {
		var bid, isBID = strings.CutPrefix(field, "$")
		if isBID && bid != "" {
			m.MID = maybe.Just(strings.ToUpper(bid))
		}
	}

	return *m, nil
}

// ProposalFor is the line offering msg to another BBS, with its ID if bids
// says the two use them.
func ProposalFor(msg mail.Message, bids bool) string {
	var line strings.Builder

	line.WriteString("S" + msg.Type.String() + " " + msg.To.To)

	if at, ok := msg.To.At.Get(); ok {
		line.WriteString(" @ " + at)
	}

	line.WriteString(" < " + msg.From)

	if bids && msg.MID != "" {
		line.WriteString(" $" + msg.MID)
	}

	return line.String()
}
