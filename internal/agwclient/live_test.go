// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build agwlive

package agwclient

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLive runs the client against two real AGW servers whose radios can hear
// each other: test-scripts/check-agwclient sets up a pair of samoyed-direwolf
// instances on cross-wired UDP audio and points this at them through
// AGWCLIENT_LIVE_A and AGWCLIENT_LIVE_B.
func TestLive(t *testing.T) {
	t.Parallel()

	var addrA, addrB = os.Getenv("AGWCLIENT_LIVE_A"), os.Getenv("AGWCLIENT_LIVE_B")
	if addrA == "" || addrB == "" {
		t.Skip("AGWCLIENT_LIVE_A and AGWCLIENT_LIVE_B name the AGW servers to test against")
	}

	var ctx, cancel = context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	var a = liveClient(ctx, t, addrA, "Q1TEST")
	var b = liveClient(ctx, t, addrB, "Q2TEST")

	var accepted = make(chan *Link, 1)

	go func() {
		var link, err = b.Accept(ctx)
		if err == nil {
			accepted <- link
		}
	}()

	var linkA, connectErr = a.Connect(ctx, 0, "Q1TEST", "Q2TEST", nil)
	require.NoError(t, connectErr)

	var linkB *Link

	select {
	case linkB = <-accepted:
	case <-ctx.Done():
		t.Fatal("B never accepted the link")
	}

	assert.Equal(t, "Q1TEST", linkB.Remote())

	// Enough to take several windows' worth of frames each way, so writing
	// has to wait on the outstanding frames count.
	var fromA = bytes.Repeat([]byte("Q1TEST to Q2TEST. "), 120)
	var fromB = bytes.Repeat([]byte("Q2TEST to Q1TEST. "), 120)

	transfer(ctx, t, linkA, linkB, fromA)
	transfer(ctx, t, linkB, linkA, fromB)

	require.NoError(t, linkA.Close())

	var _, readErr = linkB.ReadContext(ctx, make([]byte, 1))
	require.ErrorIs(t, readErr, io.EOF)
}

func liveClient(ctx context.Context, t *testing.T, addr string, call string) *Client {
	t.Helper()

	var cfg = new(Config)

	cfg.Addr = addr
	cfg.Call = call
	cfg.Ports = []int{0}

	var c = New(*cfg)
	var done = make(chan struct{})

	go func() {
		defer close(done)

		_ = c.Run(ctx)
	}()

	t.Cleanup(func() { <-done })

	select {
	case <-c.Ready():
	case <-ctx.Done():
		t.Fatalf("never connected to the AGW server at %s", addr)
	}

	return c
}

// transfer writes payload on from, and checks it all arrives on to.
func transfer(ctx context.Context, t *testing.T, from *Link, to *Link, payload []byte) {
	t.Helper()

	var received = make(chan []byte, 1)

	go func() {
		var got = make([]byte, 0, len(payload))
		var buf = make([]byte, 256)

		for len(got) < len(payload) {
			var n, err = to.ReadContext(ctx, buf)
			if err != nil {
				break
			}

			got = append(got, buf[:n]...)
		}

		received <- got
	}()

	var n, writeErr = from.WriteContext(ctx, payload)
	require.NoError(t, writeErr)
	assert.Equal(t, len(payload), n)
	require.NoError(t, from.Drain(ctx))

	assert.Equal(t, string(payload), string(<-received))
}
