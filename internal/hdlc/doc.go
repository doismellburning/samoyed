// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hdlc frames AX.25 for the line as HDLC: between 01111110 flags, bit
// stuffed so that no frame contains six ones in a row, and with an FCS.  It
// sits on the line coding in internal/linecode, beside FX.25 and IL2P, the
// other layer 2 framings.
package hdlc
