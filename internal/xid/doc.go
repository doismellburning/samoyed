// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package xid encodes and decodes the information field of AX.25 v2.2 XID
// frames, with which two stations negotiate the parameters of a connected-mode
// link: half or full duplex, REJ or selective reject, modulo 8 or 128, and the
// I field length, window size, acknowledge timer and retries.  Deciding what to
// agree to is left to the data link state machine.
package xid
