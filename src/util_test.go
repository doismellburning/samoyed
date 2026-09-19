// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The unit conversions are plain multiplications now that absence is carried
// by maybe.Maybe rather than by a sentinel the conversion had to recognise.
func Test_unit_conversions(t *testing.T) {
	assert.InDelta(t, 11.5077945, DW_KNOTS_TO_MPH(10), 0.0000001)
	assert.InDelta(t, 8.68976, DW_MPH_TO_KNOTS(10), 0.0000001)
	assert.InDelta(t, 32.808399, DW_METERS_TO_FEET(10), 0.0000001)
	assert.InDelta(t, 3.048, DW_FEET_TO_METERS(10), 0.0000001)
	assert.InDelta(t, 16.09344, DW_MILES_TO_KM(10), 0.0000001)
	assert.InDelta(t, 0.295333727, DW_MBAR_TO_INHG(10), 0.0000001)
	assert.InDelta(t, 6.21371192, DW_KM_TO_MILES(10), 0.0000001)

	// Zero is a real reading, not an absent one, and converts as such.
	assert.InDelta(t, 0.0, DW_KNOTS_TO_MPH(0), 0.0000001)
}
