// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fx25 frames AX.25 for the line as FX.25: a complete HDLC frame,
// flags, stuffing, FCS and all, wrapped in a Reed-Solomon codeblock behind a
// correlation tag.  A receiver that doesn't know FX.25 still finds the HDLC
// frame inside.  It sits on the line coding in internal/linecode, beside HDLC
// and IL2P, the other layer 2 framings.
package fx25
