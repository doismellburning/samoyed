// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package eas sends and receives the Specific Area Message Encoding (SAME)
// headers of the Emergency Alert System: plain ASCII bytes after a preamble,
// least significant bit first, with none of HDLC's flags, bit stuffing or
// NRZI.  It sits on the line coding in internal/linecode, beside the layer 2
// framers in internal/hdlc, internal/fx25 and internal/il2p.
package eas
