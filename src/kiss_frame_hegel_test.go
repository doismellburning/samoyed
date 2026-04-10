// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hegel.dev/go/hegel"
)

// Test that encapsulate followed by unwrap is the identity for arbitrary byte slices.
func Test_kiss_encapsulate_unwrap_round_trip(t *testing.T) {
	t.Run("round trip", hegel.Case(func(ht *hegel.T) {
		var in = hegel.Draw(ht, hegel.Binary(1, 1024))

		var encapsulated = kiss_encapsulate(in)
		var recovered = kiss_unwrap(encapsulated)

		assert.Equal(ht, in, recovered)
	}))
}
