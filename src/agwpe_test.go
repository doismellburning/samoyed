// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package direwolf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAGWPEHeaderBinaryRead is a regression test for a bug where binary.Read
// was called with a non-pointer AGWPEHeader value, causing it to fail with
// "invalid type direwolf.AGWPEHeader" and drop incoming connections.
func TestAGWPEHeaderBinaryRead(t *testing.T) {
	var original = new(AGWPEHeader)
	original.Portx = 1
	original.DataKind = 'C'
	original.PID = 0xF0
	original.DataLen = 42
	copy(original.CallFrom[:], "Q1TEST")
	copy(original.CallTo[:], "Q2TEST")

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, original))

	var got = new(AGWPEHeader)
	var readErr = binary.Read(&buf, binary.LittleEndian, got)
	require.NoError(t, readErr)

	assert.Equal(t, original, got)
}

func TestAGWPEMessageWriteRoundTrip(t *testing.T) {
	var payload = []byte("hello world")
	var msg = new(AGWPEMessage)
	msg.Header.DataKind = 'T'
	msg.Header.PID = 0xF0
	msg.Header.DataLen = uint32(len(payload))
	msg.Data = payload

	var buf bytes.Buffer
	_, err := msg.Write(&buf, binary.LittleEndian)
	require.NoError(t, err)

	var gotHeader = new(AGWPEHeader)
	require.NoError(t, binary.Read(&buf, binary.LittleEndian, gotHeader))
	assert.Equal(t, msg.Header, *gotHeader)

	var gotData = make([]byte, gotHeader.DataLen)
	_, err = buf.Read(gotData)
	require.NoError(t, err)
	assert.Equal(t, payload, gotData)
}
