// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package kisstnc is the TNC side of the KISS protocol: collecting frames from
// a client application and acting on them, whichever transport they arrive
// by.  The framing itself is in internal/kiss, and the transports, and the
// TNC a Handler drives, are the caller's.
package kisstnc
