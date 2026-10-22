// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMediumCarriesAX25(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		m    Medium
		want bool
	}{
		{MediumNone, false},
		{MediumRadio, true},
		{MediumIGate, false},
		{MediumNetTNC, true},
		{MediumSerialTNC, true},
		{MediumAXUDP, true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.m.CarriesAX25(), "medium %d", tt.m)
	}
}
