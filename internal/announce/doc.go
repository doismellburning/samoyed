// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package announce announces the services we listen for - KISS over TCP, AGW,
// the web interface and the metrics endpoint - on the local network with
// DNS-SD (multicast DNS service discovery), so that a client - a phone or
// tablet app, say, or a browser or Prometheus - can offer them from a list
// rather than asking for an address and port.  It answers mDNS queries itself, through
// github.com/brutella/dnssd, rather than relying on a system daemon.
package announce
