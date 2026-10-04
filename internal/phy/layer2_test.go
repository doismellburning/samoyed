// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLayer2String(t *testing.T) {
	assert.Equal(t, "AX.25", Layer2AX25.String())
	assert.Equal(t, "FX.25", Layer2FX25.String())
	assert.Equal(t, "IL2P", Layer2IL2P.String())
	assert.Equal(t, "layer2_t(7)", Layer2(7).String())
}
