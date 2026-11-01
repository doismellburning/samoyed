// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package scheduler decides when this BBS calls its partners: every so often,
// to give them the chance to send what they have, and as soon as there is mail
// queued for one.  A partner that can't be reached is tried less often, up to
// its usual interval, and mail that has waited for it too many times is held.
package scheduler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// DefaultMaxAttempts is how many failed calls a message waits through
	// before it is held for the sysop.
	DefaultMaxAttempts = 5

	// DefaultMinBackoff is how long after a failed call the partner is
	// tried again, doubling with each failure up to its interval.
	DefaultMinBackoff = time.Minute
)

// Target is a partner to call, and how often.
type Target struct {
	// Partner is the partner's callsign.
	Partner string

	// Every is how often to call it, mail queued or not.
	Every time.Duration
}

// store is what the scheduler needs of the message store.
type store interface {
	QueueFor(ctx context.Context, partner string) ([]int, error)
	RecordFailedAttempt(ctx context.Context, partner string, maxAttempts int) ([]int, error)
}

// lock is the partners in a forwarding session now, whichever side called.
type lock interface {
	TryAcquire(call string) bool
	Release(call string)
}

// Config is how a Scheduler runs.
type Config struct {
	// Call calls a partner and forwards with it, returning once the
	// session is over: nil if it went as it should.
	Call func(ctx context.Context, t Target) error

	// MaxAttempts is how many failed calls a message waits through before
	// it is held.  Zero is DefaultMaxAttempts.
	MaxAttempts int

	// MinBackoff is how long after a first failed call to try again.
	// Zero is DefaultMinBackoff.
	MinBackoff time.Duration

	// After is time.After, unless a test says otherwise.
	After func(d time.Duration) <-chan time.Time
}

// Scheduler calls partners.
type Scheduler struct {
	targets []Target
	store   store
	busy    lock
	cfg     Config
	kicks   map[string]chan struct{}
}

// New makes a Scheduler for targets.
func New(targets []Target, st store, busy lock, cfg Config) *Scheduler {
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = DefaultMinBackoff
	}

	if cfg.After == nil {
		cfg.After = time.After
	}

	var s = new(Scheduler)

	s.targets = targets
	s.store = st
	s.busy = busy
	s.cfg = cfg
	s.kicks = make(map[string]chan struct{}, len(targets))

	for _, t := range targets {
		s.kicks[t.Partner] = make(chan struct{}, 1)
	}

	return s
}

// Kick says mail has been queued for partner, so it is worth calling now.
// It never blocks, and a partner this BBS doesn't call is ignored.
func (s *Scheduler) Kick(partner string) {
	var kick, ok = s.kicks[partner]
	if !ok {
		return
	}

	select {
	case kick <- struct{}{}:
	default: // A call is coming anyway
	}
}

// Run calls the partners until ctx is cancelled, each from a goroutine of its
// own, and then waits for them.
func (s *Scheduler) Run(ctx context.Context) {
	var done = make(chan struct{}, len(s.targets))

	for _, t := range s.targets {
		go func() {
			s.run(ctx, t)

			done <- struct{}{}
		}()
	}

	for range s.targets {
		<-done
	}
}

// run calls one partner.  The first round, and one woken by a kick, calls
// only if there is mail to send; one woken by the partner's interval calls
// anyway, for whatever the partner has.
func (s *Scheduler) run(ctx context.Context, t Target) {
	var backoff time.Duration
	var poll = false

	for ctx.Err() == nil {
		backoff = s.round(ctx, t, poll, backoff)

		var wait = t.Every
		if backoff > 0 {
			wait = backoff
		}

		select {
		case <-ctx.Done():
			return
		case <-s.cfg.After(wait):
			poll = true
		case <-s.kicks[t.Partner]:
			poll = false
		}
	}
}

// round calls the partner if there is reason to, and returns the backoff
// before the next try: zero after a call that went well, or none made.
func (s *Scheduler) round(ctx context.Context, t Target, poll bool, backoff time.Duration) time.Duration {
	var entry = logrus.WithField("partner", t.Partner)

	if !poll {
		var queue, err = s.store.QueueFor(ctx, t.Partner)
		if err != nil {
			entry.WithError(err).Warn("Couldn't read the forwarding queue")

			return backoff
		}

		if len(queue) == 0 {
			return backoff
		}
	}

	if !s.busy.TryAcquire(t.Partner) {
		entry.Debug("Already forwarding with partner, so not calling")

		return backoff
	}

	entry.Info("Calling partner")

	var err = s.cfg.Call(ctx, t)

	s.busy.Release(t.Partner)

	if err == nil {
		entry.Info("Forwarded with partner")

		return 0
	}

	if ctx.Err() != nil {
		return backoff
	}

	var next = min(max(backoff*2, s.cfg.MinBackoff), t.Every)

	entry.WithError(err).WithField("retry", next).Warn("Couldn't forward with partner")

	var held, recordErr = s.store.RecordFailedAttempt(ctx, t.Partner, s.cfg.MaxAttempts)
	if recordErr != nil {
		entry.WithError(recordErr).Warn("Couldn't record the failed attempt")
	}

	if len(held) > 0 {
		entry.WithField("messages", held).Warn("Holding mail for the sysop: the partner couldn't be reached")
	}

	return next
}
