// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package linecode holds the line coding that sits between the modems and the
// layer 2 framers: NRZI, and the G3RUH scrambling that 9600 baud adds on top
// of it.  HDLC and FX.25 frames go out NRZI; IL2P skips NRZI and only, at
// most, inverts the signal.
package linecode
