// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// samoyed-bbs is a packet BBS.  Stations reach it over the air through an AGW
// server, such as samoyed-direwolf; --console runs a session on this terminal
// instead, as a given callsign.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/doismellburning/samoyed/internal/agwclient"
	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/dispatcher"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/shell"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/agwt"
	"github.com/doismellburning/samoyed/internal/bbs/transport/console"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

func main() {
	var configFile = pflag.StringP("config", "c", "samoyed-bbs.yaml", "Configuration file.")
	var configCheck = pflag.Bool("config-check", false, "Check the configuration file and exit.  Exit status is non-zero if it has errors in it.")
	var consoleCall = pflag.String("console", "", "Run one session on this terminal, as the given callsign.")
	var verbose = pflag.BoolP("verbose", "v", false, "Log sessions starting and ending, and more.")
	var help = pflag.Bool("help", false, "Display help text.")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s - packet BBS\n\n", os.Args[0])
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

	return newDispatcher(cfg, store).Serve(ctx, session)
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
	var d = newDispatcher(cfg, store)

	logrus.WithFields(logrus.Fields{"call": cfg.Call, "addr": agwCfg.Addr}).Info("Serving stations over AGW")

	var group, groupCtx = errgroup.WithContext(ctx)

	group.Go(func() error { return client.Run(groupCtx) })
	group.Go(func() error { return acceptSessions(groupCtx, listener, d) })

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

// newDispatcher makes the dispatcher every session is run by, each with a
// shell of its own over store.
func newDispatcher(cfg config.Config, store *sqlite.Store) *dispatcher.Dispatcher {
	var dcfg = new(dispatcher.Config)

	dcfg.Banner = cfg.BannerLines()
	dcfg.SID = fmt.Sprintf("[SAMBBS-%d.%d-$]", version.Major, version.Minor)
	dcfg.IdleTimeout = cfg.IdleTimeout
	dcfg.MaxLineLen = lines.DefaultMaxLen

	var scfg = shell.Config{Call: cfg.Call, Prompt: cfg.Prompt}

	return dispatcher.New(*dcfg, func(peer transport.Peer) *shell.Machine {
		return shell.New(peer, store, scfg)
	})
}
