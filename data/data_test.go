// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package data

import (
	"bytes"
	"testing"

	"github.com/doismellburning/samoyed/internal/deviceid"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/symbols"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTocallsYAML(t *testing.T) {
	var d, err = deviceid.FromYAML(TocallsYAML)
	require.NoError(t, err)

	assert.Equal(t, maybe.Just("WB2OSZ DireWolf"), d.FromDest("APDW18"))
}

func TestSymbolsNew(t *testing.T) {
	var sd = symbols.FromReader(bytes.NewReader(SymbolsNew))

	assert.Equal(t, "Jet Ski", sd.Description('J', 's'))
}
