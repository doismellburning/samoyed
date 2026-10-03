// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package agwpe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHeaderBinaryRead is a regression test for a bug where binary.Read
// was called with a non-pointer Header value, causing it to fail with
// "invalid type direwolf.AGWPEHeader" and drop incoming connections.
func TestHeaderBinaryRead(t *testing.T) {
	var original = new(Header)
	original.Portx = 1
	original.DataKind = 'C'
	original.PID = 0xF0
	original.DataLen = 42
	copy(original.CallFrom[:], "Q1TEST")
	copy(original.CallTo[:], "Q2TEST")

	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.LittleEndian, original))

	var got = new(Header)
	var readErr = binary.Read(&buf, binary.LittleEndian, got)
	require.NoError(t, readErr)

	assert.Equal(t, original, got)
}

func TestMessageWriteRoundTrip(t *testing.T) {
	var payload = []byte("hello world")
	var msg = new(Message)
	msg.Header.DataKind = 'T'
	msg.Header.PID = 0xF0
	msg.Header.DataLen = uint32(len(payload)) //nolint:gosec // G115: unchecked narrowing conversion, see #294
	msg.Data = payload

	var buf bytes.Buffer
	_, err := msg.Write(&buf, binary.LittleEndian)
	require.NoError(t, err)

	var gotHeader = new(Header)
	require.NoError(t, binary.Read(&buf, binary.LittleEndian, gotHeader))
	assert.Equal(t, msg.Header, *gotHeader)

	var gotData = make([]byte, gotHeader.DataLen)
	_, err = buf.Read(gotData)
	require.NoError(t, err)
	assert.Equal(t, payload, gotData)
}

// A callsign formats without the NUL padding of its fixed-width field.
func TestCallsignString(t *testing.T) {
	var c Callsign
	copy(c[:], "Q1TEST-1")

	assert.Equal(t, "Q1TEST-1", c.String())
	assert.Equal(t, "Connected to Q1TEST-1 ***", fmt.Sprintf("Connected to %s ***", c))
}
