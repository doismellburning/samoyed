// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package phy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Each audio device has two radio channels to itself, used or not, so the
// channel numbers of every device fit within MaxRadioChans.
func TestADevFirstChan(t *testing.T) {
	assert.Equal(t, 0, ADevFirstChan(0))
	assert.Equal(t, 2, ADevFirstChan(1))
	assert.Equal(t, MaxRadioChans, ADevFirstChan(MaxADevs-1)+2)
}
