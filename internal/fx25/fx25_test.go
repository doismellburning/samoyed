// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package fx25

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindTag(t *testing.T) {
	for ctag := CTagMin; ctag <= CTagMax; ctag++ {
		t.Run(fmt.Sprintf("ctag_%02x", ctag), func(t *testing.T) {
			var value = TagValue(ctag)

			assert.Equal(t, ctag, findTag(value))

			// Up to closeEnough bits wrong still matches...
			var damaged = value
			for bit := range closeEnough {
				damaged ^= 1 << (bit * 7)
			}

			assert.Equal(t, ctag, findTag(damaged))

			// ...but one more does not.
			damaged ^= 1 << 63
			assert.Equal(t, -1, findTag(damaged))
		})
	}

	// The reserved and undefined tags are never matched.
	assert.Equal(t, -1, findTag(tags[0].value))
	assert.Equal(t, -1, findTag(tags[0x0F].value))
}

func TestCodecMatchesTag(t *testing.T) {
	for ctag := CTagMin; ctag <= CTagMax; ctag++ {
		var c = codecFor(ctag)
		require.NotNil(t, c)

		assert.Equal(t, blockSize, c.N())
		assert.Equal(t, nRoots(ctag), c.NRoots())
		assert.Equal(t, blockSize, kDataRS(ctag)+nRoots(ctag))
		assert.LessOrEqual(t, kDataRadio(ctag), kDataRS(ctag))
		assert.LessOrEqual(t, kDataRS(ctag), MaxData)
		assert.LessOrEqual(t, nRoots(ctag), maxCheck)
	}
}
