// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package il2p

import (
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/stretchr/testify/require"
)

// The decoders are the part of Samoyed that anyone on frequency can reach.
// A target's job is to make the call; the failure it is looking for is a
// panic, so there is nothing to assert.

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	Init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []Version{Version0_4, Version0_6} {
		var encoded, length = EncodeFrame(pp, version, 0)
		require.Positive(f, length)
		f.Add(encoded, int(version))
	}

	f.Add(make([]byte, 30), int(Version0_4))

	f.Fuzz(func(t *testing.T, irec []byte, version int) {
		il2p_decode_frame(irec, Version(version))
	})
}
