// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBitFixLevelString(t *testing.T) {
	var tests = []struct {
		level BitFixLevel
		want  string
	}{
		{BitFixNone, "NONE"},
		{BitFixSingle, "SINGLE"},
		{BitFixDouble, "DOUBLE"},
		{BitFixTriple, "TRIPLE"},
		{BitFixTwoSep, "TWO_SEP"},
		{BitFixPassall, "PASSALL"},
		{BitFixLevel(9), "(Unknown BitFixLevel 9)"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.level.String())
	}
}
