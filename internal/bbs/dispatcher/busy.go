// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dispatcher

import (
	"strings"
	"sync"
)

// Busy is the partners this BBS is forwarding with now, whichever side made
// the connection, so that two sessions with one partner never run at once
// over the same queue.  Its zero value has nobody busy.
type Busy struct {
	mu    sync.Mutex
	calls map[string]bool
}

// TryAcquire marks call, with or without its SSID, as busy, if it isn't
// already, and says whether it did.
func (b *Busy) TryAcquire(call string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	var base = baseCall(call)

	if b.calls[base] {
		return false
	}

	if b.calls == nil {
		b.calls = make(map[string]bool)
	}

	b.calls[base] = true

	return true
}

// Release marks call as no longer busy.
func (b *Busy) Release(call string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.calls, baseCall(call))
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}
