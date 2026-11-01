// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errUnreachable = errors.New("no answer")

// wait is the scheduler waiting: for how long, and the channel that ends it.
type wait struct {
	d    time.Duration
	fire chan time.Time
}

// harness runs a Scheduler for one partner, Q3BBS, against fakes the test
// controls: its waits are reported on waits and end when the test fires
// them, its calls on calls, answered by results.
type harness struct {
	s       *Scheduler
	waits   chan wait
	calls   chan Target
	results chan error

	mu      sync.Mutex
	queue   []int
	failed  int
	held    []int
	busy    map[string]bool
	cancel  context.CancelFunc
	stopped chan struct{}
}

func newHarness(t *testing.T, queue ...int) *harness {
	t.Helper()

	var h = new(harness)

	h.waits = make(chan wait)
	h.calls = make(chan Target)
	h.results = make(chan error)
	h.queue = queue
	h.busy = make(map[string]bool)
	h.stopped = make(chan struct{})

	var cfg = Config{
		Call: func(_ context.Context, target Target) error {
			h.calls <- target

			return <-h.results
		},
		MaxAttempts: 2,
		MinBackoff:  time.Minute,
		After: func(d time.Duration) <-chan time.Time {
			var w = wait{d: d, fire: make(chan time.Time, 1)}

			h.waits <- w

			return w.fire
		},
	}

	h.s = New([]Target{{Partner: "Q3BBS", Every: time.Hour}}, h, h, cfg)

	var ctx, cancel = context.WithCancel(t.Context())

	h.cancel = cancel

	go func() {
		h.s.Run(ctx)
		close(h.stopped)
	}()

	t.Cleanup(h.stop)

	return h
}

func (h *harness) stop() {
	h.cancel()

	// Let a scheduler stuck reporting a wait or a call see the cancel.
	for {
		select {
		case <-h.stopped:
			return
		case <-h.waits:
		case <-h.calls:
			h.results <- context.Canceled
		}
	}
}

func (h *harness) QueueFor(context.Context, string) ([]int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]int(nil), h.queue...), nil
}

func (h *harness) RecordFailedAttempt(_ context.Context, _ string, maxAttempts int) ([]int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.failed++

	if h.failed >= maxAttempts {
		h.held = append(h.held, h.queue...)
		h.queue = nil

		return h.held, nil
	}

	return nil, nil
}

func (h *harness) TryAcquire(call string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.busy[call] {
		return false
	}

	h.busy[call] = true

	return true
}

func (h *harness) Release(call string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.busy, call)
}

func (h *harness) setQueue(queue ...int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.queue = queue
}

// expectCall waits for the scheduler to call, and answers it with result.
func (h *harness) expectCall(t *testing.T, result error) {
	t.Helper()

	select {
	case target := <-h.calls:
		assert.Equal(t, "Q3BBS", target.Partner)
		h.results <- result
	case w := <-h.waits:
		t.Fatalf("waited %v rather than calling", w.d)
	case <-time.After(5 * time.Second):
		t.Fatal("no call")
	}
}

// expectWait waits for the scheduler to wait, and checks for how long.
func (h *harness) expectWait(t *testing.T, d time.Duration) wait {
	t.Helper()

	select {
	case w := <-h.waits:
		assert.Equal(t, d, w.d)

		return w
	case <-h.calls:
		t.Fatal("called rather than waiting")
	case <-time.After(5 * time.Second):
		t.Fatal("no wait")
	}

	return wait{d: 0, fire: nil}
}

func TestCallsAtOnceWhenMailIsWaiting(t *testing.T) {
	t.Parallel()

	var h = newHarness(t, 1)

	h.expectCall(t, nil)
	h.expectWait(t, time.Hour)
}

func TestWaitsWhenNothingIsQueued(t *testing.T) {
	t.Parallel()

	var h = newHarness(t)

	var w = h.expectWait(t, time.Hour)

	// The interval calls anyway, for whatever the partner has.
	w.fire <- time.Time{}

	h.expectCall(t, nil)
	h.expectWait(t, time.Hour)
}

func TestAKickCallsIfThereIsMail(t *testing.T) {
	t.Parallel()

	var h = newHarness(t)

	h.expectWait(t, time.Hour)

	h.s.Kick("Q3BBS")
	h.expectWait(t, time.Hour) // Woken, but nothing queued after all

	h.setQueue(7)
	h.s.Kick("Q3BBS")
	h.expectCall(t, nil)
	h.expectWait(t, time.Hour)

	h.s.Kick("Q9BBS") // Not a partner this BBS calls: nothing happens
}

func TestABusyPartnerIsNotCalled(t *testing.T) {
	t.Parallel()

	var h = newHarness(t)

	h.expectWait(t, time.Hour)

	h.setQueue(1)
	h.TryAcquire("Q3BBS") // The partner has called in

	h.s.Kick("Q3BBS")
	h.expectWait(t, time.Hour)

	h.Release("Q3BBS")
	h.s.Kick("Q3BBS")
	h.expectCall(t, nil)
}

func TestFailuresBackOffAndHoldMail(t *testing.T) {
	t.Parallel()

	var h = newHarness(t, 1, 2)

	h.expectCall(t, errUnreachable)

	var w = h.expectWait(t, time.Minute)

	w.fire <- time.Time{}

	// The second failure holds the mail, MaxAttempts being 2, and the
	// backoff doubles.
	h.expectCall(t, errUnreachable)
	w = h.expectWait(t, 2*time.Minute)

	h.mu.Lock()
	assert.Equal(t, []int{1, 2}, h.held)
	h.mu.Unlock()

	// A call that goes well puts it back on its interval.
	w.fire <- time.Time{}

	h.expectCall(t, nil)
	h.expectWait(t, time.Hour)
}

func TestBackoffStopsAtTheInterval(t *testing.T) {
	t.Parallel()

	var h = newHarness(t, 1)

	h.expectCall(t, errUnreachable)

	for _, d := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		var w = h.expectWait(t, d)

		w.fire <- time.Time{}

		h.expectCall(t, errUnreachable)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	var h = newHarness(t)

	h.expectWait(t, time.Hour)
	h.cancel()

	select {
	case <-h.stopped:
	case <-time.After(5 * time.Second):
		require.Fail(t, "Run didn't return")
	}
}
