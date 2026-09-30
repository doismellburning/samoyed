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
	assert.InDelta(t, 6.21371192, DW_KM_TO_MILES(10), 0.0000001)
}
