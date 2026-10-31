// SPDX-FileCopyrightText: 2026 The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// samoyed-bbs is a packet BBS.  For now it serves only the console: a session
// on this terminal, as a given callsign.  Stations reaching it over the air,
// through an AGW server, come next.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/doismellburning/samoyed/internal/bbs/config"
	"github.com/doismellburning/samoyed/internal/bbs/dispatcher"
	"github.com/doismellburning/samoyed/internal/bbs/lines"
	"github.com/doismellburning/samoyed/internal/bbs/shell"
	"github.com/doismellburning/samoyed/internal/bbs/store/sqlite"
	"github.com/doismellburning/samoyed/internal/bbs/transport"
	"github.com/doismellburning/samoyed/internal/bbs/transport/console"
	"github.com/doismellburning/samoyed/internal/version"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
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

	if consoleCall == "" {
		fmt.Fprintf(stderr, "Nothing to serve: stations reaching the BBS over the air come later.\nUse --console CALL to run a session on this terminal.\n")

		return 1
	}

	var call = strings.ToUpper(consoleCall)
	if !config.ValidCall(call) {
		fmt.Fprintf(stderr, "--console %s: not a callsign, with an optional SSID from 0 to 15.\n", consoleCall)

		return 1
	}

	if !verbose {
		logrus.SetLevel(logrus.WarnLevel) // Keep the console for the session
	}

	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err = serveConsole(ctx, cfg, console.New(stdin, stdout, call))
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

	var dcfg = new(dispatcher.Config)

	dcfg.Banner = cfg.BannerLines()
	dcfg.SID = fmt.Sprintf("[SAMBBS-%d.%d-$]", version.Major, version.Minor)
	dcfg.IdleTimeout = cfg.IdleTimeout
	dcfg.MaxLineLen = lines.DefaultMaxLen

	var scfg = shell.Config{Call: cfg.Call, Prompt: cfg.Prompt}

	var d = dispatcher.New(*dcfg, func(peer transport.Peer) *shell.Machine {
		return shell.New(peer, store, scfg)
	})

	return d.Serve(ctx, session)
}
