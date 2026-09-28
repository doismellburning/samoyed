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

			assert.Equal(t, ctag, FindTag(value))

			// Up to closeEnough bits wrong still matches...
			var damaged = value
			for bit := range closeEnough {
				damaged ^= 1 << (bit * 7)
			}

			assert.Equal(t, ctag, FindTag(damaged))

			// ...but one more does not.
			damaged ^= 1 << 63
			assert.Equal(t, -1, FindTag(damaged))
		})
	}

	// The reserved and undefined tags are never matched.
	assert.Equal(t, -1, FindTag(tags[0].value))
	assert.Equal(t, -1, FindTag(tags[0x0F].value))
}

func TestCodecMatchesTag(t *testing.T) {
	for ctag := CTagMin; ctag <= CTagMax; ctag++ {
		var c = Codec(ctag)
		require.NotNil(t, c)

		assert.Equal(t, BlockSize, c.N())
		assert.Equal(t, NRoots(ctag), c.NRoots())
		assert.Equal(t, BlockSize, KDataRS(ctag)+NRoots(ctag))
		assert.LessOrEqual(t, KDataRadio(ctag), KDataRS(ctag))
		assert.LessOrEqual(t, KDataRS(ctag), MaxData)
		assert.LessOrEqual(t, NRoots(ctag), maxCheck)
	}
}
