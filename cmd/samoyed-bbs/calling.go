// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/dispatcher"
	"github.com/doismellburning/samoyed/internal/bbs/forward"
	"github.com/doismellburning/samoyed/internal/bbs/forward/mbl"
	"github.com/doismellburning/samoyed/internal/bbs/forward/script"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/scheduler"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/agwt"
)

// errHungUp says a partner hung up before the exchange was over.
var errHungUp = errors.New("the partner hung up before the exchange was over")

// callTargets is the partners this BBS calls, and how often.
func callTargets(cfg config.Config) []scheduler.Target {
	var targets []scheduler.Target

	for _, p := range cfg.Partners {
		if p.Connect != nil {
			targets = append(targets, scheduler.Target{Partner: baseCall(p.Call), Every: p.Connect.Every})
		}
	}

	return targets
}

// caller calls partners over the AGW server, and forwards with them.
type caller struct {
	cfg      config.Config
	partners map[string]config.Partner
	connect  func(ctx context.Context, port int, from string, to string, via []string) (*agwclient.Link, error)
	d        *dispatcher.Dispatcher
	mailbox  routedStore
}

func newCaller(cfg config.Config, client *agwclient.Client, d *dispatcher.Dispatcher, mailbox routedStore) *caller {
	var c = new(caller)

	c.cfg = cfg
	c.partners = make(map[string]config.Partner, len(cfg.Partners))
	c.connect = client.Connect
	c.d = d
	c.mailbox = mailbox

	for _, p := range cfg.Partners {
		c.partners[baseCall(p.Call)] = p
	}

	return c
}

// call connects to the partner, runs its connect script, and forwards with
// it until the exchange is over.
func (c *caller) call(ctx context.Context, t scheduler.Target) error {
	var p, ok = c.partners[t.Partner]
	if !ok || p.Connect == nil {
		return fmt.Errorf("%s is not a partner this BBS calls", t.Partner)
	}

	var conn = p.Connect

	var link, connectErr = c.connect(ctx, conn.Port, c.cfg.Call, conn.Target(p.Call), conn.Via)
	if connectErr != nil {
		return fmt.Errorf("connecting to %s: %w", conn.Target(p.Call), connectErr)
	}

	var s transport.Session = agwt.NewSession(link, agwt.DefaultDrainTimeout)

	defer func() { _ = s.Close() }()

	return forwardWith(ctx, s, c.d, c.mailbox, c.cfg, p)
}

// forwardWith runs the partner's connect script over s, and then forwards
// with it as the calling side.
func forwardWith(ctx context.Context, s transport.Session, d *dispatcher.Dispatcher, mailbox routedStore, cfg config.Config, p config.Partner) error {
	var reader = lines.NewReader(s, lines.DefaultMaxLen)

	var scriptErr = script.Run(ctx, s, reader, p.Connect.Script, os.LookupEnv, p.Connect.Timeout)
	if scriptErr != nil {
		return scriptErr
	}

	var fcfg = forwardConfig(cfg)

	var calling = forward.NewCalling(func(theirs sid.SID) forward.Machine {
		return mbl.NewCalling(p.Call, theirs, mailbox, fcfg)
	})

	var err = d.Drive(ctx, s, reader, calling)
	if errors.Is(err, io.EOF) {
		return errHungUp
	}

	return err
}

// runScheduler runs the scheduler once the AGW server is first ready, so
// that its first calls don't fail for want of it.
func runScheduler(ctx context.Context, ready <-chan struct{}, sched *scheduler.Scheduler) error {
	select {
	case <-ctx.Done():
		return nil
	case <-ready:
	}

	sched.Run(ctx)

	return nil
}

// baseCall is a callsign without its SSID, upper-cased.
func baseCall(call string) string {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return base
}
