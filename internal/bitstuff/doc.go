// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later

// Package bitstuff does HDLC framing on a whole buffer at a time: flags
// around the frame, and bit stuffing - a 0 inserted after every five 1s - so
// that the flag pattern cannot turn up inside it.  Bits go least significant
// first, as AX.25 sends them.
//
// FX.25 wraps such a frame in its codeblock, so builds and takes apart a
// whole buffer of it.  The HDLC sender and receiver do the same work a bit at
// a time, inline, as the bits go out or come in.
package bitstuff
