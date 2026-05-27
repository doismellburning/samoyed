// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import direwolf "github.com/doismellburning/samoyed/src"

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
	direwolf.AXUDPMain()
}
