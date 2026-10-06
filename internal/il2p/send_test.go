// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/linecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureIL2PBits collects the line levels a new Sender sends while fn
// runs.
func captureIL2PBits(fn func(s *Sender)) []int {
	var bits []int

	fn(NewSender(linecode.NewEncoder(func(level int) {
		bits = append(bits, level)
	}), 0))

	return bits
}

// Inverted polarity is the same pattern the other way up.
func TestIL2PPolarityInvertsEveryBit(t *testing.T) {
	var upright = captureIL2PBits(func(s *Sender) {
		s.sendByteMSBFirst(Preamble, 0)
	})
	var inverted = captureIL2PBits(func(s *Sender) {
		s.sendByteMSBFirst(Preamble, 1)
	})

	require.Len(t, inverted, len(upright))

	for i, bit := range upright {
		assert.Equal(t, 1-bit, inverted[i], "bit %d should be inverted", i)
	}
}
