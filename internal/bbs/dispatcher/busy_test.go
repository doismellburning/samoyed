// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dispatcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBusy(t *testing.T) {
	t.Parallel()

	var b = new(Busy)

	assert.True(t, b.TryAcquire("Q2BBS-1"))
	assert.False(t, b.TryAcquire("q2bbs"), "the same partner, whatever its SSID")
	assert.True(t, b.TryAcquire("Q3BBS"), "another partner")

	b.Release("Q2BBS")

	assert.True(t, b.TryAcquire("Q2BBS-4"))
}
