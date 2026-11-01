// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// samoyed-bbs is a packet BBS, and an experimental one.  Stations reach it over the air through an AGW
// server, such as samoyed-direwolf; --console runs a session on this terminal
// instead, as a given callsign.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/dispatcher"
	"github.com/doismellburning/samoyed/internal/bbs/forward/mbl"
	"github.com/doismellburning/samoyed/internal/bbs/forward/sid"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/mail"
	"github.com/doismellburning/samoyed/internal/bbs/routing"
	"github.com/doismellburning/samoyed/internal/bbs/scheduler"
	"github.com/doismellburning/samoyed/internal/bbs/shell"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/agwt"
	"github.com/doismellburning/samoyed/internal/bbs/transport/console"
	"github.com/doismellburning/samoyed/internal/maybe"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// experimentalWarning is logged on starting up, until the BBS is ready to be
// relied on.
const experimentalWarning = "samoyed-bbs is experimental: expect missing features and changes, " +
	"including to its message database, and don't rely on it for mail that matters."

func main() {
	var configFile = pflag.StringP("config", "c", "samoyed-bbs.yaml", "Configuration file.")
	var configCheck = pflag.Bool("config-check", false, "Check the configuration file and exit.  Exit status is non-zero if it has errors in it.")
	var consoleCall = pflag.String("console", "", "Run one session on this terminal, as the given callsign.")
	var verbose = pflag.BoolP("verbose", "v", false, "Log sessions starting and ending, and more.")
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - packet BBS (experimental)\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Experimental: expect missing features and changes, including to its message\ndatabase, and don't rely on it for mail that matters.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		pflag.PrintDefaults()
	}

	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	os.Exit(run(*configFile, *configCheck, *consoleCall, *verbose, os.Stdin, os.Stdout, os.Stderr))
}

// run is main, less the flags, returning the exit status.
func run(configFile string, configCheck bool, consoleCall string, verbose bool, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	var cfg, cfgErr = config.Load(configFile)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "Configuration file %s:\n", configFile)

		for line := range strings.SplitSeq(cfgErr.Error(), "\n") {
			fmt.Fprintf(stderr, "  %s\n", line)
		}

		return 1
	}

	if configCheck {
		fmt.Fprintf(stdout, "Configuration file %s is OK.\n", configFile)

		return 0
	}

	logrus.Warn(experimentalWarning)

	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if consoleCall == "" {
		return runAGW(ctx, cfg, verbose, stderr)
	}

	var call = strings.ToUpper(consoleCall)
	if !config.ValidCall(call) {
		fmt.Fprintf(stderr, "--console %s: not a callsign, with an optional SSID from 0 to 15.\n", consoleCall)

		return 1
	}

	if !verbose {
		logrus.SetLevel(logrus.WarnLevel) // Keep the console for the session
	}

	var err = serveConsole(ctx, cfg, console.New(stdin, stdout, call))
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)

		return 1
	}

	return 0
}

// runAGW serves stations through the configured AGW server until ctx is
// cancelled, returning the exit status.
func runAGW(ctx context.Context, cfg config.Config, verbose bool, stderr io.Writer) int {
	var agwCfg, ok = cfg.AGWClient().Get()
	if !ok {
		fmt.Fprintf(stderr, "Nothing to serve: the configuration has no agw block for stations to reach the BBS through.\n"+
			"Add one, or use --console CALL to run a session on this terminal.\n")

		return 1
	}

	if verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	var err = serveAGW(ctx, cfg, agwCfg)
	if err != nil {
		fmt.Fprintf(stderr, "%s\n", err)

		return 1
	}

	return 0
}

// serveConsole runs the console session.
func serveConsole(ctx context.Context, cfg config.Config, session transport.Session) error {
	var store, storeErr = sqlite.Open(ctx, cfg.Store)
	if storeErr != nil {
		return storeErr
	}

	defer func() { _ = store.Close() }()

	return newDispatcher(cfg, newMailbox(cfg, store, nil), nil).Serve(ctx, session)
}

// serveAGW serves each station that connects through the AGW server, each
// on a goroutine of its own, until ctx is cancelled.
func serveAGW(ctx context.Context, cfg config.Config, agwCfg agwclient.Config) error {
	var store, storeErr = sqlite.Open(ctx, cfg.Store)
	if storeErr != nil {
		return storeErr
	}

	defer func() { _ = store.Close() }()

	var client = agwclient.New(agwCfg)
	var listener = agwt.NewListener(client.Accept, agwt.DefaultDrainTimeout)
	var busy = new(dispatcher.Busy)

	// The scheduler is kicked when mail is queued, so the mailbox needs it,
	// and it calls through a dispatcher over that mailbox: a cycle, broken
	// by handing the mailbox a kick that reaches the scheduler once it has
	// been made, before anything runs.
	var sched *scheduler.Scheduler

	var mailbox = newMailbox(cfg, store, func(partner string) { sched.Kick(partner) })
	var d = newDispatcher(cfg, mailbox, busy)

	sched = scheduler.New(callTargets(cfg), store, busy, scheduler.Config{
		Call:        newCaller(cfg, client, d, mailbox).call,
		MaxAttempts: 0,
		MinBackoff:  0,
		After:       nil,
	})

	logrus.WithFields(logrus.Fields{"call": cfg.Call, "addr": agwCfg.Addr}).Info("Serving stations over AGW")

	var group, groupCtx = errgroup.WithContext(ctx)

	group.Go(func() error { return client.Run(groupCtx) })
	group.Go(func() error { return acceptSessions(groupCtx, listener, d) })
	group.Go(func() error { return runScheduler(groupCtx, client.Ready(), sched) })

	return group.Wait()
}

