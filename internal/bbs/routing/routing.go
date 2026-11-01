// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package routing decides where a message goes: kept here, passed to one or
// more partner BBSes, or held for the sysop because nowhere fits.  It only
// decides; the store does what it says.
package routing

import (
	"slices"
	"strings"

	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/maybe"
)

// Wildcard is the partner cover or flood that matches anything.
const Wildcard = "*"

// Self is this BBS, as routing needs it.
type Self struct {
	// Call is the BBS's callsign, without its SSID.
	Call string
}

// Partner is a BBS this one forwards to, and what it is for.
type Partner struct {
	// Call is the partner's callsign, without its SSID.
	Call string

	// Covers are the hierarchical addresses personal mail and traffic is
	// sent to this partner for, such as "GBR.EURO" for anywhere in Britain,
	// or Wildcard for anywhere at all.  A partner always covers mail
	// addressed to the partner itself.
	Covers []string

	// Floods are the bulletin distributions passed on to this partner, such
	// as "GBR" or "WW", or Wildcard for every one.
	Floods []string
}

// Decision is where a message goes.
type Decision struct {
	// Local says the message is to be kept here for users to read.
	Local bool

	// Partners are the partners to forward it to, in configuration order.
	Partners []string

	// Held, if given, says why the message is held for the sysop rather
	// than sent on.
	Held maybe.Maybe[string]
}

// Local is the decision for a message that stays here.
func Local() Decision {
	return Decision{Local: true, Partners: nil, Held: maybe.Nothing[string]()}
}

// Route decides where m goes.  origin, if given, is the partner it came from;
// it, and every BBS m has already been through, are never sent it again.
func Route(m mail.NewMessage, origin maybe.Maybe[string], self Self, partners []Partner) Decision {
	var excluded = excludedCalls(m, origin)

	if m.Type == mail.Bulletin {
		return flood(m.To, excluded, partners)
	}

	return direct(m.To, self, excluded, partners)
}

// excludedCalls are the BBSes m mustn't be sent to: the one it came from,
// and those in its path.
func excludedCalls(m mail.NewMessage, origin maybe.Maybe[string]) []string {
	var excluded = mail.Path(m.Body)

	if from, ok := origin.Get(); ok {
		excluded = append(excluded, from)
	}

	for i, call := range excluded {
		excluded[i] = baseCall(call)
	}

	return excluded
}

// direct routes personal mail and traffic: here if it's for here, otherwise
// to the partner whose cover best fits its address.
func direct(to mail.Address, self Self, excluded []string, partners []Partner) Decision {
	var bbs = to.BBS()
	if bbs == "" || strings.EqualFold(bbs, self.Call) {
		return Local()
	}

	var best = -1
	var bestScore = -1

	for i, p := range partners {
		if slices.Contains(excluded, baseCall(p.Call)) {
			continue
		}

		var score = coverScore(to, p)
		if score > bestScore {
			best, bestScore = i, score
		}
	}

	if best < 0 {
		var held = new(Decision)

		held.Held = maybe.Just("no partner covers " + maybe.FromMaybe(bbs, to.At))

		return *held
	}

	var decision = new(Decision)

	decision.Partners = []string{baseCall(partners[best].Call)}
	decision.Held = maybe.Nothing[string]()

	return *decision
}

// coverScore is how well p covers address: higher is better, and -1 is not
// at all.  Mail addressed to the partner itself fits best; then the cover
// that matches the most of the address, from its least specific end; then
// the wildcard.
func coverScore(address mail.Address, p Partner) int {
	var parts = address.AtParts()

	if strings.EqualFold(address.BBS(), baseCall(p.Call)) {
		return len(parts) + 1
	}

	var score = -1

	for _, cover := range p.Covers {
		if cover == Wildcard {
			score = max(score, 0)

			continue
		}

		var coverParts = strings.Split(strings.ToUpper(cover), ".")

		slices.Reverse(coverParts)

		// A cover naming just a BBS matches mail for that BBS.
		if len(coverParts) == 1 && strings.EqualFold(coverParts[0], address.BBS()) {
			score = max(score, len(parts)+1)

			continue
		}

		if len(coverParts) <= len(parts) && slices.Equal(coverParts, parts[:len(coverParts)]) {
			score = max(score, len(coverParts))
		}
	}

	return score
}

// flood routes a bulletin: always kept here, and passed to every partner
// whose floods include its distribution.  A bulletin with no distribution
// after its @ stays here.
func flood(to mail.Address, excluded []string, partners []Partner) Decision {
	var decision = Local()

	var areas = to.AtParts()
	if len(areas) == 0 {
		return decision
	}

	for _, p := range partners {
		if slices.Contains(excluded, baseCall(p.Call)) {
			continue
		}

		var floods = slices.ContainsFunc(p.Floods, func(f string) bool {
			return f == Wildcard || slices.ContainsFunc(areas, func(area string) bool { return strings.EqualFold(area, f) })
		})

		if floods {
			decision.Partners = append(decision.Partners, baseCall(p.Call))
		}
	}

	return decision
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}
