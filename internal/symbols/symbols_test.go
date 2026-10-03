// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package symbols

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromReader(t *testing.T) {
	var sd = FromReader(strings.NewReader(strings.Join([]string{
		"Some preamble that is not a symbol line",
		"Qs = Q1TEST Ship",
		"",
	}, "\n")))

	assert.Equal(t, "Q1TEST Ship", sd.Description('Q', 's'))
}
