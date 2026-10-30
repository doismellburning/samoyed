// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package agwclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// pidNoLayer3 is the PID of ordinary connected-mode data.
const pidNoLayer3 = 0xf0

var (
	// errRemoteClosed is how a link ends when the far end disconnects:
	// reading gets what was already received, then io.EOF.
	errRemoteClosed = io.EOF

	// errConnectionLost is how a link ends when the connection to the
	// server drops under it.
	errConnectionLost = io.ErrUnexpectedEOF

	// errNoOutstandingReply says the server didn't answer a question about
	// outstanding frames in time.
	errNoOutstandingReply = errors.New("no outstanding frames reply from AGW server")
)

// Link is one connected-mode AX.25 link.  Its methods may be called from any
// goroutine, but reads and writes are each meant for one goroutine at a time.
type Link struct {
	client *Client
	conn   *conn
	key    linkKey

	readable chan struct{} // Signalled when data arrives
	ended    chan struct{} // Closed when the link ends, however it does
	counts   chan uint32   // The latest outstanding frames count

	// writeMu serialises writes, which wait on the outstanding frames
	// count between frames, and guards noReply.
	writeMu sync.Mutex
	noReply bool // The server doesn't answer about outstanding frames

	closeOnce sync.Once

	// mu guards everything below.
	mu          sync.Mutex
	buf         []byte
	endErr      error
	closedLocal bool
}

func newLink(client *Client, cn *conn, key linkKey) *Link {
	var l = new(Link)

	l.client = client
	l.conn = cn
	l.key = key
	l.readable = make(chan struct{}, 1)
	l.ended = make(chan struct{})
	l.counts = make(chan uint32, 1)

	return l
}

// Port is the server port the link is on.
func (l *Link) Port() int { return l.key.port }

// Local is this end's callsign.
func (l *Link) Local() string { return l.key.own }

// Remote is the far end's callsign.
func (l *Link) Remote() string { return l.key.remote }

// Read reads data received on the link.  Once the far end has disconnected it
// returns io.EOF, after anything already received; if the connection to the
// server drops, io.ErrUnexpectedEOF.
func (l *Link) Read(p []byte) (int, error) {
	return l.ReadContext(context.Background(), p)
}

