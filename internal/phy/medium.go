// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: GPL-2.0-or-later AND AGPL-3.0-or-later

package phy

// Medium is the kind of communication medium associated with a channel.
type Medium int

const (
	MediumNone      Medium = iota // Channel is not valid for use.
	MediumRadio                   // Internal modem for radio.
	MediumIGate                   // Access IGate as ordinary channel.
	MediumNetTNC                  // Remote network TNC.  (new in 1.8)
	MediumSerialTNC               // Local serial TNC.  (new in 1.8)
	MediumAXUDP                   // AX.25 over UDP to other nodes.
)
