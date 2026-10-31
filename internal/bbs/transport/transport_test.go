// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package transport

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKindString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "console", Console.String())
	assert.Equal(t, "AGW", AGW.String())
	assert.Equal(t, "memory", Memory.String())
	assert.Equal(t, "unknown", Kind(0).String())
}