// acceptSessions serves each session listener hands out, until ctx is
// cancelled, and then waits for the sessions to finish.
func acceptSessions(ctx context.Context, listener *agwt.Listener, d *dispatcher.Dispatcher) error {
	var sessions sync.WaitGroup
	defer sessions.Wait()

	for {
		var s, err = listener.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}

		sessions.Go(func() { _ = d.Serve(ctx, s) })
	}
}

// routedStore is the message store with routing in front of it: a message
// taken, from a user or a partner, is routed as it is stored, and queued for
// whichever partners it is to go on to, or held if there are none.
type routedStore struct {
	*sqlite.Store

	call     string
	self     routing.Self
	partners []routing.Partner

	// kick, if given, is told of each partner a message is queued for.
	kick func(partner string)
}

// Accept routes m, which a user sent, and stores it.
func (r routedStore) Accept(ctx context.Context, m mail.NewMessage, bbsCall string) (mail.Message, error) {
	return r.accept(ctx, m, bbsCall, routing.Route(m, maybe.Nothing[string](), r.self, r.partners))
}

// AcceptForwarded routes m, which the partner from sent, and stores it.  It is
// never routed back to from.
func (r routedStore) AcceptForwarded(ctx context.Context, m mail.NewMessage, from string) (mail.Message, error) {
	return r.accept(ctx, m, r.call, routing.Route(m, maybe.Just(from), r.self, r.partners))
}

// accept stores m as route says, and kicks the partners it is queued for.
func (r routedStore) accept(ctx context.Context, m mail.NewMessage, bbsCall string, route routing.Decision) (mail.Message, error) {
	var stored, err = r.AcceptRouted(ctx, m, bbsCall, route)
	if err != nil {
		return stored, err
	}

	if r.kick != nil && route.Held.IsNothing() {
		for _, partner := range route.Partners {
			r.kick(partner)
		}
	}

	return stored, nil
}

// isPartner says whether call, with or without an SSID, is a partner's.
func (r routedStore) isPartner(call string) bool {
	var base, _, _ = strings.Cut(strings.ToUpper(call), "-")

	return slices.ContainsFunc(r.partners, func(p routing.Partner) bool { return p.Call == base })
}

// newMailbox is store with routing in front of it, kicking whatever kick is
// handed - the scheduler - when mail is queued.
func newMailbox(cfg config.Config, store *sqlite.Store, kick func(partner string)) routedStore {
	return routedStore{Store: store, call: cfg.Call, self: cfg.RoutingSelf(), partners: cfg.RoutingPartners(), kick: kick}
}

// forwardConfig is how this BBS forwards mail with its partners.  Its prompt
// ends in >, which is how the other BBS tells it is ready.
func forwardConfig(cfg config.Config) mbl.Config {
	var prompt = cfg.Prompt
	if !strings.HasSuffix(prompt, ">") {
		prompt += ">"
	}

	return mbl.Config{Call: cfg.Call, HRoute: cfg.HRouteMaybe(), Prompt: prompt, SID: sid.Ours(version.Major, version.Minor), Now: nil}
}

// newDispatcher makes the dispatcher every session is run by: a user gets a
// shell of their own over mailbox, and a partner that opens with its SID an
// exchange of mail, unless busy says it is in one already.
func newDispatcher(cfg config.Config, mailbox routedStore, busy *dispatcher.Busy) *dispatcher.Dispatcher {
	var fcfg = forwardConfig(cfg)

	var dcfg = new(dispatcher.Config)

	dcfg.Banner = cfg.BannerLines()
	dcfg.SID = fcfg.SID.String()
	dcfg.IdleTimeout = cfg.IdleTimeout
	dcfg.MaxLineLen = lines.DefaultMaxLen
	dcfg.Busy = busy

	// The console is the sysop, not a BBS.
	dcfg.Forwarder = func(peer transport.Peer, theirs sid.SID) maybe.Maybe[dispatcher.Machine] {
		if peer.Kind == transport.Console || !mailbox.isPartner(peer.Call) {
			return maybe.Nothing[dispatcher.Machine]()
		}

		return maybe.Just[dispatcher.Machine](mbl.NewAnswering(peer.Call, theirs, mailbox, fcfg))
	}

	var scfg = shell.Config{Call: cfg.Call, Prompt: cfg.Prompt, HRoute: cfg.HRouteMaybe(), Now: nil}

	return dispatcher.New(*dcfg, func(peer transport.Peer) *shell.Machine {
		return shell.New(peer, mailbox, scfg)
	})
}
