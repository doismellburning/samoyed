// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package ais

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_AIS(t *testing.T) {
	test_sextet(t)
	test_basic_parse(t)
	test_parse_errors(t)
}

func test_sextet(t *testing.T) {
	t.Helper()

	for i := range 64 {
		var ch, chErr = sextet_to_char(i)
		require.NoError(t, chErr)
		var val, err = char_to_sextet(ch)
		require.NoError(t, err)
		assert.Equal(t, i, val)
	}
}

func test_basic_parse(t *testing.T) {
	t.Helper()

	var ais = "!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*4E" // Example from https://www.aggsoft.com/ais-decoder.htm

	var aisData, err = Parse(ais)

	require.NoError(t, err)
	assert.Equal(t, "AIS 1: Position Report Class A", aisData.Description)
	assert.Equal(t, "366730000", aisData.MMSI)
	assert.Empty(t, aisData.Comment)
	assert.InDelta(t, -122, maybe.FromJust(aisData.Lon), 1)
	assert.InDelta(t, 20.8, maybe.FromJust(aisData.Knots), 1)
	assert.InDelta(t, 51.3, maybe.FromJust(aisData.Course), 1)
}

func test_parse_errors(t *testing.T) {
	t.Helper()

	var data, missingChecksumErr = Parse("!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0")
	require.Error(t, missingChecksumErr)
	assert.Nil(t, data)

	var data2, checksumMismatchErr = Parse("!AIVDM,1,1,,A,15MgK45P3@G?fl0E`JbR0OwT0@MS,0*FF")
	require.Error(t, checksumMismatchErr)
	assert.Nil(t, data2)
}
