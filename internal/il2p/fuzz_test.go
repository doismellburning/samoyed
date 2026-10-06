// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package il2p

import (
	"io"
	"testing"

	"github.com/doismellburning/samoyed/internal/ax25"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// il2pFuzzQuietly points logrus at the bin for the duration of a fuzz run,
// which has nobody to read what IL2P reports about a malformed frame.
func il2pFuzzQuietly(tb testing.TB) {
	tb.Helper()

	var saved = logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)

	tb.Cleanup(func() { logrus.SetOutput(saved) })
}

// FuzzIL2PDecodeFrame covers the IL2P receive path: header FEC, descrambling
// and the payload blocks.
func FuzzIL2PDecodeFrame(f *testing.F) {
	il2pFuzzQuietly(f)

	Init(0)

	var pp = ax25.FromText("Q1TEST>APDW17,WIDE1-1:!4237.14N/07120.83W#", true)
	require.NotNil(f, pp)

	for _, version := range []Version{Version04, Version06} {
		var encoded, length = il2p_encode_frame(pp, version, 0)
		require.Positive(f, length)
		f.Add(encoded, int(version))
	}

	f.Add(make([]byte, 30), int(Version04))

	f.Fuzz(func(t *testing.T, irec []byte, version int) {
		il2p_decode_frame(irec, Version(version))
	})
}
