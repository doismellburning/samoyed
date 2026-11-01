// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package forward

import (
	"context"
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/action"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echo is a protocol that sends the SID it was chosen for, and then echoes.
type echo struct{ theirs sid.SID }

func (e *echo) Start(context.Context) []action.Action {
	return []action.Action{action.Output{Lines: []string{"chosen for " + e.theirs.Name}}}
}

func (e *echo) Line(_ context.Context, line string) []action.Action {
	return []action.Action{action.Output{Lines: []string{"got " + line}}}
}

func newCalling(chosen *[]sid.SID) *Calling {
	return NewCalling(func(theirs sid.SID) Machine {
		*chosen = append(*chosen, theirs)

		return &echo{theirs: theirs}
	})
}

func TestCallingWaitsForTheGreeting(t *testing.T) {
	t.Parallel()

	var chosen []sid.SID
	var c = newCalling(&chosen)

	assert.Empty(t, c.Start(t.Context()), "the partner greets first")
	assert.Empty(t, c.Line(t.Context(), "Welcome to Q2BBS."))
	assert.Empty(t, c.Line(t.Context(), " [FBB-7.0-ABFHM$] "))
	assert.Empty(t, chosen, "not until the prompt")

	assert.Equal(t, []action.Action{action.Output{Lines: []string{"chosen for FBB"}}}, c.Line(t.Context(), "Q2BBS>"))
	assert.Equal(t, []action.Action{action.Output{Lines: []string{"got F>"}}}, c.Line(t.Context(), "F>"), "and then the protocol has it")

	require.Len(t, chosen, 1)
	assert.Equal(t, "ABFHM$", chosen[0].Flags)
}

func TestCallingHangsUpOnSomethingWithoutASID(t *testing.T) {
	t.Parallel()

	var chosen []sid.SID
	var c = newCalling(&chosen)

	assert.Empty(t, c.Line(t.Context(), "Welcome to Q2NODE."))
	assert.Equal(t, []action.Action{action.Disconnect{}}, c.Line(t.Context(), "Q2NODE>"))
	assert.Empty(t, chosen)
}

func TestIsPrompt(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"de Q2BBS>", ">", " Q2BBS> "} {
		assert.True(t, IsPrompt(line), line)
	}

	for _, line := range []string{"F>", "f>", "Hello", "", "> not at the end"} {
		assert.False(t, IsPrompt(line), line)
	}
}
