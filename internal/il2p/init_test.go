// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// il2p_find_rs used to report an unknown parity count and then hand back
// rsTab[0].rs anyway, which is nil until Init has run.
func TestIL2PFindRSUnknownParityCount(t *testing.T) {
	Init(0)

	var rs, err = il2p_find_rs(3)
	require.Error(t, err)
	assert.Nil(t, rs)
}

// Every shipped binary calls Init before decoding anything, so an
// uninitialised codec is a programming error - but it used to be a nil pointer
// dereference rather than something the decoder could report.
func TestIL2PDecodeBeforeInit(t *testing.T) {
	var saved = rsTab
	t.Cleanup(func() { rsTab = saved })

	for i := range rsNTab {
		rsTab[i].rs = nil
	}

	var rs, err = il2p_find_rs(HeaderParity)
	require.Error(t, err)
	assert.Nil(t, rs)

	assert.NotPanics(t, func() {
		var _, e = il2p_decode_rs(make([]byte, 15), HeaderParity)
		assert.Equal(t, -1, e)

		assert.Nil(t, il2p_decode_frame(make([]byte, 30), Version0_4))
	})

	var _, encodeErr = il2p_encode_rs(make([]byte, 13), HeaderParity)
	require.Error(t, encodeErr)
}
