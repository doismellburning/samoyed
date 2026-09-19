// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"

	direwolf "github.com/doismellburning/samoyed/src"
	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

// samoyed-axudp bridges between samoyed-direwolf (via TCP KISS) and
// remote packet radio nodes that speak AXUDP (raw AX.25 frames in UDP
// datagrams), using a YAML config file.
//
// Usage:
//
//	samoyed-axudp [--config <file>] [--udpport <n>] [--kissport <n>]
//
// Config file (axudp.yaml):
//
//	maps:
//	  - ax25addr: Q1TEST
//	    host: 192.0.2.1
//	    port: 20093
//	  - ax25addr: Q2TEST
//	    host: 192.0.2.2
//	    port: 93

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(pflag.CommandLine.Output(), `samoyed-axudp [BETA] - AXUDP bridge for samoyed-direwolf

NOTE: samoyed-axudp is beta software. Its behaviour, config file format,
and flags may change in future releases without notice.

Bridges between samoyed-direwolf (via TCP KISS) and remote packet radio
nodes that speak AXUDP (raw AX.25 frames in UDP datagrams, per RFC 1226).
samoyed-direwolf connects to samoyed-axudp using an
NCHANNEL directive in its config file.

Usage:
  samoyed-axudp [--config <file>] [--udpport <n>] [--kissport <n>]

Example config file (axudp.yaml):
  maps:
    - ax25addr: Q1TEST-1
      host: 192.0.2.1
      port: 93

Example samoyed-direwolf config to connect via samoyed-axudp:
  CHANNEL 2
  MYCALL Q1TEST
  NCHANNEL 2 localhost 8002

Flags:
`)
		pflag.PrintDefaults()
	}

	var help = pflag.Bool("help", false, "Display help text.")
	var configFile = pflag.String("config", "axudp.yaml", "Path to YAML config file")
	var udpPort = pflag.Int("udpport", 20093, "UDP port to listen on (and source from)")
	var kissPort = pflag.Int("kissport", 8002, "TCP port for KISS clients (samoyed-direwolf NCHANNEL target)")
	var verbose = pflag.Bool("verbose", false, "Log every packet sent and received")
	pflag.Parse()

	if *verbose {
		// The per-packet entries are logged at Trace, and logrus defaults to
		// Info, so they would otherwise be dropped.
		logrus.SetLevel(logrus.TraceLevel)
	}

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	var maps, parseErr = direwolf.ParseAXUDPConfig(*configFile)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: reading config: %v\n", parseErr)
		os.Exit(1)
	}

	fmt.Printf("samoyed-axudp: WARNING: this is beta software; behaviour may change in future releases\n")
	fmt.Printf("samoyed-axudp: MAP table:\n")
	for _, e := range maps {
		fmt.Printf("  %s -> %s\n", e.AX25Addr, e.Addr)
	}

	var udpAddr, resolveErr = net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", *udpPort))
	if resolveErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: resolve UDP addr: %v\n", resolveErr)
		os.Exit(1)
	}

	var udpConn, listenErr = net.ListenUDP("udp", udpAddr)
	if listenErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: UDP listen on port %d: %v\n", *udpPort, listenErr)
		os.Exit(1)
	}
	fmt.Printf("samoyed-axudp: AXUDP listening on UDP port %d\n", *udpPort)

	var kissLn, kissListenErr = new(net.ListenConfig).Listen(context.Background(), "tcp", fmt.Sprintf(":%d", *kissPort))
	if kissListenErr != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: TCP listen on port %d: %v\n", *kissPort, kissListenErr)
		os.Exit(1)
	}
	fmt.Printf("samoyed-axudp: KISS TCP server listening on port %d\n", *kissPort)

	var b = direwolf.NewAXUDPBridge(maps, udpConn)

	// Both halves run until the user interrupts us, at which point they give
	// their sockets up rather than being cut off mid-flight.  Not deferred:
	// the os.Exit below would skip it anyway, and by the time we get there the
	// halves have already returned.
	var ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt)

	// Either half failing is fatal for the bridge as a whole, so whichever
	// returns first decides: report it and exit rather than limping along with
	// traffic flowing in only one direction.  The channel is buffered so the
	// half we do not wait for cannot leak its goroutine blocking on a send.
	var errs = make(chan error, 2)
	go func() { errs <- b.RunUDPListener(ctx) }()
	go func() { errs <- b.RunKISSServer(ctx, kissLn) }()

	var err = <-errs

	stop()

	// An interrupt is not a failure: both halves return without an error, and
	// we are simply done.
	if err != nil {
		fmt.Fprintf(os.Stderr, "samoyed-axudp: %v\n", err)
		os.Exit(1)
	}
}
