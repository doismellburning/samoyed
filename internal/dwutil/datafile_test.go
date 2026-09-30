// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package dwutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenDataFileFindsSourceTreeCopy(t *testing.T) {
	var fp, err = OpenDataFile("tocalls.yaml")
	require.NoError(t, err)

	defer fp.Close()

	assert.Equal(t, "../../data/tocalls.yaml", fp.Name())
}

func TestOpenDataFileNamesSearchedLocations(t *testing.T) {
	var fp, err = OpenDataFile("no-such-data-file.txt")
	assert.Nil(t, fp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-data-file.txt")
	assert.Contains(t, err.Error(), "/usr/share/direwolf/no-such-data-file.txt")
}
