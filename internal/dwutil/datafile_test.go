// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package dwutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenDataFileFindsSourceTreeCopy(t *testing.T) {
	t.Parallel()

	var fp, err = OpenDataFile("tocalls.yaml")
	require.NoError(t, err)

	defer fp.Close()

	assert.Equal(t, "../../data/tocalls.yaml", fp.Name())
}

func TestOpenDataFileNamesSearchedLocations(t *testing.T) {
	t.Parallel()

	var fp, err = OpenDataFile("no-such-data-file.txt")
	assert.Nil(t, fp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-data-file.txt")
	assert.Contains(t, err.Error(), "/usr/share/samoyed/no-such-data-file.txt")
}

// The .deb installs the data files to /usr/share/samoyed, beside rather than
// over Dire Wolf's /usr/share/direwolf, so that is searched. Dire Wolf's
// directories are not, so an installed Dire Wolf's older copy is never used.
func TestOpenDataFileSearchesSamoyedShareNotDirewolfs(t *testing.T) {
	t.Parallel()

	var _, err = OpenDataFile("no-such-data-file.txt")
	require.Error(t, err)

	var msg = err.Error()

	assert.Contains(t, msg, "/usr/local/share/samoyed/no-such-data-file.txt")
	assert.Contains(t, msg, "/usr/share/samoyed/no-such-data-file.txt")
	assert.NotContains(t, msg, "direwolf")
}
