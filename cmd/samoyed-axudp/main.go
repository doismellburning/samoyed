// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"net"
	"os"

	direwolf "github.com/doismellburning/samoyed/src"
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

	var b = direwolf.NewAXUDPBridge(maps, udpConn, *verbose)

	go b.RunUDPListener()
	b.RunKISSServer(*kissPort)
}
