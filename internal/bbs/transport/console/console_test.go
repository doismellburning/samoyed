// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package console

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsole(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	var s = New(strings.NewReader("L\r"), &out, " q1test ")

	assert.Equal(t, transport.Peer{Call: "Q1TEST", Kind: transport.Console, Authenticated: true}, s.Peer())

	var in, err = io.ReadAll(s)
	require.NoError(t, err)
	assert.Equal(t, "L\r", string(in))

	var n, writeErr = s.Write([]byte("one\rtwo\r"))
	require.NoError(t, writeErr)
	assert.Equal(t, 8, n)
	assert.Equal(t, "one\ntwo\n", out.String(), "a console wants LF where the BBS sends CR")

	require.NoError(t, s.Close())

	var _, readErr = s.Read(make([]byte, 1))
	require.ErrorIs(t, readErr, ErrClosed)

	_, writeErr = s.Write([]byte("x"))
	require.ErrorIs(t, writeErr, ErrClosed)
}
