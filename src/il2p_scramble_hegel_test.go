// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"hegel.dev/go/hegel"
)

// Test that scramble followed by descramble is the identity for arbitrary byte slices.
func Test_il2p_scramble_descramble_round_trip(t *testing.T) {
	t.Run("round trip", hegel.Case(func(ht *hegel.T) {
		// il2p_scramble_block asserts len >= 1.
		var in = hegel.Draw(ht, hegel.Binary(1, 255))

		var scrambled = il2p_scramble_block(in)
		var recovered = il2p_descramble_block(scrambled)

		assert.Equal(ht, in, recovered)
	}))
}
