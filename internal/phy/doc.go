// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package phy holds the types and limits shared between the modems and the
// layer 2 framers (HDLC, FX.25 and IL2P) that sit on top of them, so that each
// can depend on these without depending on the rest of the TNC.
package phy
