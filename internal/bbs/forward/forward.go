// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package forward is what forwarding in each protocol shares: greeting a
// partner this BBS has called, and choosing, from the SID it sends, which
// protocol the exchange is in.
package forward

import (
	"context"
	"strings"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
)

// Machine is a forwarding session, in whichever protocol.
type Machine interface {
	Start(ctx context.Context) []action.Action
	Line(ctx context.Context, line string) []action.Action
}

// Calling is a session with a partner this BBS has called.  The partner greets
// it - a welcome, its SID, and a prompt - and once it has, the session goes on
// in the protocol choose picks for that SID.  Something that prompts without a
// SID isn't a BBS that forwards, so is hung up on.
type Calling struct {
	choose func(theirs sid.SID) Machine

	heardSID bool
	theirs   sid.SID
	next     Machine
}

// NewCalling is a session with a partner this BBS has called, going on in
// whatever protocol choose picks once the partner has greeted it.
func NewCalling(choose func(theirs sid.SID) Machine) *Calling {
	var c = new(Calling)

	c.choose = choose

	return c
}

// Start is what to send when the session begins: nothing, as the partner
// greets first.
func (c *Calling) Start(context.Context) []action.Action {
	return nil
}

// Line acts on one line from the partner.
func (c *Calling) Line(ctx context.Context, line string) []action.Action {
	if c.next != nil {
		return c.next.Line(ctx, line)
	}

	var theirs, sidErr = sid.Parse(strings.TrimSpace(line))
	if sidErr == nil {
		c.theirs = theirs
		c.heardSID = true

		return nil
	}

	if !IsPrompt(line) {
		return nil
	}

	if !c.heardSID {
		return []action.Action{action.Disconnect{}}
	}

	c.next = c.choose(c.theirs)

	return c.next.Start(ctx)
}

// IsPrompt says whether line is a BBS's prompt, which ends in >.  F> is not a
// prompt but the turn handed over.
func IsPrompt(line string) bool {
	var text = strings.TrimSpace(line)

	return strings.HasSuffix(text, ">") && !strings.EqualFold(text, "F>")
}
