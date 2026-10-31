// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package lines is the BBS's line discipline.  Packet terminals end a line
// with CR, where other terminals send LF or CRLF; the BBS takes any of them,
// and sends CR.
package lines

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// DefaultMaxLen is the longest line a Reader takes unless told otherwise.
const DefaultMaxLen = 256

// readSize is how much a Reader asks for at a time.
const readSize = 512

// ErrLineTooLong says a line went on past the Reader's limit without ending.
var ErrLineTooLong = errors.New("line too long")

// contextReader is a source that can give up a read when a context is done,
// as an agwclient.Link can.
type contextReader interface {
	ReadContext(ctx context.Context, p []byte) (int, error)
}

// deadlineReader is a source that can give up a read at a deadline, as a
// net.Conn can.
type deadlineReader interface {
	io.Reader
	SetReadDeadline(t time.Time) error
}

// Reader reads lines.  A line ends at CR, LF or CRLF; a CR at the end of one
// read and an LF at the start of the next are one ending, not two.
type Reader struct {
	src     io.Reader
	maxLen  int
	pending []byte
	afterCR bool // The last line ended with a CR, and an LF may follow
	err     error
}

// NewReader reads lines from src, of at most maxLen bytes each.
func NewReader(src io.Reader, maxLen int) *Reader {
	var r = new(Reader)

	r.src = src
	r.maxLen = maxLen

	return r
}

// ReadLine returns the next line, without its ending.  It gives up when ctx
// is done if src can - if it has a ReadContext method, or read deadlines -
// and otherwise only between reads.  A last line with no ending is returned
// as it is, then the end of the input.
func (r *Reader) ReadLine(ctx context.Context) (string, error) {
	for {
		var line, ok = r.take()
		if ok {
			return line, nil
		}

		if len(r.pending) > r.maxLen {
			return "", ErrLineTooLong
		}

		if r.err != nil {
			if len(r.pending) > 0 {
				var last = string(r.pending)
				r.pending = nil

				return last, nil
			}

			return "", r.err
		}

		var ctxErr = r.fill(ctx)
		if ctxErr != nil {
			return "", ctxErr
		}
	}
}

// take returns the first whole line pending, if there is one.
func (r *Reader) take() (string, bool) {
	if r.afterCR && len(r.pending) > 0 {
		r.afterCR = false

		if r.pending[0] == '\n' {
			r.pending = r.pending[1:]
		}
	}

	var i = bytes.IndexAny(r.pending, "\r\n")
	if i < 0 || i > r.maxLen {
		return "", false
	}

	var line = string(r.pending[:i])
	var end = r.pending[i]

	r.pending = r.pending[i+1:]

	if end == '\r' {
		r.afterCR = true
	}

	return line, true
}

// fill reads more from src into pending, recording any error to return once
// what is pending has been used up.  ctx being done is returned instead,
// since it is the caller's and says nothing about the source: a later call
// with another context can carry on reading.
func (r *Reader) fill(ctx context.Context) error {
	var buf = make([]byte, readSize)
	var n int
	var err error

	switch src := r.src.(type) {
	case contextReader:
		n, err = src.ReadContext(ctx, buf)
	case deadlineReader:
		n, err = readWithDeadline(ctx, src, buf)
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		n, err = src.Read(buf)
	}

	r.pending = append(r.pending, buf[:n]...)

	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		r.err = err
	}

	return nil
}

// readWithDeadline reads src, giving up once ctx is done, which a read
// deadline in the past makes a blocked read do at once.  The deadline is set
// only after ctx is done, so ctx.Err() says why by the time the read returns.
//
// A source can refuse a deadline - net.Pipe does once its far end has closed -
// which means only that the read can't be cut short, so it goes ahead anyway
// and says what has become of the source.
func readWithDeadline(ctx context.Context, src deadlineReader, buf []byte) (int, error) {
	_ = src.SetReadDeadline(time.Time{})

	var stop = context.AfterFunc(ctx, func() { _ = src.SetReadDeadline(time.Unix(1, 0)) })
	defer stop()

	return src.Read(buf)
}

// Raw returns what follows the last line read, for data that isn't lines -
// FBB's compressed forwarding sends binary blocks after a line saying so.  It
// starts with whatever the Reader had already read ahead, so nothing is lost.
// The Reader mustn't be used again afterwards.
func (r *Reader) Raw() io.Reader {
	var ahead = r.pending

	r.pending = nil

	if r.err != nil {
		return io.MultiReader(bytes.NewReader(ahead), errReader{err: r.err})
	}

	return io.MultiReader(bytes.NewReader(ahead), r.src)
}

// errReader reads nothing but err.
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// Write writes each line to w, ending each with CR, all in one write: over
// AX.25 that lets them share frames, where a write per line would be a frame
// per line.
func Write(w io.Writer, lines ...string) error {
	if len(lines) == 0 {
		return nil
	}

	var buf strings.Builder

	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\r')
	}

	var _, err = io.WriteString(w, buf.String())

	return err
}
