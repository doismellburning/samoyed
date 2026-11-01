// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package action is what a session's machine - the user shell, or a
// forwarding exchange - asks to be done in answer to a line: lines sent, a
// prompt, or hanging up.  The machines do no I/O themselves; the dispatcher
// carries their actions out.
package action

// Action is something to do in answer to a line.
type Action interface{ isAction() }

// Output is lines to send the station.
type Output struct{ Lines []string }

// Prompt is the prompt to send the station, when the machine is ready for
// its next line.
type Prompt struct{ Text string }

// Disconnect is ending the session.
type Disconnect struct{}

func (Output) isAction()     {}
func (Prompt) isAction()     {}
func (Disconnect) isAction() {}
