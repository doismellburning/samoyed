package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// Test that scramble followed by descramble is the identity.
func Test_il2p_scramble_round_trip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// il2p_scramble_block asserts len >= 1.
		var in = rapid.SliceOfN(rapid.Byte(), 1, 255).Draw(t, "in")

		var scrambled = il2p_scramble_block(in)
		var recovered = il2p_descramble_block(scrambled)

		assert.Equal(t, in, recovered)
	})
}
