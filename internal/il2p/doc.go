// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package il2p frames AX.25 for the line as IL2P: a compact header in place of
// the AX.25 addresses, Reed-Solomon FEC over header and payload, scrambling,
// a sync word in place of HDLC's flags, and optionally a trailing CRC.  It is
// an alternative to HDLC rather than a user of it, and sits on the line coding
// in internal/linecode beside HDLC and FX.25, the other layer 2 framings.
package il2p
