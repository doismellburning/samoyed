// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package aprs encodes and decodes the information part of APRS packets:
// it builds position, object and message reports from their components, and
// splits a received packet into the separate properties it contains.
//
// References: APRS Protocol Reference, and the frequency spec at
// http://www.aprs.org/info/freqspec.txt
package aprs
