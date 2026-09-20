// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// il2p_find_rs used to report an unknown parity count and then hand back
// Tab[0].rs anyway, which is nil until il2p_init has run.
func TestIL2PFindRSUnknownParityCount(t *testing.T) {
	il2p_init(0)

	var rs, err = il2p_find_rs(3)
	require.Error(t, err)
	assert.Nil(t, rs)
}

// Every shipped binary calls il2p_init before decoding anything, so an
// uninitialised codec is a programming error - but it used to be a nil pointer
// dereference rather than something the decoder could report.
func TestIL2PDecodeBeforeInit(t *testing.T) {
	var saved = Tab
	t.Cleanup(func() { Tab = saved })

	for i := range NTAB {
		Tab[i].rs = nil
	}

	var rs, err = il2p_find_rs(IL2P_HEADER_PARITY)
	require.Error(t, err)
	assert.Nil(t, rs)

	assert.NotPanics(t, func() {
		var _, e = il2p_decode_rs(make([]byte, 15), IL2P_HEADER_PARITY)
		assert.Equal(t, -1, e)

		assert.Nil(t, il2p_decode_frame(make([]byte, 30), IL2P_VERSION_0_4))
	})

	var _, encodeErr = il2p_encode_rs(make([]byte, 13), IL2P_HEADER_PARITY)
	require.Error(t, encodeErr)
}
