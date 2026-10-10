// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package node

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeardList(t *testing.T) {
	var h = NewHeardList(2)

	h.Heard(0, "Q1TEST", time.Unix(10, 0))
	h.Heard(1, "Q2TEST", time.Unix(20, 0))
	h.Heard(0, "Q1TEST", time.Unix(30, 0))

	var all = h.Stations(-1)
	require.Len(t, all, 2)
	assert.Equal(t, HeardStation{Port: 0, Call: "Q1TEST", Last: time.Unix(30, 0), Frames: 2}, all[0], "most recent first")

	assert.Len(t, h.Stations(1), 1)

	// Full: the one heard longest ago makes way.
	h.Heard(0, "Q3TEST", time.Unix(40, 0))
	assert.Empty(t, h.Stations(1))
	assert.Len(t, h.Stations(0), 2)
}
