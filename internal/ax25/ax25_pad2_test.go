// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package ax25

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSFramePollFinal(t *testing.T) {
	t.Parallel()

	// Any non-zero pf sets the bit, as it does for modulo 8 and for I frames.
	// Modulo 128 used to take only its lowest bit, so 2 cleared it.
	var addrs = [MaxAddrs]string{"Q1TEST", "Q2TEST"}

	for _, modulo := range []Modulo{Modulo8, Modulo128} {
		for _, pf := range []int{0, 1, 2} {
			var pp = SFrame(addrs, 2, CRCmd, FrameTypeSRR, modulo, 3, pf, nil)
			require.NotNil(t, pp)

			var _, _, gotPF, nr, _, ftype = pp.FrameType()
			assert.Equal(t, FrameTypeSRR, ftype)
			assert.Equal(t, 3, nr)
			assert.Equal(t, min(pf, 1), gotPF, "modulo %d, pf %d", modulo, pf)
		}
	}
}