// ReadContext is Read, giving up when ctx is cancelled.
func (l *Link) ReadContext(ctx context.Context, p []byte) (int, error) {
	for {
		l.mu.Lock()

		switch {
		case l.closedLocal:
			l.mu.Unlock()

			return 0, ErrLinkClosed
		case len(l.buf) > 0:
			var n = copy(p, l.buf)
			l.buf = l.buf[n:]
			l.mu.Unlock()

			return n, nil
		case l.endErr != nil:
			var err = l.endErr
			l.mu.Unlock()

			return 0, err
		}

		l.mu.Unlock()

		select {
		case <-l.readable:
		case <-l.ended:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// Write sends p over the link, in frames of no more than the client's PacLen.
// After each, it waits while the server has HighWater or more of the link's
// frames unacknowledged.
func (l *Link) Write(p []byte) (int, error) {
	return l.WriteContext(context.Background(), p)
}

// WriteContext is Write, giving up when ctx is cancelled.
func (l *Link) WriteContext(ctx context.Context, p []byte) (int, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	var cfg = &l.client.cfg
	var written = 0

	for written < len(p) {
		var endErr = l.writeErr()
		if endErr != nil {
			return written, endErr
		}

		var n = min(cfg.PacLen, len(p)-written)

		var msg, frameErr = newFrame(l.key.port, kindData, l.key.own, l.key.remote, bytes.Clone(p[written:written+n]))
		if frameErr != nil {
			return written, frameErr
		}

		msg.Header.PID = pidNoLayer3

		var sendErr = l.conn.send(msg)
		if sendErr != nil {
			return written, errConnectionLost
		}

		written += n

		var waitErr = l.waitBelow(ctx, cfg.HighWater)
		if waitErr != nil {
			return written, waitErr
		}
	}

	return written, nil
}

// Drain waits until every frame written to the link has been acknowledged.
// With a server that doesn't say, it waits FallbackDelay and hopes.
func (l *Link) Drain(ctx context.Context) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	return l.waitBelow(ctx, 1)
}

// Close disconnects the link, and waits for the server to confirm it, up to
// the client's CloseTimeout.  It is safe to call more than once.
func (l *Link) Close() error {
	var err error

	l.closeOnce.Do(func() {
		err = l.closeLink()
	})

	return err
}

func (l *Link) closeLink() error {
	defer func() {
		l.mu.Lock()
		l.closedLocal = true
		l.mu.Unlock()

		l.end(ErrLinkClosed)
		l.client.forget(l)
	}()

	select {
	case <-l.ended:
		return nil // Already gone.
	default:
	}

	l.conn.sendDisconnect(l.key)

	var timer = time.NewTimer(l.client.cfg.CloseTimeout)
	defer timer.Stop()

	select {
	case <-l.ended:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: no confirmation from the AGW server within %v", ErrLinkClosed, l.client.cfg.CloseTimeout)
	}
}

// deliver appends data received from the server.
func (l *Link) deliver(data []byte) {
	l.mu.Lock()

	if l.endErr != nil {
		l.mu.Unlock()

		return
	}

	l.buf = append(l.buf, data...)
	l.mu.Unlock()

	select {
	case l.readable <- struct{}{}:
	default: // Already signalled.
	}
}

// outstanding records the server's latest count of the link's
// unacknowledged frames, replacing any not yet looked at.
func (l *Link) outstanding(count uint32) {
	select {
	case <-l.counts:
	default:
	}

	select {
	case l.counts <- count:
	default:
	}
}

// end marks the link as ended with err, unless it already has.
func (l *Link) end(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.endErr == nil {
		l.endErr = err
		close(l.ended)
	}
}

// writeErr is why the link can't be written to, or nil if it can.
func (l *Link) writeErr() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch {
	case l.endErr == nil:
		return nil
	case errors.Is(l.endErr, errConnectionLost):
		return l.endErr
	default:
		return ErrLinkClosed
	}
}

// waitBelow waits until the server has fewer than limit of the link's frames
// unacknowledged.  A server that doesn't answer gets FallbackDelay instead,
// then and from then on.  Called with writeMu held.
func (l *Link) waitBelow(ctx context.Context, limit uint32) error {
	var cfg = &l.client.cfg

	for {
		if l.noReply {
			return l.pause(ctx, cfg.FallbackDelay)
		}

		var count, err = l.askOutstanding(ctx)
		if errors.Is(err, errNoOutstandingReply) {
			l.noReply = true

			logrus.WithFields(logrus.Fields{
				"remote":  l.key.remote,
				"timeout": cfg.YTimeout,
				"delay":   cfg.FallbackDelay,
			}).Warn("AGW server doesn't say how many frames are outstanding; pacing writes instead")

			return l.pause(ctx, cfg.FallbackDelay)
		}

		if err != nil {
			return err
		}

		if count < limit {
			return nil
		}

		var pauseErr = l.pause(ctx, cfg.YPoll)
		if pauseErr != nil {
			return pauseErr
		}
	}
}

// askOutstanding asks the server how many of the link's frames are
// unacknowledged, and waits up to YTimeout for the answer.
func (l *Link) askOutstanding(ctx context.Context) (uint32, error) {
	select {
	case <-l.counts: // Stale: from an earlier question.
	default:
	}

	var msg, frameErr = newFrame(l.key.port, kindOutstanding, l.key.own, l.key.remote, nil)
	if frameErr != nil {
		return 0, frameErr
	}

	var sendErr = l.conn.send(msg)
	if sendErr != nil {
		return 0, errConnectionLost
	}

	var timer = time.NewTimer(l.client.cfg.YTimeout)
	defer timer.Stop()

	select {
	case count := <-l.counts:
		return count, nil
	case <-timer.C:
		return 0, errNoOutstandingReply
	case <-l.ended:
		return 0, l.writeErr()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// pause waits for d, returning early with an error if the link ends or ctx
// is cancelled.
func (l *Link) pause(ctx context.Context, d time.Duration) error {
	var timer = time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-l.ended:
		return l.writeErr()
	case <-ctx.Done():
		return ctx.Err()
	}
}
