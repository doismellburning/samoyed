// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package announce announces the KISS over TCP service on the local network
// with DNS-SD (multicast DNS service discovery), so that a client - a phone or
// tablet app, say - can offer the TNC from a list rather than asking for an
// address and port.  It answers mDNS queries itself, through
// github.com/brutella/dnssd, rather than relying on a system daemon.
package announce
