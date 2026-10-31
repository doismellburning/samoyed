// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package memt

import (
	"io"
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPair(t *testing.T) {
	t.Parallel()

	var peer = transport.Peer{Call: "Q2TEST", Kind: transport.Memory, Authenticated: false}

	var s, station = Pair(peer)

	t.Cleanup(func() {
		_ = s.Close()
		_ = station.Close()
	})

	assert.Equal(t, peer, s.Peer())

	go func() {
		_, _ = station.Write([]byte("hello"))
		_ = station.Close()
	}()

	var got, err = io.ReadAll(s)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))
}
