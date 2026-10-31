// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package lines

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAll reads lines until the input ends.
func readAll(t *testing.T, r *Reader) []string {
	t.Helper()

	var got []string

	for {
		var line, err = r.ReadLine(t.Context())
		if errors.Is(err, io.EOF) {
			return got
		}

		require.NoError(t, err)

		got = append(got, line)
	}
}

// chunks reads parts one Read at a time, as frames arriving one by one would.
type chunks struct{ parts []string }

func (c *chunks) Read(p []byte) (int, error) {
	if len(c.parts) == 0 {
		return 0, io.EOF
	}

	var n = copy(p, c.parts[0])
	c.parts[0] = c.parts[0][n:]

	if c.parts[0] == "" {
		c.parts = c.parts[1:]
	}

	return n, nil
}

func TestReadLine(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		name  string
		parts []string
		want  []string
	}{
		{name: "CR", parts: []string{"one\rtwo\r"}, want: []string{"one", "two"}},
		{name: "LF", parts: []string{"one\ntwo\n"}, want: []string{"one", "two"}},
		{name: "CRLF", parts: []string{"one\r\ntwo\r\n"}, want: []string{"one", "two"}},
		{name: "CRLF split between reads", parts: []string{"one\r", "\ntwo\r"}, want: []string{"one", "two"}},
		{name: "line split between reads", parts: []string{"o", "ne\r"}, want: []string{"one"}},
		{name: "empty lines", parts: []string{"\r\r\n\n"}, want: []string{"", "", ""}},
		{name: "LF CR is two endings", parts: []string{"one\n\r"}, want: []string{"one", ""}},
		{name: "last line unended", parts: []string{"one\rtwo"}, want: []string{"one", "two"}},
		{name: "nothing", parts: nil, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got = readAll(t, NewReader(&chunks{parts: tt.parts}, DefaultMaxLen))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReadLineOneByteAtATime(t *testing.T) {
	t.Parallel()

	var r = NewReader(iotest.OneByteReader(strings.NewReader("one\r\ntwo\nthree\r")), DefaultMaxLen)
	assert.Equal(t, []string{"one", "two", "three"}, readAll(t, r))
}

func TestReadLineTooLong(t *testing.T) {
	t.Parallel()

	var r = NewReader(strings.NewReader("12345\r123456\r"), 5)

	var line, err = r.ReadLine(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "12345", line, "a line of exactly the limit is fine")

	_, err = r.ReadLine(t.Context())
	require.ErrorIs(t, err, ErrLineTooLong)
}

func TestReadLineTooLongWithoutEnding(t *testing.T) {
	t.Parallel()

	var r = NewReader(iotest.OneByteReader(strings.NewReader(strings.Repeat("x", 100))), 10)

	var _, err = r.ReadLine(t.Context())
	require.ErrorIs(t, err, ErrLineTooLong)
}

func TestReadLinePassesOnErrors(t *testing.T) {
	t.Parallel()

	var boom = errors.New("boom")
	var r = NewReader(iotest.DataErrReader(iotest.ErrReader(boom)), DefaultMaxLen)

	var _, err = r.ReadLine(t.Context())
	require.ErrorIs(t, err, boom)
}

func TestReadLineGivesUpOnADeadlineReader(t *testing.T) {
	t.Parallel()

	var bbs, station = net.Pipe()

	t.Cleanup(func() {
		_ = bbs.Close()
		_ = station.Close()
	})

	var r = NewReader(bbs, DefaultMaxLen)

	// The context's deadline passes with nothing to read.
	var timeoutCtx, cancelTimeout = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelTimeout()

	var _, timeoutErr = r.ReadLine(timeoutCtx)
	require.ErrorIs(t, timeoutErr, context.DeadlineExceeded)

	// The context is cancelled with nothing to read.
	var cancelCtx, cancel = context.WithCancel(t.Context())

	time.AfterFunc(20*time.Millisecond, cancel)

	var _, cancelErr = r.ReadLine(cancelCtx)
	require.ErrorIs(t, cancelErr, context.Canceled)

	// Neither was the source's fault, so the same reader carries on.
	go func() { _, _ = station.Write([]byte("hello\r")) }()

	var line, err = r.ReadLine(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "hello", line)
}

// contextSource is a source with a ReadContext method that never has data.
type contextSource struct{}

func (contextSource) Read([]byte) (int, error) { panic("Read called instead of ReadContext") }

func (contextSource) ReadContext(ctx context.Context, _ []byte) (int, error) {
	<-ctx.Done()

	return 0, ctx.Err()
}

func TestReadLineUsesReadContext(t *testing.T) {
	t.Parallel()

	var ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	var _, err = NewReader(contextSource{}, DefaultMaxLen).ReadLine(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReadLineChecksAPlainReaderBetweenReads(t *testing.T) {
	t.Parallel()

	var ctx, cancel = context.WithCancel(t.Context())
	cancel()

	var _, err = NewReader(strings.NewReader("one\r"), DefaultMaxLen).ReadLine(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRawKeepsWhatWasReadAhead(t *testing.T) {
	t.Parallel()

	var r = NewReader(strings.NewReader("FA proposal\r\x01binary\x00data"), DefaultMaxLen)

	var line, err = r.ReadLine(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "FA proposal", line)

	var rest, readErr = io.ReadAll(r.Raw())
	require.NoError(t, readErr)
	assert.Equal(t, []byte("\x01binary\x00data"), rest)
}

func TestRawAfterTheEnd(t *testing.T) {
	t.Parallel()

	var r = NewReader(strings.NewReader("one\rtail"), DefaultMaxLen)

	var _, err = r.ReadLine(t.Context())
	require.NoError(t, err)

	var rest, readErr = io.ReadAll(r.Raw())
	require.NoError(t, readErr)
	assert.Equal(t, []byte("tail"), rest)
}

func TestWrite(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, Write(&buf, "one", "", "two"))
	assert.Equal(t, "one\r\rtwo\r", buf.String())
}

func FuzzReadLine(f *testing.F) {
	f.Add([]byte("one\r\ntwo\nthree\r"), uint8(1))
	f.Add([]byte("\r\n\r\n\n\r"), uint8(3))
	f.Add([]byte(strings.Repeat("x", 300)), uint8(7))

	f.Fuzz(func(t *testing.T, data []byte, chunk uint8) {
		const limit = 32

		var size = int(chunk%16) + 1
		var parts []string

		for len(data) > 0 {
			var n = min(size, len(data))
			parts = append(parts, string(data[:n]))
			data = data[n:]
		}

		var r = NewReader(&chunks{parts: parts}, limit)

		for range 10000 {
			var line, err = r.ReadLine(t.Context())
			if err != nil {
				return
			}

			if len(line) > limit {
				t.Fatalf("line of %d bytes, over the limit of %d", len(line), limit)
			}

			if strings.ContainsAny(line, "\r\n") {
				t.Fatalf("line %q still has its ending", line)
			}
		}

		t.Fatal("never reached the end of the input")
	})
}
