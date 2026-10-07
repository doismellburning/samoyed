// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// il2p_find_rs used to report an unknown parity count and then hand back
// the first table entry's codec anyway.
func TestIL2PFindRSUnknownParityCount(t *testing.T) {
	var rs, err = il2p_find_rs(3)
	require.Error(t, err)
	assert.Nil(t, rs)
}
