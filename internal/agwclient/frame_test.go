// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/ccoveille/go-safecast/v2"
	"github.com/doismellburning/samoyed/internal/agwpe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encode(t *testing.T, msg *agwpe.Message) []byte {
	t.Helper()

	var buf bytes.Buffer

	require.NoError(t, writeFrame(&buf, msg))

	return buf.Bytes()
}

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()

	var msg, err = newFrame(3, kindData, "Q1TEST-1", "Q2TEST", []byte("hello"))
	require.NoError(t, err)

	var got, readErr = readFrame(bytes.NewReader(encode(t, msg)), DefaultMaxData)
	require.NoError(t, readErr)

	assert.Equal(t, byte(3), got.Header.Portx)
	assert.Equal(t, byte(kindData), got.Header.DataKind)
	assert.Equal(t, "Q1TEST-1", got.Header.CallFrom.String())
	assert.Equal(t, "Q2TEST", got.Header.CallTo.String())
	assert.Equal(t, []byte("hello"), got.Data)
}

func TestReadFrameRefusesOversizedData(t *testing.T) {
	t.Parallel()

	var msg, err = newFrame(0, kindData, "Q1TEST", "Q2TEST", make([]byte, 100))
	require.NoError(t, err)

	var _, readErr = readFrame(bytes.NewReader(encode(t, msg)), 99)
	require.ErrorIs(t, readErr, ErrFrameTooLarge)
}

func TestReadFrameEnds(t *testing.T) {
	t.Parallel()

	var msg, err = newFrame(0, kindData, "Q1TEST", "Q2TEST", []byte("hello"))
	require.NoError(t, err)

	var whole = encode(t, msg)

	t.Run("cleanly between frames", func(t *testing.T) {
		t.Parallel()

		var _, readErr = readFrame(bytes.NewReader(nil), DefaultMaxData)
		require.ErrorIs(t, readErr, io.EOF)
	})

	t.Run("part way through the header", func(t *testing.T) {
		t.Parallel()

		var _, readErr = readFrame(bytes.NewReader(whole[:10]), DefaultMaxData)
		require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
	})

	t.Run("part way through the data", func(t *testing.T) {
		t.Parallel()

		var _, readErr = readFrame(bytes.NewReader(whole[:len(whole)-2]), DefaultMaxData)
		require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
	})

	t.Run("before any data", func(t *testing.T) {
		t.Parallel()

		var _, readErr = readFrame(bytes.NewReader(whole[:len(whole)-5]), DefaultMaxData)
		require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
	})
}

func TestNewFrameRefusesLongCallsign(t *testing.T) {
	t.Parallel()

	var _, err = newFrame(0, kindConnect, "Q1TESTLONG", "Q2TEST", nil)
	require.Error(t, err)

	_, err = newFrame(0, kindConnect, "Q1TEST", "Q2TESTLONG", nil)
	require.Error(t, err)
}

func TestNewFrameRefusesBadPort(t *testing.T) {
	t.Parallel()

	for _, port := range []int{-1, 256} {
		var _, err = newFrame(port, kindConnect, "Q1TEST", "Q2TEST", nil)
		require.Error(t, err, "port %d", port)
	}
}

func TestLoginFrame(t *testing.T) {
	t.Parallel()

	var msg, err = loginFrame("Q1TEST", "correct horse")
	require.NoError(t, err)

	assert.Equal(t, byte(kindLogin), msg.Header.DataKind)
	require.Len(t, msg.Data, 2*loginFieldLen)
	assert.Equal(t, "Q1TEST", string(bytes.TrimRight(msg.Data[:loginFieldLen], "\x00")))
	assert.Equal(t, "correct horse", string(bytes.TrimRight(msg.Data[loginFieldLen:], "\x00")))

	_, err = loginFrame(string(make([]byte, loginFieldLen)), "")
	require.Error(t, err)
}

func TestConnectFrame(t *testing.T) {
	t.Parallel()

	t.Run("direct", func(t *testing.T) {
		t.Parallel()

		var msg, err = connectFrame(1, "Q1TEST", "Q2TEST", nil)
		require.NoError(t, err)

		assert.Equal(t, byte(kindConnect), msg.Header.DataKind)
		assert.Equal(t, byte(1), msg.Header.Portx)
		assert.Empty(t, msg.Data)
	})

	t.Run("via digipeaters", func(t *testing.T) {
		t.Parallel()

		var msg, err = connectFrame(0, "Q1TEST", "Q2TEST", []string{"Q3TEST", "Q4TEST-7"})
		require.NoError(t, err)

		assert.Equal(t, byte(kindConnectVia), msg.Header.DataKind)
		require.Len(t, msg.Data, 1+2*callsignLen)
		assert.Equal(t, byte(2), msg.Data[0])
		assert.Equal(t, "Q3TEST", string(bytes.TrimRight(msg.Data[1:1+callsignLen], "\x00")))
		assert.Equal(t, "Q4TEST-7", string(bytes.TrimRight(msg.Data[1+callsignLen:], "\x00")))
	})

	t.Run("too many digipeaters", func(t *testing.T) {
		t.Parallel()

		var _, err = connectFrame(0, "Q1TEST", "Q2TEST", make([]string, maxVia+1))
		require.Error(t, err)
	})
}

func TestOutstandingCount(t *testing.T) {
	t.Parallel()

	var msg = new(agwpe.Message)

	msg.Data = binary.LittleEndian.AppendUint32(nil, 7)

	var count, err = outstandingCount(msg)
	require.NoError(t, err)
	assert.Equal(t, uint32(7), count)

	msg.Data = msg.Data[:3]

	_, err = outstandingCount(msg)
	require.Error(t, err)
}

func TestNormaliseCall(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"q1test":    "Q1TEST",
		"Q1TEST-0":  "Q1TEST",
		"q1test-7":  "Q1TEST-7",
		"Q1TEST-10": "Q1TEST-10",
	} {
		assert.Equal(t, want, normaliseCall(in), in)
	}
}

func FuzzReadFrame(f *testing.F) {
	for _, kind := range []byte{kindConnect, kindData, kindDisconnect, kindOutstanding, kindPorts} {
		var msg, err = newFrame(0, kind, "Q1TEST", "Q2TEST", []byte("1;Port1 x;"))
		if err != nil {
			f.Fatal(err)
		}

		var buf bytes.Buffer

		var writeErr = writeFrame(&buf, msg)
		if writeErr != nil {
			f.Fatal(writeErr)
		}

		f.Add(buf.Bytes())
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const limit = 1024

		var msg, err = readFrame(bytes.NewReader(data), limit)
		if err != nil {
			if msg != nil {
				t.Fatal("frame returned alongside an error")
			}

			return
		}

		if len(msg.Data) > limit {
			t.Fatalf("accepted %d bytes of data, over the limit of %d", len(msg.Data), limit)
		}

		if safecast.RequireConvert[uint32](t, len(msg.Data)) != msg.Header.DataLen {
			t.Fatalf("data is %d bytes but the header says %d", len(msg.Data), msg.Header.DataLen)
		}
	})
}
